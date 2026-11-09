package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/mail"
	"github.com/Renset/heirfold/internal/store"
)

// testSite is the address of the site in the letters of the tests.
const testSite = "https://tree.example.org"

// lockedBuffer collects the server log. The server writes from several
// goroutines while the test reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// fakeMail is a mail transport that keeps the letters for the test.
type fakeMail struct {
	mu      sync.Mutex
	pending []mail.Message
	total   int
	arrived chan struct{}
	// fail, if set, gives the answer of the transport.
	fail func(mail.Message) error
}

func newFakeMail() *fakeMail {
	return &fakeMail{arrived: make(chan struct{}, 1024)}
}

func (f *fakeMail) Name() string { return "fake" }

func (f *fakeMail) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	fail := f.fail
	f.pending = append(f.pending, m)
	f.total++
	f.mu.Unlock()
	select {
	case f.arrived <- struct{}{}:
	default:
	}
	if fail != nil {
		return fail(m)
	}
	return nil
}

// setFail makes the transport answer with the result of fail. A nil value
// makes it accept every letter again.
func (f *fakeMail) setFail(fail func(mail.Message) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

func (f *fakeMail) pop(kind, to string) (mail.Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.pending {
		if m.Kind == kind && m.To == to {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return m, true
		}
	}
	return mail.Message{}, false
}

// take returns the oldest letter of this kind to this address that no test
// took before. The server sends a letter after the response, so take waits.
func (f *fakeMail) take(t *testing.T, kind, to string) mail.Message {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		if m, ok := f.pop(kind, to); ok {
			return m
		}
		select {
		case <-f.arrived:
		case <-timeout:
			t.Fatalf("no %s letter to %s", kind, to)
		}
	}
}

// count returns the number of all letters since the start.
func (f *fakeMail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

// drain forgets the letters that no test took.
func (f *fakeMail) drain() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.pending
	f.pending = nil
	return out
}

