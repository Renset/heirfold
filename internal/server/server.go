// Package server is the HTTP layer of Heirfold.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/mail"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/nn"
	"github.com/Renset/heirfold/internal/store"
)

// Config holds the settings of the server.
type Config struct {
	DataDir string
	// SecureCookies adds the Secure flag and the __Host- cookie prefix.
	SecureCookies bool
	// HSTS sends Strict-Transport-Security.
	HSTS bool
	// AllowedHosts limits the Host header. An empty list allows any host.
	AllowedHosts []string
	// TrustedProxies lists the proxies whose X-Forwarded-For Heirfold reads.
	TrustedProxies []netip.Prefix
	// MaxUpload limits the size of an uploaded file.
	MaxUpload int64
	// Import limits the parser.
	Import gramps.Limits
	// Version is the build version.
	Version string
	// MediaDir holds the photos and scans of the tree. Heirfold shows only
	// files inside this directory. An empty value turns the files off.
	MediaDir string
	// Sync keeps the tree equal to a Gramps database. With it, the tree
	// cannot be replaced by an upload.
	Sync Synchronizer
	// Mail sends the sign-in codes and the invitations. Without it the codes
	// go to the server log.
	Mail mail.Sender
	// BaseURL is the address of the site for the letters.
	BaseURL string
	// NoFaces turns off the search for faces on the pictures.
	NoFaces bool
	// MapTiles is the address of a tile service for a map with streets, with
	// the parts {z}, {x} and {y}. Without it the map is the map of the
	// program, and the server asks nobody. See maptiles.go.
	MapTiles string
	// MapCredit is the line that names the makers of the tiles under a map.
	MapCredit string
}

// Lifetimes of secrets.
const (
	// A session lasts a year from its last use, so a relative signs in once.
	sessionLifetime = 365 * 24 * time.Hour
	// recentAuth is how long after a sign-in code a person can change who has
	// access to the tree.
	recentAuth     = 30 * time.Minute
	codeLifetime   = 10 * time.Minute
	maxCodeTries   = 5
	importLifetime = 30 * time.Minute
)

// Server is the application.
type Server struct {
	cfg        Config
	store      *store.Store
	log        *slog.Logger
	setupToken string
	setupMu    sync.Mutex
	jobs       chan struct{}
	// anon holds the limits of requests without a session. A flood from
	// clients without an account cannot fill the limits of signed-in people.
	anon       *rateLimits
	deviceKey  []byte
	uploads    chan struct{}
	mail       mail.Sender
	logMail    mail.Sender
	limits     *rateLimits
	static     *staticFiles
	faces      *faceFinder
	towns      *townList
	tiles      *tileStore
	imports    *importCache
	importMu   sync.Mutex
	mediaDir   string
	images     *imageCache
	imageSlots chan struct{}
}

// New creates the server.
func New(cfg Config, st *store.Store, log *slog.Logger) (*Server, error) {
	if cfg.MaxUpload <= 0 {
		cfg.MaxUpload = 256 << 20
	}
	if cfg.Import.MaxBytes <= 0 {
		cfg.Import = gramps.DefaultLimits
	}
	tmp := filepath.Join(cfg.DataDir, "tmp")
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	static, err := loadStatic()
	if err != nil {
		return nil, err
	}
	logMail := mail.Log{Logger: log}
	sender := cfg.Mail
	if sender == nil {
		sender = logMail
	}
	token, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	deviceKey, err := loadDeviceKey(st)
	if err != nil {
		return nil, err
	}
	media, err := mediaRoot(cfg.MediaDir)
	if err != nil {
		return nil, err
	}
	nn.Workers = faceWorkers()
	return &Server{
		mediaDir: media, images: newImageCache(imageCacheBytes),
		imageSlots: make(chan struct{}, max(1, runtime.NumCPU()/2)),
		cfg:        cfg, store: st, log: log, setupToken: token,
		jobs: make(chan struct{}, 8), mail: sender, logMail: logMail,
		anon: newRateLimits(), deviceKey: deviceKey, uploads: make(chan struct{}, 2),
		limits:  newRateLimits(),
		faces:   &faceFinder{wake: make(chan struct{}, 1)},
		towns:   &townList{},
		tiles:   newTileStore(cfg.MapTiles, cfg.DataDir, cfg.Version),
		static:  static,
		imports: newImportCache(tmp, importLifetime),
	}, nil
}

