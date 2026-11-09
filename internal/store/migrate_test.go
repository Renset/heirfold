package store

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNewDatabaseHasTheSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := st.r.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("user_version %d, want %d (%v)", version, len(migrations), err)
	}
	for _, table := range []string{"meta", "object", "person_index", "family_index", "family_child", "media_index",
		"user", "session", "login_code", "audit", "change_set", "change_item", "face", "face_scan", "media_info"} {
		if countRows(t, st, "sqlite_master WHERE type = 'table' AND name = '"+table+"'") != 1 {
			t.Errorf("the database has no table %s", table)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// A second start changes nothing.
	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.r.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != len(migrations) {
		t.Fatalf("user_version after a second start: %d (%v)", version, err)
	}
	// A database of a newer program stays closed: this program does not know its tables.
	if _, err := st.w.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(len(migrations)+1)); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, path); err == nil || !strings.Contains(err.Error(), "newer program") {
		t.Fatalf("a database of a newer program opened: %v", err)
	}
}
