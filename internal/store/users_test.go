package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
)

func hashOf(s string) []byte { return auth.HashToken(s) }

const (
	testYear     = 365 * 24 * time.Hour
	testCodeLife = 10 * time.Minute
	testTries    = 5
)

var testBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// setClock makes the store read a time that the test moves.
func setClock(st *Store) func(offset time.Duration) {
	st.now = func() time.Time { return testBase }
	return func(offset time.Duration) {
		st.now = func() time.Time { return testBase.Add(offset) }
	}
}

func mustUser(t *testing.T, st *Store, email string, role auth.Role) *User {
	t.Helper()
	user, err := st.CreateUser(context.Background(), email, "Name of "+email, role, "en", 0)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

// addCode stores a sign-in code for the browser with the given secret.
func addCode(t *testing.T, st *Store, attempt string, userID int64, code string) {
	t.Helper()
	user, err := st.UserByID(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateLoginCode(context.Background(), hashOf(attempt), userID, user.Email, auth.HashCode(attempt, code), testCodeLife); err != nil {
		t.Fatal(err)
	}
}

func useCode(st *Store, attempt, code string) (*User, error) {
	return st.UseLoginCode(context.Background(), hashOf(attempt), auth.HashCode(attempt, code), testTries)
}

func countRows(t *testing.T, st *Store, table string) int {
	t.Helper()
	var n int
	// The table names are constants of the tests.
	if err := st.r.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateUser(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	setClock(st)
	admin, err := st.CreateUser(ctx, "root@example.org", "Root", auth.RoleAdmin, "ru", 0)
	if err != nil {
		t.Fatal(err)
	}
	if admin.Email != "root@example.org" || admin.DisplayName != "Root" || admin.Role != auth.RoleAdmin || admin.RoleName != "admin" ||
		admin.Lang != "ru" || admin.InvitedBy != 0 || admin.CreatedAt != testBase.Unix() || admin.LastLoginAt != nil {
		t.Fatalf("admin: %+v", admin)
	}
	anna, err := st.CreateUser(ctx, "anna@example.org", "Anna", auth.RoleMember, "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if anna.InvitedBy != admin.ID || anna.ID == admin.ID {
		t.Fatalf("anna: %+v", anna)
	}
	// The address is the name of the account, with no difference of case.
	for _, email := range []string{"anna@example.org", "ANNA@example.org"} {
		if _, err := st.CreateUser(ctx, email, "Other", auth.RoleGuest, "", 0); !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("%s: want ErrEmailTaken, got %v", email, err)
		}
	}
	byEmail, err := st.UserByEmail(ctx, "anna@example.org")
	if err != nil || byEmail.ID != anna.ID || byEmail.InvitedBy != admin.ID {
		t.Fatalf("by email: %+v %v", byEmail, err)
	}
	byID, err := st.UserByID(ctx, admin.ID)
	if err != nil || byID.Email != "root@example.org" {
		t.Fatalf("by id: %+v %v", byID, err)
	}
	if _, err := st.UserByEmail(ctx, "nobody@example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := st.UserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	users, err := st.ListUsers(ctx)
	if err != nil || len(users) != 2 || users[0].ID != admin.ID || users[1].ID != anna.ID {
		t.Fatalf("list: %+v %v", users, err)
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 2 {
		t.Fatalf("count: %d %v", n, err)
	}
}

func TestLoginCodeWorksOnce(t *testing.T) {
	st := open(t)
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "browser", anna.ID, "12345678")
	move(time.Minute)
	user, err := useCode(st, "browser", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != anna.ID || user.LastLoginAt == nil || *user.LastLoginAt != testBase.Add(time.Minute).Unix() {
		t.Fatalf("user: %+v", user)
	}
	if _, err := useCode(st, "browser", "12345678"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("second use: want ErrCodeInvalid, got %v", err)
	}
	if n := countRows(t, st, "login_code"); n != 0 {
		t.Fatalf("codes left: %d", n)
	}
	stored, err := st.UserByID(context.Background(), anna.ID)
	if err != nil || stored.LastLoginAt == nil {
		t.Fatalf("the first sign-in was not stored: %+v %v", stored, err)
	}
}

func TestLoginCodeIsNotStoredInTheClear(t *testing.T) {
	st := open(t)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "browser-secret", anna.ID, "12345678")
	var attempt, code []byte
	if err := st.r.QueryRowContext(context.Background(), `SELECT attempt_hash, code_hash FROM login_code`).Scan(&attempt, &code); err != nil {
		t.Fatal(err)
	}
	for _, value := range [][]byte{attempt, code} {
		if len(value) != 32 || string(value) == "12345678" || string(value) == "browser-secret" {
			t.Fatalf("stored value %q", value)
		}
	}
}

func TestWrongCodeFiveTimesKillsIt(t *testing.T) {
	st := open(t)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "browser", anna.ID, "12345678")
	for i := 1; i <= testTries; i++ {
		if _, err := useCode(st, "browser", "00000000"); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("try %d: want ErrCodeInvalid, got %v", i, err)
		}
	}
	if n := countRows(t, st, "login_code"); n != 0 {
		t.Fatalf("the code is still stored after %d wrong tries", testTries)
	}
	if _, err := useCode(st, "browser", "12345678"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("the right code after %d wrong tries: %v", testTries, err)
	}
	stored, err := st.UserByID(context.Background(), anna.ID)
	if err != nil || stored.LastLoginAt != nil {
		t.Fatalf("a dead code signed the user in: %+v %v", stored, err)
	}
}

func TestRightCodeWorksOnTheLastTry(t *testing.T) {
	st := open(t)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "browser", anna.ID, "12345678")
	for i := 1; i < testTries; i++ {
		if _, err := useCode(st, "browser", "00000000"); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("try %d: want ErrCodeInvalid, got %v", i, err)
		}
	}
	if _, err := useCode(st, "browser", "12345678"); err != nil {
		t.Fatalf("try %d with the right code: %v", testTries, err)
	}
}

