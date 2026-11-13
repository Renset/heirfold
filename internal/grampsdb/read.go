// Package grampsdb reads a family tree directly from the SQLite database of
// Gramps 6. It never writes to that database.
package grampsdb

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

	_ "modernc.org/sqlite" // the SQLite driver

	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/model"
)

// Errors that the reader returns.
var (
	// ErrNotFound means that the path holds no Gramps SQLite database.
	ErrNotFound = errors.New("grampsdb: no Gramps SQLite database at this path")
	// ErrUnsupported means that an older Gramps wrote the database. Gramps 6
	// upgrades a tree when it opens that tree.
	ErrUnsupported = errors.New("grampsdb: the database is older than Gramps 6.0; open the tree in Gramps 6 to upgrade it")
	// ErrBusy means that Gramps is in the middle of a change.
	ErrBusy = errors.New("grampsdb: the database is busy")
)

// schemaVersion is the first schema that keeps objects as JSON.
const schemaVersion = 21

// namespace is the XML format that matches the documents of this reader.
const namespace = "http://gramps-project.org/xml/1.7.2/"

// Locate returns the database file for a path. The path can be the file
// itself or the directory of a Gramps tree.
func Locate(path string) (string, error) {
	info, err := os.Stat(path) // #nosec G703 -- the administrator chose this path.
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if info.IsDir() {
		if backend, err := os.ReadFile(filepath.Join(path, "database.txt")); err == nil && strings.TrimSpace(string(backend)) != "sqlite" { // #nosec G304 G703 -- the administrator chose this path.
			return "", fmt.Errorf("%w: the tree uses the %q backend", ErrUnsupported, strings.TrimSpace(string(backend)))
		}
		path = filepath.Join(path, "sqlite.db")
		if _, err := os.Stat(path); err != nil { // #nosec G703 -- the administrator chose this path.
			return "", fmt.Errorf("%w: %s", ErrNotFound, path)
		}
	}
	return path, nil
}

// TreeName returns the name that Gramps shows for the tree, or an empty text.
func TreeName(file string) string {
	name, err := os.ReadFile(filepath.Join(filepath.Dir(file), "name.txt")) // #nosec G304 -- the administrator chose this path.
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(name))
}

// Stamp identifies one state of the database file. Two equal stamps mean that
// the file did not change.
type Stamp struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"modTime"`
}

// Stat returns the stamp of the database file.
func Stat(file string) (Stamp, error) {
	info, err := os.Stat(file)
	if err != nil {
		return Stamp{}, err
	}
	return Stamp{Size: info.Size(), ModTime: info.ModTime().UnixNano()}, nil
}

func open(file string) (*sql.DB, error) {
	query := url.Values{}
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout(2000)")
	query.Add("_pragma", "query_only(ON)")
	u := url.URL{Scheme: "file", OmitHost: true, Path: file, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

type setting struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type reader struct {
	tx      *sql.Tx
	limits  gramps.Limits
	objects int
}

// Read reads the whole tree in one read transaction, so the result is one
// consistent state even while Gramps is open.
func Read(ctx context.Context, path string, limits gramps.Limits) (*gramps.Result, error) {
	file, err := Locate(path)
	if err != nil {
		return nil, err
	}
	db, err := open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, wrap(err)
	}
	defer func() { _ = tx.Rollback() }()

	r := &reader{tx: tx, limits: limits}
	settings, err := r.settings(ctx)
	if err != nil {
		return nil, err
	}
	version := 0
	if raw, ok := settings["version"]; ok {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			version, _ = strconv.Atoi(text)
		}
	}
	if version < schemaVersion {
		return nil, ErrUnsupported
	}

	tree := &model.Tree{Namespace: namespace}
	if err := r.meta(ctx, tree, settings); err != nil {
		return nil, err
	}
	steps := []func() error{
		func() error { return rows(ctx, r, "tag", &tree.Tags, tag) },
		func() error { return rows(ctx, r, "event", &tree.Events, event) },
		func() error { return rows(ctx, r, "person", &tree.People, person) },
		func() error { return rows(ctx, r, "family", &tree.Families, family) },
		func() error { return rows(ctx, r, "citation", &tree.Citations, citation) },
		func() error { return rows(ctx, r, "source", &tree.Sources, source) },
		func() error { return rows(ctx, r, "place", &tree.Places, place) },
		func() error { return rows(ctx, r, "media", &tree.Media, media) },
		func() error { return rows(ctx, r, "repository", &tree.Repositories, repository) },
		func() error { return rows(ctx, r, "note", &tree.Notes, note) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	if tree.HomeHandle != "" {
		found := false
		for i := range tree.People {
			if tree.People[i].Handle == tree.HomeHandle {
				found = true
				break
			}
		}
		if !found {
			tree.HomeHandle = ""
		}
	}
	result := gramps.Check(tree)
	if version > schemaVersion {
		result.WarningsTotal++
		result.Warnings = append(result.Warnings, gramps.Warning{Code: "newer_format"})
	}
	return result, nil
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "SQLITE_BUSY"), strings.Contains(text, "database is locked"):
		return ErrBusy
	case strings.Contains(text, "no such table"), strings.Contains(text, "no such column"):
		return ErrUnsupported
	case strings.Contains(text, "not a database"), strings.Contains(text, "SQLITE_NOTADB"):
		return ErrNotFound
	}
	return fmt.Errorf("grampsdb: %w", err)
}

func (r *reader) settings(ctx context.Context) (map[string]json.RawMessage, error) {
	result, err := r.tx.QueryContext(ctx, `SELECT setting, json_data FROM metadata`)
	if err != nil {
		return nil, wrap(err)
	}
	defer func() { _ = result.Close() }()
	out := map[string]json.RawMessage{}
	for result.Next() {
		var key string
		var doc sql.NullString
		if err := result.Scan(&key, &doc); err != nil {
			return nil, wrap(err)
		}
		if !doc.Valid {
			continue
		}
		var item setting
		if json.Unmarshal([]byte(doc.String), &item) == nil {
			out[key] = item.Value
		}
	}
	return out, wrap(result.Err())
}

// rows reads one table in handle order, which is the order of a Gramps export.
func rows[R, T any](ctx context.Context, r *reader, table string, out *[]T, convert func(*R) T) error {
	// The table name comes from the fixed list in Read.
	result, err := r.tx.QueryContext(ctx, `SELECT json_data FROM `+table+` ORDER BY handle`) // #nosec G202
	if err != nil {
		return wrap(err)
	}
	defer func() { _ = result.Close() }()
	for result.Next() {
		var doc sql.NullString
		if err := result.Scan(&doc); err != nil {
			return wrap(err)
		}
		if !doc.Valid {
			return ErrUnsupported
		}
		r.objects++
		if r.limits.MaxObjects > 0 && r.objects > r.limits.MaxObjects {
			return gramps.ErrTooManyObject
		}
		var raw R
		if err := json.Unmarshal([]byte(doc.String), &raw); err != nil {
			return fmt.Errorf("grampsdb: a record in the table %s is not valid: %w", table, err)
		}
		*out = append(*out, convert(&raw))
	}
	return wrap(result.Err())
}

type rawResearcher struct {
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	Street   string `json:"street"`
	Locality string `json:"locality"`
	City     string `json:"city"`
	State    string `json:"state"`
	Country  string `json:"country"`
	Postal   string `json:"postal"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
}

