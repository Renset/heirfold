package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/privacy"
)

// User is a person who can sign in. The address is the name of the account.
type User struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        auth.Role `json:"-"`
	RoleName    string    `json:"role"`
	Lang        string    `json:"-"`
	InvitedBy   int64     `json:"-"`
	CreatedAt   int64     `json:"createdAt"`
	// LastLoginAt is nil until the person signs in for the first time.
	LastLoginAt *int64 `json:"lastLoginAt,omitempty"`
	// Person is the ID of the person in the tree who owns the account. It is
	// empty when the account has no link, or when the person left the tree.
	Person string `json:"person,omitempty"`
	// OwnBranch lets a family member change the own branch of the tree. It
	// works only with the role of a family member and with a person.
	OwnBranch bool `json:"ownBranch,omitempty"`
}

// Errors of user management.
var (
	ErrEmailTaken  = errors.New("store: an account with this address exists")
	ErrLastAdmin   = errors.New("store: cannot remove the last administrator")
	ErrCodeInvalid = errors.New("store: sign-in code is wrong or expired")
)

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var user User
	var role int
	var invitedBy sql.NullInt64
	if err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &role, &user.Lang, &invitedBy, &user.CreatedAt, &user.LastLoginAt, &user.Person, &user.OwnBranch); err != nil {
		return nil, err
	}
	user.Role = auth.Role(role)
	user.RoleName = user.Role.String()
	user.InvitedBy = invitedBy.Int64
	return &user, nil
}

// The account keeps the handle of its person, because a handle stays the
// same when Gramps gives the person a new ID. The reader gets the ID.
const userColumns = `id, email, display_name, role, lang, invited_by, created_at, last_login_at,
	coalesce((SELECT gid FROM person_index WHERE handle = user.person), ''), own_branch`

// CountUsers returns the number of users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM user`).Scan(&n)
	return n, err
}

// CreateUser adds a user. invitedBy is the ID of the user who sent the
// invitation, or 0.
func (s *Store) CreateUser(ctx context.Context, email, displayName string, role auth.Role, lang string, invitedBy int64) (*User, error) {
	now := s.now().Unix()
	var inviter any
	if invitedBy > 0 {
		inviter = invitedBy
	}
	row := s.w.QueryRowContext(ctx,
		`INSERT INTO user (email, display_name, role, lang, invited_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING `+userColumns,
		email, displayName, int(role), lang, inviter, now, now)
	user, err := scanUser(row)
	if err != nil {
		if isUniqueError(err) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return user, nil
}

func isUniqueError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// UserByEmail returns the user with this address.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	user, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userColumns+` FROM user WHERE email = ?`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// UserByID returns one user.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	user, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userColumns+` FROM user WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// ListUsers returns all users.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+userColumns+` FROM user ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, rows.Err()
}

// lastAdmin reports whether the user is the only active administrator.
func lastAdmin(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var role int
	if err := tx.QueryRowContext(ctx, `SELECT role FROM user WHERE id = ?`, id).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	if auth.Role(role) != auth.RoleAdmin {
		return false, nil
	}
	var admins int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM user WHERE role = ?`, int(auth.RoleAdmin)).Scan(&admins); err != nil {
		return false, err
	}
	return admins <= 1, nil
}

// endAccess ends every session and every waiting sign-in code of a user.
func endAccess(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM session WHERE user_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM login_code WHERE user_id = ?`, id)
	return err
}

