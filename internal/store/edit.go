package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// Errors of an edit.
var (
	// ErrConflict means that the record changed after the editor read it, or
	// that a later edit depends on the action to undo.
	ErrConflict = errors.New("store: the record changed in the meantime")
	// ErrInvalid means that the edit asks for something that the tree cannot hold.
	ErrInvalid = errors.New("store: the edit is not valid")
	// ErrSlotTaken means that the family already has both parents.
	ErrSlotTaken = errors.New("store: the family already has two parents")
	// ErrLoop means that the edit makes a person an ancestor of themself.
	ErrLoop = errors.New("store: the edit makes a person their own ancestor")
)

// Actor names the user who makes an edit.
type Actor struct {
	ID   int64
	Name string
	// Branch is the ID of the person whose branch limits the edit. It is
	// empty for an editor, who can change the whole tree.
	Branch string
	// Pictures is the ID of the person whose pictures alone the edit can
	// change. It is set for a family member who chooses the own portrait.
	Pictures string
}

// EditResult describes a finished edit.
type EditResult struct {
	// ChangeID is the number of the journal entry, or 0 if nothing changed.
	ChangeID int64 `json:"changeId,omitempty"`
	// Subject is the ID of the person that the edit is about.
	Subject string `json:"subject,omitempty"`
	// Created is the ID of the main object that the edit made.
	Created string `json:"created,omitempty"`
	// Revision is the new number of the tree state.
	Revision int64 `json:"revision"`
}

// Edit is one open change of the tree. Its methods change objects in memory.
// The store writes all of them, or none, when the function of Store.Edit ends.
type Edit struct {
	now  int64
	tree *model.Tree
	// before holds the stored document of each record.
	before map[string]string
	ids    map[string]string
	byID   map[string]string

	people   map[string]*model.Person
	families map[string]*model.Family
	events   map[string]*model.Event
	places   map[string]*model.Place
	notes    map[string]*model.Note
	media    map[string]*model.Media

	touched map[string]bool
	deleted map[string]bool
	// order keeps the records that the edit added, in the order of creation.
	added []string

	metaChanged bool
	// Subject is the ID of the person that the edit is about.
	Subject string
	// Created is the ID of the main object that the edit made.
	Created string
}

func idKey(kind model.Kind, gid string) string { return string(kind) + "\x00" + gid }

func newEdit(now int64, tree *model.Tree, before, ids map[string]string) *Edit {
	e := &Edit{
		now: now, tree: tree, before: before, ids: ids, byID: make(map[string]string, len(ids)),
		people: make(map[string]*model.Person, len(tree.People)), families: make(map[string]*model.Family, len(tree.Families)),
		events: make(map[string]*model.Event, len(tree.Events)), places: make(map[string]*model.Place, len(tree.Places)),
		notes: make(map[string]*model.Note, len(tree.Notes)), media: make(map[string]*model.Media, len(tree.Media)),
		touched: map[string]bool{}, deleted: map[string]bool{},
	}
	for key, gid := range ids {
		kind, handle, _ := strings.Cut(key, "\x00")
		e.byID[idKey(model.Kind(kind), gid)] = handle
	}
	for i := range tree.People {
		e.people[tree.People[i].Handle] = &tree.People[i]
	}
	for i := range tree.Families {
		e.families[tree.Families[i].Handle] = &tree.Families[i]
	}
	for i := range tree.Events {
		e.events[tree.Events[i].Handle] = &tree.Events[i]
	}
	for i := range tree.Places {
		e.places[tree.Places[i].Handle] = &tree.Places[i]
	}
	for i := range tree.Notes {
		e.notes[tree.Notes[i].Handle] = &tree.Notes[i]
	}
	for i := range tree.Media {
		e.media[tree.Media[i].Handle] = &tree.Media[i]
	}
	return e
}

// idPrefix is the letter that starts the IDs of each kind, as in Gramps.
var idPrefix = map[model.Kind]string{
	model.KindPerson: "I", model.KindFamily: "F", model.KindEvent: "E", model.KindPlace: "P",
	model.KindSource: "S", model.KindCitation: "C", model.KindRepository: "R", model.KindNote: "N", model.KindMedia: "O",
}

