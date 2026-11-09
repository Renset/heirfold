package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/store"
)

type ctxKey int

const (
	userKey ctxKey = iota
	tokenHashKey
	sessionKey
)

func userFrom(r *http.Request) *store.User {
	user, _ := r.Context().Value(userKey).(*store.User)
	return user
}

func sessionFrom(r *http.Request) *store.Session {
	session, _ := r.Context().Value(sessionKey).(*store.Session)
	return session
}

func (s *Server) cookieName(name string) string {
	if s.cfg.SecureCookies {
		return "__Host-" + name
	}
	return name
}

const (
	sessionCookie = "heirfold"
	// attemptCookie ties a sign-in code to the browser that asked for it.
	attemptCookie = "heirfold-signin"
	// deviceCookie marks a browser that signed in to an account before. Its
	// value is a keyed hash of the address, so nobody can make one.
	deviceCookie = "heirfold-device"
	// deviceLifetime is the longest cookie life that browsers accept.
	deviceLifetime = 400 * 24 * time.Hour
)

func (s *Server) deviceMark(email string) string {
	mac := hmac.New(sha256.New, s.deviceKey)
	mac.Write([]byte("device\x00" + email))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// markDevice remembers that this browser signed in to the account.
func (s *Server) markDevice(w http.ResponseWriter, email string) {
	s.setCookie(w, deviceCookie, s.deviceMark(email), deviceLifetime)
}

// trustedDevice reports whether the browser signed in to the account with
// this address before, or has a session of it now.
func (s *Server) trustedDevice(r *http.Request, email string) bool {
	if cookie, err := r.Cookie(s.cookieName(deviceCookie)); err == nil &&
		hmac.Equal([]byte(cookie.Value), []byte(s.deviceMark(email))) {
		return true
	}
	if cookie, err := r.Cookie(s.cookieName(sessionCookie)); err == nil {
		user, _, _, err := s.store.SessionUser(r.Context(), auth.HashToken(cookie.Value), sessionLifetime)
		return err == nil && user.Email == email
	}
	return false
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	age := int(lifetime.Seconds())
	if value == "" {
		age = -1
	}
	// Secure is off only for plain HTTP on the loopback interface.
	http.SetCookie(w, &http.Cookie{ // #nosec G124
		Name:     s.cookieName(name),
		Value:    value,
		Path:     "/",
		MaxAge:   age,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// currentSession returns the user of the session cookie, or nil.
func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) (*store.User, *store.Session, []byte, error) {
	cookie, err := r.Cookie(s.cookieName(sessionCookie))
	if err != nil {
		return nil, nil, nil, nil
	}
	hash := auth.HashToken(cookie.Value)
	user, session, extended, err := s.store.SessionUser(r.Context(), hash, sessionLifetime)
	if errors.Is(err, store.ErrNotFound) {
		s.setCookie(w, sessionCookie, "", 0)
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if user.Role < auth.RoleGuest {
		s.setCookie(w, sessionCookie, "", 0)
		return nil, nil, nil, nil
	}
	if extended {
		// The browser keeps the cookie for as long as the server keeps the session.
		s.setCookie(w, sessionCookie, cookie.Value, sessionLifetime)
	}
	return user, session, hash, nil
}

// require wraps a handler. The request must have a valid session of at least
// the given role.
func (s *Server) require(minimum auth.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, session, hash, err := s.currentSession(w, r)
		if err != nil {
			s.internal(w, err)
			return
		}
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role < minimum {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		ctx = context.WithValue(ctx, tokenHashKey, hash)
		ctx = context.WithValue(ctx, sessionKey, session)
		next(w, r.WithContext(ctx))
	}
}

// fresh reports whether the person typed a sign-in code a short time ago.
// Actions that change who has access need it, because a session lasts a year
// and a browser can stay open on a shared computer.
func (s *Server) fresh(w http.ResponseWriter, r *http.Request) bool {
	session := sessionFrom(r)
	if session != nil && time.Now().Unix()-session.AuthAt <= int64(recentAuth.Seconds()) {
		return true
	}
	writeError(w, http.StatusForbidden, "reauth_required")
	return false
}

// startSession creates a session and sets the cookie.
func (s *Server) startSession(r *http.Request, w http.ResponseWriter, user *store.User) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	id, err := auth.NewID()
	if err != nil {
		return err
	}
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), id, user.ID, deviceLabel(r.UserAgent()), sessionLifetime); err != nil {
		return err
	}
	s.setCookie(w, sessionCookie, token, sessionLifetime)
	return nil
}

// deviceLabel names a browser for the list of devices. It keeps only the
// family of the browser and of the system, never the full header.
func deviceLabel(agent string) string {
	browser := "Browser"
	for _, item := range []struct{ mark, name string }{
		{"Edg/", "Edge"}, {"EdgiOS/", "Edge"}, {"OPR/", "Opera"}, {"YaBrowser/", "Yandex Browser"},
		{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(agent, item.mark) {
			browser = item.name
			break
		}
	}
	system := ""
	for _, item := range []struct{ mark, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Mac OS X", "Mac"},
		{"Macintosh", "Mac"}, {"Windows", "Windows"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(agent, item.mark) {
			system = item.name
			break
		}
	}
	if system == "" {
		return browser
	}
	return browser + " · " + system
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(host, "[]")
}

// clientIP returns the address of the client. It reads X-Forwarded-For only
// from a trusted proxy, and it takes the last address that is not a proxy.
func (s *Server) clientIP(r *http.Request) netip.Addr {
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := remote.Addr().Unmap()
	if !s.isTrustedProxy(addr) {
		return addr
	}
	forwarded := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if err != nil {
			return addr
		}
		candidate = candidate.Unmap()
		if !s.isTrustedProxy(candidate) {
			return candidate
		}
	}
	return addr
}

func (s *Server) isTrustedProxy(addr netip.Addr) bool {
	for _, prefix := range s.cfg.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// limitKey groups addresses for rate limits. One IPv6 customer usually holds a
// whole /64, so a limit per address would be easy to escape.
func limitKey(addr netip.Addr) string {
	if !addr.IsValid() {
		return "unknown"
	}
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}

// auditIP shortens an address before it goes to the audit log.
func auditIP(addr netip.Addr) string {
	if !addr.IsValid() {
		return ""
	}
	bits := 48
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}

// audit writes one line to the audit log. The actor is named by the account
// number and the display name, so the log holds no address.
func (s *Server) audit(r *http.Request, user *store.User, action, target, detail string) {
	var id int64
	actor := ""
	if user != nil {
		id, actor = user.ID, user.DisplayName
	}
	if err := s.store.AddAudit(r.Context(), id, actor, action, target, detail, auditIP(s.clientIP(r))); err != nil {
		s.log.Error("audit write failed", "error", err)
	}
}