type harness struct {
	t      *testing.T
	store  *store.Store
	app    *Server
	server *httptest.Server
	mail   *fakeMail
	log    *lockedBuffer
	dbPath string
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	letters := newFakeMail()
	cfg := Config{DataDir: dir, Mail: letters, BaseURL: testSite}
	if mutate != nil {
		mutate(&cfg)
	}
	log := &lockedBuffer{}
	app, err := New(cfg, st, slog.New(slog.NewTextHandler(log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	h := &harness{t: t, store: st, app: app, server: ts, mail: letters, log: log, dbPath: path}
	// The letters go out after the response. They must finish before the
	// database closes.
	t.Cleanup(h.idle)
	return h
}

// idle waits until the server has no work after a response.
func (h *harness) idle() {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(h.app.jobs) > 0 {
		if time.Now().After(deadline) {
			h.t.Error("the background jobs did not finish")
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// exec changes the database file directly, for states that need time to
// pass: an old sign-in code or a session that nobody used for some days.
func (h *harness) exec(query string, args ...any) {
	h.t.Helper()
	h.idle()
	pragma := url.Values{}
	pragma.Add("_pragma", "busy_timeout(10000)")
	u := url.URL{Scheme: "file", OmitHost: true, Path: h.dbPath, RawQuery: pragma.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(query, args...); err != nil {
		h.t.Fatal(err)
	}
}

// resetLimits gives every client a full allowance again.
func (h *harness) resetLimits() {
	for _, limits := range []*rateLimits{h.app.limits, h.app.anon} {
		limits.mu.Lock()
		clear(limits.buckets)
		limits.mu.Unlock()
	}
}

type client struct {
	h    *harness
	http *http.Client
	// headers go out with every request of this client.
	headers map[string]string
}

func (h *harness) client() *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return &client{h: h, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	c.h.t.Helper()
	var reader io.Reader
	switch v := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			c.h.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.h.server.URL+path, reader)
	if err != nil {
		c.h.t.Fatal(err)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		c.h.t.Fatal(err)
	}
	return res, data
}

func (c *client) expect(status int, method, path string, body any) []byte {
	c.h.t.Helper()
	res, data := c.do(method, path, body, nil)
	if res.StatusCode != status {
		c.h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, res.StatusCode, status, data)
	}
	return data
}

// refuse expects an error answer with the given status and error code.
func (c *client) refuse(status int, code, method, path string, body any) {
	c.h.t.Helper()
	data := c.expect(status, method, path, body)
	if got := decode[map[string]string](c.h.t, data)["error"]; got != code {
		c.h.t.Fatalf("%s %s: error %q, want %q", method, path, got, code)
	}
}

// cookie returns the value of a cookie that the client keeps.
func (c *client) cookie(name string) string {
	c.h.t.Helper()
	u, err := url.Parse(c.h.server.URL)
	if err != nil {
		c.h.t.Fatal(err)
	}
	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func cookieNamed(res *http.Response, name string) *http.Cookie {
	for _, cookie := range res.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// requestCode asks for a sign-in code and returns the letter.
func (c *client) requestCode(email string) mail.Message {
	c.h.t.Helper()
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": email})
	return c.h.mail.take(c.h.t, "code", email)
}

// signIn takes the client through the real flow: it asks for a code, reads
// the code from the letter and types it. It returns the cookies of the answer.
func (c *client) signIn(email string) []*http.Cookie {
	c.h.t.Helper()
	letter := c.requestCode(email)
	res, data := c.do("POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code}, nil)
	if res.StatusCode != http.StatusOK {
		c.h.t.Fatalf("sign-in of %s: status %d: %s", email, res.StatusCode, data)
	}
	return res.Cookies()
}

// account creates a user without a session.
func (h *harness) account(email, name string, role auth.Role) *store.User {
	h.t.Helper()
	user, err := h.store.CreateUser(context.Background(), email, name, role, "", 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return user
}

// user creates a user and signs a new client in.
func (h *harness) user(email string, role auth.Role) *client {
	h.t.Helper()
	h.account(email, nameFromEmail(email), role)
	c := h.client()
	c.signIn(email)
	return c
}

func (h *harness) loadFixture() {
	h.t.Helper()
	if _, err := h.store.ReplaceTree(context.Background(), fixture.Generate(fixture.DefaultOptions("en"))); err != nil {
		h.t.Fatal(err)
	}
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return out
}

func userPath(id int64) string {
	return "/api/v1/users/" + strconv.FormatInt(id, 10)
}

func TestProtectedRoutesNeedSession(t *testing.T) {
	h := newHarness(t, nil)
	anonymous := h.client()
	// editPaths adds the routes of the editors and the journal.
	for _, route := range append([][2]string{
		{"GET", "/api/v1/me"}, {"PATCH", "/api/v1/me"}, {"POST", "/api/v1/auth/logout"},
		{"GET", "/api/v1/me/sessions"}, {"DELETE", "/api/v1/me/sessions/x"}, {"POST", "/api/v1/me/sessions/end-others"},
		{"GET", "/api/v1/tree"}, {"GET", "/api/v1/people/I0001"},
		{"GET", "/api/v1/media"}, {"GET", "/api/v1/media/O0001/image"}, {"GET", "/api/v1/media/O0001/file"},
		{"GET", "/api/v1/sync"}, {"POST", "/api/v1/sync"},
		{"POST", "/api/v1/imports"}, {"POST", "/api/v1/imports/x/commit"}, {"DELETE", "/api/v1/imports/x"},
		{"GET", "/api/v1/export.gramps"},
		{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"}, {"POST", "/api/v1/users/1/invite"},
		{"PATCH", "/api/v1/users/1"}, {"DELETE", "/api/v1/users/1"},
		{"GET", "/api/v1/audit"},
	}, editPaths()...) {
		res, _ := anonymous.do(route[0], route[1], nil, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", route[0], route[1], res.StatusCode)
		}
	}
}

// The endpoints of the time of passwords and invitation links are gone.
func TestOldEndpointsAreGone(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	for _, c := range []*client{h.client(), admin} {
		for _, route := range [][2]string{
			{"POST", "/api/v1/auth/login"}, {"POST", "/api/v1/me/password"},
			{"GET", "/api/v1/invites"}, {"POST", "/api/v1/invites"}, {"DELETE", "/api/v1/invites/x"},
			{"POST", "/api/v1/invites/check"}, {"POST", "/api/v1/invites/accept"},
		} {
			res, _ := c.do(route[0], route[1], map[string]string{"username": "root", "password": "x"}, nil)
			if res.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s: %d", route[0], route[1], res.StatusCode)
			}
		}
	}
}

func TestRolesLimitAdminRoutes(t *testing.T) {
	h := newHarness(t, nil)
	h.loadFixture()
	adminOnly := [][2]string{
		{"POST", "/api/v1/imports"}, {"POST", "/api/v1/imports/x/commit"}, {"DELETE", "/api/v1/imports/x"},
		{"PATCH", "/api/v1/users/1"}, {"GET", "/api/v1/audit"}, {"POST", "/api/v1/sync"},
	}
	// The file of the whole tree is for the people who see the whole tree.
	export := [][2]string{{"GET", "/api/v1/export.gramps"}}
	people := [][2]string{
		{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"}, {"POST", "/api/v1/users/1/invite"}, {"DELETE", "/api/v1/users/1"},
	}
	check := func(name string, c *client, routes [][2]string) {
		t.Helper()
		for _, route := range routes {
			res, body := c.do(route[0], route[1], nil, nil)
			if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"forbidden"`) {
				t.Errorf("%s %s %s: %d %s", name, route[0], route[1], res.StatusCode, body)
			}
		}
	}
	for name, role := range map[string]auth.Role{"guest": auth.RoleGuest, "member": auth.RoleMember} {
		c := h.user(name+"@example.org", role)
		if role == auth.RoleGuest {
			check(name, c, export)
		} else if res, body := c.do("GET", "/api/v1/export.gramps", nil, nil); res.StatusCode != http.StatusOK ||
			res.Header.Get("Content-Type") != "application/gzip" || len(body) < 1000 {
			t.Errorf("%s: export: status %d, type %q, %d bytes", name, res.StatusCode, res.Header.Get("Content-Type"), len(body))
		}
		check(name, c, adminOnly)
		check(name, c, people)
		// The routes of the editors and the journal.
		check(name, c, editPaths())
	}
	editor := h.user("editor@example.org", auth.RoleEditor)
	check("editor", editor, adminOnly)
	editor.expect(http.StatusOK, "GET", "/api/v1/users", nil)
	editor.expect(http.StatusOK, "GET", "/api/v1/history", nil)
	editor.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil)

	admin := h.user("admin@example.org", auth.RoleAdmin)
	admin.expect(http.StatusOK, "GET", "/api/v1/history", nil)
	admin.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil)
	admin.expect(http.StatusOK, "GET", "/api/v1/users", nil)
	admin.expect(http.StatusOK, "GET", "/api/v1/audit", nil)
	admin.expect(http.StatusOK, "GET", "/api/v1/export.gramps", nil)
}

func TestGuestGetsReducedTree(t *testing.T) {
	h := newHarness(t, nil)
	h.loadFixture()
	guestClient := h.user("guest1@example.org", auth.RoleGuest)
	guest := decode[store.Graph](t, guestClient.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	member := decode[store.Graph](t, h.user("member1@example.org", auth.RoleMember).expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	if len(guest.People) == 0 || len(guest.People) >= len(member.People) {
		t.Fatalf("guest %d, member %d", len(guest.People), len(member.People))
	}
	var hidden string
	visible := map[string]bool{}
	for _, card := range guest.People {
		visible[card.ID] = true
	}
	for _, card := range member.People {
		if !visible[card.ID] && card.Living {
			hidden = card.ID
			break
		}
	}
	if hidden == "" {
		t.Fatal("no hidden living person")
	}
	guestClient.expect(http.StatusNotFound, "GET", "/api/v1/people/"+hidden, nil)
	me := decode[accountDTO](t, guestClient.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.ViewPrivate || me.ViewLiving || me.Edit || me.Invite || me.ManageUsers || me.ManageTree || me.ImportTree {
		t.Fatalf("rights of a guest: %+v", me)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.HSTS = true })
	for _, path := range []string{"/", "/api/v1/status", "/does-not-exist", "/api/v1/nothing"} {
		res, _ := h.client().do("GET", path, nil, nil)
		csp := res.Header.Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "frame-ancestors 'none'", "require-trusted-types-for 'script'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP lacks %s", path, want)
			}
		}
		if strings.Contains(csp, "unsafe-") {
			t.Errorf("%s: CSP has an unsafe source: %s", path, csp)
		}
		for header, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "no-referrer",
			"Cross-Origin-Opener-Policy":   "same-origin",
			"Cross-Origin-Resource-Policy": "same-origin",
		} {
			if got := res.Header.Get(header); got != want {
				t.Errorf("%s: %s = %q", path, header, got)
			}
		}
		if res.Header.Get("Strict-Transport-Security") == "" {
			t.Errorf("%s: no HSTS", path)
		}
	}
}

func TestAPIResponsesAreNotStored(t *testing.T) {
	h := newHarness(t, nil)
	h.loadFixture()
	c := h.user("anna@example.org", auth.RoleMember)
	res, _ := c.do("GET", "/api/v1/me", nil, nil)
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control: %q", got)
	}
	res, _ = c.do("GET", "/api/v1/tree", nil, nil)
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "private") {
		t.Fatalf("Cache-Control: %q", got)
	}
}

func TestTreeRevalidates(t *testing.T) {
	h := newHarness(t, nil)
	h.loadFixture()
	c := h.user("anna@example.org", auth.RoleMember)
	first, _ := c.do("GET", "/api/v1/tree", nil, nil)
	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	second, _ := c.do("GET", "/api/v1/tree", nil, map[string]string{"If-None-Match": etag})
	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("status %d", second.StatusCode)
	}
}

func TestCrossOriginWritesAreBlocked(t *testing.T) {
	h := newHarness(t, nil)
	c := h.user("anna@example.org", auth.RoleMember)
	res, _ := c.do("POST", "/api/v1/auth/logout", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", res.StatusCode)
	}
	res, _ = c.do("POST", "/api/v1/auth/logout", nil, map[string]string{"Origin": "https://evil.example"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", res.StatusCode)
	}
	c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	// Another site must not make a browser ask for a code or type one.
	sent := h.mail.count()
	for path, body := range map[string]map[string]string{
		"/api/v1/auth/code":   {"email": "anna@example.org"},
		"/api/v1/auth/verify": {"code": "12345678"},
		"/api/v1/setup":       {"token": h.app.SetupToken(), "email": "evil@example.org"},
	} {
		res, _ = h.client().do("POST", path, body, map[string]string{"Sec-Fetch-Site": "cross-site"})
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d", path, res.StatusCode)
		}
	}
	h.idle()
	if h.mail.count() != sent {
		t.Fatal("a request from another site sent a letter")
	}
}

func TestHostCheck(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.AllowedHosts = []string{"family.example"} })
	req, err := http.NewRequest("GET", h.server.URL+"/api/v1/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
	req.Host = "family.example"
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestBodyLimits(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	c.expect(http.StatusRequestEntityTooLarge, "POST", "/api/v1/auth/code", []byte(`{"email":"`+strings.Repeat("a", maxJSONBody)+`"}`))
	c.expect(http.StatusBadRequest, "POST", "/api/v1/auth/code", []byte(`{"email":"a@example.org","extra":1}`))
	c.expect(http.StatusBadRequest, "POST", "/api/v1/auth/code", []byte(`not json`))
	c.expect(http.StatusBadRequest, "POST", "/api/v1/auth/verify", []byte(`{"code":"12345678","email":"a@example.org"}`))
	// A client of the time of passwords gets a clear refusal.
	c.expect(http.StatusBadRequest, "POST", "/api/v1/setup", []byte(`{"token":"x","username":"root","password":"x"}`))
}

func itoa(n int64) string {
	data, _ := json.Marshal(n)
	return string(data)
}

func TestImportFlow(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	data, err := gramps.Marshal(fixture.Generate(fixture.DefaultOptions("en")))
	if err != nil {
		t.Fatal(err)
	}
	admin.expect(http.StatusUnprocessableEntity, "POST", "/api/v1/imports", []byte("not a tree"))

	type preview struct {
		ID       string `json:"id"`
		Replaces *struct {
			People int `json:"people"`
		} `json:"replaces"`
		Counts struct {
			People int `json:"people"`
		} `json:"counts"`
	}
	first := decode[preview](t, admin.expect(http.StatusOK, "POST", "/api/v1/imports", data))
	if first.Counts.People == 0 || first.Replaces != nil {
		t.Fatalf("preview: %+v", first)
	}
	graph := decode[store.Graph](t, admin.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	if len(graph.People) != 0 {
		t.Fatal("the preview wrote data")
	}
	admin.expect(http.StatusOK, "POST", "/api/v1/imports/"+first.ID+"/commit", map[string]bool{"replace": false})
	admin.expect(http.StatusNotFound, "POST", "/api/v1/imports/"+first.ID+"/commit", map[string]bool{"replace": true})
	graph = decode[store.Graph](t, admin.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	if len(graph.People) != first.Counts.People {
		t.Fatalf("people: %d", len(graph.People))
	}

	second := decode[preview](t, admin.expect(http.StatusOK, "POST", "/api/v1/imports", data))
	if second.Replaces == nil || second.Replaces.People != first.Counts.People {
		t.Fatalf("preview: %+v", second)
	}
	admin.expect(http.StatusConflict, "POST", "/api/v1/imports/"+second.ID+"/commit", map[string]bool{"replace": false})
	admin.expect(http.StatusOK, "POST", "/api/v1/imports/"+second.ID+"/commit", map[string]bool{"replace": true})

	audit := string(admin.expect(http.StatusOK, "GET", "/api/v1/audit", nil))
	for _, action := range []string{"login.success", "import.preview", "import.commit"} {
		if !strings.Contains(audit, action) {
			t.Errorf("audit lacks %s", action)
		}
	}
}

func TestUploadLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxUpload = 1024 })
	admin := h.user("root@example.org", auth.RoleAdmin)
	admin.expect(http.StatusRequestEntityTooLarge, "POST", "/api/v1/imports", bytes.Repeat([]byte("a"), 4096))
}

func TestClientIP(t *testing.T) {
	s := &Server{cfg: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}
	cases := []struct {
		remote, forwarded, want string
	}{
		{"203.0.113.9:1234", "", "203.0.113.9"},
		{"203.0.113.9:1234", "1.2.3.4", "203.0.113.9"},
		{"10.0.0.2:1234", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:1234", "1.2.3.4, 198.51.100.7, 10.0.0.3", "198.51.100.7"},
		{"10.0.0.2:1234", "not-an-ip", "10.0.0.2"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remote
		if tc.forwarded != "" {
			req.Header.Set("X-Forwarded-For", tc.forwarded)
		}
		if got := s.clientIP(req).String(); got != tc.want {
			t.Errorf("%s + %q: got %s, want %s", tc.remote, tc.forwarded, got, tc.want)
		}
	}
}

func TestAuditIPIsShortened(t *testing.T) {
	if got := auditIP(netip.MustParseAddr("203.0.113.9")); got != "203.0.113.0/24" {
		t.Fatalf("got %s", got)
	}
	if got := auditIP(netip.MustParseAddr("2001:db8:1:2:3::9")); got != "2001:db8:1::/48" {
		t.Fatalf("got %s", got)
	}
}

func TestRateLimitBuckets(t *testing.T) {
	l := newRateLimits()
	rule := bucketRule{capacity: 2, perSec: 0.0001}
	for i := 0; i < 2; i++ {
		if !l.allow("a", rule) {
			t.Fatal("first two must pass")
		}
	}
	if l.allow("a", rule) {
		t.Fatal("third must fail")
	}
	if !l.allow("b", rule) {
		t.Fatal("another key must pass")
	}
}

func TestStaticFilesAreSafe(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	res, body := c.do("GET", "/", nil, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "text/html") || len(body) == 0 {
		t.Fatalf("index: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	for _, path := range []string{"/../../etc/passwd", "/.gitkeep", "/secret.db"} {
		u, err := url.Parse(h.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res, _ := c.do("GET", u.RequestURI(), nil, nil)
		if res.StatusCode == http.StatusOK {
			t.Errorf("%s: served", path)
		}
	}
}

func TestLimitKeyGroupsIPv6Networks(t *testing.T) {
	a := limitKey(netip.MustParseAddr("2001:db8:1:2:aaaa::1"))
	b := limitKey(netip.MustParseAddr("2001:db8:1:2:bbbb::2"))
	c := limitKey(netip.MustParseAddr("2001:db8:1:3::1"))
	if a != b || a == c {
		t.Fatalf("keys: %s %s %s", a, b, c)
	}
	if limitKey(netip.MustParseAddr("203.0.113.9")) != "203.0.113.9" {
		t.Fatal("an IPv4 address must stay whole")
	}
}

func TestFullLimiterStillServesNewClients(t *testing.T) {
	l := newRateLimits()
	busy := bucketRule{capacity: 5, perSec: 0.0001}
	for i := 0; i < maxBuckets; i++ {
		key := "k" + itoa(int64(i))
		l.allow(key, busy)
	}
	if !l.allow("new-client", rulePublic) {
		t.Fatal("a new client was refused")
	}
	if len(l.buckets) > maxBuckets+1 {
		t.Fatalf("buckets: %d", len(l.buckets))
	}
}