// newHandle returns a handle in the form of Gramps: a time part and a random part.
func (e *Edit) newHandle() string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return "_" + strconv.FormatInt(e.now*10000, 16) + hex.EncodeToString(raw[:])
}

// newID returns the next free ID of a kind, for example I0042.
func (e *Edit) newID(kind model.Kind) string {
	prefix := idPrefix[kind]
	highest := 0
	for key := range e.byID {
		k, gid, _ := strings.Cut(key, "\x00")
		if model.Kind(k) != kind || !strings.HasPrefix(gid, prefix) {
			continue
		}
		if n, err := strconv.Atoi(gid[len(prefix):]); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s%04d", prefix, highest+1)
}

// register gives a new object its handle and ID and marks it as changed.
func (e *Edit) register(kind model.Kind, object *model.Object) {
	object.Handle = e.newHandle()
	object.ID = e.newID(kind)
	object.Change = e.now
	key := recordKey(kind, object.Handle)
	e.ids[key] = object.ID
	e.byID[idKey(kind, object.ID)] = object.Handle
	e.touched[key] = true
	e.added = append(e.added, key)
}

func (e *Edit) touch(kind model.Kind, handle string) {
	e.touched[recordKey(kind, handle)] = true
}

func (e *Edit) remove(kind model.Kind, handle string) {
	key := recordKey(kind, handle)
	e.deleted[key] = true
	e.touched[key] = true
	delete(e.byID, idKey(kind, e.ids[key]))
}

// handleOf returns the handle of the object with this ID.
func (e *Edit) handleOf(kind model.Kind, gid string) (string, bool) {
	handle, ok := e.byID[idKey(kind, gid)]
	return handle, ok
}

// ID returns the unique ID of an object.
func (e *Edit) ID(kind model.Kind, handle string) string {
	return e.ids[recordKey(kind, handle)]
}

// Person returns the person with this ID.
func (e *Edit) Person(gid string) (*model.Person, error) {
	if handle, ok := e.handleOf(model.KindPerson, gid); ok {
		return e.people[handle], nil
	}
	return nil, ErrNotFound
}

// Family returns the family with this ID.
func (e *Edit) Family(gid string) (*model.Family, error) {
	if handle, ok := e.handleOf(model.KindFamily, gid); ok {
		return e.families[handle], nil
	}
	return nil, ErrNotFound
}

// Event returns the event with this ID.
func (e *Edit) Event(gid string) (*model.Event, error) {
	if handle, ok := e.handleOf(model.KindEvent, gid); ok {
		return e.events[handle], nil
	}
	return nil, ErrNotFound
}

// Note returns the note with this ID.
func (e *Edit) Note(gid string) (*model.Note, error) {
	if handle, ok := e.handleOf(model.KindNote, gid); ok {
		return e.notes[handle], nil
	}
	return nil, ErrNotFound
}

// HiddenNote reports whether the note with this ID is a hidden note.
func (e *Edit) HiddenNote(gid string) bool {
	note, err := e.Note(gid)
	return err == nil && note != nil && bool(note.Private)
}

// finish moves the added and removed objects into the tree.
func (e *Edit) finish() {
	gone := func(kind model.Kind, handle string) bool { return e.deleted[recordKey(kind, handle)] }
	isNew := map[string]bool{}
	for _, key := range e.added {
		isNew[key] = true
	}
	t := e.tree
	t.People = rebuild(t.People, e.people, model.KindPerson, gone, isNew, e.added, func(p *model.Person) string { return p.Handle })
	t.Families = rebuild(t.Families, e.families, model.KindFamily, gone, isNew, e.added, func(f *model.Family) string { return f.Handle })
	t.Events = rebuild(t.Events, e.events, model.KindEvent, gone, isNew, e.added, func(v *model.Event) string { return v.Handle })
	t.Places = rebuild(t.Places, e.places, model.KindPlace, gone, isNew, e.added, func(p *model.Place) string { return p.Handle })
	t.Notes = rebuild(t.Notes, e.notes, model.KindNote, gone, isNew, e.added, func(n *model.Note) string { return n.Handle })
	t.Media = rebuild(t.Media, e.media, model.KindMedia, gone, isNew, e.added, func(m *model.Media) string { return m.Handle })
}

func rebuild[T any](old []T, items map[string]*T, kind model.Kind, gone func(model.Kind, string) bool,
	isNew map[string]bool, added []string, handle func(*T) string) []T {
	out := make([]T, 0, len(items))
	for i := range old {
		if !gone(kind, handle(&old[i])) {
			out = append(out, old[i])
		}
	}
	for _, key := range added {
		k, h, _ := strings.Cut(key, "\x00")
		if model.Kind(k) != kind || !isNew[key] || gone(kind, h) {
			continue
		}
		if item := items[h]; item != nil {
			out = append(out, *item)
		}
	}
	return out
}

// docChange is one object before and after an edit. A nil document means
// that the object does not exist on that side.
type docChange struct {
	kind    model.Kind
	handle  string
	gid     string
	before  []byte
	after   []byte
	private bool
	change  int64
}

// current returns the document of a touched record, or nil if it is deleted.
func (e *Edit) current(key string) ([]byte, bool, int64, error) {
	if e.deleted[key] {
		return nil, false, 0, nil
	}
	kind, handle, _ := strings.Cut(key, "\x00")
	var value any
	var object *model.Object
	switch model.Kind(kind) {
	case model.KindPerson:
		if item := e.people[handle]; item != nil {
			value, object = item, &item.Object
		}
	case model.KindFamily:
		if item := e.families[handle]; item != nil {
			value, object = item, &item.Object
		}
	case model.KindEvent:
		if item := e.events[handle]; item != nil {
			value, object = item, &item.Object
		}
	case model.KindPlace:
		if item := e.places[handle]; item != nil {
			value, object = item, &item.Object
		}
	case model.KindNote:
		if item := e.notes[handle]; item != nil {
			value, object = item, &item.Object
		}
	case model.KindMedia:
		if item := e.media[handle]; item != nil {
			value, object = item, &item.Object
		}
	}
	if object == nil {
		return nil, false, 0, fmt.Errorf("store: edit touched an unknown record of kind %s", kind)
	}
	doc, err := json.Marshal(value)
	if err != nil {
		return nil, false, 0, err
	}
	// A real change gets a new time stamp, as Gramps does.
	if stored, ok := e.before[key]; !ok || stored != string(doc) {
		object.Change = e.now
		if doc, err = json.Marshal(value); err != nil {
			return nil, false, 0, err
		}
	}
	return doc, bool(object.Private), object.Change, nil
}

// kindMeta marks the part of the tree that belongs to no object: the home
// person, the bookmarks and the header. The journal keeps it like an object,
// so an undo brings it back.
const (
	kindMeta   model.Kind = "meta"
	metaHandle            = "tree"
)

// putMeta writes the meta document, or removes it for nil.
func putMeta(ctx context.Context, tx *sql.Tx, doc []byte) error {
	if doc == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM meta WHERE key = 'tree'`)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('tree', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(doc))
	return err
}

// Rev returns the state number of a stored record. It is a hash of the
// stored document, so any change gives another number, also two changes in
// one second. It fits into the safe integers of JavaScript.
func (e *Edit) Rev(kind model.Kind, handle string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(e.before[recordKey(kind, handle)]))
	return int64(h.Sum64() & (1<<53 - 1))
}

// checkRev refuses an edit that starts from another state of the record.
func (e *Edit) checkRev(kind model.Kind, handle string, rev int64) error {
	if rev != e.Rev(kind, handle) {
		return ErrConflict
	}
	return nil
}

// changes returns what the edit did, in a stable order.
func (e *Edit) changes() ([]docChange, error) {
	keys := make([]string, 0, len(e.touched))
	for key := range e.touched {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []docChange
	for _, key := range keys {
		kind, handle, _ := strings.Cut(key, "\x00")
		change := docChange{kind: model.Kind(kind), handle: handle, gid: e.ids[key]}
		if stored, ok := e.before[key]; ok {
			change.before = []byte(stored)
		}
		doc, private, stamp, err := e.current(key)
		if err != nil {
			return nil, err
		}
		change.after, change.private, change.change = doc, private, stamp
		if change.before == nil && change.after == nil {
			continue
		}
		if bytes.Equal(change.before, change.after) {
			continue
		}
		out = append(out, change)
	}
	if e.metaChanged {
		after, err := json.Marshal(e.tree.Meta())
		if err != nil {
			return nil, err
		}
		change := docChange{kind: kindMeta, handle: metaHandle, after: after}
		if stored, ok := e.before[recordKey(kindMeta, metaHandle)]; ok {
			change.before = []byte(stored)
		}
		if !bytes.Equal(change.before, change.after) {
			out = append(out, change)
		}
	}
	return out, nil
}

// Edit runs fn on the tree and stores the result as one journal entry. All
// edits run one after the other. An error from fn cancels the edit.
func (s *Store) Edit(ctx context.Context, actor Actor, action string, fn func(*Edit) error) (*EditResult, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tree, docs, ids, err := loadTree(ctx, tx)
	if err != nil {
		return nil, err
	}
	if tree.Namespace == "" {
		tree.Namespace = defaultNamespace
	}
	e := newEdit(s.now().Unix(), tree, docs, ids)
	var guard *branchGuard
	if actor.Branch != "" {
		var ok bool
		if guard, ok = e.guard(actor.Branch); !ok {
			return nil, ErrOutsideBranch
		}
	}
	if err := fn(e); err != nil {
		return nil, err
	}
	changes, err := e.changes()
	if err != nil {
		return nil, err
	}
	if guard != nil {
		if err := guard.check(e, changes); err != nil {
			return nil, err
		}
	}
	if actor.Pictures != "" {
		if err := e.onlyPictures(actor.Pictures, changes); err != nil {
			return nil, err
		}
	}
	result := &EditResult{Subject: e.Subject, Created: e.Created}
	if len(changes) == 0 {
		result.Revision, err = s.Revision(ctx)
		return result, err
	}
	e.finish()
	id, err := s.applyChanges(ctx, tx, actor, action, e.Subject, 0, changes, tree, ids)
	if err != nil {
		return nil, err
	}
	if err := writeMeta(ctx, tx, tree.Meta()); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'revision'`).Scan(&result.Revision); err != nil {
		return nil, err
	}
	result.ChangeID = id
	return result, tx.Commit()
}

