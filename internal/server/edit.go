package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// maxEditBody limits the body of an edit. A long note needs more than the
// usual small limit.
const maxEditBody = 512 << 10

// readEdit decodes the body of an edit request.
func readEdit(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxEditBody)
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

// editable wraps a handler that changes the tree. While the tree follows a
// Gramps database, Gramps holds the main copy, so Heirfold changes nothing.
func (s *Server) editable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Sync != nil {
			writeError(w, http.StatusConflict, "managed_by_gramps")
			return
		}
		if r.Method != http.MethodGet && !s.limits.allow("edit:"+strconv.FormatInt(userFrom(r).ID, 10), ruleEdit) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next(w, r)
	}
}

// branchEditor wraps a handler for the people who can change the tree: an
// editor, and a family member who can change the own branch.
func (s *Server) branchEditor(next http.HandlerFunc) http.HandlerFunc {
	return s.require(auth.RoleMember, func(w http.ResponseWriter, r *http.Request) {
		if user := userFrom(r); !user.Role.CanEdit() && !user.EditsOwnBranch() {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

// pictureEditor wraps a handler that changes the pictures of the person in
// the path. An editor and a family member with the own branch pass as for any
// edit. Every other family member passes only for the person of the own
// account: each person chooses the own portrait.
func (s *Server) pictureEditor(next http.HandlerFunc) http.HandlerFunc {
	return s.require(auth.RoleMember, func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		if !user.Role.CanEdit() && !user.EditsOwnBranch() && (user.Person == "" || user.Person != r.PathValue("id")) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	})
}

// actor names the user of the request for the journal. For a family member
// it also names what limits the edit: the own branch, or, without the right
// to change the branch, the pictures of the own person.
func actor(r *http.Request) store.Actor {
	user := userFrom(r)
	out := store.Actor{ID: user.ID, Name: user.DisplayName}
	switch {
	case user.Role.CanEdit():
	case user.EditsOwnBranch():
		out.Branch = user.Person
	default:
		// The store refuses every edit of an account without a person.
		out.Pictures = user.Person
		if out.Pictures == "" {
			out.Pictures = "-"
		}
	}
	return out
}

func (s *Server) editError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrOutsideBranch):
		writeError(w, http.StatusForbidden, "outside_branch")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "changed")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid")
	case errors.Is(err, store.ErrSlotTaken):
		writeError(w, http.StatusConflict, "parents_full")
	case errors.Is(err, store.ErrLoop):
		writeError(w, http.StatusConflict, "loop")
	default:
		s.internal(w, err)
	}
}

// edit runs one change of the tree for the user of the request and writes
// the answer.
func (s *Server) edit(w http.ResponseWriter, r *http.Request, action string, fn func(*store.Edit) error) {
	result, err := s.store.Edit(r.Context(), actor(r), action, fn)
	if err != nil {
		s.editError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleEditPerson(w http.ResponseWriter, r *http.Request) {
	var out *store.EditPerson
	branch := actor(r).Branch
	err := s.store.Inspect(r.Context(), func(e *store.Edit) error {
		if branch != "" && !e.InBranch(branch, r.PathValue("id")) {
			return store.ErrOutsideBranch
		}
		var err error
		out, err = e.Describe(r.PathValue("id"))
		if err == nil && branch != "" {
			// A family member edits the own branch and reads no hidden note.
			out.Notes = slices.DeleteFunc(out.Notes, func(note store.EditNote) bool { return note.Hidden })
		}
		return err
	})
	if err != nil {
		s.editError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleEditPlaces(w http.ResponseWriter, r *http.Request) {
	var out []store.PlaceOption
	if err := s.store.Inspect(r.Context(), func(e *store.Edit) error {
		out = e.Places()
		return nil
	}); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": out})
}

type relationRequest struct {
	Person string `json:"person"`
	As     string `json:"as"`
	Family string `json:"family"`
}

type personCreateRequest struct {
	Person   store.PersonInput `json:"person"`
	Relation *relationRequest  `json:"relation"`
}

// handlePersonCreate adds a person. With a relation, the new person becomes
// the parent, the spouse, the child or the sibling of a person in the tree.
func (s *Server) handlePersonCreate(w http.ResponseWriter, r *http.Request) {
	var req personCreateRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "person.add", func(e *store.Edit) error {
		person, err := e.CreatePerson(req.Person)
		if err != nil {
			return err
		}
		if req.Relation == nil {
			return nil
		}
		return e.Link(req.Relation.Person, req.Relation.As, person.ID, req.Relation.Family)
	})
}

type personUpdateRequest struct {
	Rev    int64             `json:"rev"`
	Person store.PersonInput `json:"person"`
}

func (s *Server) handlePersonUpdate(w http.ResponseWriter, r *http.Request) {
	var req personUpdateRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "person.update", func(e *store.Edit) error {
		return e.UpdatePerson(r.PathValue("id"), req.Rev, req.Person)
	})
}

func (s *Server) handlePersonDelete(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "person.delete", func(e *store.Edit) error {
		return e.DeletePerson(r.PathValue("id"))
	})
}

type linkRequest struct {
	Person string `json:"person"`
	As     string `json:"as"`
	Other  string `json:"other"`
	Family string `json:"family"`
}

func (s *Server) handleLinkCreate(w http.ResponseWriter, r *http.Request) {
	var req linkRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "link.add", func(e *store.Edit) error {
		return e.Link(req.Person, req.As, req.Other, req.Family)
	})
}