// loadDeviceKey returns the key that signs the device cookies. It stays the
// same across restarts, so a browser stays known.
func loadDeviceKey(st *store.Store) ([]byte, error) {
	ctx := context.Background()
	text, ok, err := st.MetaGet(ctx, "device_key")
	if err != nil {
		return nil, err
	}
	if ok {
		if key, err := base64.RawURLEncoding.DecodeString(text); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	token, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	if err := st.MetaSet(ctx, "device_key", token); err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(token)
}

// SetupToken returns the one-time token of the first-run page.
func (s *Server) SetupToken() string { return s.setupToken }

// NeedsSetup reports whether the server has no users yet.
func (s *Server) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	return n == 0, err
}

// Run performs periodic maintenance until the context ends.
func (s *Server) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()
	go s.runFaces(ctx)
	go s.runFileInfo(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.limits.sweep()
			s.anon.sweep()
			s.imports.sweep()
			if err := s.store.DeleteExpired(ctx); err != nil {
				s.log.Error("maintenance failed", "error", err)
			}
		case <-daily.C:
			if err := s.store.RefreshLiving(ctx); err != nil {
				s.log.Error("refresh of living people failed", "error", err)
			}
			if s.tiles != nil {
				s.tiles.sweep(time.Now())
			}
		}
	}
}

