package grampsdb

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/model"
)

// compareTrees reports every object that differs between two trees.
func compareTrees(t *testing.T, got, want *model.Tree) {
	t.Helper()
	normalize := func(tree *model.Tree) {
		tree.Header.Created = model.Created{}
		if tree.Header.Researcher != nil && *tree.Header.Researcher == (model.Researcher{}) {
			tree.Header.Researcher = nil
		}
	}
	normalize(got)
	normalize(want)
	gotMeta, _ := json.Marshal(got.Meta())
	wantMeta, _ := json.Marshal(want.Meta())
	if string(gotMeta) != string(wantMeta) {
		t.Errorf("metadata differs\n got: %s\nwant: %s", gotMeta, wantMeta)
	}
	gotRecords, err := got.Records()
	if err != nil {
		t.Fatal(err)
	}
	wantRecords, err := want.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(gotRecords) != len(wantRecords) {
		t.Errorf("got %d objects, want %d", len(gotRecords), len(wantRecords))
	}
	index := map[string]string{}
	for _, record := range wantRecords {
		index[string(record.Kind)+record.Handle] = string(record.Doc)
	}
	shown := map[model.Kind]int{}
	for _, record := range gotRecords {
		expected, ok := index[string(record.Kind)+record.Handle]
		if ok && expected == string(record.Doc) {
			continue
		}
		shown[record.Kind]++
		if shown[record.Kind] <= 3 {
			t.Errorf("%s %s differs\n got: %s\nwant: %s", record.Kind, record.ID, record.Doc, expected)
		}
	}
	for kind, count := range shown {
		t.Errorf("%d objects of kind %s differ", count, kind)
	}
}

// TestOracle compares a direct read with the XML that Gramps exported from the
// same database. Set both variables to run it against a tree of your own:
//
//	HEIRFOLD_ORACLE_DB=/path/to/tree HEIRFOLD_ORACLE_XML=/path/to/export.gramps go test ./internal/grampsdb -run Oracle
func TestOracle(t *testing.T) {
	db, file := os.Getenv("HEIRFOLD_ORACLE_DB"), os.Getenv("HEIRFOLD_ORACLE_XML")
	if db == "" || file == "" {
		t.Skip("HEIRFOLD_ORACLE_DB and HEIRFOLD_ORACLE_XML are not set")
	}
	direct, err := Read(context.Background(), db, gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	export, err := os.Open(file) // #nosec G304 -- the developer chose this path.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = export.Close() }()
	parsed, err := gramps.Parse(export, gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	compareTrees(t, direct.Tree, parsed.Tree)
	if direct.WarningsTotal != parsed.WarningsTotal {
		t.Errorf("got %d warnings, want %d", direct.WarningsTotal, parsed.WarningsTotal)
	}
}