// bookmarkKeys lists the settings with bookmarks in the order of a Gramps export.
var bookmarkKeys = []struct{ key, target string }{
	{"bookmarks", "person"}, {"family_bookmarks", "family"}, {"event_bookmarks", "event"},
	{"source_bookmarks", "source"}, {"citation_bookmarks", "citation"}, {"place_bookmarks", "place"},
	{"media_bookmarks", "media"}, {"repo_bookmarks", "repository"}, {"note_bookmarks", "note"},
}

func (r *reader) meta(ctx context.Context, tree *model.Tree, settings map[string]json.RawMessage) error {
	if raw, ok := settings["researcher"]; ok {
		var who rawResearcher
		if json.Unmarshal(raw, &who) == nil {
			address := who.Addr
			if address == "" {
				address = who.Street
			}
			researcher := model.Researcher{
				Name: fix(who.Name), Addr: fix(address), Locality: fix(who.Locality), City: fix(who.City),
				State: fix(who.State), Country: fix(who.Country), Postal: fix(who.Postal),
				Phone: fix(who.Phone), Email: fix(who.Email),
			}
			if researcher != (model.Researcher{}) {
				tree.Header.Researcher = &researcher
			}
		}
	}
	if raw, ok := settings["media-path"]; ok {
		var path string
		if json.Unmarshal(raw, &path) == nil {
			tree.Header.MediaPath = fix(path)
		}
	}
	if raw, ok := settings["default-person-handle"]; ok {
		var handle string
		if json.Unmarshal(raw, &handle) == nil && handle != "" {
			tree.HomeHandle = link(handle)
		}
	}
	if raw, ok := settings["name_formats"]; ok {
		var formats [][]any
		if json.Unmarshal(raw, &formats) == nil {
			for _, format := range formats {
				if len(format) < 4 {
					continue
				}
				number, _ := format[0].(float64)
				name, _ := format[1].(string)
				pattern, _ := format[2].(string)
				active := 0
				if on, ok := format[3].(bool); ok && on {
					active = 1
				}
				tree.NameFormats = append(tree.NameFormats, model.NameFormat{
					Number: strconv.Itoa(int(number)), Name: name, FmtStr: pattern, Active: &active,
				})
			}
		}
	}
	for _, item := range bookmarkKeys {
		var handles []string
		if raw, ok := settings[item.key]; ok && json.Unmarshal(raw, &handles) == nil {
			for _, handle := range handles {
				tree.Bookmarks = append(tree.Bookmarks, model.Bookmark{Target: item.target, HLink: link(handle)})
			}
		}
	}
	groups, err := r.tx.QueryContext(ctx, `SELECT name, grouping FROM name_group ORDER BY name`)
	if err != nil {
		return wrap(err)
	}
	defer func() { _ = groups.Close() }()
	for groups.Next() {
		var key, value sql.NullString
		if err := groups.Scan(&key, &value); err != nil {
			return wrap(err)
		}
		tree.NameMaps = append(tree.NameMaps, model.NameMap{Type: "group_as", Key: fix(key.String), Value: fix(value.String)})
	}
	return wrap(groups.Err())
}
