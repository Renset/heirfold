package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/mail"
	"github.com/Renset/heirfold/internal/store"
)

const yearSeconds = 365 * 24 * 60 * 60

// wrongCode returns a code of the right form that is not the given code.
func wrongCode(code string) string {
	last := code[len(code)-1]
	return code[:len(code)-1] + string('0'+(last-'0'+1)%10)
}

func (h *harness) userByEmail(email string) *store.User {
	h.t.Helper()
	user, err := h.store.UserByEmail(context.Background(), email)
	if err != nil {
		h.t.Fatalf("%s: %v", email, err)
	}
	return user
}

// auditText returns the audit log as the API sends it.
func (h *harness) auditText() string {
	h.t.Helper()
	entries, err := h.store.ListAudit(context.Background(), 1000)
	if err != nil {
		h.t.Fatal(err)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

// people returns the list of people with access, by display name.
func (c *client) people() map[string]personDTO {
	c.h.t.Helper()
	list := decode[struct {
		Users []personDTO `json:"users"`
	}](c.h.t, c.expect(http.StatusOK, "GET", "/api/v1/users", nil))
	out := make(map[string]personDTO, len(list.Users))
	for _, person := range list.Users {
		out[person.DisplayName] = person
	}
	return out
}

// invite sends an invitation and returns the new person and the letter.
func (c *client) invite(email, name, role string) (personDTO, mail.Message) {
	c.h.t.Helper()
	data := c.expect(http.StatusCreated, "POST", "/api/v1/users", map[string]string{"email": email, "displayName": name, "role": role})
	person := decode[personDTO](c.h.t, data)
	return person, c.h.mail.take(c.h.t, "invite", person.Email)
}

// makeStale moves the last sign-in code of every session into the past.
func (h *harness) makeStale(age time.Duration) {
	h.t.Helper()
	h.exec(`UPDATE session SET auth_at = ?`, time.Now().Add(-age).Unix())
}

func TestSetup(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	status := decode[map[string]bool](t, c.expect(http.StatusOK, "GET", "/api/v1/status", nil))
	if !status["needsSetup"] || status["codesInLog"] {
		t.Fatalf("status: %v", status)
	}
	request := map[string]string{"token": "wrong", "email": "  Root@Example.org ", "displayName": "Roza", "lang": "ru"}
	c.refuse(http.StatusForbidden, "setup_denied", "POST", "/api/v1/setup", request)
	request["token"] = h.app.SetupToken()
	for _, bad := range []string{"", "root", "Roza <root@example.org>", "root@localhost", "root@example.org\r\nBcc: x@example.com"} {
		request["email"] = bad
		c.refuse(http.StatusBadRequest, "bad_email", "POST", "/api/v1/setup", request)
	}
	request["email"] = "  Root@Example.org "
	request["displayName"] = "Ro\x00za"
	c.refuse(http.StatusBadRequest, "bad_name", "POST", "/api/v1/setup", request)
	request["displayName"] = "Roza"
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)

	created := decode[accountDTO](t, c.expect(http.StatusCreated, "POST", "/api/v1/setup", request))
	if created.User == nil || created.User.Email != "root@example.org" || created.User.DisplayName != "Roza" || created.User.RoleName != "admin" {
		t.Fatalf("created: %+v", created.User)
	}
	me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.User.RoleName != "admin" || !me.ManageUsers || !me.Invite || !me.Edit || !me.ManageTree || !me.ImportTree ||
		!me.ViewPrivate || !me.ViewLiving || me.Source != "heirfold" {
		t.Fatalf("me: %+v", me)
	}
	if age := time.Now().Unix() - me.AuthAt; age < 0 || age > 60 {
		t.Fatalf("authAt is %d seconds old", age)
	}
	if stored := h.userByEmail("root@example.org"); stored.Lang != "ru" || stored.Role != auth.RoleAdmin {
		t.Fatalf("stored: %+v", stored)
	}

	request["email"] = "second@example.org"
	h.client().refuse(http.StatusForbidden, "setup_denied", "POST", "/api/v1/setup", request)
	status = decode[map[string]bool](t, c.expect(http.StatusOK, "GET", "/api/v1/status", nil))
	if status["needsSetup"] {
		t.Fatal("setup is still needed")
	}
	// The link from the console counts as a fresh proof, so the first
	// administrator can give every role at once.
	c.invite("editor@example.org", "Egor", "editor")
	h.idle()
	if h.mail.count() != 1 {
		t.Fatalf("setup must send no letter of its own: %d letters", h.mail.count())
	}
	if audit := h.auditText(); !strings.Contains(audit, "setup.complete") {
		t.Fatalf("audit: %s", audit)
	}
}

func TestSetupNamesTheAdministratorByTheAddress(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	c.expect(http.StatusCreated, "POST", "/api/v1/setup", map[string]string{"token": h.app.SetupToken(), "email": "roza@example.org"})
	me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.User.DisplayName != "roza" || me.User.Lang != "" {
		t.Fatalf("me: %+v", me.User)
	}
}

func TestSignInFlow(t *testing.T) {
	h := newHarness(t, nil)
	anna := h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	res, body := c.do("POST", "/api/v1/auth/code", map[string]string{"email": "  Anna@Example.org "}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	answer := decode[map[string]any](t, body)
	if answer["ok"] != true || answer["minutes"] != float64(10) || len(answer) != 2 {
		t.Fatalf("answer: %s", body)
	}
	attempt := cookieNamed(res, "heirfold-signin")
	if attempt == nil || attempt.Value == "" || !attempt.HttpOnly || attempt.SameSite != http.SameSiteStrictMode || attempt.Path != "/" || attempt.MaxAge != 15*60 {
		t.Fatalf("attempt cookie: %+v", attempt)
	}
	if cookieNamed(res, "heirfold") != nil {
		t.Fatal("a request for a code gave a session")
	}

	letter := h.mail.take(t, "code", "anna@example.org")
	code, ok := auth.CleanCode(letter.Code)
	if !ok || code != letter.Code {
		t.Fatalf("code %q", letter.Code)
	}
	if letter.UserID != anna.ID || letter.Subject != "Your sign-in code for the family tree" {
		t.Fatalf("letter: %+v", letter)
	}
	for _, want := range []string{code[:4] + " " + code[4:], testSite, "10 minutes"} {
		if !strings.Contains(letter.Text, want) {
			t.Errorf("the letter lacks %q: %s", want, letter.Text)
		}
	}
	// The secret of the browser is not in the letter and not in the answer.
	if strings.Contains(letter.Text, attempt.Value) || strings.Contains(string(body), code) {
		t.Fatal("a secret is in the wrong place")
	}
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)

	// A person can type the code as the letter shows it.
	res, body = c.do("POST", "/api/v1/auth/verify", map[string]string{"code": " " + code[:4] + " " + code[4:] + " "}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("verify: status %d: %s", res.StatusCode, body)
	}
	account := decode[accountDTO](t, body)
	if account.User.ID != anna.ID || account.User.Email != "anna@example.org" || account.User.RoleName != "member" ||
		!account.ViewPrivate || !account.ViewLiving || account.Edit || account.Invite || account.ManageUsers {
		t.Fatalf("account: %+v user=%+v", account, account.User)
	}
	if cookieNamed(res, "heirfold") == nil {
		t.Fatal("no session cookie")
	}
	if cleared := cookieNamed(res, "heirfold-signin"); cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("the attempt cookie stays: %+v", cleared)
	}
	me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.User.ID != anna.ID || me.AuthAt == 0 {
		t.Fatalf("me: %+v", me)
	}
	if stored := h.userByEmail("anna@example.org"); stored.LastLoginAt == nil {
		t.Fatal("the first sign-in was not stored")
	}

	// The code works one time, also for the browser that asked for it.
	replay := h.client()
	res, body = replay.do("POST", "/api/v1/auth/verify", map[string]string{"code": code}, map[string]string{"Cookie": "heirfold-signin=" + attempt.Value})
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "invalid_code") {
		t.Fatalf("second use of the code: %d %s", res.StatusCode, body)
	}
	audit := h.auditText()
	if !strings.Contains(audit, `"login.success"`) || !strings.Contains(audit, `"actor":"Anna"`) {
		t.Fatalf("audit: %s", audit)
	}
}

func TestUnknownAddressGetsNoLetter(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	stranger := h.client()
	unknownRes, unknownBody := stranger.do("POST", "/api/v1/auth/code", map[string]string{"email": "nobody@example.org"}, nil)
	h.idle()
	if h.mail.count() != 0 {
		t.Fatalf("an unknown address got %d letters", h.mail.count())
	}
	knownRes, knownBody := h.client().do("POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"}, nil)
	h.mail.take(t, "code", "anna@example.org")
	if unknownRes.StatusCode != http.StatusOK || knownRes.StatusCode != http.StatusOK || string(unknownBody) != string(knownBody) {
		t.Fatalf("the answers differ: %d %s vs %d %s", unknownRes.StatusCode, unknownBody, knownRes.StatusCode, knownBody)
	}
	// Both answers look the same down to the cookie.
	a, b := cookieNamed(unknownRes, "heirfold-signin"), cookieNamed(knownRes, "heirfold-signin")
	if a == nil || b == nil || a.Value == "" || len(a.Value) != len(b.Value) || a.MaxAge != b.MaxAge || a.HttpOnly != b.HttpOnly || a.SameSite != b.SameSite {
		t.Fatalf("the cookies differ: %+v vs %+v", a, b)
	}
	if len(unknownRes.Cookies()) != len(knownRes.Cookies()) {
		t.Fatal("the number of cookies differs")
	}
	stranger.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": "12345678"})
	if _, err := h.store.UserByEmail(context.Background(), "nobody@example.org"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a request for a code made an account: %v", err)
	}
}

func TestWrongCodeIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	letter := c.requestCode("anna@example.org")
	c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": wrongCode(letter.Code)})
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	// Text that is not a code is refused and does not count as a try.
	for _, text := range []string{"", "1234", "abcdefgh", letter.Code + "1", "1234_5678"} {
		c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": text})
	}
	// One wrong try does not end the code.
	c.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	if audit := h.auditText(); !strings.Contains(audit, `"login.failed"`) {
		t.Fatalf("audit: %s", audit)
	}
}

func TestFiveWrongCodesEndTheCode(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	letter := c.requestCode("anna@example.org")
	for i := 0; i < maxCodeTries; i++ {
		c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": wrongCode(letter.Code)})
	}
	c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	if maxCodeTries != 5 {
		t.Fatalf("maxCodeTries is %d", maxCodeTries)
	}
	// The person asks for a new code and signs in.
	c.signIn("anna@example.org")
	c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
}

func TestCodeNeedsTheBrowserThatAskedForIt(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	owner := h.client()
	letter := owner.requestCode("anna@example.org")

	// A browser without the cookie of the attempt.
	bare := h.client()
	for i := 0; i < maxCodeTries+1; i++ {
		bare.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	}
	// A browser with a cookie of its own attempt for another account.
	other := h.client()
	other.requestCode("boris@example.org")
	other.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	// A browser with an invented cookie.
	res, _ := h.client().do("POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code}, map[string]string{"Cookie": "heirfold-signin=invented"})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an invented cookie: status %d", res.StatusCode)
	}
	for _, c := range []*client{bare, other} {
		c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	}
	// The tries of the other browsers did not use up the code.
	account := decode[accountDTO](t, owner.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code}))
	if account.User.Email != "anna@example.org" {
		t.Fatalf("account: %+v", account.User)
	}
}

func TestOldCodeIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	letter := c.requestCode("anna@example.org")
	h.exec(`UPDATE login_code SET expires_at = ?`, time.Now().Add(-time.Second).Unix())
	c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	if codeLifetime != 10*time.Minute {
		t.Fatalf("the letters promise 10 minutes, the code lives for %s", codeLifetime)
	}
}

func TestSessionCookie(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	cookies := h.client().signIn("anna@example.org")
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "heirfold" {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || session.Secure || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.Domain != "" {
		t.Fatalf("bad cookie: %+v", session)
	}
	if session.MaxAge != yearSeconds {
		t.Fatalf("Max-Age %d, want %d", session.MaxAge, yearSeconds)
	}
	if len(session.Value) < 43 {
		t.Fatalf("a short token: %q", session.Value)
	}
}

func TestSecureCookies(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SecureCookies = true })
	h.account("anna@example.org", "Anna", auth.RoleMember)
	// Whether a cookie jar sends a Secure cookie over plain HTTP to the
	// loopback interface depends on the jar, so this client has no jar and the
	// test carries the cookies by hand.
	c := &client{h: h, http: &http.Client{}}
	res, _ := c.do("POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"}, nil)
	attempt := cookieNamed(res, "__Host-heirfold-signin")
	if attempt == nil || !attempt.HttpOnly || !attempt.Secure || attempt.SameSite != http.SameSiteStrictMode || attempt.Path != "/" || attempt.Domain != "" {
		t.Fatalf("bad attempt cookie: %+v", attempt)
	}
	letter := h.mail.take(t, "code", "anna@example.org")
	verify := map[string]string{"code": letter.Code}
	// The name without the prefix does not count.
	res, _ = c.do("POST", "/api/v1/auth/verify", verify, map[string]string{"Cookie": "heirfold-signin=" + attempt.Value})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a cookie without the prefix: status %d", res.StatusCode)
	}
	res, body := c.do("POST", "/api/v1/auth/verify", verify, map[string]string{"Cookie": "__Host-heirfold-signin=" + attempt.Value})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("verify: status %d: %s", res.StatusCode, body)
	}
	session := cookieNamed(res, "__Host-heirfold")
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode || session.Path != "/" || session.Domain != "" || session.MaxAge != yearSeconds {
		t.Fatalf("bad cookie: %+v", session)
	}
	if res, _ := c.do("GET", "/api/v1/me", nil, map[string]string{"Cookie": "__Host-heirfold=" + session.Value}); res.StatusCode != http.StatusOK {
		t.Fatalf("me: status %d", res.StatusCode)
	}
	if res, _ := c.do("GET", "/api/v1/me", nil, map[string]string{"Cookie": "heirfold=" + session.Value}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session cookie without the prefix: status %d", res.StatusCode)
	}
}

func TestDisabledUserGetsNoLetter(t *testing.T) {
	h := newHarness(t, nil)
	h.account("gone@example.org", "Gone", auth.RoleDisabled)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	_, disabled := c.do("POST", "/api/v1/auth/code", map[string]string{"email": "gone@example.org"}, nil)
	h.idle()
	if h.mail.count() != 0 {
		t.Fatalf("a disabled user got %d letters", h.mail.count())
	}
	res, unknown := h.client().do("POST", "/api/v1/auth/code", map[string]string{"email": "nobody@example.org"}, nil)
	if res.StatusCode != http.StatusOK || string(disabled) != string(unknown) {
		t.Fatalf("a disabled account looks different: %s vs %s", disabled, unknown)
	}
	c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": "12345678"})
}

func TestUserDisabledAfterTheLetterCannotSignIn(t *testing.T) {
	h := newHarness(t, nil)
	anna := h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	c := h.client()
	letter := c.requestCode("anna@example.org")
	if _, err := h.store.SetUserRole(context.Background(), anna.ID, auth.RoleDisabled); err != nil {
		t.Fatal(err)
	}
	c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
}

func TestCodeRequestsAreRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	c := h.client()
	request := map[string]string{"email": "anna@example.org"}
	for i := 1; i <= 3; i++ {
		c.expect(http.StatusOK, "POST", "/api/v1/auth/code", request)
	}
	res, body := c.do("POST", "/api/v1/auth/code", request, nil)
	if res.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "rate_limited") {
		t.Fatalf("the fourth request: %d %s", res.StatusCode, body)
	}
	if cookieNamed(res, "heirfold-signin") != nil {
		t.Fatal("a refused request changed the cookie of the attempt")
	}
	// The limit counts the client together with the address, in any spelling.
	h.client().refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/auth/code", map[string]string{"email": " ANNA@example.org"})
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "boris@example.org"})
	h.mail.take(t, "code", "boris@example.org")
	h.idle()
	if n := h.mail.count(); n != 4 {
		t.Fatalf("letters: %d, want 3 for the first address and 1 for the second", n)
	}
	if audit := h.auditText(); strings.Count(audit, "login.throttled") != 1 {
		t.Fatalf("audit: %s", audit)
	}
}

func TestThrottledCodeRequestsWriteOneAuditRow(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	c := h.client()
	for i := 0; i < 40; i++ {
		c.do("POST", "/api/v1/auth/code", map[string]string{"email": "someone@example.org"}, nil)
	}
	audit := string(admin.expect(http.StatusOK, "GET", "/api/v1/audit", nil))
	if n := strings.Count(audit, "login.throttled"); n != 1 {
		t.Fatalf("login.throttled rows: %d", n)
	}
}

func TestLongAddressStopsBeforeTheLimiter(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	long := strings.Repeat("a", auth.MaxEmailLength) + "@example.org"
	c.refuse(http.StatusBadRequest, "bad_email", "POST", "/api/v1/auth/code", map[string]string{"email": long})
	c.refuse(http.StatusBadRequest, "bad_email", "POST", "/api/v1/auth/code", map[string]string{"email": "   "})
	c.refuse(http.StatusBadRequest, "bad_email", "POST", "/api/v1/auth/code", map[string]string{})
	if size := len(h.app.limits.buckets); size != 0 {
		t.Fatalf("buckets: %d", size)
	}
}

func TestAttackerCannotLockOutTheRealUser(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	})
	h.account("anna@example.org", "Anna", auth.RoleMember)
	attacker := h.client()
	attacker.headers = map[string]string{"X-Forwarded-For": "203.0.113.9"}
	request := map[string]string{"email": "anna@example.org"}
	for i := 0; i < 12; i++ {
		attacker.do("POST", "/api/v1/auth/code", request, nil)
	}
	res, _ := attacker.do("POST", "/api/v1/auth/code", request, nil)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the attacker is not limited: %d", res.StatusCode)
	}
	h.idle()
	if flood := len(h.mail.drain()); flood != 3 {
		t.Fatalf("the attacker caused %d letters", flood)
	}
	owner := h.client()
	owner.headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	owner.signIn("anna@example.org")
	owner.expect(http.StatusOK, "GET", "/api/v1/me", nil)
}

// One client can ask for three codes in a row. Those codes must not push out
// the code that the owner of the account already waits for.
func TestStrangerCannotPushOutTheWaitingCode(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	})
	h.account("anna@example.org", "Anna", auth.RoleMember)
	owner := h.client()
	owner.headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	letter := owner.requestCode("anna@example.org")

	stranger := h.client()
	stranger.headers = map[string]string{"X-Forwarded-For": "203.0.113.9"}
	request := map[string]string{"email": "anna@example.org"}
	allowed := 0
	for i := 0; i < 8; i++ {
		if res, _ := stranger.do("POST", "/api/v1/auth/code", request, nil); res.StatusCode == http.StatusOK {
			allowed++
		}
	}
	h.idle()
	if flood := len(h.mail.drain()); allowed != 3 || flood != 3 {
		t.Fatalf("the stranger got %d answers and caused %d letters, want 3 and 3", allowed, flood)
	}
	owner.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	owner.expect(http.StatusOK, "GET", "/api/v1/me", nil)
}

