package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/store"
)

// accountDTO tells the interface who the person is and what they can do.
type accountDTO struct {
	User        *store.User `json:"user"`
	ViewPrivate bool        `json:"viewPrivate"`
	ViewLiving  bool        `json:"viewLiving"`
	// Edit is true when the person can change the tree now. It is false for
	// everybody while the tree follows a Gramps database.
	Edit bool `json:"edit"`
	// EditOwn is true when the person can change the own branch of the tree
	// now: the person of the account, the descendants and the partners.
	EditOwn bool `json:"editOwn"`
	// Pictures is true when the person can choose the own portrait now: the
	// account has a person in the tree, and Heirfold keeps the main copy.
	Pictures    bool `json:"pictures"`
	Invite      bool `json:"invite"`
	ManageUsers bool `json:"manageUsers"`
	ImportTree  bool `json:"importTree"`
	ManageTree  bool `json:"manageTree"`
	// Export is true when the person can download the whole tree.
	Export bool `json:"export"`
	// Gallery is true when the person can open the photo gallery.
	Gallery bool `json:"gallery"`
	// MapTiles is true when the server gives the pictures of a map with
	// streets. MapCredit names their makers.
	MapTiles  bool   `json:"mapTiles"`
	MapCredit string `json:"mapCredit,omitempty"`
	// Lang is the language that the person chose, or an empty text.
	Lang string `json:"lang"`
	// Source is "gramps" when the tree follows a Gramps database, and
	// "heirfold" when Heirfold keeps the main copy.
	Source string `json:"source"`
	// AuthAt is the time of the last sign-in code of this session.
	AuthAt int64 `json:"authAt"`
}

func (s *Server) source() string {
	if s.cfg.Sync != nil {
		return "gramps"
	}
	return "heirfold"
}

func (s *Server) toAccountDTO(user *store.User, authAt int64) accountDTO {
	scope := user.Role.Scope()
	own := s.cfg.Sync == nil
	return accountDTO{
		User: user, ViewPrivate: scope.Private, ViewLiving: scope.Living,
		Edit:        user.Role.CanEdit() && own,
		EditOwn:     user.EditsOwnBranch() && own,
		Pictures:    scope.Living && user.Person != "" && own,
		Invite:      user.Role.CanInvite(),
		ManageUsers: user.Role.CanManageUsers(),
		// A tree that follows Gramps takes no upload.
		ImportTree: user.Role.CanManageTree() && own,
		ManageTree: user.Role.CanManageTree(),
		// A file of the whole tree is for the people who see the whole tree.
		Export: scope.Private && scope.Living, Lang: user.Lang,
		// The gallery is for the same people: it holds photos of living relatives.
		Gallery:  scope.Private && scope.Living,
		MapTiles: s.tiles != nil && scope.Private && scope.Living, MapCredit: s.cfg.MapCredit,
		Source: s.source(), AuthAt: authAt,
	}
}

func cleanDisplayName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n > 80 {
		return "", false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	return name, true
}

