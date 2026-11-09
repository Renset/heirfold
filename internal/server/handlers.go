package server

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// writeCompressed sends JSON that the browser can cache and revalidate.
func writeCompressed(w http.ResponseWriter, r *http.Request, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	// The body depends on the role, so a shared cache must not keep it.
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Cookie, Accept-Encoding")
	h.Set("Content-Type", "application/json; charset=utf-8")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	if len(data) > 1024 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		if _, err := gz.Write(data); err != nil {
			return err
		}
		return gz.Close()
	}
	_, err = w.Write(data)
	return err
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	view := s.store.View(userFrom(r).Role.Scope())
	graph, err := view.Graph(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := writeCompressed(w, r, graph); err != nil {
		s.log.Warn("response failed", "error", err)
	}
}

func (s *Server) handlePerson(w http.ResponseWriter, r *http.Request) {
	view := s.store.View(userFrom(r).Role.Scope())
	detail, err := view.Person(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := writeCompressed(w, r, detail); err != nil {
		s.log.Warn("response failed", "error", err)
	}
}

// importCache keeps uploaded files until the user confirms or cancels.
type importCache struct {
	mu       sync.Mutex
	dir      string
	lifetime time.Duration
	entries  map[string]importEntry
}

type importEntry struct {
	path    string
	expires time.Time
}

func newImportCache(dir string, lifetime time.Duration) *importCache {
	return &importCache{dir: dir, lifetime: lifetime, entries: make(map[string]importEntry)}
}

func (c *importCache) add(path string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = importEntry{path: path, expires: time.Now().Add(c.lifetime)}
	return id, nil
}

// take removes the entry and returns its file. The caller deletes the file.
func (c *importCache) take(id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok {
		return "", false
	}
	delete(c.entries, id)
	if time.Now().After(entry.expires) {
		_ = os.Remove(entry.path)
		return "", false
	}
	return entry.path, true
}

func (c *importCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for id, entry := range c.entries {
		if now.After(entry.expires) {
			_ = os.Remove(entry.path)
			delete(c.entries, id)
		}
	}
}

type importPreview struct {
	ID            string           `json:"id"`
	Counts        model.Counts     `json:"counts"`
	Warnings      []gramps.Warning `json:"warnings"`
	WarningsTotal int              `json:"warningsTotal"`
	Replaces      *model.Counts    `json:"replaces,omitempty"`
}

func importErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, gramps.ErrTooLarge), errors.Is(err, gramps.ErrTooManyObject), errors.Is(err, gramps.ErrTooDeep):
		return http.StatusRequestEntityTooLarge, "too_large"
	case errors.Is(err, gramps.ErrNotGramps):
		return http.StatusUnprocessableEntity, "not_gramps"
	}
	return http.StatusInternalServerError, "internal"
}

func (s *Server) handleImportUpload(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sync != nil {
		writeError(w, http.StatusConflict, "managed_by_gramps")
		return
	}
	if !s.importMu.TryLock() {
		writeError(w, http.StatusConflict, "import_busy")
		return
	}
	defer s.importMu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUpload)
	file, err := os.CreateTemp(filepath.Join(s.cfg.DataDir, "tmp"), "upload-*")
	if err != nil {
		s.internal(w, err)
		return
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	_, copyErr := io.Copy(file, r.Body)
	if closeErr := file.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(copyErr, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		writeError(w, http.StatusBadRequest, "upload_failed")
		return
	}
	result, err := s.parseFile(path)
	if err != nil {
		status, code := importErrorCode(err)
		if status == http.StatusInternalServerError {
			s.internal(w, err)
			return
		}
		writeError(w, status, code)
		return
	}
	preview := importPreview{Counts: result.Tree.Counts(), Warnings: result.Warnings, WarningsTotal: result.WarningsTotal}
	if preview.Warnings == nil {
		preview.Warnings = []gramps.Warning{}
	}
	hasTree, err := s.store.HasTree(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if hasTree {
		counts, err := s.store.Counts(r.Context())
		if err != nil {
			s.internal(w, err)
			return
		}
		preview.Replaces = &counts
	}
	id, err := s.imports.add(path)
	if err != nil {
		s.internal(w, err)
		return
	}
	keep = true
	preview.ID = id
	s.audit(r, userFrom(r), "import.preview", "", fmt.Sprintf("people=%d", preview.Counts.People))
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) parseFile(path string) (*gramps.Result, error) {
	file, err := os.Open(path) // #nosec G304 -- the server created this path itself.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return gramps.Parse(file, s.cfg.Import)
}

type commitRequest struct {
	Replace bool `json:"replace"`
}

func (s *Server) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sync != nil {
		writeError(w, http.StatusConflict, "managed_by_gramps")
		return
	}
	var req commitRequest
	if !readJSON(w, r, &req) {
		return
	}
	// A new tree replaces everything, so it needs a fresh sign-in code.
	if !s.fresh(w, r) {
		return
	}
	if !s.importMu.TryLock() {
		writeError(w, http.StatusConflict, "import_busy")
		return
	}
	defer s.importMu.Unlock()
	hasTree, err := s.store.HasTree(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if hasTree && !req.Replace {
		writeError(w, http.StatusConflict, "tree_exists")
		return
	}
	path, ok := s.imports.take(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "import_expired")
		return
	}
	defer func() { _ = os.Remove(path) }()
	result, err := s.parseFile(path)
	if err != nil {
		status, code := importErrorCode(err)
		if status == http.StatusInternalServerError {
			s.internal(w, err)
			return
		}
		writeError(w, status, code)
		return
	}
	if hasTree {
		// The old tree and its journal of edits go away. A copy stays on disk.
		if _, err := s.store.Backup(r.Context(), filepath.Join(s.cfg.DataDir, "backups")); err != nil {
			s.internal(w, err)
			return
		}
	}
	living, err := s.store.ReplaceTree(r.Context(), result.Tree)
	if err != nil {
		s.internal(w, err)
		return
	}
	counts := result.Tree.Counts()
	s.audit(r, userFrom(r), "import.commit", "", fmt.Sprintf("people=%d replace=%t", counts.People, hasTree))
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts, "living": living})
}

func (s *Server) handleImportCancel(w http.ResponseWriter, r *http.Request) {
	path, ok := s.imports.take(r.PathValue("id"))
	if ok {
		_ = os.Remove(path)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	// The file holds private records and living people.
	if scope := user.Role.Scope(); !scope.Private || !scope.Living {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !s.limits.allow("export:"+strconv.FormatInt(user.ID, 10), ruleExport) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	tree, err := s.store.ExportTree(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if !user.Role.Scope().Notes {
		// A family member gets the whole tree without the text of the
		// hidden notes. The empty notes stay, so no link of the file breaks.
		for i := range tree.Notes {
			if tree.Notes[i].Private {
				tree.Notes[i].Text, tree.Notes[i].Styles = "", nil
			}
		}
	}
	s.audit(r, userFrom(r), "export", "", fmt.Sprintf("people=%d", len(tree.People)))
	name := "heirfold-export-" + time.Now().UTC().Format("2006-01-02") + ".gramps"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	gz := gzip.NewWriter(w)
	if err := gramps.Write(gz, tree); err != nil {
		s.log.Warn("export failed", "error", err)
		return
	}
	if err := gz.Close(); err != nil {
		s.log.Warn("export failed", "error", err)
	}
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListAudit(r.Context(), 200)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
