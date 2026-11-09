// Package store keeps a family tree and its users in one SQLite file.
//
// Read access to genealogical data goes through View. A View always applies a
// privacy scope, and the package exports no other way to read that data for a
// viewer. See docs/adr/0003.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// The pure Go SQLite driver keeps the build free of cgo.
	_ "modernc.org/sqlite"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// ErrNotFound means that the record does not exist or the viewer cannot see it.
var ErrNotFound = errors.New("store: not found")

// Store is an open database.
type Store struct {
	w   *sql.DB // one connection, for all writes
	r   *sql.DB // a read-only pool
	now func() time.Time
}

// Open opens or creates the database file and applies the migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	s := &Store{w: w, r: r, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.refreshIndex(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store: rebuild the index: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("store: set file mode: %w", err)
	}
	return s, nil
}

func dsn(path string, readOnly bool) string {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Add("_pragma", "foreign_keys(ON)")
	if readOnly {
		query.Add("_pragma", "query_only(ON)")
	} else {
		query.Set("_txlock", "immediate")
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}
	return u.String()
}

// Close closes the database.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.w.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("store: the database has schema %d, and this program knows schema %d; use a newer program", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.w.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", i+1, err)
		}
		// PRAGMA takes no bound parameters; the value is an integer.
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Card is the short form of a person.
type Card struct {
	ID    string `json:"id"`
	Given string `json:"given,omitempty"`
	// Surname is the surname that the person carried last: see model.Surnames.
	Surname string `json:"surname,omitempty"`
	// BirthSurname is the surname at birth when it differs from Surname.
	BirthSurname string `json:"birthSurname,omitempty"`
	// Married is true when Surname comes from a name from a marriage.
	Married    bool            `json:"married,omitempty"`
	Patronymic string          `json:"patronymic,omitempty"`
	Gender     string          `json:"gender,omitempty"`
	Birth      *model.DateView `json:"birth,omitempty"`
	Death      *model.DateView `json:"death,omitempty"`
	Living     bool            `json:"living,omitempty"`
	Photo      *Photo          `json:"photo,omitempty"`
}

// ReplaceTree replaces the whole tree in one transaction. The second result
// is the number of people that Heirfold treats as living. The journal of
// edits belongs to the old tree, so it goes away with that tree.
func (s *Store) ReplaceTree(ctx context.Context, t *model.Tree) (int, error) {
	records, err := t.Records()
	if err != nil {
		return 0, err
	}
	ids := assignIDs(records)
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"object", "change_set"} {
		// The table names come from the constant list above.
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil { // #nosec G202
			return 0, err
		}
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO object (kind, handle, gid, private, change, doc) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = insert.Close() }()
	for _, rec := range records {
		if _, err := insert.ExecContext(ctx, string(rec.Kind), rec.Handle, ids[recordKey(rec.Kind, rec.Handle)], rec.Private, rec.Change, string(rec.Doc)); err != nil {
			return 0, fmt.Errorf("store: insert object: %w", err)
		}
	}
	livingCount, err := s.reindex(ctx, tx, t, ids)
	if err != nil {
		return 0, err
	}
	if err := writeMeta(ctx, tx, t.Meta()); err != nil {
		return 0, err
	}
	if err := bumpRevision(ctx, tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return livingCount, nil
}

func writeMeta(ctx context.Context, tx *sql.Tx, meta model.Meta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('tree', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(data))
	return err
}