func TestLogoutEndsSession(t *testing.T) {
	h := newHarness(t, nil)
	c := h.user("anna@example.org", auth.RoleMember)
	token := c.cookie("heirfold")
	if token == "" {
		t.Fatal("no session cookie")
	}
	res, _ := c.do("POST", "/api/v1/auth/logout", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if cleared := cookieNamed(res, "heirfold"); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("the cookie stays: %+v", cleared)
	}
	c.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	// The server forgot the session, so a copy of the cookie is of no use.
	if res, _ := h.client().do("GET", "/api/v1/me", nil, map[string]string{"Cookie": "heirfold=" + token}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the old token still works: %d", res.StatusCode)
	}
}

func TestNewCodeReplacesTheSessionOfTheBrowser(t *testing.T) {
	h := newHarness(t, nil)
	c := h.user("anna@example.org", auth.RoleMember)
	first := c.cookie("heirfold")
	c.signIn("anna@example.org")
	second := c.cookie("heirfold")
	if first == "" || second == "" || first == second {
		t.Fatalf("tokens: %q %q", first, second)
	}
	if res, _ := h.client().do("GET", "/api/v1/me", nil, map[string]string{"Cookie": "heirfold=" + first}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the first session of the browser still works: %d", res.StatusCode)
	}
	list := decode[struct {
		Sessions []store.Session `json:"sessions"`
	}](t, c.expect(http.StatusOK, "GET", "/api/v1/me/sessions", nil))
	if len(list.Sessions) != 1 || !list.Sessions[0].Current {
		t.Fatalf("sessions: %+v", list.Sessions)
	}
}

func TestSessionCookieIsRenewedWithTheSession(t *testing.T) {
	h := newHarness(t, nil)
	c := h.user("anna@example.org", auth.RoleMember)
	res, _ := c.do("GET", "/api/v1/me", nil, nil)
	if res.StatusCode != http.StatusOK || cookieNamed(res, "heirfold") != nil {
		t.Fatalf("a new session got a new cookie: %d %+v", res.StatusCode, res.Cookies())
	}
	// Two days pass.
	h.exec(`UPDATE session SET expires_at = expires_at - ?`, 2*24*60*60)
	token := c.cookie("heirfold")
	res, _ = c.do("GET", "/api/v1/me", nil, nil)
	renewed := cookieNamed(res, "heirfold")
	if res.StatusCode != http.StatusOK || renewed == nil || renewed.Value != token || renewed.MaxAge != yearSeconds || !renewed.HttpOnly || renewed.SameSite != http.SameSiteStrictMode {
		t.Fatalf("the cookie was not renewed: %d %+v", res.StatusCode, renewed)
	}
	res, _ = c.do("GET", "/api/v1/me", nil, nil)
	if res.StatusCode != http.StatusOK || cookieNamed(res, "heirfold") != nil {
		t.Fatalf("the cookie came again on the same day: %d", res.StatusCode)
	}
	// A year passes without a visit.
	h.exec(`UPDATE session SET expires_at = ?`, time.Now().Add(-time.Second).Unix())
	res, _ = h.client().do("GET", "/api/v1/me", nil, map[string]string{"Cookie": "heirfold=" + token})
	if cleared := cookieNamed(res, "heirfold"); res.StatusCode != http.StatusUnauthorized || cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("an expired session: %d %+v", res.StatusCode, cleared)
	}
}

func TestGuestAndMemberGetNoListOfPeople(t *testing.T) {
	h := newHarness(t, nil)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	for _, role := range []auth.Role{auth.RoleGuest, auth.RoleMember} {
		c := h.user(role.String()+"@example.org", role)
		res, body := c.do("GET", "/api/v1/users", nil, nil)
		if res.StatusCode != http.StatusForbidden || strings.Contains(string(body), "example.org") || strings.Contains(string(body), "Roza") {
			t.Fatalf("%s: %d %s", role, res.StatusCode, body)
		}
		c.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/users", map[string]string{"email": "x@example.org", "role": "guest"})
	}
	if n, err := h.store.CountUsers(context.Background()); err != nil || n != 3 {
		t.Fatalf("users: %d %v", n, err)
	}
}

func TestEditorInvitesGuestsAndMembers(t *testing.T) {
	h := newHarness(t, nil)
	editorUser := h.account("editor@example.org", "Elena Editor", auth.RoleEditor)
	editor := h.client()
	editor.signIn("editor@example.org")

	data := editor.expect(http.StatusCreated, "POST", "/api/v1/users",
		map[string]string{"email": "  Nina@Example.org ", "displayName": " Нина ", "role": "member", "lang": "ru"})
	nina := decode[personDTO](t, data)
	if nina.Email != "nina@example.org" || nina.DisplayName != "Нина" || nina.Role != "member" || !nina.Pending || !nina.Mine || nina.Self || nina.LastLoginAt != nil {
		t.Fatalf("created: %+v", nina)
	}
	letter := h.mail.take(t, "invite", "nina@example.org")
	if letter.Kind != "invite" || letter.UserID != nina.ID || letter.Code != "" || letter.Subject != "Приглашение в семейное древо" {
		t.Fatalf("letter: %+v", letter)
	}
	for _, want := range []string{testSite, "Elena Editor"} {
		if !strings.Contains(letter.Text, want) {
			t.Errorf("the letter lacks %q: %s", want, letter.Text)
		}
	}
	if stored := h.userByEmail("nina@example.org"); stored.InvitedBy != editorUser.ID || stored.Lang != "ru" || stored.Role != auth.RoleMember {
		t.Fatalf("stored: %+v", stored)
	}

	// The inviter names the person. A name from the address would show a
	// part of the address to every editor.
	for _, name := range []string{"", "   "} {
		editor.refuse(http.StatusBadRequest, "bad_name", "POST", "/api/v1/users", map[string]string{"email": "gleb@example.org", "displayName": name, "role": "guest"})
	}
	editor.refuse(http.StatusBadRequest, "bad_name", "POST", "/api/v1/users", map[string]string{"email": "gleb@example.org", "role": "guest"})
	if _, err := h.store.UserByEmail(context.Background(), "gleb@example.org"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused invitation made an account: %v", err)
	}
	// Without a language the letter is in English.
	guest, letter := editor.invite("gleb@example.org", " Gleb ", "guest")
	if guest.DisplayName != "Gleb" || guest.Role != "guest" || letter.Subject != "Invitation to the family tree" ||
		!strings.Contains(letter.Text, "Elena Editor invites you") || !strings.Contains(letter.Text, testSite) {
		t.Fatalf("guest: %+v letter: %+v", guest, letter)
	}

	// An editor gives no role that can change the tree or its people.
	for _, role := range []string{"editor", "admin"} {
		editor.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/users", map[string]string{"email": role + "2@example.org", "role": role})
	}
	for _, role := range []string{"disabled", "owner", "contributor", "root", ""} {
		editor.refuse(http.StatusBadRequest, "bad_role", "POST", "/api/v1/users", map[string]string{"email": "x@example.org", "role": role})
	}
	for _, email := range []string{"", "nina", "Nina <nina2@example.org>", "nina2@localhost", "nina2@example.org\nBcc: x@example.com"} {
		editor.refuse(http.StatusBadRequest, "bad_email", "POST", "/api/v1/users", map[string]string{"email": email, "role": "member"})
	}
	editor.refuse(http.StatusBadRequest, "bad_name", "POST", "/api/v1/users", map[string]string{"email": "x@example.org", "displayName": strings.Repeat("я", 81), "role": "member"})
	editor.refuse(http.StatusConflict, "email_taken", "POST", "/api/v1/users", map[string]string{"email": "NINA@example.org", "displayName": "Nina", "role": "guest"})
	editor.refuse(http.StatusConflict, "email_taken", "POST", "/api/v1/users", map[string]string{"email": "editor@example.org", "displayName": "Elena", "role": "guest"})
	h.idle()
	if n, err := h.store.CountUsers(context.Background()); err != nil || n != 3 {
		t.Fatalf("users: %d %v", n, err)
	}
	if h.mail.count() != 3 {
		t.Fatalf("letters: %d, want the code of the editor and two invitations", h.mail.count())
	}

	// The invited person signs in with a code, like everybody.
	visitor := h.client()
	visitor.signIn("nina@example.org")
	me := decode[accountDTO](t, visitor.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.User.RoleName != "member" || me.User.DisplayName != "Нина" || me.Invite {
		t.Fatalf("me: %+v", me.User)
	}
	people := editor.people()
	if people["Нина"].Pending || people["Нина"].LastLoginAt == nil || !people["Gleb"].Pending {
		t.Fatalf("people: %+v", people)
	}
	audit := h.auditText()
	if strings.Count(audit, `"user.invite"`) != 2 || !strings.Contains(audit, `"actor":"Elena Editor"`) {
		t.Fatalf("audit: %s", audit)
	}
}

func TestAdminInvitesEditorWithARecentCode(t *testing.T) {
	h := newHarness(t, nil)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	admin := h.client()
	admin.signIn("root@example.org")
	editor, letter := admin.invite("editor@example.org", "Egor", "editor")
	if editor.Role != "editor" || !strings.Contains(letter.Text, "Roza invites you") {
		t.Fatalf("editor: %+v letter: %s", editor, letter.Text)
	}
	// 29 minutes after the code the administrator can still do it.
	h.makeStale(29 * time.Minute)
	if second, _ := admin.invite("admin2@example.org", "Alla", "admin"); second.Role != "admin" {
		t.Fatalf("second administrator: %+v", second)
	}

	h.makeStale(31 * time.Minute)
	for _, role := range []string{"editor", "admin"} {
		admin.refuse(http.StatusForbidden, "reauth_required", "POST", "/api/v1/users", map[string]string{"email": "late@example.org", "role": role})
	}
	if _, err := h.store.UserByEmail(context.Background(), "late@example.org"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused invitation made an account: %v", err)
	}
	// A member or a guest needs no recent code.
	admin.invite("member@example.org", "Mila", "member")
	admin.invite("guest@example.org", "Gleb", "guest")

	// A new code in the same browser opens the action again.
	admin.signIn("root@example.org")
	admin.invite("late@example.org", "Lev", "editor")
}

func TestAccessChangesNeedARecentCode(t *testing.T) {
	h := newHarness(t, nil)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	target := h.account("anna@example.org", "Anna", auth.RoleMember)
	admin := h.client()
	admin.signIn("root@example.org")
	anna := h.client()
	anna.signIn("anna@example.org")
	pending, _ := admin.invite("pending@example.org", "Pavel", "member")

	h.makeStale(31 * time.Minute)
	me := decode[accountDTO](t, admin.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if age := time.Now().Unix() - me.AuthAt; age < 31*60 || age > 32*60 {
		t.Fatalf("authAt is %d seconds old", age)
	}
	path := userPath(target.ID)
	admin.refuse(http.StatusForbidden, "reauth_required", "PATCH", path, map[string]string{"role": "editor"})
	admin.refuse(http.StatusForbidden, "reauth_required", "PATCH", path, map[string]string{"role": "disabled"})
	admin.refuse(http.StatusForbidden, "reauth_required", "PATCH", path, map[string]string{"email": "other@example.org"})
	admin.refuse(http.StatusForbidden, "reauth_required", "PATCH", path, map[string]string{"displayName": "Other", "role": "guest"})
	admin.refuse(http.StatusForbidden, "reauth_required", "DELETE", path, nil)
	stored := h.userByEmail("anna@example.org")
	if stored.Role != auth.RoleMember || stored.DisplayName != "Anna" {
		t.Fatalf("a refused change was stored: %+v", stored)
	}
	anna.expect(http.StatusOK, "GET", "/api/v1/me", nil)

	// A change of the shown name and the withdrawal of an unused invitation
	// give nobody new access.
	renamed := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]string{"displayName": " Анна Орлова "}))
	if renamed.DisplayName != "Анна Орлова" || renamed.Role != "member" {
		t.Fatalf("renamed: %+v", renamed)
	}
	admin.expect(http.StatusOK, "DELETE", userPath(pending.ID), nil)
	anna.expect(http.StatusOK, "GET", "/api/v1/me", nil)

	// After a new code the role changes, and it applies at once.
	admin.signIn("root@example.org")
	changed := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]string{"role": "guest"}))
	if changed.Role != "guest" {
		t.Fatalf("changed: %+v", changed)
	}
	anna.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	anna.signIn("anna@example.org")
	if me := decode[accountDTO](t, anna.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.User.RoleName != "guest" || me.ViewLiving {
		t.Fatalf("me: %+v", me)
	}
	admin.expect(http.StatusOK, "DELETE", path, nil)
	anna.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	audit := h.auditText()
	for _, want := range []string{`"user.role"`, `"member-\u003eguest"`, `"user.delete"`} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit lacks %s: %s", want, audit)
		}
	}
}

