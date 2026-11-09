package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// The card shows the surname from the marriage and keeps the surname at
// birth. A guest does not see a private name, so a private married name
// leaves the guest with the surname at birth.
func TestCardShowsTheMarriedSurnameAndTheSurnameAtBirth(t *testing.T) {
	tree := editTree()
	for i := range tree.People {
		if tree.People[i].Handle == "_olga" {
			tree.People[i].Names = []model.Name{
				{Type: model.NameBirth, First: "Olga", Surnames: []model.Surname{{Value: "Lebedeva"}}},
				{Type: model.NameMarried, Alt: true, Private: true, First: "Olga", Surnames: []model.Surname{{Value: "Orlova"}}},
			}
		}
	}
	st, _ := editStore(t, tree)
	full := cardByID(fullGraph(t, st, privacy.Full), "I0010")
	if full.Surname != "Orlova" || full.BirthSurname != "Lebedeva" || !full.Married || full.Given != "Olga" {
		t.Fatalf("full card: %+v", full)
	}
	public := cardByID(fullGraph(t, st, privacy.Public), "I0010")
	if public == nil {
		t.Skip("the test tree hides this person from guests")
	}
	if public.Surname != "Lebedeva" || public.BirthSurname != "" || public.Married {
		t.Fatalf("public card: %+v", public)
	}
}

// A store from older rules rebuilds the cards once when it opens, so a tree
// that nobody edits gets the new surnames too.
func TestStoreRebuildsOldCardsWhenItOpens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	tree := editTree()
	for i := range tree.People {
		if tree.People[i].Handle == "_olga" {
			tree.People[i].Names = append(tree.People[i].Names,
				model.Name{Type: model.NameMarried, Alt: true, First: "Olga", Surnames: []model.Surname{{Value: "Orlova"}}})
		}
	}
	if _, err := st.ReplaceTree(ctx, tree); err != nil {
		t.Fatal(err)
	}
	// The cards of an older version knew no surname at birth.
	if _, err := st.w.ExecContext(ctx, `UPDATE person_index SET card_full = json_remove(json_set(card_full, '$.surname', 'Lebedeva'), '$.birthSurname', '$.married')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.w.ExecContext(ctx, `DELETE FROM meta WHERE key = 'index_version'`); err != nil {
		t.Fatal(err)
	}
	before := revision(t, st)
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0010"); card.Surname != "Lebedeva" || card.BirthSurname != "" {
		t.Fatalf("the old card: %+v", card)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0010"); card.Surname != "Orlova" || card.BirthSurname != "Lebedeva" {
		t.Fatalf("the card after the store opened: %+v", card)
	}
	// The pages load the tree again, because the revision changed.
	if revision(t, st) == before {
		t.Fatalf("the revision stayed %d", before)
	}
	// A second opening rebuilds nothing.
	after := revision(t, st)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if st, err = Open(ctx, path); err != nil {
		t.Fatal(err)
	}
	if revision(t, st) != after {
		t.Fatalf("the second opening changed the revision")
	}
}