func TestCodeBelongsToOneBrowser(t *testing.T) {
	st := open(t)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "browser", anna.ID, "12345678")
	// Another browser knows the code but not the secret of the first browser.
	for i := 0; i < 2*testTries; i++ {
		if _, err := useCode(st, "other-browser", "12345678"); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("want ErrCodeInvalid, got %v", err)
		}
	}
	// Those tries did not count against the code of the first browser.
	if _, err := useCode(st, "browser", "12345678"); err != nil {
		t.Fatalf("the owner of the code: %v", err)
	}
}

func TestLoginCodeExpires(t *testing.T) {
	st := open(t)
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "late", anna.ID, "12345678")
	move(testCodeLife)
	if _, err := useCode(st, "late", "12345678"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("at the end of the lifetime: want ErrCodeInvalid, got %v", err)
	}
	move(0)
	addCode(t, st, "early", anna.ID, "87654321")
	move(testCodeLife - time.Second)
	if _, err := useCode(st, "early", "87654321"); err != nil {
		t.Fatalf("one second before the end: %v", err)
	}
}

func TestOnlyNewestFiveCodesStay(t *testing.T) {
	st := open(t)
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	boris := mustUser(t, st, "boris@example.org", auth.RoleMember)
	addCode(t, st, "boris", boris.ID, "55555555")
	attempts := []string{"first", "second", "third", "fourth", "fifth", "sixth"}
	for i, attempt := range attempts {
		move(time.Duration(i) * time.Second)
		addCode(t, st, attempt, anna.ID, "1111111"+string(rune('1'+i)))
	}
	var left int
	if err := st.r.QueryRowContext(context.Background(), `SELECT count(*) FROM login_code WHERE user_id = ?`, anna.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != maxWaitingCodes || maxWaitingCodes != 5 {
		t.Fatalf("waiting codes: %d, limit %d", left, maxWaitingCodes)
	}
	if _, err := useCode(st, "first", "11111111"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("the oldest code: want ErrCodeInvalid, got %v", err)
	}
	// The limit is per user.
	if _, err := useCode(st, "boris", "55555555"); err != nil {
		t.Fatalf("the code of another user: %v", err)
	}
	if _, err := useCode(st, "second", "11111112"); err != nil {
		t.Fatalf("the fifth newest code: %v", err)
	}
}

// The limit is larger than the number of codes that one client can ask for
// in a row (3, ruleCodeUser of the server), so a stranger cannot push out the
// code that the owner waits for.
func TestThreeNewCodesKeepTheWaitingCode(t *testing.T) {
	st := open(t)
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	addCode(t, st, "owner", anna.ID, "12345678")
	for i, attempt := range []string{"stranger-1", "stranger-2", "stranger-3"} {
		move(time.Duration(i+1) * time.Second)
		addCode(t, st, attempt, anna.ID, "00000000")
	}
	if _, err := useCode(st, "owner", "12345678"); err != nil {
		t.Fatalf("the code of the owner: %v", err)
	}
}

// created_at has a resolution of one second. When many codes of one user come
// in the same second, the code that was stored last must stay.
func TestNewestCodeStaysWithinOneSecond(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	// The hashes are plain bytes here. The test runs with both orders of the
	// hashes, because the order breaks a tie on created_at.
	for name, attempts := range map[string][]byte{
		"newest sorts last":  {1, 2, 3, 4, 5, 6, 7, 8},
		"newest sorts first": {18, 17, 16, 15, 14, 13, 12, 11},
	} {
		for i, attempt := range attempts {
			if err := st.CreateLoginCode(ctx, []byte{attempt}, anna.ID, anna.Email, auth.HashCode("a", "12345678"), testCodeLife); err != nil {
				t.Fatal(err)
			}
			var newest int
			if err := st.r.QueryRowContext(ctx, `SELECT count(*) FROM login_code WHERE attempt_hash = ?`, []byte{attempt}).Scan(&newest); err != nil {
				t.Fatal(err)
			}
			if newest != 1 {
				t.Fatalf("%s: code %d of the same second did not stay", name, i+1)
			}
			if n := countRows(t, st, "login_code"); n > maxWaitingCodes {
				t.Fatalf("%s: %d codes wait", name, n)
			}
		}
		last := attempts[len(attempts)-1]
		if _, err := st.UseLoginCode(ctx, []byte{last}, auth.HashCode("a", "12345678"), testTries); err != nil {
			t.Fatalf("%s: the newest code: %v", name, err)
		}
	}
}

// A code is stored only for the address that the account has now, and only
// for an account that can sign in. The letter of a request that started
// before a change must not open the account for the old address.
func TestCodeIsNotStoredForAnOldAddressOrADisabledAccount(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	mustUser(t, st, "root@example.org", auth.RoleAdmin)
	store := func(attempt string, id int64, email string) error {
		return st.CreateLoginCode(ctx, hashOf(attempt), id, email, auth.HashCode(attempt, "12345678"), testCodeLife)
	}
	for name, err := range map[string]error{
		"another address":         store("a", anna.ID, "other@example.org"),
		"the address in capitals": store("b", anna.ID, "ANNA@example.org"),
		"no address":              store("c", anna.ID, ""),
		"an unknown account":      store("d", 999, "anna@example.org"),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: want ErrNotFound, got %v", name, err)
		}
	}
	if n := countRows(t, st, "login_code"); n != 0 {
		t.Fatalf("refused codes are stored: %d", n)
	}
	// The address changes after the request.
	if _, err := st.SetUserEmail(ctx, anna.ID, "anna@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := store("e", anna.ID, "anna@example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old address: want ErrNotFound, got %v", err)
	}
	if err := store("f", anna.ID, "anna@example.com"); err != nil {
		t.Fatalf("the new address: %v", err)
	}
	// The account loses its access after the request.
	if _, err := st.SetUserRole(ctx, anna.ID, auth.RoleDisabled); err != nil {
		t.Fatal(err)
	}
	if err := store("g", anna.ID, "anna@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a disabled account: want ErrNotFound, got %v", err)
	}
	for _, attempt := range []string{"a", "b", "c", "d", "e", "g"} {
		if _, err := useCode(st, attempt, "12345678"); !errors.Is(err, ErrCodeInvalid) {
			t.Errorf("the refused code %q works: %v", attempt, err)
		}
	}
}