func TestUserUpdateChecksItsInput(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	target := h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	path := userPath(target.ID)
	for _, role := range []string{"owner", "contributor", "root", ""} {
		admin.refuse(http.StatusBadRequest, "bad_role", "PATCH", path, map[string]string{"role": role})
	}
	for _, email := range []string{"", "anna", "Anna <anna@example.com>", "anna@localhost"} {
		admin.refuse(http.StatusBadRequest, "bad_email", "PATCH", path, map[string]string{"email": email})
	}
	for _, name := range []string{"", "  ", "a\nb", strings.Repeat("x", 81)} {
		admin.refuse(http.StatusBadRequest, "bad_name", "PATCH", path, map[string]string{"displayName": name})
	}
	admin.refuse(http.StatusConflict, "email_taken", "PATCH", path, map[string]string{"email": "Boris@example.org"})
	admin.refuse(http.StatusNotFound, "not_found", "PATCH", userPath(999), map[string]string{"role": "guest"})
	admin.refuse(http.StatusBadRequest, "bad_request", "PATCH", "/api/v1/users/abc", map[string]string{"role": "guest"})
	admin.refuse(http.StatusBadRequest, "bad_request", "PATCH", path, map[string]string{"password": "x"})
	stored := h.userByEmail("anna@example.org")
	if stored.Role != auth.RoleMember || stored.DisplayName != "Anna" {
		t.Fatalf("a refused change was stored: %+v", stored)
	}
}

// A request that the server refuses must not store a part of itself.
func TestRefusedUpdateChangesNothing(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	target := h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	path := userPath(target.ID)
	admin.refuse(http.StatusBadRequest, "bad_email", "PATCH", path, map[string]string{"displayName": "Changed", "email": "not an address"})
	admin.refuse(http.StatusBadRequest, "bad_role", "PATCH", path, map[string]string{"displayName": "Changed", "role": "owner"})
	admin.refuse(http.StatusBadRequest, "bad_role", "PATCH", path, map[string]string{"displayName": "Changed", "email": "anna2@example.org", "role": "owner"})
	admin.refuse(http.StatusBadRequest, "bad_name", "PATCH", path, map[string]string{"displayName": "a\nb", "email": "anna2@example.org", "role": "guest"})
	admin.refuse(http.StatusConflict, "email_taken", "PATCH", path, map[string]string{"displayName": "Changed", "email": "boris@example.org", "role": "guest"})
	stored := h.userByEmail("anna@example.org")
	if stored.DisplayName != "Anna" || stored.Role != auth.RoleMember {
		t.Fatalf("a refused request changed the account: %+v", stored)
	}
	if _, err := h.store.UserByEmail(context.Background(), "anna2@example.org"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused request changed the address: %v", err)
	}
	if audit := h.auditText(); strings.Contains(audit, `"user.role"`) || strings.Contains(audit, `"user.email"`) {
		t.Fatalf("a refused request wrote to the audit log: %s", audit)
	}
}

