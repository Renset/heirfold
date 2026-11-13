// Package grampstest builds a Gramps tree for tests.
//
// The data holds invented people only. Gramps 6.0.8 made both files from one
// tree: sink.jsonl lists the rows of its SQLite database, and sink-export.xml
// is its own XML export of that database.
package grampstest

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	_ "embed" // the test data
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite" // the SQLite driver
)

//go:embed sink.jsonl
var rows []byte

//go:embed sink-export.xml
var export []byte

// ExportXML returns the XML that Gramps exported from the test tree.
func ExportXML() []byte { return export }

type row struct {
	Table    string `json:"table"`
	Handle   string `json:"handle"`
	JSON     string `json:"json"`
	Setting  string `json:"setting"`
	Name     string `json:"name"`
	Grouping string `json:"grouping"`
}

var objectTables = []string{"person", "family", "event", "place", "source", "citation", "repository", "media", "note", "tag"}

// BuildTree writes a Gramps tree directory and returns its path. A version
// other than "" replaces the schema version of the database.
func BuildTree(t testing.TB, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "database.txt"), []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "name.txt"), []byte("Sink\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range objectTables {
		if _, err := db.ExecContext(context.Background(), `CREATE TABLE `+table+` (handle VARCHAR(50) PRIMARY KEY NOT NULL, json_data TEXT)`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE metadata (setting VARCHAR(50) PRIMARY KEY NOT NULL, json_data TEXT, value BLOB);
CREATE TABLE name_group (name VARCHAR(50) PRIMARY KEY NOT NULL, grouping TEXT)`); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(rows))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var item row
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		switch item.Table {
		case "metadata":
			if item.Setting == "version" && version != "" {
				item.JSON = `{"type":"str","value":"` + version + `"}`
			}
			_, err = db.ExecContext(context.Background(), `INSERT INTO metadata (setting, json_data) VALUES (?, ?)`, item.Setting, item.JSON)
		case "name_group":
			_, err = db.ExecContext(context.Background(), `INSERT INTO name_group (name, grouping) VALUES (?, ?)`, item.Name, item.Grouping)
		default:
			if !slices.Contains(objectTables, item.Table) {
				t.Fatalf("unknown table %q in the test data", item.Table)
			}
			// The table name is one of the fixed names above.
			_, err = db.ExecContext(context.Background(), `INSERT INTO `+item.Table+` (handle, json_data) VALUES (?, ?)`, item.Handle, item.JSON) // #nosec G202
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Exec changes the tree the way an edit in Gramps does.
func Exec(t testing.TB, dir, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "sqlite.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}