func TestUsedCodeRemovesOtherCodes(t *testing.T) {
	st := open(t)
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	boris := mustUser(t, st, "boris@example.org", auth.RoleMember)
	addCode(t, st, "phone", anna.ID, "11111111")
	move(time.Second)
	addCode(t, st, "laptop", anna.ID, "22222222")
	addCode(t, st, "boris", boris.ID, "33333333")
	if _, err := useCode(st, "laptop", "22222222"); err != nil {
		t.Fatal(err)
	}
	if _, err := useCode(st, "phone", "11111111"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("the other code of the user: want ErrCodeInvalid, got %v", err)
	}
	if user, err := useCode(st, "boris", "33333333"); err != nil || user.ID != boris.ID {
		t.Fatalf("the code of another user: %+v %v", user, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	move := setClock(st)
	user := mustUser(t, st, "anna@example.org", auth.RoleMember)
	if err := st.CreateSession(ctx, hashOf("t1"), "s1", user.ID, "Firefox · Linux", time.Hour); err != nil {
		t.Fatal(err)
	}
	got, session, extended, err := st.SessionUser(ctx, hashOf("t1"), time.Hour)
	if err != nil || got.ID != user.ID || got.Email != user.Email || got.Role != auth.RoleMember || got.RoleName != "member" {
		t.Fatalf("session user: %+v %v", got, err)
	}
	if extended || session.ID != "s1" || session.Label != "Firefox · Linux" || !session.Current ||
		session.AuthAt != testBase.Unix() || session.CreatedAt != testBase.Unix() || session.ExpiresAt != testBase.Add(time.Hour).Unix() {
		t.Fatalf("session: %+v extended=%v", session, extended)
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("other"), time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// The token itself is not in the database.
	var stored []byte
	if err := st.r.QueryRowContext(ctx, `SELECT token_hash FROM session`).Scan(&stored); err != nil || string(stored) == "t1" {
		t.Fatalf("stored token: %q %v", stored, err)
	}
	move(time.Hour)
	if _, _, _, err := st.SessionUser(ctx, hashOf("t1"), time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session: %v", err)
	}
	if n := countRows(t, st, "session"); n != 0 {
		t.Fatalf("the expired session stays in the database: %d", n)
	}
}

func TestSessionExtendsOnceADay(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	move := setClock(st)
	user := mustUser(t, st, "anna@example.org", auth.RoleMember)
	if err := st.CreateSession(ctx, hashOf("t"), "s1", user.ID, "", testYear); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		at       time.Duration
		extended bool
		end      time.Duration // the end of the session after the call
		seen     time.Duration
	}{
		{0, false, testYear, 0},
		{23 * time.Hour, false, testYear, 0},
		{24 * time.Hour, true, 24*time.Hour + testYear, 24 * time.Hour},
		{25 * time.Hour, false, 24*time.Hour + testYear, 24 * time.Hour},
		{47 * time.Hour, false, 24*time.Hour + testYear, 24 * time.Hour},
		{48 * time.Hour, true, 48*time.Hour + testYear, 48 * time.Hour},
		// A use on the last second of the year gives a whole year again.
		{48*time.Hour + testYear - time.Second, true, 48*time.Hour + 2*testYear - time.Second, 48*time.Hour + testYear - time.Second},
	}
	for _, step := range steps {
		move(step.at)
		_, session, extended, err := st.SessionUser(ctx, hashOf("t"), testYear)
		if err != nil {
			t.Fatalf("%s: %v", step.at, err)
		}
		if extended != step.extended || session.ExpiresAt != testBase.Add(step.end).Unix() || session.LastSeenAt != testBase.Add(step.seen).Unix() {
			t.Errorf("%s: extended=%v end=%s seen=%s", step.at, extended,
				time.Unix(session.ExpiresAt, 0).Sub(testBase), time.Unix(session.LastSeenAt, 0).Sub(testBase))
		}
		// The time of the sign-in code does not move with the session.
		if session.AuthAt != testBase.Unix() {
			t.Errorf("%s: auth_at moved to %d", step.at, session.AuthAt)
		}
		var end int64
		if err := st.r.QueryRowContext(ctx, `SELECT expires_at FROM session`).Scan(&end); err != nil || end != session.ExpiresAt {
			t.Errorf("%s: stored end %d, returned %d (%v)", step.at, end, session.ExpiresAt, err)
		}
	}
	// A session that nobody uses for the whole lifetime ends.
	last := steps[len(steps)-1]
	move(last.end)
	if _, _, _, err := st.SessionUser(ctx, hashOf("t"), testYear); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after a year without use: want ErrNotFound, got %v", err)
	}
}

func TestListSessions(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	boris := mustUser(t, st, "boris@example.org", auth.RoleMember)
	create := func(token, id string, userID int64, label string, lifetime time.Duration) {
		t.Helper()
		if err := st.CreateSession(ctx, hashOf(token), id, userID, label, lifetime); err != nil {
			t.Fatal(err)
		}
	}
	create("old", "s-old", anna.ID, "Safari · iPhone", testYear)
	create("short", "s-short", anna.ID, "Chrome · Windows", time.Hour)
	create("boris", "s-boris", boris.ID, "Firefox · Linux", testYear)
	move(2 * time.Hour)
	create("new", "s-new", anna.ID, "Firefox · Mac", testYear)

	sessions, err := st.ListSessions(ctx, anna.ID, "s-old")
	if err != nil {
		t.Fatal(err)
	}
	// The newest use comes first. The expired session and the session of
	// another user are not in the list.
	if len(sessions) != 2 || sessions[0].ID != "s-new" || sessions[1].ID != "s-old" {
		t.Fatalf("sessions: %+v", sessions)
	}
	if sessions[0].Current || !sessions[1].Current {
		t.Fatalf("current mark: %+v", sessions)
	}
	if sessions[0].Label != "Firefox · Mac" || sessions[0].CreatedAt != testBase.Add(2*time.Hour).Unix() {
		t.Fatalf("session: %+v", sessions[0])
	}
	none, err := st.ListSessions(ctx, 999, "")
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("an unknown user must give an empty list: %+v %v", none, err)
	}
}