// The last administrator cannot lose the role. The same request must not
// change the address or the name either.
func TestRefusedRoleChangeOfLastAdminChangesNothing(t *testing.T) {
	// BUG: internal/server/account.go:552-567. handleUserUpdate stores the new
	// address (line 552) before SetUserRole finds that the account is the last
	// administrator (line 565). The request gets 409 last_admin, but the
	// address is changed and all sessions of the account are ended.
	h := newHarness(t, nil)
	adminUser := h.account("root@example.org", "Roza", auth.RoleAdmin)
	admin := h.client()
	admin.signIn("root@example.org")
	admin.refuse(http.StatusConflict, "last_admin", "PATCH", userPath(adminUser.ID),
		map[string]string{"displayName": "Changed", "email": "root2@example.org", "role": "member"})
	stored, err := h.store.UserByID(context.Background(), adminUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "root@example.org" || stored.DisplayName != "Roza" || stored.Role != auth.RoleAdmin {
		t.Fatalf("a refused request changed the account: %+v", stored)
	}
}

func TestAdminChangesAnAddress(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	target := h.account("anna@example.org", "Anna", auth.RoleMember)
	anna := h.client()
	anna.signIn("anna@example.org")
	// The same address in another spelling is no change.
	admin.expect(http.StatusOK, "PATCH", userPath(target.ID), map[string]string{"email": " ANNA@example.org "})
	anna.expect(http.StatusOK, "GET", "/api/v1/me", nil)

	changed := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", userPath(target.ID), map[string]string{"email": " Anna@Example.com "}))
	if changed.Email != "anna@example.com" || changed.ID != target.ID {
		t.Fatalf("changed: %+v", changed)
	}
	// The sessions end, because the person behind the account can be another.
	anna.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	sent := h.mail.count()
	anna.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"})
	h.idle()
	if h.mail.count() != sent {
		t.Fatal("the old address got a letter")
	}
	anna.signIn("anna@example.com")
	if me := decode[accountDTO](t, anna.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.User.ID != target.ID || me.User.Email != "anna@example.com" {
		t.Fatalf("me: %+v", me.User)
	}
	if audit := h.auditText(); !strings.Contains(audit, `"user.email"`) {
		t.Fatalf("audit: %s", audit)
	}
}

func TestEditorWithdrawsOnlyAnOwnUnusedInvitation(t *testing.T) {
	h := newHarness(t, nil)
	adminUser := h.account("root@example.org", "Roza", auth.RoleAdmin)
	editorUser := h.account("editor@example.org", "Elena", auth.RoleEditor)
	otherEditor := h.account("editor2@example.org", "Egor", auth.RoleEditor)
	oldMember := h.account("old@example.org", "Olga", auth.RoleMember)
	admin, editor, second := h.client(), h.client(), h.client()
	admin.signIn("root@example.org")
	editor.signIn("editor@example.org")
	second.signIn("editor2@example.org")

	unused, _ := editor.invite("unused@example.org", "Ulyana", "member")
	used, _ := editor.invite("used@example.org", "Usman", "guest")
	byAdmin, _ := admin.invite("byadmin@example.org", "Boris", "member")
	bySecond, _ := second.invite("bysecond@example.org", "Bella", "guest")
	h.client().signIn("used@example.org")

	for name, id := range map[string]int64{
		"the administrator": adminUser.ID, "another editor": otherEditor.ID, "an old member": oldMember.ID,
		"an invitation of the administrator": byAdmin.ID, "an invitation of another editor": bySecond.ID,
		"an own invitation that was used": used.ID,
	} {
		res, body := editor.do("DELETE", userPath(id), nil, nil)
		if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"forbidden"`) {
			t.Errorf("the editor removes %s: %d %s", name, res.StatusCode, body)
		}
		res, body = editor.do("POST", userPath(id)+"/invite", nil, nil)
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("the editor invites %s again: %d %s", name, res.StatusCode, body)
		}
	}
	editor.refuse(http.StatusNotFound, "not_found", "DELETE", userPath(999), nil)
	editor.refuse(http.StatusBadRequest, "bad_request", "DELETE", "/api/v1/users/abc", nil)
	if n, err := h.store.CountUsers(context.Background()); err != nil || n != 8 {
		t.Fatalf("users: %d %v", n, err)
	}

	editor.expect(http.StatusOK, "DELETE", userPath(unused.ID), nil)
	if _, err := h.store.UserByID(context.Background(), unused.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the invitation stays: %v", err)
	}
	sent := h.mail.count()
	h.client().expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "unused@example.org"})
	h.idle()
	if h.mail.count() != sent {
		t.Fatal("a withdrawn invitation still gets a code")
	}
	// The administrator removes everybody, with a recent code.
	admin.expect(http.StatusOK, "DELETE", userPath(used.ID), nil)
	admin.expect(http.StatusOK, "DELETE", userPath(bySecond.ID), nil)
	admin.expect(http.StatusOK, "DELETE", userPath(editorUser.ID), nil)
	editor.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	// The invitation of an administrator stays when its sender stays.
	if left := h.userByEmail("byadmin@example.org"); left.InvitedBy != adminUser.ID {
		t.Fatalf("left: %+v", left)
	}
}

func TestEditorCannotWithdrawAfterARoleChange(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	invited, _ := editor.invite("pending@example.org", "Pavel", "member")
	// The administrator makes the invited person an editor.
	admin.expect(http.StatusOK, "PATCH", userPath(invited.ID), map[string]string{"role": "editor"})
	editor.refuse(http.StatusForbidden, "forbidden", "DELETE", userPath(invited.ID), nil)
	editor.refuse(http.StatusForbidden, "forbidden", "POST", userPath(invited.ID)+"/invite", nil)
}

func TestEditorSeesOnlyAddressesOfOwnInvitations(t *testing.T) {
	h := newHarness(t, nil)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	h.account("editor@example.org", "Elena", auth.RoleEditor)
	h.account("old@example.org", "Olga", auth.RoleMember)
	h.account("editor2@example.org", "Egor", auth.RoleEditor)
	admin, editor := h.client(), h.client()
	admin.signIn("root@example.org")
	editor.signIn("editor@example.org")
	editor.invite("mine@example.org", "Mila", "member")
	admin.invite("byadmin@example.org", "Boris", "guest")

	raw := string(editor.expect(http.StatusOK, "GET", "/api/v1/users", nil))
	for _, hidden := range []string{"root@example.org", "old@example.org", "editor2@example.org", "byadmin@example.org"} {
		if strings.Contains(raw, hidden) {
			t.Errorf("the editor sees %s", hidden)
		}
	}
	if strings.Count(raw, "@") != 2 {
		t.Errorf("the editor must see two addresses: %s", raw)
	}
	people := editor.people()
	if len(people) != 6 {
		t.Fatalf("people: %+v", people)
	}
	if self := people["Elena"]; self.Email != "editor@example.org" || !self.Self || self.Mine || self.Role != "editor" {
		t.Errorf("self: %+v", self)
	}
	if mine := people["Mila"]; mine.Email != "mine@example.org" || !mine.Mine || mine.Self || !mine.Pending || mine.Role != "member" {
		t.Errorf("mine: %+v", mine)
	}
	for _, name := range []string{"Roza", "Olga", "Egor", "Boris"} {
		if person := people[name]; person.Email != "" || person.Mine || person.Self || person.Role == "" || person.ID == 0 {
			t.Errorf("%s: %+v", name, person)
		}
	}
	// The answer to an invitation of the editor shows the address, the answer
	// of the second sign-in of the invited person changes nothing about it.
	h.client().signIn("mine@example.org")
	if mine := editor.people()["Mila"]; mine.Email != "mine@example.org" || mine.Pending {
		t.Errorf("mine after the sign-in: %+v", mine)
	}

	all := admin.people()
	for name, email := range map[string]string{
		"Roza": "root@example.org", "Elena": "editor@example.org", "Olga": "old@example.org",
		"Egor": "editor2@example.org", "Mila": "mine@example.org", "Boris": "byadmin@example.org",
	} {
		if all[name].Email != email {
			t.Errorf("the administrator sees %q for %s", all[name].Email, name)
		}
	}
	if !all["Roza"].Self || !all["Boris"].Mine || all["Mila"].Mine {
		t.Errorf("marks of the administrator: %+v", all)
	}
}

func TestNobodyRemovesTheOwnAccount(t *testing.T) {
	h := newHarness(t, nil)
	adminUser := h.account("root@example.org", "Roza", auth.RoleAdmin)
	secondUser := h.account("root2@example.org", "Rita", auth.RoleAdmin)
	editorUser := h.account("editor@example.org", "Elena", auth.RoleEditor)
	for email, id := range map[string]int64{"root@example.org": adminUser.ID, "root2@example.org": secondUser.ID, "editor@example.org": editorUser.ID} {
		c := h.client()
		c.signIn(email)
		c.refuse(http.StatusBadRequest, "own_account", "DELETE", userPath(id), nil)
		c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	}
	if n, err := h.store.CountUsers(context.Background()); err != nil || n != 3 {
		t.Fatalf("users: %d %v", n, err)
	}
}

func TestLastAdministratorStays(t *testing.T) {
	h := newHarness(t, nil)
	adminUser := h.account("root@example.org", "Roza", auth.RoleAdmin)
	h.account("editor@example.org", "Elena", auth.RoleEditor)
	admin := h.client()
	admin.signIn("root@example.org")
	for _, role := range []string{"editor", "member", "guest", "disabled"} {
		admin.refuse(http.StatusConflict, "last_admin", "PATCH", userPath(adminUser.ID), map[string]string{"role": role})
	}
	admin.refuse(http.StatusBadRequest, "own_account", "DELETE", userPath(adminUser.ID), nil)
	if me := decode[accountDTO](t, admin.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.User.RoleName != "admin" {
		t.Fatalf("me: %+v", me.User)
	}

	// With a second administrator the first one can go, but not both.
	second, _ := admin.invite("root2@example.org", "Rita", "admin")
	other := h.client()
	other.signIn("root2@example.org")
	demoted := decode[personDTO](t, other.expect(http.StatusOK, "PATCH", userPath(adminUser.ID), map[string]string{"role": "member"}))
	if demoted.Role != "member" {
		t.Fatalf("demoted: %+v", demoted)
	}
	admin.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	other.refuse(http.StatusConflict, "last_admin", "PATCH", userPath(second.ID), map[string]string{"role": "editor"})
	other.refuse(http.StatusBadRequest, "own_account", "DELETE", userPath(second.ID), nil)
	other.expect(http.StatusOK, "DELETE", userPath(adminUser.ID), nil)
	if stored := h.userByEmail("root2@example.org"); stored.Role != auth.RoleAdmin {
		t.Fatalf("the last administrator: %+v", stored)
	}
}

func TestSessionsListAndEnd(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	laptop, phone, tablet, boris := h.client(), h.client(), h.client(), h.client()
	laptop.headers = map[string]string{"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"}
	phone.headers = map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"}
	laptop.signIn("anna@example.org")
	phone.signIn("anna@example.org")
	tablet.signIn("anna@example.org")
	boris.signIn("boris@example.org")

	type listDTO struct {
		Sessions []store.Session `json:"sessions"`
	}
	list := func(c *client) map[string]store.Session {
		t.Helper()
		raw := c.expect(http.StatusOK, "GET", "/api/v1/me/sessions", nil)
		// The list names a session by its ID and never by its secret.
		for _, secret := range []string{c.cookie("heirfold"), "token", "Mozilla"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("the list of sessions holds %q: %s", secret, raw)
			}
		}
		out := map[string]store.Session{}
		for _, session := range decode[listDTO](t, raw).Sessions {
			if session.ID == "" || session.CreatedAt == 0 || session.LastSeenAt == 0 {
				t.Fatalf("session: %+v", session)
			}
			out[session.Label] = session
		}
		return out
	}
	sessions := list(laptop)
	if len(sessions) != 3 || !sessions["Firefox · Linux"].Current || sessions["Safari · iPhone"].Current || sessions["Browser"].Current {
		t.Fatalf("sessions of the laptop: %+v", sessions)
	}
	if fromPhone := list(phone); !fromPhone["Safari · iPhone"].Current || fromPhone["Firefox · Linux"].Current {
		t.Fatalf("sessions of the phone: %+v", fromPhone)
	}
	if own := list(boris); len(own) != 1 {
		t.Fatalf("sessions of another user: %+v", own)
	}

	laptop.refuse(http.StatusBadRequest, "current_session", "DELETE", "/api/v1/me/sessions/"+sessions["Firefox · Linux"].ID, nil)
	laptop.refuse(http.StatusNotFound, "not_found", "DELETE", "/api/v1/me/sessions/unknown", nil)
	// Another user cannot end a session, also with the right ID.
	boris.refuse(http.StatusNotFound, "not_found", "DELETE", "/api/v1/me/sessions/"+sessions["Safari · iPhone"].ID, nil)
	phone.expect(http.StatusOK, "GET", "/api/v1/me", nil)

	laptop.expect(http.StatusOK, "DELETE", "/api/v1/me/sessions/"+sessions["Safari · iPhone"].ID, nil)
	phone.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	tablet.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	if left := list(laptop); len(left) != 2 {
		t.Fatalf("sessions after the end of one: %+v", left)
	}

	laptop.expect(http.StatusOK, "POST", "/api/v1/me/sessions/end-others", nil)
	tablet.expect(http.StatusUnauthorized, "GET", "/api/v1/me", nil)
	laptop.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	boris.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	if left := list(laptop); len(left) != 1 || !left["Firefox · Linux"].Current {
		t.Fatalf("sessions after the end of the others: %+v", left)
	}
	audit := h.auditText()
	if strings.Count(audit, `"session.end"`) != 1 || strings.Count(audit, `"session.end_others"`) != 1 {
		t.Fatalf("audit: %s", audit)
	}
}

func TestDeviceLabel(t *testing.T) {
	for agent, want := range map[string]string{
		"":                   "Browser",
		"Go-http-client/1.1": "Browser",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36":                   "Chrome · Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0":     "Edge · Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15":             "Safari · Mac",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36":             "Chrome · Android",
		"Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/140.0.0.0 Mobile/15E148 Safari/604.1": "Chrome · iPad",
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                            "Firefox · Linux",
	} {
		if got := deviceLabel(agent); got != want {
			t.Errorf("%q: got %q, want %q", agent, got, want)
		}
	}
	// The label is short, whatever the header holds.
	if got := deviceLabel(strings.Repeat("Firefox/<script>", 500)); got != "Firefox" {
		t.Errorf("a long header gave %q", got)
	}
}

func TestInviteAgain(t *testing.T) {
	h := newHarness(t, nil)
	h.account("editor@example.org", "Elena", auth.RoleEditor)
	h.account("root@example.org", "Roza", auth.RoleAdmin)
	editor, admin := h.client(), h.client()
	editor.signIn("editor@example.org")
	admin.signIn("root@example.org")
	invited, _ := editor.invite("nina@example.org", "Nina", "member")
	path := userPath(invited.ID) + "/invite"

	editor.expect(http.StatusOK, "POST", path, nil)
	again := h.mail.take(t, "invite", "nina@example.org")
	if again.UserID != invited.ID || !strings.Contains(again.Text, "Elena invites you") || !strings.Contains(again.Text, testSite) {
		t.Fatalf("letter: %+v", again)
	}
	// An administrator can send the invitation of an editor again.
	admin.expect(http.StatusOK, "POST", path, nil)
	if letter := h.mail.take(t, "invite", "nina@example.org"); !strings.Contains(letter.Text, "Roza invites you") {
		t.Fatalf("letter: %+v", letter)
	}
	// One person gets few letters, whoever asks.
	editor.refuse(http.StatusTooManyRequests, "rate_limited", "POST", path, nil)
	admin.refuse(http.StatusTooManyRequests, "rate_limited", "POST", path, nil)
	h.idle()
	if left := h.mail.drain(); len(left) != 0 {
		t.Fatalf("letters after the limit: %+v", left)
	}

	h.client().signIn("nina@example.org")
	admin.refuse(http.StatusConflict, "not_pending", "POST", path, nil)
	editor.refuse(http.StatusForbidden, "forbidden", "POST", path, nil)
	admin.refuse(http.StatusNotFound, "not_found", "POST", userPath(999)+"/invite", nil)
	if audit := h.auditText(); strings.Count(audit, `"user.invite_again"`) != 2 {
		t.Fatalf("audit: %s", audit)
	}
}

func TestLettersFollowTheLanguage(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org", "lang": "ru"})
	letter := h.mail.take(t, "code", "anna@example.org")
	if letter.Subject != "Код для входа в семейное древо" || !strings.Contains(letter.Text, "10 минут") ||
		!strings.Contains(letter.Text, letter.Code[:4]+" "+letter.Code[4:]) || !strings.Contains(letter.Text, testSite) {
		t.Fatalf("letter: %+v", letter)
	}
	// The language of a request serves one letter. The server does not keep
	// it, and a language that Heirfold does not have gives the stored one,
	// which is English for an account without a language.
	if stored := h.userByEmail("anna@example.org"); stored.Lang != "" {
		t.Fatalf("a request for a code stored the language %q", stored.Lang)
	}
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org", "lang": "de"})
	letter = h.mail.take(t, "code", "anna@example.org")
	if letter.Subject != "Your sign-in code for the family tree" {
		t.Fatalf("letter: %+v", letter)
	}
	c.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})

	me := decode[accountDTO](t, c.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"lang": "ru", "displayName": "  Анна Орлова "}))
	if me.User.DisplayName != "Анна Орлова" || me.User.Email != "anna@example.org" || me.AuthAt == 0 {
		t.Fatalf("me: %+v", me.User)
	}
	if stored := h.userByEmail("anna@example.org"); stored.Lang != "ru" {
		t.Fatalf("PATCH /api/v1/me did not store the language: %+v", stored)
	}
	for _, name := range []string{"", "   ", "a\tb", strings.Repeat("я", 81)} {
		c.refuse(http.StatusBadRequest, "bad_name", "PATCH", "/api/v1/me", map[string]string{"displayName": name})
	}
	// A person cannot change the own role or address this way.
	c.refuse(http.StatusBadRequest, "bad_request", "PATCH", "/api/v1/me", map[string]string{"role": "admin"})
	c.refuse(http.StatusBadRequest, "bad_request", "PATCH", "/api/v1/me", map[string]string{"email": "other@example.org"})
	// A language that Heirfold does not have changes nothing.
	c.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"lang": "de"})
	if stored := h.userByEmail("anna@example.org"); stored.DisplayName != "Анна Орлова" || stored.Lang != "ru" || stored.Role != auth.RoleMember {
		t.Fatalf("stored: %+v", stored)
	}
	c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	// The stored language now chooses the letter of a request without one.
	if letter := c.requestCode("anna@example.org"); letter.Subject != "Код для входа в семейное древо" {
		t.Fatalf("letter after the change of language: %+v", letter)
	}
	h.resetLimits()
	c.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"lang": "en"})
	if letter := c.requestCode("anna@example.org"); letter.Subject != "Your sign-in code for the family tree" {
		t.Fatalf("letter after the second change of language: %+v", letter)
	}
}

// A refused change of the own name must not store the language of the same
// request.
func TestRefusedOwnUpdateChangesNothing(t *testing.T) {
	// DOUBT: internal/server/account.go:279-289. handleMeUpdate stores the
	// language (line 280) before it checks the display name (line 287), so a
	// request that gets 400 bad_name still changes the language.
	h := newHarness(t, nil)
	c := h.user("anna@example.org", auth.RoleMember)
	c.refuse(http.StatusBadRequest, "bad_name", "PATCH", "/api/v1/me", map[string]string{"lang": "ru", "displayName": "  "})
	if stored := h.userByEmail("anna@example.org"); stored.Lang != "" {
		t.Fatalf("a refused request stored the language %q", stored.Lang)
	}
}

// A request for a code needs no session, so anybody can send one for any
// address. It must not change the language that the owner of the account set.
func TestAnonymousCodeRequestKeepsTheStoredLanguage(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if _, err := h.store.CreateUser(ctx, "anna@example.org", "Anna", auth.RoleMember, "en", 0); err != nil {
		t.Fatal(err)
	}
	editor := h.user("editor@example.org", auth.RoleEditor)
	editor.expect(http.StatusCreated, "POST", "/api/v1/users",
		map[string]string{"email": "nina@example.org", "displayName": "Нина", "role": "member", "lang": "ru"})
	if letter := h.mail.take(t, "invite", "nina@example.org"); letter.Subject != "Приглашение в семейное древо" {
		t.Fatalf("invitation: %+v", letter)
	}

	stranger := h.client()
	// A letter uses the language of the account, not the language of the page
	// that asked: a browser in another language must not change what the
	// owner reads.
	stranger.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org", "lang": "ru"})
	if letter := h.mail.take(t, "code", "anna@example.org"); letter.Subject != "Your sign-in code for the family tree" {
		t.Fatalf("the language of the account must choose this letter: %+v", letter)
	}
	stranger.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "nina@example.org", "lang": "en"})
	if letter := h.mail.take(t, "code", "nina@example.org"); letter.Subject != "Код для входа в семейное древо" {
		t.Fatalf("the language of the account must choose this letter: %+v", letter)
	}
	h.idle()
	if stored := h.userByEmail("anna@example.org"); stored.Lang != "en" {
		t.Fatalf("a stranger changed the language of an account to %q", stored.Lang)
	}
	if stored := h.userByEmail("nina@example.org"); stored.Lang != "ru" {
		t.Fatalf("a stranger changed the language of an invitation to %q", stored.Lang)
	}
	// The next letters follow the stored language again.
	owner := h.client()
	if letter := owner.requestCode("anna@example.org"); letter.Subject != "Your sign-in code for the family tree" {
		t.Fatalf("a later request without a language: %+v", letter)
	}
	editor.expect(http.StatusOK, "POST", userPath(h.userByEmail("nina@example.org").ID)+"/invite", nil)
	if letter := h.mail.take(t, "invite", "nina@example.org"); letter.Subject != "Приглашение в семейное древо" {
		t.Fatalf("the invitation after the request of the stranger: %+v", letter)
	}
}

// A wrong code and a flood of requests are two events. One client gets one
// audit line of each kind in five minutes, in any order.
func TestFailedAndThrottledAuditLinesHaveOwnBuckets(t *testing.T) {
	flood := func(c *client) {
		for i := 0; i < 6; i++ {
			c.do("POST", "/api/v1/auth/code", map[string]string{"email": "nobody@example.org"}, nil)
		}
	}
	wrong := func(c *client, code string) {
		for i := 0; i < 3; i++ {
			c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": wrongCode(code)})
		}
	}
	for _, first := range []string{"failed", "throttled"} {
		h := newHarness(t, nil)
		h.account("anna@example.org", "Anna", auth.RoleMember)
		c := h.client()
		letter := c.requestCode("anna@example.org")
		if first == "failed" {
			wrong(c, letter.Code)
			flood(c)
		} else {
			flood(c)
			wrong(c, letter.Code)
		}
		audit := h.auditText()
		if failed, throttled := strings.Count(audit, `"login.failed"`), strings.Count(audit, `"login.throttled"`); failed != 1 || throttled != 1 {
			t.Errorf("%s first: %d login.failed and %d login.throttled lines, want 1 and 1: %s", first, failed, throttled, audit)
		}
	}
}

var codeInLog = regexp.MustCompile(`code=(\d{8})\b`)

func TestCodesGoToTheLogWithoutMail(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Mail = nil })
	anna := h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	status := decode[map[string]bool](t, c.expect(http.StatusOK, "GET", "/api/v1/status", nil))
	if !status["codesInLog"] || status["needsSetup"] {
		t.Fatalf("status: %v", status)
	}
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"})
	h.idle()
	log := h.log.String()
	found := codeInLog.FindStringSubmatch(log)
	if found == nil || !strings.Contains(log, fmt.Sprintf("user=%d", anna.ID)) || !strings.Contains(log, "kind=code") {
		t.Fatalf("the log has no code: %s", log)
	}
	if strings.Contains(log, "@") || strings.Contains(log, "example.org") {
		t.Fatalf("the log holds the address: %s", log)
	}
	c.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": found[1]})
	c.expect(http.StatusOK, "GET", "/api/v1/me", nil)
}

// An account from the time of passwords has a name that is no address. Its
// code goes to the server log, also when the server has a mail transport.
func TestAccountWithoutAddressGetsTheCodeInTheLog(t *testing.T) {
	h := newHarness(t, nil)
	root := h.account("root", "Roza", auth.RoleAdmin)
	c := h.client()
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": " Root "})
	h.idle()
	if h.mail.count() != 0 {
		t.Fatalf("the mail transport got a letter for a name that is no address: %+v", h.mail.drain())
	}
	log := h.log.String()
	found := codeInLog.FindStringSubmatch(log)
	if found == nil || !strings.Contains(log, fmt.Sprintf("user=%d", root.ID)) {
		t.Fatalf("the log has no code: %s", log)
	}
	c.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": found[1]})
	if me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.User.ID != root.ID || !me.ManageUsers {
		t.Fatalf("me: %+v", me)
	}
}

func TestMailFailureIsLoggedWithoutTheAddress(t *testing.T) {
	h := newHarness(t, nil)
	anna := h.account("anna@example.org", "Anna", auth.RoleMember)
	h.mail.setFail(func(m mail.Message) error {
		return fmt.Errorf("550 5.1.1 <%s>: mailbox of %s unavailable", m.To, m.To)
	})
	c := h.client()
	// The answer does not tell that the letter failed.
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"})
	h.idle()
	log := h.log.String()
	for _, want := range []string{"letter not sent", "transport=fake", fmt.Sprintf("user=%d", anna.ID), "mailbox of <address> unavailable"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q: %s", want, log)
		}
	}
	if strings.Contains(log, "anna@example.org") || strings.Contains(log, "code=") {
		t.Fatalf("the log holds the address or the code: %s", log)
	}
}

func TestMailPanicDoesNotStopTheServer(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	h.mail.setFail(func(mail.Message) error { panic("mail transport is broken: anna@example.org") })
	c := h.client()
	c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"})
	h.idle()
	if log := h.log.String(); !strings.Contains(log, "panic in background job") || strings.Contains(log, "anna@example.org") {
		t.Fatalf("log: %s", log)
	}
	h.mail.setFail(nil)
	h.mail.drain()
	c.signIn("anna@example.org")
}

// TestLogsHoldNoAddress runs the flows that handle addresses and then reads
// the audit log and the server log.
func TestLogsHoldNoAddress(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.client()
	admin.expect(http.StatusCreated, "POST", "/api/v1/setup",
		map[string]string{"token": h.app.SetupToken(), "email": "r.admin@example.org", "displayName": "Roza"})
	h.account("e.editor@example.org", "Elena", auth.RoleEditor)
	editor := h.client()
	editor.signIn("e.editor@example.org")

	// Invitations: accepted, refused, sent again, withdrawn.
	nina, _ := editor.invite("n.member@example.org", "Nina", "member")
	gleb, _ := editor.invite("g.guest@example.org", "Gleb", "guest")
	editor.expect(http.StatusConflict, "POST", "/api/v1/users", map[string]string{"email": "n.member@example.org", "displayName": "Nina", "role": "guest"})
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/users", map[string]string{"email": "no.name@example.org", "role": "guest"})
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/users", map[string]string{"email": "broken@@example.org", "role": "guest"})
	editor.expect(http.StatusForbidden, "POST", "/api/v1/users", map[string]string{"email": "second.admin@example.org", "role": "admin"})
	editor.expect(http.StatusOK, "POST", userPath(gleb.ID)+"/invite", nil)
	h.mail.take(t, "invite", "g.guest@example.org")
	editor.expect(http.StatusOK, "DELETE", userPath(gleb.ID), nil)

	// Sign-in: unknown address, wrong code, right code, too many requests.
	visitor := h.client()
	visitor.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "unknown.person@example.org"})
	letter := visitor.requestCode("n.member@example.org")
	visitor.expect(http.StatusUnauthorized, "POST", "/api/v1/auth/verify", map[string]string{"code": wrongCode(letter.Code)})
	visitor.expect(http.StatusOK, "POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code})
	// One client gets one audit row in five minutes. This test wants the row
	// of the wrong code and the row of the flood.
	h.resetLimits()
	for i := 0; i < 5; i++ {
		h.client().do("POST", "/api/v1/auth/code", map[string]string{"email": "flood.target@example.org"}, nil)
	}

	// A letter that the mail server refuses with the address in its answer.
	h.mail.setFail(func(m mail.Message) error { return fmt.Errorf("550 no mailbox %s here", m.To) })
	admin.expect(http.StatusCreated, "POST", "/api/v1/users", map[string]string{"email": "b.bounce@example.org", "displayName": "Boris", "role": "member"})
	h.idle()
	h.mail.setFail(nil)
	h.mail.drain()

	// Changes of an account, sessions, removal, sign-out.
	admin.expect(http.StatusOK, "PATCH", userPath(nina.ID), map[string]string{"email": "n.changed@example.com"})
	admin.expect(http.StatusConflict, "PATCH", userPath(nina.ID), map[string]string{"email": "e.editor@example.org"})
	admin.expect(http.StatusOK, "PATCH", userPath(nina.ID), map[string]string{"role": "guest", "displayName": "Nina Guest"})
	visitor.signIn("n.changed@example.com")
	visitor.expect(http.StatusOK, "POST", "/api/v1/me/sessions/end-others", nil)
	visitor.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"displayName": "Nina"})
	admin.expect(http.StatusOK, "DELETE", userPath(nina.ID), nil)
	editor.expect(http.StatusOK, "GET", "/api/v1/users", nil)
	editor.expect(http.StatusOK, "POST", "/api/v1/auth/logout", nil)
	h.idle()

	audit := string(admin.expect(http.StatusOK, "GET", "/api/v1/audit", nil))
	for _, action := range []string{
		"setup.complete", "login.success", "login.failed", "login.throttled", "user.invite", "user.invite_again",
		"user.email", "user.role", "user.delete", "session.end_others", "logout",
	} {
		if !strings.Contains(audit, `"`+action+`"`) {
			t.Errorf("the audit log lacks %s", action)
		}
	}
	log := h.log.String()
	for _, want := range []string{"letter not sent", "<address>", `route="POST /api/v1/auth/code"`, `route="PATCH /api/v1/users/{id}"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the server log lacks %q", want)
		}
	}
	for name, text := range map[string]string{"audit log": audit, "server log": log} {
		for _, part := range []string{
			"@", "example.org", "example.com",
			"r.admin", "e.editor", "n.member", "n.changed", "g.guest", "b.bounce", "unknown.person", "flood.target", "second.admin", "broken", "no.name",
		} {
			if strings.Contains(text, part) {
				t.Errorf("the %s holds %q:\n%s", name, part, text)
			}
		}
	}
	// The logs also keep the codes and the tokens to themselves.
	for name, text := range map[string]string{"audit log": audit, "server log": log} {
		for _, secret := range []string{letter.Code, admin.cookie("heirfold"), h.app.SetupToken()} {
			if strings.Contains(text, secret) {
				t.Errorf("the %s holds a secret", name)
			}
		}
	}
}

// trustProxy lets a test choose the client address with X-Forwarded-For.
func trustProxy(c *Config) {
	c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
}

// waitingCodes returns the number of sign-in codes in the database.
func (h *harness) waitingCodes() int {
	h.t.Helper()
	h.idle()
	db, err := sql.Open("sqlite", "file:"+h.dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM login_code`).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func TestVerifySetsTheDeviceCookie(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	letter := c.requestCode("anna@example.org")
	res, body := c.do("POST", "/api/v1/auth/verify", map[string]string{"code": letter.Code}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("verify: %d %s", res.StatusCode, body)
	}
	device := cookieNamed(res, "heirfold-device")
	if device == nil || len(device.Value) < 40 || !device.HttpOnly || device.SameSite != http.SameSiteStrictMode || device.Path != "/" ||
		device.MaxAge != 400*24*60*60 {
		t.Fatalf("device cookie: %+v", device)
	}
	// The value is no address and no part of one.
	if strings.Contains(device.Value, "anna") || strings.Contains(device.Value, "example") {
		t.Fatalf("the device cookie shows the address: %q", device.Value)
	}
	// The first administrator gets one at the setup too.
	setup := h.client()
	res, _ = setup.do("POST", "/api/v1/setup", map[string]string{"token": h.app.SetupToken(), "email": "root@example.org", "displayName": "Roza"}, nil)
	if res.StatusCode != http.StatusForbidden {
		// The harness has an account, so the setup is over; a new harness checks it.
		t.Fatalf("setup with an account: %d", res.StatusCode)
	}
	fresh := newHarness(t, nil)
	res, _ = fresh.client().do("POST", "/api/v1/setup", map[string]string{"token": fresh.app.SetupToken(), "email": "root@example.org", "displayName": "Roza"}, nil)
	if res.StatusCode != http.StatusCreated || cookieNamed(res, "heirfold-device") == nil {
		t.Fatalf("setup: %d, cookies %+v", res.StatusCode, res.Cookies())
	}
	// A wrong code sets no device cookie.
	other := h.client()
	wrong := other.requestCode("anna@example.org")
	res, _ = other.do("POST", "/api/v1/auth/verify", map[string]string{"code": wrongCode(wrong.Code)}, nil)
	if cookieNamed(res, "heirfold-device") != nil {
		t.Fatal("a wrong code set the device cookie")
	}
}

// Strangers can use up the allowance of an address. A browser that signed in
// to the account before still gets a code.
func TestKnownBrowserGetsACodeAfterAFlood(t *testing.T) {
	h := newHarness(t, trustProxy)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	h.account("boris@example.org", "Boris", auth.RoleMember)
	// The owner signed in on this laptop some time ago.
	laptop := h.client()
	laptop.headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	laptop.signIn("anna@example.org")
	if laptop.cookie("heirfold-device") == "" {
		t.Fatal("no device cookie")
	}
	laptop.expect(http.StatusOK, "POST", "/api/v1/auth/logout", nil)
	h.mail.drain()

	// Strangers from many addresses use up the allowance of the address.
	request := map[string]string{"email": "anna@example.org"}
	for i := 0; i < 40; i++ {
		stranger := h.client()
		stranger.headers = map[string]string{"X-Forwarded-For": "203.0.113." + itoa(int64(i+1))}
		stranger.do("POST", "/api/v1/auth/code", request, nil)
	}
	h.idle()
	if flood := len(h.mail.drain()); flood > int(ruleCodeAccount.capacity)+1 {
		t.Fatalf("the strangers caused %d letters", flood)
	}
	newcomer := h.client()
	newcomer.headers = map[string]string{"X-Forwarded-For": "192.0.2.50"}
	newcomer.refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/auth/code", request)
	// A forged device cookie and the cookie of another account do not help.
	newcomer.refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/auth/code", request)
	res, _ := newcomer.do("POST", "/api/v1/auth/code", request, map[string]string{"Cookie": "heirfold-device=" + strings.Repeat("A", 43)})
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a forged device cookie: %d", res.StatusCode)
	}
	borisBrowser := h.client()
	borisBrowser.headers = map[string]string{"X-Forwarded-For": "192.0.2.60"}
	borisBrowser.signIn("boris@example.org")
	borisBrowser.refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/auth/code", request)

	// The laptop of the owner signs in.
	laptop.signIn("anna@example.org")
	laptop.expect(http.StatusOK, "GET", "/api/v1/me", nil)
	// The trusted allowance has its own limit.
	h.mail.drain()
	sent := 0
	for i := 0; i < int(ruleCodeTrusted.capacity)+3; i++ {
		if res, _ := laptop.do("POST", "/api/v1/auth/code", request, nil); res.StatusCode == http.StatusOK {
			sent++
		}
	}
	if sent >= int(ruleCodeTrusted.capacity)+3 || sent < int(ruleCodeTrusted.capacity)-2 {
		t.Fatalf("a known browser got %d codes in a row", sent)
	}
}

// A browser with a session of the account counts as known.
func TestSessionOfTheAccountCountsAsAKnownBrowser(t *testing.T) {
	h := newHarness(t, trustProxy)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	c := h.client()
	c.headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	cookies := c.signIn("anna@example.org")
	var session string
	for _, cookie := range cookies {
		if cookie.Name == "heirfold" {
			session = cookie.Value
		}
	}
	// A client without the device cookie, with the session only.
	bare := &client{h: h, http: &http.Client{}, headers: map[string]string{"X-Forwarded-For": "198.51.100.7", "Cookie": "heirfold=" + session}}
	request := map[string]string{"email": "anna@example.org"}
	// The pair of client and address allows 3 codes; the session gives more.
	for i := 0; i < int(ruleCodeUser.capacity)+2; i++ {
		bare.expect(http.StatusOK, "POST", "/api/v1/auth/code", request)
	}
	h.idle()
	// A client of the same address without the session shares the allowance
	// of that address and the account, of which the first sign-in took one.
	stranger := h.client()
	stranger.headers = map[string]string{"X-Forwarded-For": "198.51.100.7"}
	for i := 0; i < int(ruleCodeUser.capacity)-1; i++ {
		stranger.expect(http.StatusOK, "POST", "/api/v1/auth/code", request)
	}
	stranger.refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/auth/code", request)
}

// A code that cannot go out is not stored, so nobody can guess it.
func TestNoCodeIsStoredWhenTheMailAllowanceIsEmpty(t *testing.T) {
	h := newHarness(t, nil)
	h.account("anna@example.org", "Anna", auth.RoleMember)
	known := h.client()
	known.signIn("anna@example.org")
	known.expect(http.StatusOK, "POST", "/api/v1/auth/logout", nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	if h.waitingCodes() != 0 {
		t.Fatal("codes wait after the sign-in")
	}
	// The allowance for letters to strangers is empty.
	for h.app.anon.allow("mail-strangers", ruleMailGlobal) {
	}
	sent := h.mail.count()
	stranger := h.client()
	stranger.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "anna@example.org"})
	h.idle()
	if h.mail.count() != sent || h.waitingCodes() != 0 {
		t.Fatalf("letters %d -> %d, codes %d", sent, h.mail.count(), h.waitingCodes())
	}
	stranger.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": "12345678"})
	if log := h.log.String(); !strings.Contains(log, "too many letters") || strings.Contains(log, "anna@example.org") {
		t.Fatalf("log: %s", log)
	}
	// Letters to known browsers and invitations have their own allowance.
	known.signIn("anna@example.org")
	editor.invite("nina@example.org", "Nina", "member")
}

// Many clients that type wrong codes write a limited number of audit lines.
func TestAnonymousAuditLinesHaveAGlobalLimit(t *testing.T) {
	h := newHarness(t, trustProxy)
	for i := 0; i < 40; i++ {
		c := h.client()
		c.headers = map[string]string{"X-Forwarded-For": "203.0.113." + itoa(int64(i+1))}
		// The request gives the browser the cookie of an attempt.
		c.expect(http.StatusOK, "POST", "/api/v1/auth/code", map[string]string{"email": "nobody" + itoa(int64(i)) + "@example.org"})
		for j := 0; j < 2; j++ {
			c.refuse(http.StatusUnauthorized, "invalid_code", "POST", "/api/v1/auth/verify", map[string]string{"code": "12345678"})
		}
	}
	if n := strings.Count(h.auditText(), `"login.failed"`); n != int(ruleAuditAnon.capacity) {
		t.Fatalf("login.failed lines: %d, want %v", n, ruleAuditAnon.capacity)
	}
	// The global limit does not touch the lines of signed-in people.
	h.user("anna@example.org", auth.RoleMember)
	if !strings.Contains(h.auditText(), `"login.success"`) {
		t.Fatal("the sign-in has no audit line")
	}
}

func TestSafeInviter(t *testing.T) {
	for name, want := range map[string]string{
		"Elena":                   "Elena",
		"Елена Орлова":            "Елена Орлова",
		"Dr. Elena":               "Dr. Elena",
		"Elena.":                  "Elena.",
		"Anna-Maria O'Brien":      "Anna-Maria O'Brien",
		"evil.example":            "",
		"Elena (evil.example)":    "",
		"https://evil.example":    "",
		"Visit evil:8080":         "",
		"anna@example.org":        "",
		"<b>Elena</b>":            "",
		"path/to":                 "",
		`back\slash`:              "",
		"www evil":                "",
		"WWW Evil":                "",
		"Elena. see":              "",
		"tree.example.org Elena ": "",
	} {
		if got := safeInviter(name); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}

// An editor cannot put a link into the letter of the server through the own
// display name.
func TestInvitationDropsAnInviterNameWithALink(t *testing.T) {
	h := newHarness(t, nil)
	h.account("editor@example.org", "Visit evil.example now", auth.RoleEditor)
	editor := h.client()
	editor.signIn("editor@example.org")
	_, letter := editor.invite("nina@example.org", "Nina", "member")
	if strings.Contains(letter.Text, "evil") || !strings.Contains(letter.Text, "You have an invitation to the family tree.") || !strings.Contains(letter.Text, testSite) {
		t.Fatalf("letter: %s", letter.Text)
	}
}
