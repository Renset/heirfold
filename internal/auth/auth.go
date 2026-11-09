// Package auth holds roles, one-time sign-in codes and secret tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/mail"
	"strings"

	"github.com/Renset/heirfold/internal/privacy"
)

// Role is the level of access of a user. The numbers of the four roles match
// the Gramps Web roles of the same name.
type Role int

// The roles, from the lowest to the highest.
const (
	RoleDisabled Role = -1
	RoleGuest    Role = 0
	RoleMember   Role = 1
	RoleEditor   Role = 3
	RoleAdmin    Role = 5
)

var roleNames = map[Role]string{
	RoleDisabled: "disabled",
	RoleGuest:    "guest",
	RoleMember:   "member",
	RoleEditor:   "editor",
	RoleAdmin:    "admin",
}

// String returns the stable name of the role.
func (r Role) String() string {
	if name, ok := roleNames[r]; ok {
		return name
	}
	return "disabled"
}

// ParseRole converts a role name to a role.
func ParseRole(name string) (Role, bool) {
	for role, value := range roleNames {
		if value == name {
			return role, true
		}
	}
	return RoleDisabled, false
}

// Valid reports whether r is a defined role.
func (r Role) Valid() bool {
	_, ok := roleNames[r]
	return ok
}

// Scope returns what the role can see. Guests see no private records and no
// living people.
func (r Role) Scope() privacy.Scope {
	switch {
	case r >= RoleEditor:
		return privacy.Full
	case r >= RoleMember:
		return privacy.Family
	}
	return privacy.Public
}

// CanEdit reports whether the role can change the tree.
func (r Role) CanEdit() bool { return r >= RoleEditor }

// CanInvite reports whether the role can invite people.
func (r Role) CanInvite() bool { return r >= RoleEditor }

// CanGrant reports whether the role can give the target role to a person.
// A tree editor invites guests and family members. An administrator gives
// every role.
func (r Role) CanGrant(target Role) bool {
	if !target.Valid() {
		return false
	}
	if r >= RoleAdmin {
		return true
	}
	return r >= RoleEditor && (target == RoleGuest || target == RoleMember)
}

// CanManageUsers reports whether the role can change and remove users.
func (r Role) CanManageUsers() bool { return r >= RoleAdmin }

// CanManageTree reports whether the role can replace, export and follow the tree.
func (r Role) CanManageTree() bool { return r >= RoleAdmin }

// MaxEmailLength is the longest address that Heirfold accepts.
const MaxEmailLength = 254

// NormalizeEmail returns the form of an address that Heirfold stores.
func NormalizeEmail(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// ValidEmail reports whether the text is one plain e-mail address. The text
// must be in the normal form.
func ValidEmail(address string) bool {
	if address == "" || len(address) > MaxEmailLength || address != NormalizeEmail(address) {
		return false
	}
	if strings.ContainsAny(address, " \t\r\n<>,;:\"()[]\\") {
		return false
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address || parsed.Name != "" {
		return false
	}
	at := strings.LastIndexByte(address, '@')
	domain := address[at+1:]
	return at > 0 && strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// CodeDigits is the length of a sign-in code.
const CodeDigits = 8

// NewCode returns a random sign-in code of CodeDigits digits.
func NewCode() (string, error) {
	limit := big.NewInt(1)
	for range CodeDigits {
		limit.Mul(limit, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", CodeDigits, n), nil
}

// CleanCode removes the spaces and dashes that a person can type in a code.
// The second result is false if the rest is not CodeDigits digits.
func CleanCode(text string) (string, bool) {
	var b strings.Builder
	for _, c := range text {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-' || c == ' ':
		default:
			return "", false
		}
	}
	code := b.String()
	return code, len(code) == CodeDigits
}

// HashCode returns the value to store for a sign-in code. The hash includes
// the secret of the browser that asked for the code, so the stored value does
// not show the code to a person who reads the database.
func HashCode(attempt, code string) []byte {
	sum := sha256.Sum256([]byte("heirfold-code\x00" + attempt + "\x00" + code))
	return sum[:]
}

// EqualHash compares two hashes in constant time.
func EqualHash(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// NewToken returns a random secret with 256 bits of entropy.
func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// NewID returns a short random identifier. It is not a secret.
func NewID() (string, error) {
	raw := make([]byte, 9)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken returns the value to store in place of a secret token.
// SHA-256 is enough because the token has 256 bits of entropy.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