// Inspect runs fn on the current tree and stores nothing. The edit pages use
// it to read a person in the form that the forms need.
func (s *Store) Inspect(ctx context.Context, fn func(*Edit) error) error {
	tree, docs, ids, err := loadTree(ctx, s.r)
	if err != nil {
		return err
	}
	return fn(newEdit(s.now().Unix(), tree, docs, ids))
}

// defaultNamespace is the Gramps XML format of a tree that starts in Heirfold.
const defaultNamespace = "http://gramps-project.org/xml/1.7.2/"

// applyChanges writes the objects and the journal entry, then rebuilds the
// tables that the views read.
func (s *Store) applyChanges(ctx context.Context, tx *sql.Tx, actor Actor, action, subject string, undoes int64,
	changes []docChange, tree *model.Tree, ids map[string]string) (int64, error) {
	var actorID any
	if actor.ID > 0 {
		actorID = actor.ID
	}
	var undoesID any
	if undoes > 0 {
		undoesID = undoes
	}
	var setID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO change_set (at, actor_id, actor, action, subject, undoes) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		s.now().Unix(), actorID, actor.Name, action, subject, undoesID).Scan(&setID); err != nil {
		return 0, err
	}
	for _, change := range changes {
		var before, after any
		if change.before != nil {
			before = string(change.before)
		}
		switch {
		case change.kind == kindMeta:
			if change.after != nil {
				after = string(change.after)
			}
			if err := putMeta(ctx, tx, change.after); err != nil {
				return 0, err
			}
		case change.after == nil:
			if _, err := tx.ExecContext(ctx, `DELETE FROM object WHERE kind = ? AND handle = ?`, string(change.kind), change.handle); err != nil {
				return 0, err
			}
		default:
			after = string(change.after)
			if _, err := tx.ExecContext(ctx, `INSERT INTO object (kind, handle, gid, private, change, doc) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (kind, handle) DO UPDATE SET gid = excluded.gid, private = excluded.private, change = excluded.change, doc = excluded.doc`,
				string(change.kind), change.handle, change.gid, change.private, change.change, after); err != nil {
				return 0, fmt.Errorf("store: write object: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO change_item (set_id, kind, handle, gid, before, after) VALUES (?, ?, ?, ?, ?, ?)`,
			setID, string(change.kind), change.handle, change.gid, before, after); err != nil {
			return 0, err
		}
	}
	if _, err := s.reindex(ctx, tx, tree, ids); err != nil {
		return 0, err
	}
	if err := bumpRevision(ctx, tx); err != nil {
		return 0, err
	}
	return setID, nil
}

// ChangeEntry is one line of the journal of edits.
type ChangeEntry struct {
	ID      int64  `json:"id"`
	At      int64  `json:"at"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	Subject string `json:"subject,omitempty"`
	// Objects is the number of objects that the action changed.
	Objects int `json:"objects"`
	// Undoes is the entry that this entry took back.
	Undoes int64 `json:"undoes,omitempty"`
	// Undone is true when a later entry took this entry back.
	Undone bool `json:"undone,omitempty"`
}

// ListChanges returns the newest journal entries. With a subject, it returns
// only the entries about that person.
func (s *Store) ListChanges(ctx context.Context, subject string, limit int) ([]ChangeEntry, error) {
	return s.ListChangesBy(ctx, 0, subject, limit)
}

// ChangeActor returns the ID of the user who made a journal entry, or 0.
func (s *Store) ChangeActor(ctx context.Context, id int64) (int64, error) {
	var actor sql.NullInt64
	err := s.r.QueryRowContext(ctx, `SELECT actor_id FROM change_set WHERE id = ?`, id).Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return actor.Int64, err
}

// ListChangesBy is ListChanges for the entries of one user. An actor of 0
// means all users.
func (s *Store) ListChangesBy(ctx context.Context, actorID int64, subject string, limit int) ([]ChangeEntry, error) {
	query := `SELECT c.id, c.at, c.actor, c.action, c.subject, coalesce(c.undoes, 0), c.undone_by IS NOT NULL,
		(SELECT count(*) FROM change_item i WHERE i.set_id = c.id) FROM change_set c WHERE 1 = 1`
	args := []any{}
	if subject != "" {
		query += ` AND c.subject = ?`
		args = append(args, subject)
	}
	if actorID > 0 {
		query += ` AND c.actor_id = ?`
		args = append(args, actorID)
	}
	query += ` ORDER BY c.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := []ChangeEntry{}
	for rows.Next() {
		var entry ChangeEntry
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor, &entry.Action, &entry.Subject, &entry.Undoes, &entry.Undone, &entry.Objects); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// CountChanges returns the number of journal entries. It is more than zero
// when the tree has edits that were made in Heirfold.
func (s *Store) CountChanges(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM change_set`).Scan(&n)
	return n, err
}

// UndoChange takes one journal entry back. It works only while every object
// of the entry still has the state that the entry left, so a later edit of
// the same object must be undone first.
func (s *Store) UndoChange(ctx context.Context, actor Actor, id int64) (*EditResult, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var subject string
	var undone sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT subject, undone_by FROM change_set WHERE id = ?`, id).Scan(&subject, &undone)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if undone.Valid {
		return nil, ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind, handle, gid, before, after FROM change_item WHERE set_id = ?`, id)
	if err != nil {
		return nil, err
	}
	var changes []docChange
	for rows.Next() {
		var kind, handle, gid string
		var before, after sql.NullString
		if err := rows.Scan(&kind, &handle, &gid, &before, &after); err != nil {
			_ = rows.Close()
			return nil, err
		}
		// The undo swaps the two sides.
		change := docChange{kind: model.Kind(kind), handle: handle, gid: gid}
		if after.Valid {
			change.before = []byte(after.String)
		}
		if before.Valid {
			change.after = []byte(before.String)
		}
		changes = append(changes, change)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, ErrConflict
	}
	for i := range changes {
		change := &changes[i]
		var doc string
		var err error
		if change.kind == kindMeta {
			err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tree'`).Scan(&doc)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT doc FROM object WHERE kind = ? AND handle = ?`, string(change.kind), change.handle).Scan(&doc)
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if change.before != nil {
				return nil, ErrConflict
			}
		case err != nil:
			return nil, err
		case change.before == nil || doc != string(change.before):
			return nil, ErrConflict
		}
		if change.after != nil && change.kind != kindMeta {
			var object model.Object
			if err := json.Unmarshal(change.after, &object); err != nil {
				return nil, err
			}
			change.private, change.change = bool(object.Private), object.Change
			// An ID can be taken again after the object went away.
			var other string
			err := tx.QueryRowContext(ctx, `SELECT handle FROM object WHERE kind = ? AND gid = ? AND handle <> ?`, string(change.kind), change.gid, change.handle).Scan(&other)
			if err == nil {
				return nil, ErrConflict
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
	}
	// An object that the undo removes can have got new links after the entry,
	// for example a place that a later event uses. The undo must not leave
	// those links pointing at nothing.
	inEntry := map[string]bool{}
	for _, change := range changes {
		inEntry[recordKey(change.kind, change.handle)] = true
	}
	for _, change := range changes {
		if change.after != nil || change.kind == kindMeta {
			continue
		}
		needle := `"` + change.handle + `"`
		users, err := tx.QueryContext(ctx, `SELECT kind, handle FROM object WHERE instr(doc, ?) > 0`, needle)
		if err != nil {
			return nil, err
		}
		blocked := false
		for users.Next() {
			var kind, handle string
			if err := users.Scan(&kind, &handle); err != nil {
				_ = users.Close()
				return nil, err
			}
			if !inEntry[recordKey(model.Kind(kind), handle)] {
				blocked = true
			}
		}
		if err := users.Close(); err != nil {
			return nil, err
		}
		var meta string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tree'`).Scan(&meta); err == nil && strings.Contains(meta, needle) &&
			!inEntry[recordKey(kindMeta, metaHandle)] {
			blocked = true
		}
		if blocked {
			return nil, ErrConflict
		}
	}
	// The objects go in first, then the tree is read again for the indexes.
	setID, err := s.writeUndo(ctx, tx, actor, subject, id, changes)
	if err != nil {
		return nil, err
	}
	result := &EditResult{ChangeID: setID, Subject: subject}
	if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'revision'`).Scan(&result.Revision); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

func (s *Store) writeUndo(ctx context.Context, tx *sql.Tx, actor Actor, subject string, undoes int64, changes []docChange) (int64, error) {
	for _, change := range changes {
		if change.kind == kindMeta {
			if err := putMeta(ctx, tx, change.after); err != nil {
				return 0, err
			}
			continue
		}
		if change.after == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM object WHERE kind = ? AND handle = ?`, string(change.kind), change.handle); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO object (kind, handle, gid, private, change, doc) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, handle) DO UPDATE SET gid = excluded.gid, private = excluded.private, change = excluded.change, doc = excluded.doc`,
			string(change.kind), change.handle, change.gid, change.private, change.change, string(change.after)); err != nil {
			return 0, err
		}
	}
	tree, _, ids, err := loadTree(ctx, tx)
	if err != nil {
		return 0, err
	}
	// applyChanges writes the same objects again, which changes nothing, and
	// adds the journal entry and the indexes.
	setID, err := s.applyChanges(ctx, tx, actor, "undo", subject, undoes, changes, tree, ids)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE change_set SET undone_by = ? WHERE id = ?`, setID, undoes); err != nil {
		return 0, err
	}
	return setID, nil
}