// nameFromEmail makes a first display name from the part before the @ sign.
func nameFromEmail(email string) string {
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

type setupRequest struct {
	Token       string `json:"token"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Lang        string `json:"lang"`
}

// handleSetup creates the first administrator. The link from the server
// console proves that the person controls the server, so no code is needed.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ip := limitKey(s.clientIP(r))
	if !s.anon.allow("setup:"+ip, rulePublic) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var req setupRequest
	if !readJSON(w, r, &req) {
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	needed, err := s.NeedsSetup(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if !needed || !constantTimeEqual(req.Token, s.setupToken) {
		writeError(w, http.StatusForbidden, "setup_denied")
		return
	}
	email := auth.NormalizeEmail(req.Email)
	if !auth.ValidEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_email")
		return
	}
	display, ok := cleanDisplayName(req.DisplayName)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_name")
		return
	}
	if display == "" {
		display = nameFromEmail(email)
	}
	lang := ""
	if validLang(req.Lang) {
		lang = req.Lang
	}
	user, err := s.store.CreateUser(r.Context(), email, display, auth.RoleAdmin, lang, 0)
	if err != nil {
		s.internal(w, err)
		return
	}
	s.audit(r, user, "setup.complete", strconv.FormatInt(user.ID, 10), "")
	s.markDevice(w, user.Email)
	if err := s.startSession(r, w, user); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.toAccountDTO(user, time.Now().Unix()))
}

type codeRequest struct {
	Email string `json:"email"`
	Lang  string `json:"lang"`
}

// handleCodeRequest sends a sign-in code. The answer is the same for a known
// and an unknown address, so nobody can learn who has an account.
func (s *Server) handleCodeRequest(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if !readJSON(w, r, &req) {
		return
	}
	email := auth.NormalizeEmail(req.Email)
	if email == "" || len(email) > auth.MaxEmailLength {
		writeError(w, http.StatusBadRequest, "bad_email")
		return
	}
	ip := limitKey(s.clientIP(r))
	// A browser that signed in to this account before, or that has a session
	// of it, gets its own allowance. A stranger cannot use that up, so the
	// owner always gets a code. Other requests count against the client, the
	// pair of client and address, and the address.
	trusted := s.trustedDevice(r, email)
	allowed := s.anon.allow("code-ip:"+ip, ruleCodeIP)
	if allowed && trusted {
		allowed = s.anon.allow("code-trusted:"+email, ruleCodeTrusted)
	} else if allowed {
		allowed = s.anon.allow("code-user:"+ip+":"+email, ruleCodeUser) && s.anon.allow("code-account:"+email, ruleCodeAccount)
	}
	if !allowed {
		s.auditAnon(r, ip, "login.throttled")
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	attempt, err := auth.NewToken()
	if err != nil {
		s.internal(w, err)
		return
	}
	user, err := s.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, err)
		return
	}
	s.setCookie(w, attemptCookie, attempt, codeLifetime+5*time.Minute)
	if err == nil && user.Role >= auth.RoleGuest {
		lang := req.Lang
		// The work happens after the answer, so its time tells nothing.
		s.background(func(ctx context.Context) { s.sendCode(ctx, user, attempt, lang, trusted) })
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "minutes": int(codeLifetime.Minutes())})
}

type verifyRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleCodeVerify(w http.ResponseWriter, r *http.Request) {
	ip := limitKey(s.clientIP(r))
	if !s.anon.allow("verify-ip:"+ip, ruleVerifyIP) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var req verifyRequest
	if !readJSON(w, r, &req) {
		return
	}
	cookie, err := r.Cookie(s.cookieName(attemptCookie))
	code, ok := auth.CleanCode(req.Code)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	user, err := s.store.UseLoginCode(r.Context(), auth.HashToken(cookie.Value), auth.HashCode(cookie.Value, code), maxCodeTries)
	if errors.Is(err, store.ErrCodeInvalid) {
		s.auditAnon(r, ip, "login.failed")
		writeError(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	if user.Role < auth.RoleGuest {
		writeError(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	// A new code replaces the session of this browser, if it had one.
	if old, err := r.Cookie(s.cookieName(sessionCookie)); err == nil {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(old.Value)); err != nil {
			s.internal(w, err)
			return
		}
	}
	if err := s.startSession(r, w, user); err != nil {
		s.internal(w, err)
		return
	}
	s.setCookie(w, attemptCookie, "", 0)
	s.markDevice(w, user.Email)
	s.audit(r, user, "login.success", "", "")
	writeJSON(w, http.StatusOK, s.toAccountDTO(user, time.Now().Unix()))
}

// auditAnon writes an audit line about a client without an account. One
// client writes at most one line of a kind in 5 minutes, and all of them
// together a few lines in a minute.
func (s *Server) auditAnon(r *http.Request, ip, action string) {
	if s.anon.allow("audit:"+action+":"+ip, ruleAuditThrottle) && s.anon.allow("audit-anon", ruleAuditAnon) {
		s.audit(r, nil, action, "", "")
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	hash, _ := r.Context().Value(tokenHashKey).([]byte)
	if err := s.store.DeleteSession(r.Context(), hash); err != nil {
		s.internal(w, err)
		return
	}
	s.setCookie(w, sessionCookie, "", 0)
	s.audit(r, userFrom(r), "logout", "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.toAccountDTO(userFrom(r), sessionFrom(r).AuthAt))
}

type meUpdateRequest struct {
	DisplayName *string `json:"displayName"`
	Lang        *string `json:"lang"`
	// Person is the ID of the person in the tree who is the user. An empty
	// text removes the link.
	Person *string `json:"person"`
}

func (s *Server) handleMeUpdate(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	var req meUpdateRequest
	if !readJSON(w, r, &req) {
		return
	}
	// The whole request is checked before any part of it is stored.
	display := ""
	if req.DisplayName != nil {
		clean, ok := cleanDisplayName(*req.DisplayName)
		if !ok || clean == "" {
			writeError(w, http.StatusBadRequest, "bad_name")
			return
		}
		display = clean
	}
	// A link to a person gives no rights by itself, so each user sets the own
	// link. With the right to change the own branch the link decides what the
	// user can change, so only an administrator sets it.
	if req.Person != nil && user.OwnBranch {
		writeError(w, http.StatusForbidden, "person_locked")
		return
	}
	// The user can name only a person that the user can see.
	if req.Person != nil {
		updated, err := s.store.SetUserPerson(r.Context(), user.ID, *req.Person, user.Role.Scope())
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "bad_person")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		user = updated
		s.audit(r, user, "user.person", strconv.FormatInt(user.ID, 10), "")
	}
	if req.Lang != nil && validLang(*req.Lang) {
		if err := s.store.SetUserLang(r.Context(), user.ID, *req.Lang); err != nil {
			s.internal(w, err)
			return
		}
		user.Lang = *req.Lang
	}
	if req.DisplayName != nil {
		updated, err := s.store.SetUserName(r.Context(), user.ID, display)
		if err != nil {
			s.internal(w, err)
			return
		}
		user = updated
	}
	writeJSON(w, http.StatusOK, s.toAccountDTO(user, sessionFrom(r).AuthAt))
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context(), userFrom(r).ID, sessionFrom(r).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == sessionFrom(r).ID {
		// The person ends the own session with the sign-out action.
		writeError(w, http.StatusBadRequest, "current_session")
		return
	}
	err := s.store.DeleteSessionByID(r.Context(), userFrom(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	s.audit(r, userFrom(r), "session.end", "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSessionsEndOthers(w http.ResponseWriter, r *http.Request) {
	hash, _ := r.Context().Value(tokenHashKey).([]byte)
	if err := s.store.DeleteOtherSessions(r.Context(), userFrom(r).ID, hash); err != nil {
		s.internal(w, err)
		return
	}
	s.audit(r, userFrom(r), "session.end_others", "", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// personDTO is one line of the list of people with access.
type personDTO struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	// Email is empty when the viewer has no right to see the address.
	Email       string `json:"email,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	LastLoginAt *int64 `json:"lastLoginAt,omitempty"`
	// Pending is true until the person signs in for the first time.
	Pending bool `json:"pending,omitempty"`
	// Mine is true when the viewer sent this invitation.
	Mine bool `json:"mine,omitempty"`
	Self bool `json:"self,omitempty"`
	// Person is the ID of the person in the tree who owns the account.
	Person string `json:"person,omitempty"`
	// OwnBranch lets a family member change the own branch of the tree.
	OwnBranch bool `json:"ownBranch,omitempty"`
}

// toPersonDTO shows an address only to an administrator, to the inviter and
// to the owner of the account.
func toPersonDTO(viewer *store.User, user *store.User) personDTO {
	dto := personDTO{
		ID: user.ID, DisplayName: user.DisplayName, Role: user.RoleName, CreatedAt: user.CreatedAt,
		LastLoginAt: user.LastLoginAt, Pending: user.LastLoginAt == nil,
		Mine: user.InvitedBy == viewer.ID, Self: user.ID == viewer.ID, Person: user.Person,
		OwnBranch: user.OwnBranch,
	}
	if viewer.Role.CanManageUsers() || dto.Mine || dto.Self {
		dto.Email = user.Email
	}
	return dto
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	viewer := userFrom(r)
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]personDTO, 0, len(users))
	for i := range users {
		out = append(out, toPersonDTO(viewer, &users[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type inviteRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Lang        string `json:"lang"`
	// Person is the ID of the person in the tree who gets the account.
	Person string `json:"person"`
	// OwnBranch lets the new family member change the own branch.
	OwnBranch bool `json:"ownBranch"`
}

// handleUserCreate invites a person: it creates the account and sends a
// letter. The person then signs in with a code like everybody else.
func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	actor := userFrom(r)
	var req inviteRequest
	if !readJSON(w, r, &req) {
		return
	}
	role, ok := auth.ParseRole(req.Role)
	if !ok || role < auth.RoleGuest {
		writeError(w, http.StatusBadRequest, "bad_role")
		return
	}
	if !actor.Role.CanGrant(role) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// The right to change a branch is a setting of an administrator. It needs
	// a family member and a person in the tree.
	if req.OwnBranch && !actor.Role.CanManageUsers() {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if req.OwnBranch && (role != auth.RoleMember || req.Person == "") {
		writeError(w, http.StatusBadRequest, "needs_person")
		return
	}
	// A new editor or administrator changes who controls the tree, and so
	// does a person who can change a branch.
	if (role >= auth.RoleEditor || req.OwnBranch) && !s.fresh(w, r) {
		return
	}
	email := auth.NormalizeEmail(req.Email)
	if !auth.ValidEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_email")
		return
	}
	// The inviter names the person. A name from the address would show a part
	// of the address to every editor.
	display, ok := cleanDisplayName(req.DisplayName)
	if !ok || display == "" {
		writeError(w, http.StatusBadRequest, "bad_name")
		return
	}
	if req.Person != "" {
		if _, err := s.store.PersonHandle(r.Context(), req.Person, actor.Role.Scope()); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "bad_person")
			return
		} else if err != nil {
			s.internal(w, err)
			return
		}
	}
	if !s.limits.allow("invite:"+strconv.FormatInt(actor.ID, 10), ruleInvite) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	lang := actor.Lang
	if validLang(req.Lang) {
		lang = req.Lang
	}
	user, err := s.store.CreateUser(r.Context(), email, display, role, lang, actor.ID)
	if errors.Is(err, store.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "email_taken")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	if req.Person != "" {
		// The person can leave the tree between the check and this line. The
		// invitation then goes out without the link.
		if linked, err := s.store.SetUserPerson(r.Context(), user.ID, req.Person, actor.Role.Scope()); err == nil {
			user = linked
		} else if !errors.Is(err, store.ErrNotFound) {
			s.internal(w, err)
			return
		}
	}
	if req.OwnBranch && user.Person != "" {
		granted, err := s.store.SetUserOwnBranch(r.Context(), user.ID, true)
		if err != nil {
			s.internal(w, err)
			return
		}
		user = granted
		s.audit(r, actor, "user.branch", strconv.FormatInt(user.ID, 10), "on")
	}
	s.audit(r, actor, "user.invite", strconv.FormatInt(user.ID, 10), role.String())
	inviter := actor.DisplayName
	s.background(func(ctx context.Context) { s.sendInvite(ctx, user, inviter) })
	writeJSON(w, http.StatusCreated, toPersonDTO(actor, user))
}