// Handler returns the root handler with all middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/code", s.handleCodeRequest)
	mux.HandleFunc("POST /api/v1/auth/verify", s.handleCodeVerify)
	mux.HandleFunc("POST /api/v1/auth/logout", s.require(auth.RoleGuest, s.handleLogout))
	mux.HandleFunc("GET /api/v1/me", s.require(auth.RoleGuest, s.handleMe))
	mux.HandleFunc("PATCH /api/v1/me", s.require(auth.RoleGuest, s.handleMeUpdate))
	mux.HandleFunc("GET /api/v1/me/sessions", s.require(auth.RoleGuest, s.handleSessions))
	mux.HandleFunc("DELETE /api/v1/me/sessions/{id}", s.require(auth.RoleGuest, s.handleSessionDelete))
	mux.HandleFunc("POST /api/v1/me/sessions/end-others", s.require(auth.RoleGuest, s.handleSessionsEndOthers))
	mux.HandleFunc("GET /api/v1/tree", s.require(auth.RoleGuest, s.handleTree))
	mux.HandleFunc("GET /api/v1/people/{id}", s.require(auth.RoleGuest, s.handlePerson))
	mux.HandleFunc("GET /api/v1/media", s.require(auth.RoleGuest, s.handleMediaList))
	mux.HandleFunc("GET /api/v1/media/{id}/image", s.require(auth.RoleGuest, s.handleMediaImage))
	mux.HandleFunc("GET /api/v1/media/{id}/file", s.require(auth.RoleGuest, s.handleMediaFile))
	mux.HandleFunc("GET /api/v1/media/{id}/document", s.require(auth.RoleGuest, s.handleMediaDocument))
	mux.HandleFunc("GET /api/v1/media/{id}/info", s.require(auth.RoleGuest, s.handleMediaInfo))
	mux.HandleFunc("GET /api/v1/gallery", s.require(auth.RoleMember, s.handleGallery))
	mux.HandleFunc("GET /api/v1/gallery/{id}/found", s.require(auth.RoleEditor, s.handleFoundFaces))
	mux.HandleFunc("GET /api/v1/places/search", s.require(auth.RoleEditor, s.handlePlaceSearch))
	mux.HandleFunc("GET /api/v1/places/nearest", s.require(auth.RoleEditor, s.handlePlaceNearest))
	mux.HandleFunc("GET /api/v1/places/around", s.require(auth.RoleMember, s.handlePlacesAround))
	mux.HandleFunc("GET /api/v1/map/tiles/{z}/{x}/{y}", s.require(auth.RoleMember, s.handleMapTile))
	mux.HandleFunc("GET /geo/detail/{square}", s.handleMapDetail)
	mux.HandleFunc("DELETE /api/v1/gallery/{id}/found/{face}", s.require(auth.RoleEditor, s.editable(s.handleFaceDismiss)))
	mux.HandleFunc("GET /api/v1/sync", s.require(auth.RoleGuest, s.handleSyncState))
	mux.HandleFunc("POST /api/v1/sync", s.require(auth.RoleAdmin, s.handleSyncNow))
	mux.HandleFunc("POST /api/v1/imports", s.require(auth.RoleAdmin, s.handleImportUpload))
	mux.HandleFunc("POST /api/v1/imports/{id}/commit", s.require(auth.RoleAdmin, s.handleImportCommit))
	mux.HandleFunc("DELETE /api/v1/imports/{id}", s.require(auth.RoleAdmin, s.handleImportCancel))
	// A family member sees the whole tree, so a family member can take a copy.
	mux.HandleFunc("GET /api/v1/export.gramps", s.require(auth.RoleMember, s.handleExport))
	mux.HandleFunc("GET /api/v1/users", s.require(auth.RoleEditor, s.handleUsers))
	mux.HandleFunc("POST /api/v1/users", s.require(auth.RoleEditor, s.handleUserCreate))
	mux.HandleFunc("POST /api/v1/users/{id}/invite", s.require(auth.RoleEditor, s.handleUserInviteAgain))
	mux.HandleFunc("PATCH /api/v1/users/{id}", s.require(auth.RoleAdmin, s.handleUserUpdate))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.require(auth.RoleEditor, s.handleUserDelete))
	mux.HandleFunc("GET /api/v1/audit", s.require(auth.RoleAdmin, s.handleAudit))

	// An editor changes the whole tree. A family member with the right for
	// the own branch uses the same routes, and the store checks each change.
	editor := func(next http.HandlerFunc) http.HandlerFunc { return s.require(auth.RoleEditor, s.editable(next)) }
	branch := func(next http.HandlerFunc) http.HandlerFunc { return s.branchEditor(s.editable(next)) }
	mux.HandleFunc("GET /api/v1/edit/people/{id}", branch(s.handleEditPerson))
	mux.HandleFunc("GET /api/v1/edit/places", branch(s.handleEditPlaces))
	mux.HandleFunc("POST /api/v1/edit/people", branch(s.handlePersonCreate))
	mux.HandleFunc("PUT /api/v1/edit/people/{id}", branch(s.handlePersonUpdate))
	mux.HandleFunc("DELETE /api/v1/edit/people/{id}", editor(s.handlePersonDelete))
	mux.HandleFunc("POST /api/v1/edit/links", branch(s.handleLinkCreate))
	mux.HandleFunc("DELETE /api/v1/edit/families/{id}/members/{person}", editor(s.handleLinkDelete))
	mux.HandleFunc("PUT /api/v1/edit/families/{id}", branch(s.handleFamilyUpdate))
	mux.HandleFunc("POST /api/v1/edit/people/{id}/events", branch(s.handleEventCreate(model.KindPerson)))
	mux.HandleFunc("POST /api/v1/edit/families/{id}/events", branch(s.handleEventCreate(model.KindFamily)))
	mux.HandleFunc("PUT /api/v1/edit/events/{id}", branch(s.handleEventUpdate))
	mux.HandleFunc("DELETE /api/v1/edit/people/{id}/events/{event}", branch(s.handleEventDelete(model.KindPerson)))
	mux.HandleFunc("DELETE /api/v1/edit/families/{id}/events/{event}", branch(s.handleEventDelete(model.KindFamily)))
	mux.HandleFunc("POST /api/v1/edit/people/{id}/notes", branch(s.handleNoteCreate))
	mux.HandleFunc("PUT /api/v1/edit/notes/{id}", branch(s.handleNoteUpdate))
	mux.HandleFunc("DELETE /api/v1/edit/people/{id}/notes/{note}", branch(s.handleNoteDelete))
	pictures := func(next http.HandlerFunc) http.HandlerFunc { return s.pictureEditor(s.editable(next)) }
	mux.HandleFunc("POST /api/v1/edit/people/{id}/photo", pictures(s.handlePhotoUpload))
	mux.HandleFunc("PUT /api/v1/edit/people/{id}/media/{media}", pictures(s.handlePhotoUpdate))
	mux.HandleFunc("DELETE /api/v1/edit/people/{id}/media/{media}", pictures(s.handleMediaRemove))
	mux.HandleFunc("DELETE /api/v1/edit/people/{id}/portrait", pictures(s.handlePortraitClear))
	mux.HandleFunc("POST /api/v1/edit/gallery", editor(s.handleGalleryUpload))
	mux.HandleFunc("PUT /api/v1/edit/gallery/{id}", editor(s.handleGalleryUpdate))
	mux.HandleFunc("DELETE /api/v1/edit/gallery/{id}", editor(s.handleGalleryRemove))
	mux.HandleFunc("PUT /api/v1/edit/gallery/{id}/faces/{person}", editor(s.handleFaceMark))
	mux.HandleFunc("DELETE /api/v1/edit/gallery/{id}/faces/{person}", editor(s.handleFaceUnmark))
	mux.HandleFunc("PUT /api/v1/edit/home", editor(s.handleHomeSet))
	mux.HandleFunc("GET /api/v1/history", s.branchEditor(s.handleHistory))
	mux.HandleFunc("POST /api/v1/history/{id}/undo", branch(s.handleHistoryUndo))
	mux.HandleFunc("/api/", s.handleAPINotFound)
	mux.Handle("/", s.static)

	protection := http.NewCrossOriginProtection()
	var handler http.Handler = mux
	handler = protection.Handler(handler)
	handler = s.checkHost(handler)
	handler = s.securityHeaders(handler)
	handler = s.logRequests(handler)
	return s.recoverPanics(handler)
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, isError := v.(error); isError && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				// The value can hold user input, so it does not go to the log.
				s.log.Error("panic in handler")
				writeError(w, http.StatusInternalServerError, "internal")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logRequests writes the route pattern and not the path, because a path holds
// the IDs of people.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		s.log.Info("request", "method", r.Method, "route", r.Pattern, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

// contentSecurityPolicy forbids everything, then allows only the app itself.
// Trusted Types block every assignment of an HTML string to the DOM.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; " +
	"connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; " +
	"require-trusted-types-for 'script'; trusted-types 'none'"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "accelerometer=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		if s.cfg.HSTS {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// checkHost rejects unknown Host headers, which stops DNS rebinding attacks.
func (s *Server) checkHost(next http.Handler) http.Handler {
	if len(s.cfg.AllowedHosts) == 0 {
		return next
	}
	allowed := make(map[string]struct{}, len(s.cfg.AllowedHosts))
	for _, host := range s.cfg.AllowedHosts {
		allowed[host] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[stripPort(r.Host)]; !ok {
			writeError(w, http.StatusMisdirectedRequest, "bad_host")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := s.NeedsSetup(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	// codesInLog tells the sign-in page that the server sends no letters.
	writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": needsSetup, "codesInLog": s.mail.Name() == "log"})
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found")
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

const maxJSONBody = 64 << 10

// readJSON decodes a small JSON body and rejects unknown fields.
func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large")
			return false
		}
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