// SetUserRole changes a role. It keeps at least one active administrator and
// ends the sessions of the user, so the new role applies at once.
func (s *Store) SetUserRole(ctx context.Context, id int64, role auth.Role) (*User, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if role != auth.RoleAdmin {
		last, err := lastAdmin(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if last {
			return nil, ErrLastAdmin
		}
	}
	row := tx.QueryRowContext(ctx, `UPDATE user SET role = ?, updated_at = ? WHERE id = ? RETURNING `+userColumns, int(role), s.now().Unix(), id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := endAccess(ctx, tx, id); err != nil {
		return nil, err
	}
	return user, tx.Commit()
}

// SetUserEmail changes the address of an account and ends its sessions.
func (s *Store) SetUserEmail(ctx context.Context, id int64, email string) (*User, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRowContext(ctx, `UPDATE user SET email = ?, updated_at = ? WHERE id = ? RETURNING `+userColumns, email, s.now().Unix(), id)
	user, err := scanUser(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case isUniqueError(err):
		return nil, ErrEmailTaken
	case err != nil:
		return nil, err
	}
	if err := endAccess(ctx, tx, id); err != nil {
		return nil, err
	}
	return user, tx.Commit()
}

// SetUserName changes the name that the interface shows for a user.
func (s *Store) SetUserName(ctx context.Context, id int64, displayName string) (*User, error) {
	row := s.w.QueryRowContext(ctx, `UPDATE user SET display_name = ?, updated_at = ? WHERE id = ? RETURNING `+userColumns, displayName, s.now().Unix(), id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// PersonHandle returns the handle of a person that the scope can see.
func (s *Store) PersonHandle(ctx context.Context, gid string, scope privacy.Scope) (string, error) {
	var handle string
	err := s.r.QueryRowContext(ctx,
		`SELECT handle FROM person_index WHERE gid = ?1 AND (private = 0 OR ?2) AND (living = 0 OR ?3)`,
		gid, scope.Private, scope.Living).Scan(&handle)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return handle, err
}

// SetUserPerson links an account to a person in the tree. An empty ID removes
// the link. scope limits the choice to the people that the actor can see.
func (s *Store) SetUserPerson(ctx context.Context, id int64, gid string, scope privacy.Scope) (*User, error) {
	handle := ""
	if gid != "" {
		found, err := s.PersonHandle(ctx, gid, scope)
		if err != nil {
			return nil, err
		}
		handle = found
	}
	row := s.w.QueryRowContext(ctx, `UPDATE user SET person = ?, updated_at = ? WHERE id = ? RETURNING `+userColumns, handle, s.now().Unix(), id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// SetUserOwnBranch lets a user change the own branch of the tree, or takes
// that right away.
func (s *Store) SetUserOwnBranch(ctx context.Context, id int64, on bool) (*User, error) {
	row := s.w.QueryRowContext(ctx, `UPDATE user SET own_branch = ?, updated_at = ? WHERE id = ? RETURNING `+userColumns, on, s.now().Unix(), id)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

// EditsOwnBranch reports whether the user can change the own branch now.
func (u *User) EditsOwnBranch() bool {
	return u.Role == auth.RoleMember && u.OwnBranch && u.Person != ""
}

// SetUserLang stores the language for the letters to a user.
func (s *Store) SetUserLang(ctx context.Context, id int64, lang string) error {
	_, err := s.w.ExecContext(ctx, `UPDATE user SET lang = ? WHERE id = ? AND lang <> ?`, lang, id, lang)
	return err
}

// DeleteUser removes a user with all sessions. It keeps at least one
// administrator.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	last, err := lastAdmin(ctx, tx, id)
	if err != nil {
		return err
	}
	if last {
		return ErrLastAdmin
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// maxWaitingCodes limits the sign-in codes that wait for one user. It is
// larger than the number of codes that one client can ask for in a row, so
// one stranger cannot push out the code that the owner waits for.
const maxWaitingCodes = 5

// CreateLoginCode stores a sign-in code. Only the newest codes of a user stay
// valid, so a flood of requests cannot widen the target for a guess.
// The code is stored only while the account still has this address and an
// active role, so a code never goes to an address that changed meanwhile.
func (s *Store) CreateLoginCode(ctx context.Context, attemptHash []byte, userID int64, email string, codeHash []byte, lifetime time.Duration) error {
	now := s.now()
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	var role int
	err = tx.QueryRowContext(ctx, `SELECT email, role FROM user WHERE id = ?`, userID).Scan(&current, &role)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (current != email || auth.Role(role) < auth.RoleGuest)) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// The old codes go first, so the new code always stays.
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_code WHERE user_id = ? AND attempt_hash NOT IN
		(SELECT attempt_hash FROM login_code WHERE user_id = ? ORDER BY created_at DESC, attempt_hash LIMIT ?)`, userID, userID, maxWaitingCodes-1); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO login_code (attempt_hash, user_id, code_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		attemptHash, userID, codeHash, now.Unix(), now.Add(lifetime).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// UseLoginCode checks a sign-in code. hash returns the hash of the typed code.
// A right code is used up together with all other codes of the user. A wrong
// code counts as a try, and the code dies after maxTries tries.
func (s *Store) UseLoginCode(ctx context.Context, attemptHash, codeHash []byte, maxTries int) (*User, error) {
	now := s.now().Unix()
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	var want []byte
	var tries int
	err = tx.QueryRowContext(ctx, `SELECT user_id, code_hash, tries FROM login_code WHERE attempt_hash = ? AND expires_at > ?`, attemptHash, now).Scan(&userID, &want, &tries)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	if tries >= maxTries || !auth.EqualHash(want, codeHash) {
		query := `UPDATE login_code SET tries = tries + 1 WHERE attempt_hash = ?`
		if tries+1 >= maxTries {
			query = `DELETE FROM login_code WHERE attempt_hash = ?`
		}
		if _, err := tx.ExecContext(ctx, query, attemptHash); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrCodeInvalid
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_code WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	user, err := scanUser(tx.QueryRowContext(ctx, `UPDATE user SET last_login_at = ? WHERE id = ? RETURNING `+userColumns, now, userID))
	if err != nil {
		return nil, err
	}
	return user, tx.Commit()
}

// Session is one signed-in browser of a user.
type Session struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	AuthAt     int64  `json:"-"`
	ExpiresAt  int64  `json:"-"`
	Current    bool   `json:"current,omitempty"`
}

// CreateSession stores a session. The token itself is not stored.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, id string, userID int64, label string, lifetime time.Duration) error {
	now := s.now()
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO session (token_hash, id, user_id, label, created_at, last_seen_at, auth_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenHash, id, userID, label, now.Unix(), now.Unix(), now.Unix(), now.Add(lifetime).Unix())
	return err
}

// sessionRefresh is how often a session in use moves its end forward.
const sessionRefresh = 24 * time.Hour

// SessionUser returns the user of a valid session. A session in use lasts for
// the whole lifetime again: the store moves its end forward once a day. The
// third result reports that move, so the caller can renew the cookie.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte, lifetime time.Duration) (*User, *Session, bool, error) {
	now := s.now().Unix()
	row := s.r.QueryRowContext(ctx, `SELECT u.id, u.email, u.display_name, u.role, u.lang, u.invited_by, u.created_at, u.last_login_at,
		coalesce((SELECT gid FROM person_index WHERE handle = u.person), ''), u.own_branch,
		s.id, s.label, s.created_at, s.last_seen_at, s.auth_at, s.expires_at
		FROM session s JOIN user u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash)
	var user User
	var session Session
	var role int
	var invitedBy sql.NullInt64
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &role, &user.Lang, &invitedBy, &user.CreatedAt, &user.LastLoginAt, &user.Person, &user.OwnBranch,
		&session.ID, &session.Label, &session.CreatedAt, &session.LastSeenAt, &session.AuthAt, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, ErrNotFound
	}
	if err != nil {
		return nil, nil, false, err
	}
	user.Role = auth.Role(role)
	user.RoleName = user.Role.String()
	user.InvitedBy = invitedBy.Int64
	session.Current = true
	if session.ExpiresAt <= now {
		if _, err := s.w.ExecContext(ctx, `DELETE FROM session WHERE token_hash = ?`, tokenHash); err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, ErrNotFound
	}
	extended := false
	if next := now + int64(lifetime.Seconds()); next-session.ExpiresAt >= int64(sessionRefresh.Seconds()) {
		if _, err := s.w.ExecContext(ctx, `UPDATE session SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?`, now, next, tokenHash); err != nil {
			return nil, nil, false, err
		}
		session.LastSeenAt, session.ExpiresAt, extended = now, next, true
	}
	return &user, &session, extended, nil
}

// ListSessions returns the sessions of a user, the newest use first.
func (s *Store) ListSessions(ctx context.Context, userID int64, current string) ([]Session, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, label, created_at, last_seen_at, auth_at, expires_at FROM session
		WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC, created_at DESC`, userID, s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	sessions := []Session{}
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.Label, &session.CreatedAt, &session.LastSeenAt, &session.AuthAt, &session.ExpiresAt); err != nil {
			return nil, err
		}
		session.Current = session.ID == current
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM session WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteSessionByID ends one session of a user.
func (s *Store) DeleteSessionByID(ctx context.Context, userID int64, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM session WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOtherSessions ends all sessions of a user except one.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keep []byte) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM session WHERE user_id = ? AND token_hash <> ?`, userID, keep)
	return err
}

// The audit log keeps one year or the newest rows, whichever is less.
const (
	auditRetention = 365 * 24 * time.Hour
	auditMaxRows   = 100_000
)

// DeleteExpired removes old sessions, sign-in codes and audit rows.
func (s *Store) DeleteExpired(ctx context.Context) error {
	now := s.now().Unix()
	if _, err := s.w.ExecContext(ctx, `DELETE FROM session WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	if _, err := s.w.ExecContext(ctx, `DELETE FROM login_code WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	if _, err := s.w.ExecContext(ctx, `DELETE FROM audit WHERE at <= ?`, now-int64(auditRetention.Seconds())); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `DELETE FROM audit WHERE id <= (SELECT max(id) FROM audit) - ?`, auditMaxRows)
	return err
}

// AuditEntry is one line of the audit log.
type AuditEntry struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
	IP     string `json:"ip,omitempty"`
}

// AddAudit appends one line to the audit log.
func (s *Store) AddAudit(ctx context.Context, actorID int64, actor, action, target, detail, ip string) error {
	var id any
	if actorID > 0 {
		id = actorID
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO audit (at, actor_id, actor, action, target, detail, ip) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.now().Unix(), id, actor, action, target, detail, ip)
	return err
}

// ListAudit returns the newest lines of the audit log.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, at, actor, action, target, detail, ip FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := []AuditEntry{}
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.Action, &entry.Target, &entry.Detail, &entry.IP); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