func (s *Server) handleLinkDelete(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "link.remove", func(e *store.Edit) error {
		return e.Unlink(r.PathValue("id"), r.PathValue("person"))
	})
}

type familyUpdateRequest struct {
	Rel string `json:"rel"`
}

func (s *Server) handleFamilyUpdate(w http.ResponseWriter, r *http.Request) {
	var req familyUpdateRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "family.update", func(e *store.Edit) error {
		return e.SetFamilyType(r.PathValue("id"), req.Rel)
	})
}

type eventRequest struct {
	Rev   int64            `json:"rev"`
	Event store.EventInput `json:"event"`
}

func (s *Server) handleEventCreate(kind model.Kind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req eventRequest
		if !readEdit(w, r, &req) {
			return
		}
		s.edit(w, r, "event.add", func(e *store.Edit) error {
			return e.AddEvent(store.Owner{Kind: kind, ID: r.PathValue("id")}, req.Event)
		})
	}
}

func (s *Server) handleEventUpdate(w http.ResponseWriter, r *http.Request) {
	var req eventRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "event.update", func(e *store.Edit) error {
		// The journal lists the change under the person whose page sent it.
		if subject := r.URL.Query().Get("person"); subject != "" {
			if _, err := e.Person(subject); err == nil {
				e.Subject = subject
			}
		}
		return e.UpdateEvent(r.PathValue("id"), req.Rev, req.Event)
	})
}

func (s *Server) handleEventDelete(kind model.Kind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.edit(w, r, "event.remove", func(e *store.Edit) error {
			return e.RemoveEvent(store.Owner{Kind: kind, ID: r.PathValue("id")}, r.PathValue("event"))
		})
	}
}

type noteRequest struct {
	Rev  int64  `json:"rev"`
	Text string `json:"text"`
	// Hidden makes the note a note for editors and administrators only.
	// Without it a new note is open, and an old note stays as it is.
	Hidden *bool `json:"hidden"`
}

func (s *Server) handleNoteCreate(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "note.add", func(e *store.Edit) error {
		return e.AddNote(r.PathValue("id"), req.Text, req.Hidden != nil && *req.Hidden)
	})
}

func (s *Server) handleNoteUpdate(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "note.update", func(e *store.Edit) error {
		// For a family member a hidden note does not exist.
		if actor(r).Branch != "" && e.HiddenNote(r.PathValue("id")) {
			return store.ErrNotFound
		}
		if subject := r.URL.Query().Get("person"); subject != "" {
			if _, err := e.Person(subject); err == nil {
				e.Subject = subject
			}
		}
		return e.UpdateNote(r.PathValue("id"), req.Rev, req.Text, req.Hidden)
	})
}

func (s *Server) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "note.remove", func(e *store.Edit) error {
		if actor(r).Branch != "" && e.HiddenNote(r.PathValue("note")) {
			return store.ErrNotFound
		}
		return e.RemoveNote(r.PathValue("id"), r.PathValue("note"))
	})
}

type homeRequest struct {
	Person string `json:"person"`
}

func (s *Server) handleHomeSet(w http.ResponseWriter, r *http.Request) {
	var req homeRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "home.set", func(e *store.Edit) error {
		return e.SetHome(req.Person)
	})
}

// handleHistory shows the journal. A family member who changes the own
// branch sees the own entries only.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	var own int64
	if !user.Role.CanEdit() {
		own = user.ID
	}
	entries, err := s.store.ListChangesBy(r.Context(), own, r.URL.Query().Get("person"), 200)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleHistoryUndo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	user := userFrom(r)
	// A family member takes back the own actions only. The guard of the branch
	// does not run: an undo brings back a state that was there before.
	if !user.Role.CanEdit() {
		author, err := s.store.ChangeActor(r.Context(), id)
		if err != nil {
			s.editError(w, err)
			return
		}
		if author != user.ID {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	result, err := s.store.UndoChange(r.Context(), store.Actor{ID: user.ID, Name: user.DisplayName}, id)
	if err != nil {
		s.editError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
