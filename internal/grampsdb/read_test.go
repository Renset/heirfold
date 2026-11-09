package grampsdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/grampsdb/grampstest"
)

func fileSum(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- a test file.
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func TestReadMatchesTheGrampsExport(t *testing.T) {
	dir := grampstest.BuildTree(t, "")
	before := fileSum(t, filepath.Join(dir, "sqlite.db"))
	direct, err := Read(context.Background(), dir, gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := gramps.Parse(bytes.NewReader(grampstest.ExportXML()), gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	counts := direct.Tree.Counts()
	if counts.People != 5 || counts.Media != 4 || counts.Notes != 4 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
	if direct.Tree.HomeHandle != "_i0001" || direct.Tree.Header.MediaPath != "/srv/invented-media" {
		t.Fatalf("metadata: home %q, media path %q", direct.Tree.HomeHandle, direct.Tree.Header.MediaPath)
	}
	compareTrees(t, direct.Tree, parsed.Tree)
	if direct.WarningsTotal != 0 {
		t.Errorf("warnings: %+v", direct.Warnings)
	}

	// The reader must leave the database of Gramps exactly as it was.
	if after := fileSum(t, filepath.Join(dir, "sqlite.db")); after != before {
		t.Error("the read changed the database file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("the read left extra files: %v", entries)
	}
}

func TestReadAcceptsTheDatabaseFile(t *testing.T) {
	dir := grampstest.BuildTree(t, "")
	result, err := Read(context.Background(), filepath.Join(dir, "sqlite.db"), gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tree.Counts().People != 5 {
		t.Fatal("wrong tree")
	}
	if got := TreeName(filepath.Join(dir, "sqlite.db")); got != "Sink" {
		t.Fatalf("tree name %q", got)
	}
}

func TestReadRefusesWhatItCannotRead(t *testing.T) {
	ctx := context.Background()
	if _, err := Read(ctx, filepath.Join(t.TempDir(), "missing"), gramps.DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing path: %v", err)
	}
	if _, err := Read(ctx, t.TempDir(), gramps.DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty directory: %v", err)
	}
	old := grampstest.BuildTree(t, "20")
	if _, err := Read(ctx, old, gramps.DefaultLimits); !errors.Is(err, ErrUnsupported) {
		t.Errorf("old schema: %v", err)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "database.txt"), []byte("postgresql"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ctx, other, gramps.DefaultLimits); !errors.Is(err, ErrUnsupported) {
		t.Errorf("other backend: %v", err)
	}
	text := filepath.Join(t.TempDir(), "sqlite.db")
	if err := os.WriteFile(text, []byte("this is not a database, it is only a long line of plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ctx, text, gramps.DefaultLimits); !errors.Is(err, ErrNotFound) {
		t.Errorf("not a database: %v", err)
	}
	if _, err := Read(ctx, grampstest.BuildTree(t, ""), gramps.Limits{MaxObjects: 10}); !errors.Is(err, gramps.ErrTooManyObject) {
		t.Errorf("object limit: %v", err)
	}
}

func TestNewerSchemaGivesAWarning(t *testing.T) {
	result, err := Read(context.Background(), grampstest.BuildTree(t, "22"), gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if result.WarningsTotal != 1 || result.Warnings[0].Code != "newer_format" {
		t.Fatalf("warnings: %+v", result.Warnings)
	}
}

func TestStatSeesAChange(t *testing.T) {
	dir := grampstest.BuildTree(t, "")
	file := filepath.Join(dir, "sqlite.db")
	first, err := Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	second, err := Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("a new modification time must give a new stamp")
	}
}