// targetUser reads the user of the path.
func (s *Server) targetUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return nil, false
	}
	user, err := s.store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.internal(w, err)
		return nil, false
	}
	return user, true
}

// ownsInvite reports whether the actor can send again or withdraw the
// invitation of the target: an administrator always, an editor only for an
// own invitation that nobody has used yet.
func ownsInvite(actor, target *store.User) bool {
	if actor.Role.CanManageUsers() {
		return true
	}
	return target.InvitedBy == actor.ID && target.LastLoginAt == nil && actor.Role.CanGrant(target.Role)
}

func (s *Server) handleUserInviteAgain(w http.ResponseWriter, r *http.Request) {
	actor := userFrom(r)
	target, ok := s.targetUser(w, r)
	if !ok {
		return
	}
	if !ownsInvite(actor, target) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if target.LastLoginAt != nil || target.Role < auth.RoleGuest {
		writeError(w, http.StatusConflict, "not_pending")
		return
	}
	if !s.limits.allow("invite:"+strconv.FormatInt(actor.ID, 10), ruleInvite) ||
		!s.limits.allow("invite-again:"+strconv.FormatInt(target.ID, 10), ruleInviteAgain) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	s.audit(r, actor, "user.invite_again", strconv.FormatInt(target.ID, 10), "")
	inviter := actor.DisplayName
	s.background(func(ctx context.Context) { s.sendInvite(ctx, target, inviter) })
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type userUpdateRequest struct {
	Role        *string `json:"role"`
	Email       *string `json:"email"`
	DisplayName *string `json:"displayName"`
	Person      *string `json:"person"`
	OwnBranch   *bool   `json:"ownBranch"`
}

// handleUserUpdate changes the role, the address or the name of an account.
// Only an administrator can do it, and only a short time after a sign-in code.
func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	actor := userFrom(r)
	target, ok := s.targetUser(w, r)
	if !ok {
		return
	}
	var req userUpdateRequest
	if !readJSON(w, r, &req) {
		return
	}
	if (req.Role != nil || req.Email != nil || (req.OwnBranch != nil && *req.OwnBranch)) && !s.fresh(w, r) {
		return
	}
	// The whole request is checked before any part of it is stored.
	display, email, role := target.DisplayName, target.Email, target.Role
	if req.DisplayName != nil {
		clean, ok := cleanDisplayName(*req.DisplayName)
		if !ok || clean == "" {
			writeError(w, http.StatusBadRequest, "bad_name")
			return
		}
		display = clean
	}
	if req.Email != nil {
		email = auth.NormalizeEmail(*req.Email)
		if !auth.ValidEmail(email) {
			writeError(w, http.StatusBadRequest, "bad_email")
			return
		}
	}
	if req.Role != nil {
		parsed, ok := auth.ParseRole(*req.Role)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_role")
			return
		}
		role = parsed
	}
	if req.Person != nil && *req.Person != "" {
		if _, err := s.store.PersonHandle(r.Context(), *req.Person, actor.Role.Scope()); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "bad_person")
			return
		} else if err != nil {
			s.internal(w, err)
			return
		}
	}
	// The right to change the own branch needs a family member with a person.
	if req.OwnBranch != nil && *req.OwnBranch {
		person := target.Person
		if req.Person != nil {
			person = *req.Person
		}
		if role != auth.RoleMember || person == "" {
			writeError(w, http.StatusBadRequest, "needs_person")
			return
		}
	}
	// The last administrator keeps the role. The check runs before any write,
	// so a refused request changes nothing.
	if target.Role == auth.RoleAdmin && role != auth.RoleAdmin {
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			s.internal(w, err)
			return
		}
		admins := 0
		for i := range users {
			if users[i].Role == auth.RoleAdmin {
				admins++
			}
		}
		if admins <= 1 {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
	}
	id := strconv.FormatInt(target.ID, 10)
	updated := target
	// The address and the role can fail, so they go before the name.
	if email != target.Email {
		user, err := s.store.SetUserEmail(r.Context(), target.ID, email)
		if errors.Is(err, store.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email_taken")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		updated = user
		s.audit(r, actor, "user.email", id, "")
	}
	if role != target.Role {
		user, err := s.store.SetUserRole(r.Context(), target.ID, role)
		if errors.Is(err, store.ErrLastAdmin) {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		updated = user
		s.audit(r, actor, "user.role", id, target.RoleName+"->"+user.RoleName)
	}
	if display != target.DisplayName {
		user, err := s.store.SetUserName(r.Context(), target.ID, display)
		if err != nil {
			s.internal(w, err)
			return
		}
		updated = user
	}
	if req.Person != nil && *req.Person != updated.Person {
		user, err := s.store.SetUserPerson(r.Context(), target.ID, *req.Person, actor.Role.Scope())
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "bad_person")
			return
		}
		if err != nil {
			s.internal(w, err)
			return
		}
		updated = user
		s.audit(r, actor, "user.person", id, "")
	}
	// The right goes away with the role or the person that it needs.
	branch := updated.OwnBranch
	if req.OwnBranch != nil {
		branch = *req.OwnBranch
	}
	if updated.Role != auth.RoleMember || updated.Person == "" {
		branch = false
	}
	if branch != updated.OwnBranch {
		user, err := s.store.SetUserOwnBranch(r.Context(), target.ID, branch)
		if err != nil {
			s.internal(w, err)
			return
		}
		updated = user
		state := "off"
		if branch {
			state = "on"
		}
		s.audit(r, actor, "user.branch", id, state)
	}
	writeJSON(w, http.StatusOK, toPersonDTO(actor, updated))
}

// handleUserDelete removes an account. An editor can withdraw only an own
// invitation that nobody has used.
func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	actor := userFrom(r)
	target, ok := s.targetUser(w, r)
	if !ok {
		return
	}
	if target.ID == actor.ID {
		writeError(w, http.StatusBadRequest, "own_account")
		return
	}
	if !ownsInvite(actor, target) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// The removal of a person who already signed in needs a fresh code.
	if target.LastLoginAt != nil && !s.fresh(w, r) {
		return
	}
	err := s.store.DeleteUser(r.Context(), target.ID)
	if errors.Is(err, store.ErrLastAdmin) {
		writeError(w, http.StatusConflict, "last_admin")
		return
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, err)
		return
	}
	s.audit(r, actor, "user.delete", strconv.FormatInt(target.ID, 10), target.RoleName)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
