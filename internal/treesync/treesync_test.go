package treesync

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/grampsdb/grampstest"
	"github.com/Renset/heirfold/internal/privacy"
	"github.com/Renset/heirfold/internal/store"
)

func newSyncer(t *testing.T, tree string) (*Syncer, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "heirfold.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	syncer, err := New(st, Options{Path: tree}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return syncer, st
}

// touch gives the database file a new modification time, as a write by Gramps does.
func touch(t *testing.T, tree string, offset time.Duration) {
	t.Helper()
	at := time.Now().Add(offset)
	if err := os.Chtimes(filepath.Join(tree, "sqlite.db"), at, at); err != nil {
		t.Fatal(err)
	}
}

func TestSyncFollowsTheGrampsTree(t *testing.T) {
	ctx := context.Background()
	tree := grampstest.BuildTree(t, "")
	syncer, st := newSyncer(t, tree)

	changed, err := syncer.Sync(ctx, false)
	if err != nil || !changed {
		t.Fatalf("first sync: changed=%t err=%v", changed, err)
	}
	first, err := st.Revision(ctx)
	if err != nil || first == 0 {
		t.Fatalf("revision %d, %v", first, err)
	}
	status := syncer.Status()
	if status.Source != "gramps" || status.Name != "Sink" || status.Counts == nil || status.Counts.People != 5 || status.Error != "" {
		t.Fatalf("status: %+v", status)
	}

	// The file did not change, so the next check reads nothing.
	if changed, err := syncer.Sync(ctx, false); err != nil || changed {
		t.Fatalf("second sync: changed=%t err=%v", changed, err)
	}
	// A new time with the same content does not replace the tree.
	touch(t, tree, time.Hour)
	if changed, err := syncer.Sync(ctx, false); err != nil || changed {
		t.Fatalf("touched file: changed=%t err=%v", changed, err)
	}
	if again, _ := st.Revision(ctx); again != first {
		t.Fatalf("revision moved from %d to %d without a change", first, again)
	}

	// An edit in Gramps reaches Heirfold.
	grampstest.Exec(t, tree, `UPDATE person SET json_data = replace(json_data, '"first_name":"Pip"', '"first_name":"Pippin"') WHERE handle = 'i0003'`)
	touch(t, tree, 2*time.Hour)
	if changed, err := syncer.Sync(ctx, false); err != nil || !changed {
		t.Fatalf("edited tree: changed=%t err=%v", changed, err)
	}
	if next, _ := st.Revision(ctx); next <= first {
		t.Fatalf("revision stayed at %d after a change", next)
	}
	detail, err := st.View(privacy.Full).Person(ctx, "I0003")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Given != "Pippin" {
		t.Fatalf("given name %q", detail.Given)
	}

	// A person that Gramps deleted leaves Heirfold too.
	grampstest.Exec(t, tree, `DELETE FROM person WHERE handle = 'i0003'`)
	touch(t, tree, 3*time.Hour)
	if changed, err := syncer.Sync(ctx, false); err != nil || !changed {
		t.Fatalf("deleted person: changed=%t err=%v", changed, err)
	}
	if _, err := st.View(privacy.Full).Person(ctx, "I0003"); err != store.ErrNotFound {
		t.Fatalf("deleted person is still there: %v", err)
	}
}

func TestSyncSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	tree := grampstest.BuildTree(t, "")
	path := filepath.Join(t.TempDir(), "heirfold.db")
	open := func() (*Syncer, *store.Store) {
		st, err := store.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		syncer, err := New(st, Options{Path: tree}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		return syncer, st
	}
	syncer, st := open()
	if changed, err := syncer.Sync(ctx, false); err != nil || !changed {
		t.Fatalf("first sync: changed=%t err=%v", changed, err)
	}
	_ = st.Close()

	syncer, st = open()
	defer func() { _ = st.Close() }()
	if changed, err := syncer.Sync(ctx, false); err != nil || changed {
		t.Fatalf("sync after restart: changed=%t err=%v", changed, err)
	}
	if status := syncer.Status(); status.Counts == nil || status.SyncedAt == 0 {
		t.Fatalf("status after restart: %+v", status)
	}
	// A forced sync reads the database but keeps an equal tree.
	if changed, err := syncer.Sync(ctx, true); err != nil || changed {
		t.Fatalf("forced sync: changed=%t err=%v", changed, err)
	}
}

func TestSyncReportsAProblemAndRecovers(t *testing.T) {
	ctx := context.Background()
	tree := grampstest.BuildTree(t, "")
	syncer, _ := newSyncer(t, tree)
	if _, err := syncer.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tree, "sqlite.db")
	if err := os.Rename(file, file+".away"); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.Sync(ctx, false); err == nil {
		t.Fatal("a missing file must give an error")
	}
	if status := syncer.Status(); status.Error != "not_found" || status.Counts == nil {
		t.Fatalf("status: %+v", status)
	}
	if err := os.Rename(file+".away", file); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	if status := syncer.Status(); status.Error != "" {
		t.Fatalf("status: %+v", status)
	}
}

func TestStatusHoldsNoNames(t *testing.T) {
	tree := grampstest.BuildTree(t, "")
	syncer, _ := newSyncer(t, tree)
	if _, err := syncer.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	syncer.log = slog.New(slog.NewTextHandler(&log, nil))
	if _, err := syncer.Sync(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	grampstest.Exec(t, tree, `UPDATE person SET json_data = replace(json_data, 'Orlin', 'Orlando')`)
	touch(t, tree, time.Hour)
	if _, err := syncer.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Orlando", "Quillfeather", "Velda"} {
		if strings.Contains(log.String(), name) {
			t.Fatalf("the log holds the name %s", name)
		}
	}
	if !strings.Contains(log.String(), "people=5") {
		t.Fatalf("the log must hold the counts: %s", log.String())
	}
}
