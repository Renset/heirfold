package auth

import (
	"strings"
	"testing"
)

func TestRoleScopes(t *testing.T) {
	if scope := RoleGuest.Scope(); scope.Private || scope.Living {
		t.Fatalf("guest scope: %+v", scope)
	}
	if scope := RoleDisabled.Scope(); scope.Private || scope.Living {
		t.Fatalf("disabled scope: %+v", scope)
	}
	for _, role := range []Role{RoleMember, RoleEditor, RoleAdmin} {
		if scope := role.Scope(); !scope.Private || !scope.Living {
			t.Fatalf("%s scope: %+v", role, scope)
		}
	}
}

func TestRoleNames(t *testing.T) {
	for _, name := range []string{"guest", "member", "editor", "admin", "disabled"} {
		role, ok := ParseRole(name)
		if !ok || role.String() != name || !role.Valid() {
			t.Errorf("%s: %v %v", name, role, ok)
		}
	}
	// The two roles that Heirfold had before are gone.
	for _, name := range []string{"root", "owner", "contributor", "", "Admin"} {
		if role, ok := ParseRole(name); ok || role != RoleDisabled {
			t.Errorf("%q: unknown role accepted as %v", name, role)
		}
	}
}

func TestRoleNumbersMatchGrampsWeb(t *testing.T) {
	want := map[Role]int{RoleDisabled: -1, RoleGuest: 0, RoleMember: 1, RoleEditor: 3, RoleAdmin: 5}
	for role, number := range want {
		if int(role) != number {
			t.Errorf("%s: %d, want %d", role, int(role), number)
		}
	}
	// The numbers of the removed roles are not roles and have no rights.
	for _, role := range []Role{2, 4, 6, -2} {
		if role.Valid() || role.String() != "disabled" {
			t.Errorf("role %d: valid=%v name=%s", int(role), role.Valid(), role)
		}
		if RoleAdmin.CanGrant(role) {
			t.Errorf("an administrator can grant the unknown role %d", int(role))
		}
	}
}

func TestRoleRights(t *testing.T) {
	cases := []struct {
		role                             Role
		edit, invite, manageUsers, manag bool
	}{
		{RoleDisabled, false, false, false, false},
		{RoleGuest, false, false, false, false},
		{RoleMember, false, false, false, false},
		{RoleEditor, true, true, false, false},
		{RoleAdmin, true, true, true, true},
	}
	for _, tc := range cases {
		if tc.role.CanEdit() != tc.edit || tc.role.CanInvite() != tc.invite ||
			tc.role.CanManageUsers() != tc.manageUsers || tc.role.CanManageTree() != tc.manag {
			t.Errorf("%s: edit=%v invite=%v users=%v tree=%v", tc.role,
				tc.role.CanEdit(), tc.role.CanInvite(), tc.role.CanManageUsers(), tc.role.CanManageTree())
		}
	}
}