// reindex rebuilds the tables that the views read from the given tree. ids
// maps each record to its unique ID. It returns the number of living people.
func (s *Store) reindex(ctx context.Context, tx *sql.Tx, t *model.Tree, ids map[string]string) (int, error) {
	living := privacy.Living(t, s.now().Year())
	events := make(map[string]*model.Event, len(t.Events))
	for i := range t.Events {
		events[t.Events[i].Handle] = &t.Events[i]
	}
	media := make(map[string]*model.Media, len(t.Media))
	for i := range t.Media {
		media[t.Media[i].Handle] = &t.Media[i]
	}
	for _, table := range []string{"person_index", "family_index", "family_child"} {
		// The table names come from the constant list above.
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil { // #nosec G202
			return 0, err
		}
	}

	insertPerson, err := tx.PrepareContext(ctx, `INSERT INTO person_index (handle, gid, private, living, gender, card_full, card_public) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = insertPerson.Close() }()
	livingCount := 0
	// A guest gets no portrait from a file that a living person leads to.
	restricted := mediaOfLiving(t, living)
	for i := range t.People {
		person := &t.People[i]
		gid := ids[recordKey(model.KindPerson, person.Handle)]
		isLiving := living[person.Handle]
		if isLiving {
			livingCount++
		}
		full, public := personCards(person, gid, events, isLiving)
		full.Photo = personPhoto(person, media, ids, nil)
		public.Photo = personPhoto(person, media, ids, restricted)
		fullJSON, err := json.Marshal(full)
		if err != nil {
			return 0, err
		}
		publicJSON, err := json.Marshal(public)
		if err != nil {
			return 0, err
		}
		if _, err := insertPerson.ExecContext(ctx, person.Handle, gid, bool(person.Private), isLiving, person.Gender, string(fullJSON), string(publicJSON)); err != nil {
			return 0, fmt.Errorf("store: insert person: %w", err)
		}
	}

	insertFamily, err := tx.PrepareContext(ctx, `INSERT INTO family_index (handle, gid, private, rel, father, mother, married_full, married_public) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = insertFamily.Close() }()
	insertChild, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO family_child (family, child, ord, private) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = insertChild.Close() }()
	for i := range t.Families {
		family := &t.Families[i]
		gid := ids[recordKey(model.KindFamily, family.Handle)]
		rel, father, mother := "", "", ""
		if family.Rel != nil {
			rel = family.Rel.Type
		}
		if family.Father != nil {
			father = family.Father.HLink
		}
		if family.Mother != nil {
			mother = family.Mother.HLink
		}
		full, public := marriageDates(family, events)
		if _, err := insertFamily.ExecContext(ctx, family.Handle, gid, bool(family.Private), rel, father, mother, full, public); err != nil {
			return 0, fmt.Errorf("store: insert family: %w", err)
		}
		for ord, child := range family.ChildRefs {
			if _, err := insertChild.ExecContext(ctx, family.Handle, child.HLink, ord, bool(child.Private)); err != nil {
				return 0, fmt.Errorf("store: insert child: %w", err)
			}
		}
	}
	if err := writeMediaIndex(ctx, tx, t, ids, living); err != nil {
		return 0, fmt.Errorf("store: index media: %w", err)
	}
	return livingCount, nil
}

// bumpRevision marks a change of the tree. A client compares the number to
// know when to load the tree again.
func bumpRevision(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('revision', '1')
ON CONFLICT (key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)`)
	return err
}

// Revision returns the number of the current state of the tree.
func (s *Store) Revision(ctx context.Context) (int64, error) {
	var text string
	err := s.r.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'revision'`).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(text, 10, 64)
}

// MediaPath returns the media directory that the tree names in its header.
func (s *Store) MediaPath(ctx context.Context) (string, error) {
	text, ok, err := s.MetaGet(ctx, "tree")
	if err != nil || !ok {
		return "", err
	}
	var meta model.Meta
	if err := json.Unmarshal([]byte(text), &meta); err != nil {
		return "", err
	}
	return meta.Header.MediaPath, nil
}