func TestDeleteSessions(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	boris := mustUser(t, st, "boris@example.org", auth.RoleMember)
	for token, userID := range map[string]int64{"a1": anna.ID, "a2": anna.ID, "a3": anna.ID, "a4": anna.ID, "b1": boris.ID} {
		if err := st.CreateSession(ctx, hashOf(token), "id-"+token, userID, "", testYear); err != nil {
			t.Fatal(err)
		}
	}
	alive := func(token string) bool {
		_, _, _, err := st.SessionUser(ctx, hashOf(token), testYear)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		return err == nil
	}
	if err := st.DeleteSession(ctx, hashOf("a1")); err != nil {
		t.Fatal(err)
	}
	if alive("a1") || !alive("a2") {
		t.Fatal("DeleteSession ended the wrong session")
	}
	// A user cannot end the session of another user by its ID.
	if err := st.DeleteSessionByID(ctx, anna.ID, "id-b1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := st.DeleteSessionByID(ctx, anna.ID, "id-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := st.DeleteSessionByID(ctx, anna.ID, "id-a2"); err != nil {
		t.Fatal(err)
	}
	if alive("a2") || !alive("b1") {
		t.Fatal("DeleteSessionByID ended the wrong session")
	}
	if err := st.DeleteOtherSessions(ctx, anna.ID, hashOf("a3")); err != nil {
		t.Fatal(err)
	}
	if !alive("a3") || alive("a4") || !alive("b1") {
		t.Fatalf("after DeleteOtherSessions: a3=%v a4=%v b1=%v", alive("a3"), alive("a4"), alive("b1"))
	}
}

func TestRoleChangeEndsSessions(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	admin := mustUser(t, st, "root@example.org", auth.RoleAdmin)
	user := mustUser(t, st, "anna@example.org", auth.RoleEditor)
	if err := st.CreateSession(ctx, hashOf("t"), "s1", user.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, hashOf("root"), "s2", admin.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	addCode(t, st, "browser", user.ID, "12345678")
	changed, err := st.SetUserRole(ctx, user.ID, auth.RoleGuest)
	if err != nil || changed.Role != auth.RoleGuest || changed.RoleName != "guest" {
		t.Fatalf("role change: %+v %v", changed, err)
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("t"), time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old session survived: %v", err)
	}
	// A code that waits must not give a session with the old role in mind.
	if _, err := useCode(st, "browser", "12345678"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("the waiting code survived: %v", err)
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("root"), time.Hour); err != nil {
		t.Fatalf("the session of another user ended: %v", err)
	}
	if _, err := st.SetUserRole(ctx, 999, auth.RoleGuest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRoleChangeKeepsTheLastAdmin(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	admin := mustUser(t, st, "root@example.org", auth.RoleAdmin)
	mustUser(t, st, "anna@example.org", auth.RoleEditor)
	if err := st.CreateSession(ctx, hashOf("root"), "s1", admin.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, role := range []auth.Role{auth.RoleEditor, auth.RoleMember, auth.RoleGuest, auth.RoleDisabled} {
		if _, err := st.SetUserRole(ctx, admin.ID, role); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("%s: want ErrLastAdmin, got %v", role, err)
		}
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("root"), time.Hour); err != nil {
		t.Fatalf("a refused change ended the session: %v", err)
	}
	second := mustUser(t, st, "second@example.org", auth.RoleAdmin)
	// A disabled account is not an administrator, so it does not count.
	if _, err := st.SetUserRole(ctx, second.ID, auth.RoleDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserRole(ctx, admin.ID, auth.RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("want ErrLastAdmin, got %v", err)
	}
	if _, err := st.SetUserRole(ctx, second.ID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserRole(ctx, admin.ID, auth.RoleMember); err != nil {
		t.Fatalf("with a second administrator: %v", err)
	}
}

func TestDeleteUserKeepsTheLastAdmin(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	admin := mustUser(t, st, "root@example.org", auth.RoleAdmin)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	if err := st.DeleteUser(ctx, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("want ErrLastAdmin, got %v", err)
	}
	if _, err := st.UserByID(ctx, admin.ID); err != nil {
		t.Fatalf("the last administrator is gone: %v", err)
	}
	if err := st.CreateSession(ctx, hashOf("t"), "s1", anna.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	addCode(t, st, "browser", anna.ID, "12345678")
	if err := st.DeleteUser(ctx, anna.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserByEmail(ctx, "anna@example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if sessions, codes := countRows(t, st, "session"), countRows(t, st, "login_code"); sessions != 0 || codes != 0 {
		t.Fatalf("the removed user left %d sessions and %d codes", sessions, codes)
	}
	if err := st.DeleteUser(ctx, anna.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second removal: want ErrNotFound, got %v", err)
	}
	second := mustUser(t, st, "second@example.org", auth.RoleAdmin)
	if err := st.DeleteUser(ctx, admin.ID); err != nil {
		t.Fatalf("with a second administrator: %v", err)
	}
	if err := st.DeleteUser(ctx, second.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("want ErrLastAdmin, got %v", err)
	}
}

func TestSetUserEmail(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	mustUser(t, st, "boris@example.org", auth.RoleMember)
	if err := st.CreateSession(ctx, hashOf("t"), "s1", anna.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, taken := range []string{"boris@example.org", "BORIS@example.org"} {
		if _, err := st.SetUserEmail(ctx, anna.ID, taken); !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("%s: want ErrEmailTaken, got %v", taken, err)
		}
	}
	// A refused change leaves the account and its session as they were.
	if user, _, _, err := st.SessionUser(ctx, hashOf("t"), time.Hour); err != nil || user.Email != "anna@example.org" {
		t.Fatalf("after a refused change: %+v %v", user, err)
	}
	addCode(t, st, "browser", anna.ID, "12345678")
	changed, err := st.SetUserEmail(ctx, anna.ID, "anna@example.com")
	if err != nil || changed.Email != "anna@example.com" || changed.ID != anna.ID {
		t.Fatalf("change: %+v %v", changed, err)
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("t"), time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the session survived the change of address: %v", err)
	}
	// The code went to the old address.
	if _, err := useCode(st, "browser", "12345678"); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("the code for the old address survived: %v", err)
	}
	if _, err := st.UserByEmail(ctx, "anna@example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old address still names an account: %v", err)
	}
	if _, err := st.SetUserEmail(ctx, 999, "x@example.org"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSetUserNameAndLang(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	if err := st.CreateSession(ctx, hashOf("t"), "s1", anna.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	changed, err := st.SetUserName(ctx, anna.ID, "Анна Орлова")
	if err != nil || changed.DisplayName != "Анна Орлова" || changed.Email != anna.Email {
		t.Fatalf("name: %+v %v", changed, err)
	}
	if _, err := st.SetUserName(ctx, 999, "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := st.SetUserLang(ctx, anna.ID, "ru"); err != nil {
		t.Fatal(err)
	}
	// The name and the language change nothing about access.
	user, _, _, err := st.SessionUser(ctx, hashOf("t"), time.Hour)
	if err != nil || user.Lang != "ru" || user.DisplayName != "Анна Орлова" {
		t.Fatalf("session user: %+v %v", user, err)
	}
}

func TestDeleteExpiredRemovesSessionsAndCodes(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	move := setClock(st)
	anna := mustUser(t, st, "anna@example.org", auth.RoleMember)
	if err := st.CreateSession(ctx, hashOf("short"), "s1", anna.ID, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, hashOf("long"), "s2", anna.ID, "", testYear); err != nil {
		t.Fatal(err)
	}
	addCode(t, st, "old", anna.ID, "11111111")
	move(time.Hour)
	addCode(t, st, "new", anna.ID, "22222222")
	if err := st.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if sessions, codes := countRows(t, st, "session"), countRows(t, st, "login_code"); sessions != 1 || codes != 1 {
		t.Fatalf("left: %d sessions, %d codes", sessions, codes)
	}
	if _, _, _, err := st.SessionUser(ctx, hashOf("long"), testYear); err != nil {
		t.Fatalf("the long session: %v", err)
	}
	if _, err := useCode(st, "new", "22222222"); err != nil {
		t.Fatalf("the new code: %v", err)
	}
}