func TestCanGrant(t *testing.T) {
	all := []Role{RoleDisabled, RoleGuest, RoleMember, RoleEditor, RoleAdmin}
	want := map[Role]map[Role]bool{
		RoleDisabled: {},
		RoleGuest:    {},
		RoleMember:   {},
		RoleEditor:   {RoleGuest: true, RoleMember: true},
		RoleAdmin:    {RoleDisabled: true, RoleGuest: true, RoleMember: true, RoleEditor: true, RoleAdmin: true},
	}
	for _, actor := range all {
		for _, target := range all {
			if got := actor.CanGrant(target); got != want[actor][target] {
				t.Errorf("%s grants %s: got %v, want %v", actor, target, got, want[actor][target])
			}
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"anna@example.org":          "anna@example.org",
		"  Anna@Example.ORG \t\r\n": "anna@example.org",
		"":                          "",
		"   ":                       "",
	}
	for in, want := range cases {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	longest := strings.Repeat("a", MaxEmailLength-len("@example.org")) + "@example.org"
	accept := []string{
		"anna@example.org",
		"anna.orlova+tree@mail.example.com",
		"a@b.example",
		"o'brien@example.org",
		longest,
	}
	for _, address := range accept {
		if !ValidEmail(address) {
			t.Errorf("%q: refused", address)
		}
	}
	reject := map[string]string{
		"":                                     "empty",
		"anna":                                 "no domain",
		"@example.org":                         "no local part",
		"anna@":                                "empty domain",
		"anna@localhost":                       "no dot in the domain",
		"anna@.example.org":                    "domain starts with a dot",
		"anna@example.org.":                    "domain ends with a dot",
		"Anna Orlova <anna@example.org>":       "display name",
		"<anna@example.org>":                   "angle brackets",
		"\"Anna\" <anna@example.org>":          "quoted display name",
		"anna@example.org (Anna)":              "comment",
		"anna orlova@example.org":              "space in the local part",
		"anna@exam ple.org":                    "space in the domain",
		" anna@example.org":                    "leading space, not normalized",
		"anna@example.org ":                    "trailing space, not normalized",
		"anna@example.org\r\n":                 "CR LF at the end",
		"anna@example.org\nBcc: b@example.org": "header injection",
		"anna\r@example.org":                   "CR inside",
		"anna\t@example.org":                   "tab inside",
		"Anna@example.org":                     "uppercase local part, not normalized",
		"anna@Example.org":                     "uppercase domain, not normalized",
		"anna@example.org,boris@example.org":   "two addresses",
		"anna@example.org;boris@example.org":   "two addresses with a semicolon",
		"anna@boris@example.org":               "two @ signs",
		"a" + longest:                          "longer than the limit",
	}
	for address, why := range reject {
		if ValidEmail(address) {
			t.Errorf("%q accepted: %s", address, why)
		}
	}
}

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != CodeDigits || CodeDigits != 8 {
			t.Fatalf("code %q: length %d", code, len(code))
		}
		if strings.Trim(code, "0123456789") != "" {
			t.Fatalf("code %q is not all digits", code)
		}
		if clean, ok := CleanCode(code); !ok || clean != code {
			t.Fatalf("code %q does not pass CleanCode: %q %v", code, clean, ok)
		}
		seen[code] = true
	}
	// 200 codes out of 10^8 repeat with a chance of about 1 in 5000.
	if len(seen) < 199 {
		t.Fatalf("only %d different codes in 200", len(seen))
	}
}

func TestCleanCode(t *testing.T) {
	good := map[string]string{
		"12345678":        "12345678",
		"1234 5678":       "12345678",
		"1234-5678":       "12345678",
		" 12 34-56 78 ":   "12345678",
		"1234 5678":       "12345678", // A no-break space, as a mail program can copy it.
		"0000 0000":       "00000000",
		"1-2-3-4-5-6-7-8": "12345678",
	}
	for in, want := range good {
		if got, ok := CleanCode(in); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", " ", "-", "1234567", "123456789", "1234 567", "1234567a", "1234.5678", "1234_5678",
		"1234\t5678", "1234\n5678", "+12345678", "１２３４５６７８", "١٢٣٤٥٦٧٨", "abcdefgh",
	} {
		if got, ok := CleanCode(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestHashCodeDependsOnBrowserSecret(t *testing.T) {
	a := HashCode("attempt-a", "12345678")
	if !EqualHash(a, HashCode("attempt-a", "12345678")) {
		t.Fatal("the same input gives another hash")
	}
	if EqualHash(a, HashCode("attempt-b", "12345678")) {
		t.Fatal("the hash does not depend on the secret of the browser")
	}
	if EqualHash(a, HashCode("attempt-a", "12345679")) {
		t.Fatal("the hash does not depend on the code")
	}
	// The parts must not run together.
	if EqualHash(HashCode("ab", "c"), HashCode("a", "bc")) {
		t.Fatal("the hash mixes the secret and the code")
	}
	if strings.Contains(string(a), "12345678") {
		t.Fatal("the hash holds the code")
	}
	if EqualHash(a, nil) || EqualHash(a, a[:len(a)-1]) {
		t.Fatal("a shorter value is equal")
	}
}

func TestTokens(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) < 43 {
		t.Fatalf("weak tokens: %q %q", a, b)
	}
	if string(HashToken(a)) == a {
		t.Fatal("the hash equals the token")
	}
	if EqualHash(HashToken(a), HashToken(b)) || !EqualHash(HashToken(a), HashToken(a)) {
		t.Fatal("token hashes compare wrong")
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		// The ID goes into a path of the API.
		if len(id) != 12 || strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("id %q came twice", id)
		}
		seen[id] = true
	}
}