// MetaGet returns a stored setting.
func (s *Store) MetaGet(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.r.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// MetaSet stores a setting.
func (s *Store) MetaSet(ctx context.Context, key, value string) error {
	_, err := s.w.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func recordKey(kind model.Kind, handle string) string {
	return string(kind) + "\x00" + handle
}

// assignIDs returns an ID for every record that is unique within its kind.
// Gramps permits duplicate IDs; a web address needs a unique one.
func assignIDs(records []model.Record) map[string]string {
	out := make(map[string]string, len(records))
	used := make(map[string]struct{}, len(records))
	next := make(map[string]int)
	for _, rec := range records {
		id := rec.ID
		if id == "" {
			id = rec.Handle
		}
		base := string(rec.Kind) + "\x00" + id
		candidate := id
		for n := max(next[base], 2); ; n++ {
			key := string(rec.Kind) + "\x00" + candidate
			if _, taken := used[key]; !taken {
				used[key] = struct{}{}
				next[base] = n
				break
			}
			candidate = id + "-" + strconv.Itoa(n)
		}
		out[recordKey(rec.Kind, rec.Handle)] = candidate
	}
	return out
}

// primaryEvent returns the first visible event of one group for a person.
func primaryEvent(person *model.Person, events map[string]*model.Event, match func(string) bool, publicOnly bool) *model.Event {
	for _, ref := range person.EventRefs {
		if ref.Role != "" && !strings.EqualFold(ref.Role, "Primary") {
			continue
		}
		if publicOnly && bool(ref.Private) {
			continue
		}
		event := events[ref.HLink]
		if event == nil || (publicOnly && bool(event.Private)) || !match(event.Type) {
			continue
		}
		return event
	}
	return nil
}

func isType(want string) func(string) bool {
	return func(t string) bool { return strings.EqualFold(strings.TrimSpace(t), want) }
}

func personCards(person *model.Person, gid string, events map[string]*model.Event, living bool) (full, public Card) {
	build := func(publicOnly bool) Card {
		card := Card{ID: gid, Gender: person.Gender, Living: living}
		var name model.Name
		found := false
		visible := make([]model.Name, 0, len(person.Names))
		for _, candidate := range person.Names {
			if publicOnly && bool(candidate.Private) {
				continue
			}
			visible = append(visible, candidate)
			if !found || (!bool(candidate.Alt) && bool(name.Alt)) {
				name = candidate
				found = true
			}
		}
		if found {
			card.Given = strings.TrimSpace(name.First)
			_, card.Patronymic = model.SplitName(name)
			card.Surname, card.BirthSurname, card.Married = model.Surnames(visible)
		}
		if event := primaryEvent(person, events, isType("birth"), publicOnly); event != nil {
			card.Birth = event.View()
		} else if event := primaryEvent(person, events, isType("baptism"), publicOnly); event != nil {
			card.Birth = event.View()
		}
		if event := primaryEvent(person, events, isType("death"), publicOnly); event != nil {
			card.Death = event.View()
		} else if event := primaryEvent(person, events, isType("burial"), publicOnly); event != nil {
			card.Death = event.View()
		}
		return card
	}
	return build(false), build(true)
}

func marriageDates(family *model.Family, events map[string]*model.Event) (full, public string) {
	encode := func(publicOnly bool) string {
		for _, ref := range family.EventRefs {
			if publicOnly && bool(ref.Private) {
				continue
			}
			event := events[ref.HLink]
			if event == nil || (publicOnly && bool(event.Private)) || !strings.EqualFold(event.Type, "Marriage") {
				continue
			}
			view := event.View()
			if view == nil {
				return ""
			}
			data, err := json.Marshal(view)
			if err != nil {
				return ""
			}
			return string(data)
		}
		return ""
	}
	return encode(false), encode(true)
}

// HasTree reports whether a tree exists.
func (s *Store) HasTree(ctx context.Context) (bool, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM object)`).Scan(&n)
	return n == 1, err
}

// Counts returns the number of objects of each kind, without any privacy
// filter. Only administration pages use it.
func (s *Store) Counts(ctx context.Context) (model.Counts, error) {
	var counts model.Counts
	rows, err := s.r.QueryContext(ctx, `SELECT kind, count(*) FROM object GROUP BY kind`)
	if err != nil {
		return counts, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return counts, err
		}
		switch model.Kind(kind) {
		case model.KindPerson:
			counts.People = n
		case model.KindFamily:
			counts.Families = n
		case model.KindEvent:
			counts.Events = n
		case model.KindPlace:
			counts.Places = n
		case model.KindSource:
			counts.Sources = n
		case model.KindCitation:
			counts.Citations = n
		case model.KindRepository:
			counts.Repositories = n
		case model.KindNote:
			counts.Notes = n
		case model.KindMedia:
			counts.Media = n
		case model.KindTag:
			counts.Tags = n
		}
	}
	return counts, rows.Err()
}

// ExportTree loads the complete tree with no privacy filter. Only an
// administrator can reach it, through the export endpoint.
func (s *Store) ExportTree(ctx context.Context) (*model.Tree, error) {
	t, _, _, err := loadTree(ctx, s.r)
	return t, err
}

// querier is the read part that a database and a transaction share.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// loadTree reads the whole tree. The second result maps each record to its
// stored document, and the third result maps each record to its unique ID.
func loadTree(ctx context.Context, q querier) (*model.Tree, map[string]string, map[string]string, error) {
	t := &model.Tree{}
	docs := map[string]string{}
	ids := map[string]string{}
	var meta string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tree'`).Scan(&meta)
	if errors.Is(err, sql.ErrNoRows) {
		return t, docs, ids, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var m model.Meta
	docs[recordKey(kindMeta, metaHandle)] = meta
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		return nil, nil, nil, err
	}
	t.Namespace, t.Header, t.NameFormats, t.HomeHandle, t.DefaultID, t.Bookmarks, t.NameMaps =
		m.Namespace, m.Header, m.NameFormats, m.HomeHandle, m.DefaultID, m.Bookmarks, m.NameMaps

	rows, err := q.QueryContext(ctx, `SELECT kind, handle, gid, doc FROM object ORDER BY kind, gid`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, handle, gid, doc string
		if err := rows.Scan(&kind, &handle, &gid, &doc); err != nil {
			return nil, nil, nil, err
		}
		key := recordKey(model.Kind(kind), handle)
		docs[key], ids[key] = doc, gid
		data := []byte(doc)
		switch model.Kind(kind) {
		case model.KindTag:
			err = appendDoc(data, &t.Tags)
		case model.KindEvent:
			err = appendDoc(data, &t.Events)
		case model.KindPerson:
			err = appendDoc(data, &t.People)
		case model.KindFamily:
			err = appendDoc(data, &t.Families)
		case model.KindCitation:
			err = appendDoc(data, &t.Citations)
		case model.KindSource:
			err = appendDoc(data, &t.Sources)
		case model.KindPlace:
			err = appendDoc(data, &t.Places)
		case model.KindMedia:
			err = appendDoc(data, &t.Media)
		case model.KindRepository:
			err = appendDoc(data, &t.Repositories)
		case model.KindNote:
			err = appendDoc(data, &t.Notes)
		}
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return t, docs, ids, rows.Err()
}

func appendDoc[T any](data []byte, out *[]T) error {
	var item T
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	*out = append(*out, item)
	return nil
}

// RefreshLiving recomputes who is treated as living. The answer changes with
// the calendar, so the server runs this on every start and once a day.
func (s *Store) RefreshLiving(ctx context.Context) error {
	t, err := s.ExportTree(ctx)
	if err != nil {
		return err
	}
	if len(t.People) == 0 {
		return nil
	}
	living := privacy.Living(t, s.now().Year())
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	update, err := tx.PrepareContext(ctx, `UPDATE person_index SET living = ?, card_full = json_set(card_full, '$.living', json(?)), card_public = json_set(card_public, '$.living', json(?)) WHERE handle = ? AND living <> ?`)
	if err != nil {
		return err
	}
	defer func() { _ = update.Close() }()
	changed := int64(0)
	for i := range t.People {
		handle := t.People[i].Handle
		value := living[handle]
		text := strconv.FormatBool(value)
		result, err := update.ExecContext(ctx, value, text, text, handle, value)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err == nil {
			changed += n
		}
	}
	// The files follow the people: a file that only the dead lead to opens for guests.
	restricted := mediaOfLiving(t, living)
	for i := range t.Media {
		handle := t.Media[i].Handle
		result, err := tx.ExecContext(ctx, `UPDATE media_index SET living = ? WHERE handle = ? AND living <> ?`, restricted[handle], handle, restricted[handle])
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err == nil {
			changed += n
		}
	}
	if changed > 0 {
		if err := bumpRevision(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// View returns read access limited to the given scope.
func (s *Store) View(scope privacy.Scope) *View {
	return &View{db: s.r, scope: scope}
}

// indexVersion names the rules that build the tables of the views from the
// tree: the cards of people and the index of media. A change of a rule raises
// it, and the store rebuilds the tables once when it opens. Without that a
// tree that nobody edits keeps old cards.
const indexVersion = "1"

// refreshIndex rebuilds the tables of the views when they come from older rules.
func (s *Store) refreshIndex(ctx context.Context) error {
	var stored string
	err := s.w.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'index_version'`).Scan(&stored)
	if err == nil && stored == indexVersion {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tree, _, ids, err := loadTree(ctx, tx)
	if err != nil {
		return err
	}
	if len(tree.People) > 0 || len(tree.Media) > 0 {
		if _, err := s.reindex(ctx, tx, tree, ids); err != nil {
			return err
		}
		if err := bumpRevision(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('index_version', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, indexVersion); err != nil {
		return err
	}
	return tx.Commit()
}
