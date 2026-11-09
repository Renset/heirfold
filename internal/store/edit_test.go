package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// elena and egor are the editors of the tests.
var (
	elena = Actor{ID: 7, Name: "Elena Editor"}
	egor  = Actor{ID: 8, Name: "Egor Editor"}
)

func flag(v bool) *model.Flag {
	f := model.Flag(v)
	return &f
}

func primaryRef(handle string) model.EventRef { return model.EventRef{HLink: handle, Role: "Primary"} }

// editTree is a small invented family. All its people are dead, so a guest
// sees them.
//
//	Ivan (I0001) + Maria (I0002) = family F0001 with the child Pyotr (I0003).
//	Olga (I0010) is no relative. She names Ivan in an association.
func editTree() *model.Tree {
	t := &model.Tree{Namespace: "http://gramps-project.org/xml/1.7.2/", HomeHandle: "_ivan"}
	t.Header = model.Header{Created: model.Created{Date: "2026-01-01", Version: "6.0.8"}}
	t.Bookmarks = []model.Bookmark{{Target: "person", HLink: "_ivan"}, {Target: "person", HLink: "_olga"}}
	t.Sources = []model.Source{{Object: obj("_s1", "S0001", false), Title: "Parish book"}}
	t.Citations = []model.Citation{{Object: obj("_c1", "C0001", false), Page: "page 12", Confidence: 2, Source: model.Ref{HLink: "_s1"}}}
	t.Places = []model.Place{{Object: obj("_p1", "P0001", false), Type: "Town", Title: "Old Town", Names: []model.PlaceName{{Value: "Old Town"}}}}
	t.Media = []model.Media{{Object: obj("_o1", "O0001", false), File: model.MediaFile{Src: "photos/old.jpg", Mime: "image/jpeg", Description: "Old photo"}}}
	t.Notes = []model.Note{
		{Object: obj("_n1", "N0001", false), Type: "Person Note", Text: "A note with a bold word.",
			Styles: []model.NoteStyle{{Name: "bold", Ranges: []model.StyleRange{{Start: 14, End: 18}}}}},
		{Object: obj("_n2", "N0002", false), Type: "General", Text: "A note that two people share."},
	}
	t.Events = []model.Event{
		{Object: obj("_e1", "E0001", false), Type: "Birth", Dated: dated("1850"), Place: &model.Ref{HLink: "_p1"}},
		{Object: obj("_e2", "E0002", false), Type: "Death", Dated: dated("1910")},
		{Object: obj("_e3", "E0003", false), Type: "Marriage", Dated: dated("1875")},
		{Object: obj("_e4", "E0004", false), Type: "Census", Dated: dated("1897")},
		{Object: obj("_e5", "E0005", false), Type: "Birth", Dated: dated("1855")},
		{Object: obj("_e6", "E0006", false), Type: "Death", Dated: dated("1920")},
		{Object: obj("_e7", "E0007", false), Type: "Birth", Dated: dated("1880")},
		{Object: obj("_e8", "E0008", false), Type: "Death", Dated: dated("1950")},
		{Object: obj("_e9", "E0009", false), Type: "Birth", Dated: dated("1860")},
		{Object: obj("_e10", "E0010", false), Type: "Death", Dated: dated("1930")},
	}
	t.People = []model.Person{
		{
			Object: obj("_ivan", "I0001", false), Gender: "M",
			Names: []model.Name{
				{
					Type: "Birth Name", First: "Ivan", Group: "Orlov group", FamilyNick: "Orly",
					Surnames: []model.Surname{
						{Value: "Orlov", Primary: flag(true)},
						{Value: "Sokolov", Primary: flag(false), Connector: "-"},
						{Value: "Petrovich", Primary: flag(false), Derivation: "Patronymic"},
					},
					Notes: []model.Ref{{HLink: "_n2"}}, Citations: []model.Ref{{HLink: "_c1"}},
				},
				{Alt: true, Type: "Also Known As", First: "John", Nick: "Johnny", Surnames: []model.Surname{{Value: "Orloff"}}},
			},
			EventRefs:  []model.EventRef{primaryRef("_e1"), primaryRef("_e2"), primaryRef("_e4")},
			MediaRefs:  []model.MediaRef{{HLink: "_o1"}},
			Attributes: []model.Attribute{{Type: "Occupation", Value: "Miller"}},
			ParentIn:   []model.Ref{{HLink: "_f1"}},
			Notes:      []model.Ref{{HLink: "_n1"}, {HLink: "_n2"}},
			Citations:  []model.Ref{{HLink: "_c1"}},
		},
		{
			Object: obj("_maria", "I0002", false), Gender: "F", Names: name("Maria", "Orlova"),
			EventRefs: []model.EventRef{primaryRef("_e5"), primaryRef("_e6"), primaryRef("_e4")},
			ParentIn:  []model.Ref{{HLink: "_f1"}}, Notes: []model.Ref{{HLink: "_n2"}},
		},
		{
			Object: obj("_pyotr", "I0003", false), Gender: "M",
			Names: []model.Name{{Type: "Birth Name", First: "Pyotr", Surnames: []model.Surname{
				{Value: "Orlov"}, {Value: "Ivanovich", Primary: flag(false), Derivation: "Patronymic"}}}},
			EventRefs: []model.EventRef{primaryRef("_e7"), primaryRef("_e8")},
			ChildOf:   []model.Ref{{HLink: "_f1"}},
		},
		{
			Object: obj("_olga", "I0010", false), Gender: "F", Names: name("Olga", "Lebedeva"),
			EventRefs:  []model.EventRef{primaryRef("_e9"), primaryRef("_e10")},
			PersonRefs: []model.PersonRef{{HLink: "_ivan", Rel: "Godfather"}},
		},
	}
	t.Families = []model.Family{{
		Object: obj("_f1", "F0001", false), Rel: &model.FamilyRel{Type: "Married"},
		Father: &model.Ref{HLink: "_ivan"}, Mother: &model.Ref{HLink: "_maria"},
		EventRefs: []model.EventRef{{HLink: "_e3", Role: "Family"}}, ChildRefs: []model.ChildRef{{HLink: "_pyotr"}},
	}}
	return t
}

// editStore returns a store with the given tree and a clock that the test
// moves. tick moves the clock one second forward.
func editStore(t *testing.T, tree *model.Tree) (st *Store, tick func()) {
	t.Helper()
	st = open(t)
	move := setClock(st)
	if tree != nil {
		if _, err := st.ReplaceTree(context.Background(), tree); err != nil {
			t.Fatal(err)
		}
	}
	offset := time.Duration(0)
	return st, func() {
		offset += time.Second
		move(offset)
	}
}

// mustEdit runs an edit that must succeed.
func mustEdit(t *testing.T, st *Store, action string, fn func(*Edit) error) *EditResult {
	t.Helper()
	result, err := st.Edit(context.Background(), elena, action, fn)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return result
}

func exported(t *testing.T, st *Store) *model.Tree {
	t.Helper()
	tree, err := st.ExportTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func revision(t *testing.T, st *Store) int64 {
	t.Helper()
	rev, err := st.Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func journalSize(t *testing.T, st *Store) int {
	t.Helper()
	n, err := st.CountChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// refuse runs an edit that must fail with the given error and must store
// nothing.
func refuse(t *testing.T, st *Store, want error, name string, fn func(*Edit) error) {
	t.Helper()
	before, rev, entries := exported(t, st), revision(t, st), journalSize(t, st)
	result, err := st.Edit(context.Background(), elena, "test.refused", fn)
	if !errors.Is(err, want) {
		t.Errorf("%s: got %v (result %+v), want %v", name, err, result, want)
	}
	if err == nil {
		return
	}
	if !reflect.DeepEqual(before, exported(t, st)) || revision(t, st) != rev || journalSize(t, st) != entries {
		t.Errorf("%s: a refused edit changed the store", name)
	}
}

// noChange runs an edit that must succeed and change nothing.
func noChange(t *testing.T, st *Store, name string, fn func(*Edit) error) {
	t.Helper()
	before, rev, entries := exported(t, st), revision(t, st), journalSize(t, st)
	result, err := st.Edit(context.Background(), elena, "test.same", fn)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.ChangeID != 0 {
		t.Errorf("%s: ChangeID %d for an edit that changes nothing", name, result.ChangeID)
	}
	if result.Revision != rev || revision(t, st) != rev || journalSize(t, st) != entries {
		t.Errorf("%s: revision %d -> %d (result %d), journal %d -> %d", name, rev, revision(t, st), result.Revision, entries, journalSize(t, st))
	}
	if !reflect.DeepEqual(before, exported(t, st)) {
		t.Errorf("%s: the tree changed", name)
	}
}

func personInput(gender, given, surname, patronymic string) PersonInput {
	return PersonInput{Gender: gender, Names: []NameInput{{Index: -1, Given: given, Surname: surname, Patronymic: patronymic}}}
}

// addPerson creates a person and returns the ID.
func addPerson(t *testing.T, st *Store, gender, given, surname string) string {
	t.Helper()
	result := mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(personInput(gender, given, surname, ""))
		return err
	})
	if result.Created == "" || result.ChangeID == 0 {
		t.Fatalf("result of a new person: %+v", result)
	}
	return result.Created
}

// updatePerson changes a person from the state that the store has now.
func updatePerson(e *Edit, id string, in PersonInput) error {
	person, err := e.Person(id)
	if err != nil {
		return err
	}
	return e.UpdatePerson(id, e.Rev(model.KindPerson, person.Handle), in)
}

// updateEvent changes an event from the state that the store has now.
func updateEvent(e *Edit, id string, in EventInput) error {
	event, err := e.Event(id)
	if err != nil {
		return err
	}
	return e.UpdateEvent(id, e.Rev(model.KindEvent, event.Handle), in)
}

// updateNote changes a note from the state that the store has now.
func updateNote(e *Edit, id, text string) error {
	note, err := e.Note(id)
	if err != nil {
		return err
	}
	return e.UpdateNote(id, e.Rev(model.KindNote, note.Handle), text, nil)
}

func link(t *testing.T, st *Store, person, as, other, family string) *EditResult {
	t.Helper()
	return mustEdit(t, st, "link.add", func(e *Edit) error { return e.Link(person, as, other, family) })
}

func describe(t *testing.T, st *Store, id string) *EditPerson {
	t.Helper()
	var out *EditPerson
	err := st.Inspect(context.Background(), func(e *Edit) error {
		var err error
		out, err = e.Describe(id)
		return err
	})
	if err != nil {
		t.Fatalf("describe %s: %v", id, err)
	}
	return out
}

func personByID(tree *model.Tree, id string) *model.Person {
	for i := range tree.People {
		if tree.People[i].ID == id {
			return &tree.People[i]
		}
	}
	return nil
}

func familyByID(tree *model.Tree, id string) *model.Family {
	for i := range tree.Families {
		if tree.Families[i].ID == id {
			return &tree.Families[i]
		}
	}
	return nil
}

func eventByID(tree *model.Tree, id string) *model.Event {
	for i := range tree.Events {
		if tree.Events[i].ID == id {
			return &tree.Events[i]
		}
	}
	return nil
}

func noteByID(tree *model.Tree, id string) *model.Note {
	for i := range tree.Notes {
		if tree.Notes[i].ID == id {
			return &tree.Notes[i]
		}
	}
	return nil
}

func mediaByID(tree *model.Tree, id string) *model.Media {
	for i := range tree.Media {
		if tree.Media[i].ID == id {
			return &tree.Media[i]
		}
	}
	return nil
}

func placeByTitle(tree *model.Tree, title string) []model.Place {
	var out []model.Place
	for _, place := range tree.Places {
		if strings.EqualFold(place.Title, title) {
			out = append(out, place)
		}
	}
	return out
}

func fullGraph(t *testing.T, st *Store, scope privacy.Scope) *Graph {
	t.Helper()
	graph, err := st.View(scope).Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func cardByID(graph *Graph, id string) *Card {
	for i := range graph.People {
		if graph.People[i].ID == id {
			return &graph.People[i]
		}
	}
	return nil
}

func graphFamily(graph *Graph, id string) *GraphFamily {
	for i := range graph.Families {
		if graph.Families[i].ID == id {
			return &graph.Families[i]
		}
	}
	return nil
}

// checkLinks exports the tree and checks that every link has a target and
// that the links between people and families agree in both directions.
func checkLinks(t *testing.T, st *Store) *model.Tree {
	t.Helper()
	tree := exported(t, st)
	if res := gramps.Check(tree); res.WarningsTotal != 0 {
		t.Errorf("the tree has broken links: %+v", res.Warnings)
	}
	people := map[string]*model.Person{}
	families := map[string]*model.Family{}
	events, notes, media, places := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	ids := map[string]bool{}
	unique := func(kind string, o model.Object) {
		if !strings.HasPrefix(o.Handle, "_") || o.ID == "" {
			t.Errorf("%s with handle %q and ID %q", kind, o.Handle, o.ID)
		}
		for _, key := range []string{kind + " ID " + o.ID, "handle " + o.Handle} {
			if ids[key] {
				t.Errorf("duplicate %s", key)
			}
			ids[key] = true
		}
	}
	for i := range tree.People {
		people[tree.People[i].Handle] = &tree.People[i]
		unique("person", tree.People[i].Object)
	}
	for i := range tree.Families {
		families[tree.Families[i].Handle] = &tree.Families[i]
		unique("family", tree.Families[i].Object)
	}
	for i := range tree.Events {
		events[tree.Events[i].Handle] = true
		unique("event", tree.Events[i].Object)
	}
	for i := range tree.Notes {
		notes[tree.Notes[i].Handle] = true
		unique("note", tree.Notes[i].Object)
	}
	for i := range tree.Media {
		media[tree.Media[i].Handle] = true
		unique("media", tree.Media[i].Object)
	}
	for i := range tree.Places {
		places[tree.Places[i].Handle] = true
		unique("place", tree.Places[i].Object)
	}
	for _, person := range people {
		seen := map[string]bool{}
		for _, ref := range person.ChildOf {
			family := families[ref.HLink]
			if family == nil || !isChild(family, person.Handle) || seen["c"+ref.HLink] {
				t.Errorf("%s: ChildOf has a family that does not list the person as a child, or lists it twice", person.ID)
			}
			seen["c"+ref.HLink] = true
		}
		for _, ref := range person.ParentIn {
			family := families[ref.HLink]
			if family == nil || !isParent(family, person.Handle) || seen["p"+ref.HLink] {
				t.Errorf("%s: ParentIn has a family that does not list the person as a parent, or lists it twice", person.ID)
			}
			seen["p"+ref.HLink] = true
		}
		for _, ref := range person.EventRefs {
			if !events[ref.HLink] {
				t.Errorf("%s: link to an event that does not exist", person.ID)
			}
		}
		for _, ref := range person.Notes {
			if !notes[ref.HLink] {
				t.Errorf("%s: link to a note that does not exist", person.ID)
			}
		}
		for _, ref := range person.MediaRefs {
			if !media[ref.HLink] {
				t.Errorf("%s: link to a media object that does not exist", person.ID)
			}
		}
		for _, ref := range person.PersonRefs {
			if people[ref.HLink] == nil {
				t.Errorf("%s: association with a person who does not exist", person.ID)
			}
		}
	}
	for _, family := range families {
		for _, parent := range []*model.Ref{family.Father, family.Mother} {
			if parent == nil {
				continue
			}
			if person := people[parent.HLink]; person == nil || !hasRef(person.ParentIn, family.Handle) {
				t.Errorf("%s: a parent does not exist or lacks the link back", family.ID)
			}
		}
		if family.Father != nil && family.Mother != nil && family.Father.HLink == family.Mother.HLink {
			t.Errorf("%s: one person is both parents", family.ID)
		}
		seen := map[string]bool{}
		for _, child := range family.ChildRefs {
			if person := people[child.HLink]; person == nil || !hasRef(person.ChildOf, family.Handle) || seen[child.HLink] {
				t.Errorf("%s: a child does not exist, lacks the link back or is listed twice", family.ID)
			}
			seen[child.HLink] = true
		}
		for _, ref := range family.EventRefs {
			if !events[ref.HLink] {
				t.Errorf("%s: link to an event that does not exist", family.ID)
			}
		}
	}
	for i := range tree.Events {
		if place := tree.Events[i].Place; place != nil && !places[place.HLink] {
			t.Errorf("%s: link to a place that does not exist", tree.Events[i].ID)
		}
	}
	if tree.HomeHandle != "" && people[tree.HomeHandle] == nil {
		t.Errorf("the home person does not exist")
	}
	// The tables of the views hold the same people.
	if graph := fullGraph(t, st, privacy.Full); len(graph.People) != len(tree.People) {
		t.Errorf("the graph has %d people, the tree has %d", len(graph.People), len(tree.People))
	}
	return tree
}

func handlesOf(refs []model.Ref) []string {
	out := []string{}
	for _, ref := range refs {
		out = append(out, ref.HLink)
	}
	return out
}

func childHandles(family *model.Family) []string {
	out := []string{}
	for _, ref := range family.ChildRefs {
		out = append(out, ref.HLink)
	}
	return out
}

func refOf(ref *model.Ref) string {
	if ref == nil {
		return ""
	}
	return ref.HLink
}

func TestCreatePerson(t *testing.T) {
	st, _ := editStore(t, editTree())
	ctx := context.Background()
	before := revision(t, st)
	result := mustEdit(t, st, "person.add", func(e *Edit) error {
		person, err := e.CreatePerson(personInput("F", "  Anna ", "Orlova", "Ivanovna"))
		if err == nil && (person.ID != "I0011" || !strings.HasPrefix(person.Handle, "_")) {
			t.Errorf("new person inside the edit: %+v", person.Object)
		}
		return err
	})
	// The highest ID of the tree is I0010.
	if result.Created != "I0011" || result.Subject != "I0011" || result.ChangeID == 0 || result.Revision != before+1 {
		t.Fatalf("result: %+v, revision before %d", result, before)
	}
	tree := checkLinks(t, st)
	person := personByID(tree, "I0011")
	if person == nil {
		t.Fatal("the person is not in the tree")
	}
	if !strings.HasPrefix(person.Handle, "_") || len(person.Handle) < 12 || person.Change != testBase.Unix() || person.Gender != "F" || bool(person.Private) {
		t.Fatalf("person: %+v", person.Object)
	}
	want := []model.Name{{Type: "Birth Name", First: "Anna", Surnames: []model.Surname{
		{Value: "Orlova", Primary: flag(true)}, {Value: "Ivanovna", Derivation: "Patronymic", Primary: flag(false)}}}}
	if !reflect.DeepEqual(person.Names, want) {
		t.Fatalf("names: %+v", person.Names)
	}

	card := cardByID(fullGraph(t, st, privacy.Full), "I0011")
	if card == nil || card.Given != "Anna" || card.Surname != "Orlova" || card.Patronymic != "Ivanovna" || card.Gender != "F" {
		t.Fatalf("card in the graph: %+v", card)
	}
	detail, err := st.View(privacy.Full).Person(ctx, "I0011")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Names) != 1 || detail.Names[0] != (NameView{Type: "Birth Name", Given: "Anna", Surname: "Orlova", Patronymic: "Ivanovna"}) ||
		detail.Given != "Anna" || detail.Surname != "Orlova" || detail.Patronymic != "Ivanovna" {
		t.Fatalf("detail: %+v names %+v", detail.Card, detail.Names)
	}

	// All fields of the form arrive.
	full := mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(PersonInput{Gender: "U", Private: true, Names: []NameInput{
			{Index: -1, Type: "Married Name", Given: "Boris", Surname: "Lebedev", Suffix: "Jr.", Title: "Dr.", Nick: "Borya", Call: "Bob"},
			{Index: -1, Given: "Bob"},
		}})
		return err
	})
	if full.Created != "I0012" {
		t.Fatalf("the second new person: %+v", full)
	}
	second := personByID(exported(t, st), "I0012")
	wantNames := []model.Name{
		{Type: "Married Name", First: "Boris", Call: "Bob", Suffix: "Jr.", Title: "Dr.", Nick: "Borya", Surnames: []model.Surname{{Value: "Lebedev", Primary: flag(true)}}},
		{Alt: true, Type: "Birth Name", First: "Bob"},
	}
	if second.Gender != "U" || !bool(second.Private) || !reflect.DeepEqual(second.Names, wantNames) {
		t.Fatalf("second person: %+v", second)
	}
	if second.Handle == person.Handle {
		t.Fatal("two people share a handle")
	}
}

func TestNewIDsFollowTheHighestNumber(t *testing.T) {
	tree := editTree()
	// IDs that are no numbers do not count.
	tree.People = append(tree.People,
		model.Person{Object: obj("_x1", "IX", false), Gender: "U", Names: name("X", "One")},
		model.Person{Object: obj("_x2", "I0042", false), Gender: "U", Names: name("X", "Two")},
		model.Person{Object: obj("_x3", "I41", false), Gender: "U", Names: name("X", "Three")},
		model.Person{Object: obj("_x4", "J0900", false), Gender: "U", Names: name("X", "Four")},
	)
	st, _ := editStore(t, tree)
	var created []string
	result := mustEdit(t, st, "many", func(e *Edit) error {
		for i := 0; i < 3; i++ {
			person, err := e.CreatePerson(personInput("U", fmt.Sprintf("New %d", i), "", ""))
			if err != nil {
				return err
			}
			created = append(created, person.ID)
		}
		if err := e.AddEvent(Owner{Kind: model.KindPerson, ID: created[0]}, EventInput{Type: "Birth", Place: &PlaceInput{Name: "New Town"}}); err != nil {
			return err
		}
		if e.Created != "E0011" {
			t.Errorf("new event: %s", e.Created)
		}
		if err := e.AddNote(created[0], "text", false); err != nil {
			return err
		}
		if e.Created != "N0003" {
			t.Errorf("new note: %s", e.Created)
		}
		if err := e.AddPhoto(created[0], "heirfold/a.jpg", "image/jpeg", ""); err != nil {
			return err
		}
		if e.Created != "O0002" {
			t.Errorf("new media: %s", e.Created)
		}
		return e.Link(created[0], AsSpouse, created[1], "")
	})
	if !reflect.DeepEqual(created, []string{"I0043", "I0044", "I0045"}) {
		t.Fatalf("new IDs: %v", created)
	}
	if result.Subject != "I0043" {
		t.Fatalf("subject: %+v", result)
	}
	after := checkLinks(t, st)
	if len(placeByTitle(after, "New Town")) != 1 || placeByTitle(after, "New Town")[0].ID != "P0002" {
		t.Fatalf("new place: %+v", placeByTitle(after, "New Town"))
	}
	if familyByID(after, "F0002") == nil {
		t.Fatal("the new family is not F0002")
	}
	// The next edit goes on from there.
	if id := addPerson(t, st, "U", "Later", ""); id != "I0046" {
		t.Fatalf("the next ID: %s", id)
	}
	// A tree with nothing in it starts at 1.
	empty, _ := editStore(t, nil)
	if id := addPerson(t, empty, "F", "First", "Person"); id != "I0001" {
		t.Fatalf("the first ID of an empty tree: %s", id)
	}
	first := checkLinks(t, empty)
	if len(first.People) != 1 || first.Namespace == "" {
		t.Fatalf("the first person of an empty tree: %+v, namespace %q", first.Counts(), first.Namespace)
	}
}

func TestUpdatePersonKeepsThePartsOutsideTheForm(t *testing.T) {
	st, tick := editStore(t, editTree())
	before := personByID(exported(t, st), "I0001")
	form := describe(t, st, "I0001")
	if form.Rev == 0 || form.Gender != "M" || form.Private || !form.Home || len(form.Names) != 2 {
		t.Fatalf("form: %+v", form)
	}
	if form.Names[0] != (NameInput{Index: 0, Type: "Birth Name", Given: "Ivan", Surname: "Orlov", Patronymic: "Petrovich"}) ||
		form.Names[1] != (NameInput{Index: 1, Type: "Also Known As", Given: "John", Surname: "Orloff", Nick: "Johnny"}) {
		t.Fatalf("names of the form: %+v", form.Names)
	}
	tick()
	input := PersonInput{Gender: form.Gender, Private: form.Private, Names: slices.Clone(form.Names)}
	input.Names[0].Given = "Ioann"
	result := mustEdit(t, st, "person.update", func(e *Edit) error { return e.UpdatePerson("I0001", form.Rev, input) })
	if result.ChangeID == 0 || result.Subject != "I0001" || result.Created != "" {
		t.Fatalf("result: %+v", result)
	}
	after := personByID(checkLinks(t, st), "I0001")
	if after.Names[0].First != "Ioann" {
		t.Fatalf("the given name: %q", after.Names[0].First)
	}
	if after.Change != testBase.Add(time.Second).Unix() {
		t.Fatalf("the time stamp of the change: %d", after.Change)
	}
	// Everything else is as before: the second surname, the connector, the
	// group, the citation and the note of the name, and the rest of the person.
	want := *before
	want.Change = after.Change
	want.Names = slices.Clone(before.Names)
	want.Names[0].First = "Ioann"
	if !reflect.DeepEqual(&want, after) {
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(after)
		t.Fatalf("an update of the given name changed more:\nwant %s\n got %s", a, b)
	}
	if got := describe(t, st, "I0001"); got.Rev == 0 || got.Names[0].Given != "Ioann" {
		t.Fatalf("form after the update: %+v", got)
	}
}

func TestUpdatePersonRemovesAndAddsSurnameParts(t *testing.T) {
	st, _ := editStore(t, editTree())
	surnames := func() []model.Surname { return personByID(exported(t, st), "I0003").Names[0].Surnames }
	update := func(surname, patronymic string) {
		t.Helper()
		mustEdit(t, st, "person.update", func(e *Edit) error {
			return updatePerson(e, "I0003", PersonInput{Gender: "M", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: "Pyotr", Surname: surname, Patronymic: patronymic}}})
		})
	}
	values := func() (plain, patronymics []string) {
		for _, part := range surnames() {
			if isPatronymic(part) {
				patronymics = append(patronymics, part.Value)
			} else {
				plain = append(plain, part.Value)
			}
		}
		return plain, patronymics
	}
	check := func(step string, wantPlain, wantPatronymics []string) {
		t.Helper()
		plain, patronymics := values()
		if !slices.Equal(plain, wantPlain) || !slices.Equal(patronymics, wantPatronymics) {
			t.Fatalf("%s: surnames %v, patronymics %v", step, plain, patronymics)
		}
		form := describe(t, st, "I0003").Names[0]
		wantSurname, wantPatronymic := "", ""
		if len(wantPlain) > 0 {
			wantSurname = wantPlain[0]
		}
		if len(wantPatronymics) > 0 {
			wantPatronymic = wantPatronymics[0]
		}
		if form.Surname != wantSurname || form.Patronymic != wantPatronymic {
			t.Fatalf("%s: the form shows %q and %q", step, form.Surname, form.Patronymic)
		}
	}
	check("start", []string{"Orlov"}, []string{"Ivanovich"})
	update("Orlov", "")
	check("without the patronymic", []string{"Orlov"}, nil)
	update("", "")
	check("without both", nil, nil)
	if got := surnames(); len(got) != 0 {
		t.Fatalf("surname parts stay: %+v", got)
	}
	update("", "Ivanovich")
	check("patronymic only", nil, []string{"Ivanovich"})
	update("Sokolov", "Ivanovich")
	check("both again", []string{"Sokolov"}, []string{"Ivanovich"})
	// The surname comes before the patronymic.
	if parts := surnames(); len(parts) != 2 || parts[0].Value != "Sokolov" || parts[1].Derivation != "Patronymic" {
		t.Fatalf("order of the parts: %+v", parts)
	}
	update("", "Ivanovich")
	check("without the surname", nil, []string{"Ivanovich"})

	// On a name with two surnames, the form removes only the one it shows.
	mustEdit(t, st, "person.update", func(e *Edit) error {
		return updatePerson(e, "I0001", PersonInput{Gender: "M", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: "Ivan", Surname: "", Patronymic: "Petrovich"}}})
	})
	ivan := personByID(checkLinks(t, st), "I0001").Names[0]
	if len(ivan.Surnames) != 2 || ivan.Surnames[0].Value != "Sokolov" || ivan.Surnames[0].Connector != "-" || ivan.Surnames[1].Value != "Petrovich" {
		t.Fatalf("surname parts of the name with two surnames: %+v", ivan.Surnames)
	}
	if len(ivan.Citations) != 1 || ivan.Group != "Orlov group" {
		t.Fatalf("the name lost its other parts: %+v", ivan)
	}
}

// Gramps wants one primary surname when a name has surnames.
func TestNameKeepsOnePrimarySurnamePart(t *testing.T) {
	// BUG: internal/store/edit_ops.go:125-149. When a name has only a
	// patronymic, applyName makes the patronymic the primary part (line 148).
	// When the editor adds a surname later, the new part is primary too (line
	// 126) and the patronymic keeps its flag, so the name has two primary
	// parts. The comment at line 140 says that Gramps wants one.
	st, _ := editStore(t, editTree())
	for _, step := range [][2]string{{"", "Ivanovich"}, {"Sokolov", "Ivanovich"}, {"", "Ivanovich"}, {"Orlov", ""}, {"Orlov", "Ivanovich"}} {
		mustEdit(t, st, "person.update", func(e *Edit) error {
			return updatePerson(e, "I0003", PersonInput{Gender: "M", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: "Pyotr", Surname: step[0], Patronymic: step[1]}}})
		})
		parts := personByID(exported(t, st), "I0003").Names[0].Surnames
		primary := 0
		for _, part := range parts {
			if part.Primary == nil || bool(*part.Primary) {
				primary++
			}
		}
		if primary != 1 {
			t.Fatalf("surname %q, patronymic %q: %d primary parts: %s", step[0], step[1], primary, asJSON(t, parts))
		}
	}
}

func TestFirstNameOfTheInputIsTheMainName(t *testing.T) {
	st, _ := editStore(t, editTree())
	input := PersonInput{Gender: "M", Names: []NameInput{
		{Index: 1, Type: "Also Known As", Given: "John", Surname: "Orloff", Nick: "Johnny"},
		{Index: -1, Type: "Married Name", Given: "Jan", Surname: "Orlowski"},
		{Index: 0, Type: "Birth Name", Given: "Ivan", Surname: "Orlov", Patronymic: "Petrovich"},
	}}
	mustEdit(t, st, "person.update", func(e *Edit) error { return updatePerson(e, "I0001", input) })
	names := personByID(checkLinks(t, st), "I0001").Names
	if len(names) != 3 {
		t.Fatalf("names: %+v", names)
	}
	for i, want := range []string{"John", "Jan", "Ivan"} {
		if names[i].First != want || bool(names[i].Alt) != (i > 0) {
			t.Errorf("name %d: %q alt=%v", i, names[i].First, names[i].Alt)
		}
	}
	// The stored name moved with its hidden parts.
	if len(names[2].Citations) != 1 || names[2].Group != "Orlov group" || len(names[2].Surnames) != 3 {
		t.Fatalf("the old main name lost parts: %+v", names[2])
	}
	card := cardByID(fullGraph(t, st, privacy.Full), "I0001")
	// The card shows the surname from the marriage and the surname at birth.
	if card.Given != "John" || card.Surname != "Orlowski" || card.BirthSurname != "Orlov" || !card.Married {
		t.Fatalf("card: %+v", card)
	}
	form := describe(t, st, "I0001")
	if form.Names[0].Given != "John" || form.Names[0].Index != 0 || form.Names[2].Given != "Ivan" {
		t.Fatalf("form: %+v", form.Names)
	}
	// A name that the input leaves out goes away.
	mustEdit(t, st, "person.update", func(e *Edit) error {
		return updatePerson(e, "I0001", PersonInput{Gender: "M", Names: []NameInput{{Index: 2, Type: "Birth Name", Given: "Ivan", Surname: "Orlov", Patronymic: "Petrovich"}}})
	})
	names = personByID(exported(t, st), "I0001").Names
	if len(names) != 1 || names[0].First != "Ivan" || bool(names[0].Alt) || len(names[0].Citations) != 1 {
		t.Fatalf("names after the removal: %+v", names)
	}
}

func TestUpdatePersonChecksTheRevision(t *testing.T) {
	st, tick := editStore(t, editTree())
	input := personInput("F", "Maria", "Orlova", "")
	input.Names[0].Index = 0
	update := func(rev int64, given string) func(*Edit) error {
		in := PersonInput{Gender: "F", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: given, Surname: "Orlova"}}}
		return func(e *Edit) error { return e.UpdatePerson("I0002", rev, in) }
	}
	old := describe(t, st, "I0002").Rev
	if old == 0 {
		t.Fatalf("rev of an imported person: %d", old)
	}
	tick()
	refuse(t, st, ErrConflict, "a rev that never was", update(old+5, "Marya"))
	mustEdit(t, st, "person.update", update(old, "Marya"))
	current := describe(t, st, "I0002").Rev
	if current == old {
		t.Fatalf("the rev did not change: %d", current)
	}
	tick()
	// Another editor still has the old state.
	refuse(t, st, ErrConflict, "the rev before the last change", update(old, "Mariya"))
	if got := personByID(exported(t, st), "I0002").Names[0].First; got != "Marya" {
		t.Fatalf("a refused update changed the name to %q", got)
	}
	mustEdit(t, st, "person.update", update(current, "Mariya"))
	tick()
	// The check has no way around it: a request without a rev is refused too.
	for _, rev := range []int64{0, -1, current, current - 1, testBase.Unix()} {
		refuse(t, st, ErrConflict, fmt.Sprintf("rev %d", rev), update(rev, "Masha"))
	}
	if got := personByID(exported(t, st), "I0002").Names[0].First; got != "Mariya" {
		t.Fatalf("name: %q", got)
	}
	refuse(t, st, ErrNotFound, "unknown person", func(e *Edit) error { return updatePerson(e, "I0999", input) })
}

// Two editors open the same person. Both save within one second. The second
// one must get a conflict, or the change of the first one is lost unseen.
func TestStaleRevIsRefusedWithinOneSecond(t *testing.T) {
	// The rev is a hash of the stored document, so two changes in one second
	// give two revs.
	st, _ := editStore(t, editTree())
	id := addPerson(t, st, "F", "Anna", "Orlova")
	rev := describe(t, st, id).Rev
	save := func(given string) func(*Edit) error {
		return func(e *Edit) error {
			return e.UpdatePerson(id, rev, PersonInput{Gender: "F", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: given, Surname: "Orlova"}}})
		}
	}
	// The clock does not move.
	mustEdit(t, st, "person.update", save("Hanna"))
	if now := describe(t, st, id).Rev; now == rev {
		t.Errorf("the rev is still %d after a change", rev)
	}
	refuse(t, st, ErrConflict, "the second save with the same rev", save("Anya"))
}

func TestPersonInputIsChecked(t *testing.T) {
	st, _ := editStore(t, editTree())
	long := strings.Repeat("я", maxNameLength+1)
	many := make([]NameInput, maxNames+1)
	for i := range many {
		many[i] = NameInput{Index: -1, Given: "A"}
	}
	bad := map[string]PersonInput{
		"unknown gender":        {Gender: "Z", Names: []NameInput{{Index: -1, Given: "A"}}},
		"no gender":             {Names: []NameInput{{Index: -1, Given: "A"}}},
		"gender in lower case":  {Gender: "m", Names: []NameInput{{Index: -1, Given: "A"}}},
		"no names":              {Gender: "M"},
		"too many names":        {Gender: "M", Names: many},
		"long given name":       {Gender: "M", Names: []NameInput{{Index: -1, Given: long}}},
		"long surname":          {Gender: "M", Names: []NameInput{{Index: -1, Surname: long}}},
		"long patronymic":       {Gender: "M", Names: []NameInput{{Index: -1, Patronymic: long}}},
		"long type":             {Gender: "M", Names: []NameInput{{Index: -1, Type: long}}},
		"long nick":             {Gender: "M", Names: []NameInput{{Index: -1, Nick: long}}},
		"long second name":      {Gender: "M", Names: []NameInput{{Index: -1, Given: "A"}, {Index: -1, Title: long}}},
		"index of no name":      {Gender: "M", Names: []NameInput{{Index: 5, Given: "A"}}},
		"one stored name twice": {Gender: "M", Names: []NameInput{{Index: 0, Given: "A"}, {Index: 0, Given: "B"}}},
	}
	for name, input := range bad {
		refuse(t, st, ErrInvalid, "create: "+name, func(e *Edit) error {
			_, err := e.CreatePerson(input)
			return err
		})
		refuse(t, st, ErrInvalid, "update: "+name, func(e *Edit) error { return updatePerson(e, "I0002", input) })
	}
	// A new person has no stored names, so every index is wrong.
	refuse(t, st, ErrInvalid, "create with an index", func(e *Edit) error {
		_, err := e.CreatePerson(PersonInput{Gender: "M", Names: []NameInput{{Index: 0, Given: "A"}}})
		return err
	})

	// The limit itself is fine, and the limit counts letters, not bytes.
	edge := strings.Repeat("я", maxNameLength)
	result := mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(PersonInput{Gender: "X", Names: slices.Repeat([]NameInput{{Index: -1, Given: edge, Surname: edge}}, maxNames)})
		return err
	})
	if got := personByID(exported(t, st), result.Created); len(got.Names) != maxNames || got.Names[0].First != edge || got.Gender != "X" {
		t.Fatalf("a person at the limits: %d names", len(got.Names))
	}

	// Control characters go away, and the outer spaces with them.
	result = mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(PersonInput{Gender: "F", Names: []NameInput{{
			Index: -1, Type: "Birth\x00 Name", Given: " An\x00na\n", Surname: "Or\tlo\rva\x7f", Patronymic: "\x1bIvanovna\u0085",
			Suffix: "\nJr.", Title: "Dr.\x00", Nick: "\x07Anya", Call: "An\x00ya ",
		}}})
		return err
	})
	got := personByID(exported(t, st), result.Created).Names[0]
	want := model.Name{Type: "Birth Name", First: "Anna", Call: "Anya", Suffix: "Jr.", Title: "Dr.", Nick: "Anya", Surnames: []model.Surname{
		{Value: "Orlova", Primary: flag(true)}, {Value: "Ivanovna", Derivation: "Patronymic", Primary: flag(false)}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned name: %+v", got)
	}
	data, _ := json.Marshal(personByID(exported(t, st), result.Created))
	for _, c := range string(data) {
		if c < 0x20 || c == 0x7f || c == 0x85 {
			t.Fatalf("a control character is stored: %q", data)
		}
	}
}

// A person opens the edit form and saves it with no change. Nothing must
// change in the tree.
func TestSavingTheFormAsItIsChangesNothing(t *testing.T) {
	tree := fixture.Generate(fixture.DefaultOptions("en"))
	st, tick := editStore(t, tree)
	start := exported(t, st)
	tick()
	// Each edit reads the whole tree, so the test takes a part of the people.
	step := max(1, len(tree.People)/60)
	for i := 0; i < len(tree.People); i += step {
		id := tree.People[i].ID
		form := describe(t, st, id)
		input := PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names}
		result, err := st.Edit(context.Background(), elena, "person.update", func(e *Edit) error { return e.UpdatePerson(id, form.Rev, input) })
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if result.ChangeID != 0 {
			before, _ := json.Marshal(tree.People[i].Names)
			after, _ := json.Marshal(personByID(exported(t, st), id).Names)
			t.Fatalf("%s: saving the unchanged form changed the person:\nbefore %s\n after %s", id, before, after)
		}
		for _, event := range form.Events {
			in := EventInput{Type: event.Type, Date: event.Date, Place: event.Place, Description: event.Description, Private: event.Private}
			result, err := st.Edit(context.Background(), elena, "event.update", func(e *Edit) error { return e.UpdateEvent(event.ID, event.Rev, in) })
			if err != nil {
				t.Fatalf("%s, event %s: %v", id, event.ID, err)
			}
			if result.ChangeID != 0 {
				t.Fatalf("%s: saving the unchanged form of the event %s changed it", id, event.ID)
			}
		}
		for _, note := range form.Notes {
			noChange(t, st, id+" note "+note.ID, func(e *Edit) error { return e.UpdateNote(note.ID, note.Rev, note.Text, nil) })
		}
	}
	if journalSize(t, st) != 0 || !reflect.DeepEqual(start, exported(t, st)) {
		t.Fatal("the tree changed")
	}
}

// The form shows a surname with its prefix as one text. Saving that form
// must not double the prefix.
func TestSavingTheFormKeepsASurnameWithAPrefix(t *testing.T) {
	// BUG: internal/store/edit_ops.go:1003 with internal/store/edit_ops.go:119.
	// Describe fills the surname field through model.SplitName, which joins
	// the prefix and the value ("von Berg"). applyName writes the field back
	// into Surname.Value and keeps Surname.Prefix, so the stored name becomes
	// "von" + "von Berg" after a save without a change.
	tree := editTree()
	tree.People[3].Names = []model.Name{{Type: "Birth Name", First: "Olga", Surnames: []model.Surname{{Prefix: "von", Value: "Berg"}}}}
	st, tick := editStore(t, tree)
	tick()
	form := describe(t, st, "I0010")
	noChange(t, st, "the unchanged form", func(e *Edit) error {
		return e.UpdatePerson("I0010", form.Rev, PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names})
	})
	if got := describe(t, st, "I0010").Names[0].Surname; got != "von Berg" {
		t.Fatalf("the surname is now %q", got)
	}
}

// The form shows the primary surname. Saving the form must write to that
// surname part and to no other.
func TestSavingTheFormKeepsANameWhosePrimarySurnameIsNotFirst(t *testing.T) {
	// BUG: internal/store/edit_ops.go:108-119. model.SplitName shows the first
	// surname part that is primary. applyName writes the field into the first
	// part that is no patronymic, primary or not. For the parts [Lebedeva (not
	// primary), Orlova (primary)] a save without a change gives [Orlova,
	// Orlova] and loses "Lebedeva".
	tree := editTree()
	tree.People[3].Names = []model.Name{{Type: "Birth Name", First: "Olga", Surnames: []model.Surname{
		{Value: "Lebedeva", Primary: flag(false)}, {Value: "Orlova", Primary: flag(true)}}}}
	st, tick := editStore(t, tree)
	tick()
	form := describe(t, st, "I0010")
	if form.Names[0].Surname != "Orlova" {
		t.Fatalf("the form shows %q", form.Names[0].Surname)
	}
	noChange(t, st, "the unchanged form", func(e *Edit) error {
		return e.UpdatePerson("I0010", form.Rev, PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names})
	})
}

// Gramps stores a name without a surname as one empty surname part. Saving
// the form must keep that part, also when the given name changes.
func TestSavingTheFormKeepsAnEmptySurnamePart(t *testing.T) {
	tree := editTree()
	tree.People[3].Names = []model.Name{{Type: "Birth Name", First: "Olga", Surnames: []model.Surname{{Value: ""}}}}
	st, tick := editStore(t, tree)
	tick()
	form := describe(t, st, "I0010")
	noChange(t, st, "the unchanged form", func(e *Edit) error {
		return e.UpdatePerson("I0010", form.Rev, PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names})
	})
	form.Names[0].Given = "Olesya"
	mustEdit(t, st, "person.update", func(e *Edit) error {
		if err := e.UpdatePerson("I0010", form.Rev, PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names}); err != nil {
			return err
		}
		person, err := e.Person("I0010")
		if err != nil {
			return err
		}
		if got := person.Names[0]; got.First != "Olesya" || len(got.Surnames) != 1 || got.Surnames[0].Value != "" {
			t.Errorf("the name is now %+v", got)
		}
		return nil
	})
}

func TestLinkParent(t *testing.T) {
	st, _ := editStore(t, editTree())
	child := addPerson(t, st, "F", "Anna", "Orlova")
	father := addPerson(t, st, "M", "Boris", "Orlov")
	mother := addPerson(t, st, "F", "Vera", "Orlova")
	third := addPerson(t, st, "M", "Gleb", "Orlov")

	// The child has no family: a new one appears.
	result := link(t, st, child, AsParent, father, "")
	if result.ChangeID == 0 || result.Subject != child {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	c, f := personByID(tree, child), personByID(tree, father)
	if len(c.ChildOf) != 1 || len(f.ParentIn) != 1 || c.ChildOf[0] != f.ParentIn[0] || len(c.ParentIn) != 0 || len(f.ChildOf) != 0 {
		t.Fatalf("links: child %+v %+v, father %+v %+v", c.ChildOf, c.ParentIn, f.ChildOf, f.ParentIn)
	}
	family := families(tree)[c.ChildOf[0].HLink]
	if family == nil || family.ID != "F0002" || refOf(family.Father) != f.Handle || family.Mother != nil ||
		!slices.Equal(childHandles(family), []string{c.Handle}) || family.Rel == nil || family.Rel.Type != "Unknown" {
		t.Fatalf("new family: %+v", family)
	}
	// The second parent goes into the same family.
	link(t, st, child, AsParent, mother, "")
	tree = checkLinks(t, st)
	family = familyByID(tree, "F0002")
	if len(tree.Families) != 2 || refOf(family.Father) != personByID(tree, father).Handle || refOf(family.Mother) != personByID(tree, mother).Handle ||
		!slices.Equal(childHandles(family), []string{personByID(tree, child).Handle}) {
		t.Fatalf("family with two parents: %+v", family)
	}
	graph := graphFamily(fullGraph(t, st, privacy.Full), "F0002")
	if graph == nil || graph.Father != father || graph.Mother != mother || !slices.Equal(graph.Children, []string{child}) {
		t.Fatalf("family in the graph: %+v", graph)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), child)
	if err != nil || len(detail.Parents) != 1 || detail.Parents[0].FamilyID != "F0002" ||
		detail.Parents[0].Father == nil || detail.Parents[0].Father.ID != father || detail.Parents[0].Mother == nil || detail.Parents[0].Mother.ID != mother {
		t.Fatalf("parents in the view: %+v %v", detail, err)
	}

	refuse(t, st, ErrSlotTaken, "a third parent", func(e *Edit) error { return e.Link(child, AsParent, third, "") })
	refuse(t, st, ErrSlotTaken, "a third parent with the family", func(e *Edit) error { return e.Link(child, AsParent, third, "F0002") })
	refuse(t, st, ErrSlotTaken, "a third parent in the old family", func(e *Edit) error { return e.Link("I0003", AsParent, third, "") })
	// The family must be one in which the person is a child.
	refuse(t, st, ErrInvalid, "a family of other people", func(e *Edit) error { return e.Link(child, AsParent, third, "F0001") })
	refuse(t, st, ErrInvalid, "the own family as a parent", func(e *Edit) error { return e.Link(father, AsParent, third, "F0002") })

	// The same links again change nothing.
	noChange(t, st, "the father again", func(e *Edit) error { return e.Link(child, AsParent, father, "") })
	noChange(t, st, "the mother again, with the family", func(e *Edit) error { return e.Link(child, AsParent, mother, "F0002") })
}

func families(tree *model.Tree) map[string]*model.Family {
	out := map[string]*model.Family{}
	for i := range tree.Families {
		out[tree.Families[i].Handle] = &tree.Families[i]
	}
	return out
}

// The gender chooses the place of a parent while that place is free.
func TestParentPlacesFollowTheGender(t *testing.T) {
	st, _ := editStore(t, editTree())
	child := addPerson(t, st, "U", "Child", "")
	mother := addPerson(t, st, "F", "Vera", "")
	father := addPerson(t, st, "M", "Boris", "")
	link(t, st, child, AsParent, mother, "")
	tree := checkLinks(t, st)
	family := familyByID(tree, "F0002")
	if family.Father != nil || refOf(family.Mother) != personByID(tree, mother).Handle {
		t.Fatalf("a woman as the first parent: %+v", family)
	}
	link(t, st, child, AsParent, father, "")
	tree = checkLinks(t, st)
	family = familyByID(tree, "F0002")
	if refOf(family.Father) != personByID(tree, father).Handle || refOf(family.Mother) != personByID(tree, mother).Handle {
		t.Fatalf("both parents: %+v", family)
	}
	// Two parents of the same gender both get a place.
	second := addPerson(t, st, "U", "Second", "")
	a := addPerson(t, st, "F", "Alla", "")
	b := addPerson(t, st, "F", "Bella", "")
	link(t, st, second, AsParent, a, "")
	link(t, st, second, AsParent, b, "")
	tree = checkLinks(t, st)
	family = familyByID(tree, "F0003")
	got := []string{refOf(family.Father), refOf(family.Mother)}
	want := []string{personByID(tree, a).Handle, personByID(tree, b).Handle}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("two mothers: %+v", family)
	}
}

func TestLinkSpouse(t *testing.T) {
	st, _ := editStore(t, editTree())
	anna := addPerson(t, st, "F", "Anna", "")
	boris := addPerson(t, st, "M", "Boris", "")
	result := link(t, st, anna, AsSpouse, boris, "")
	if result.ChangeID == 0 || result.Subject != anna {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	family := familyByID(tree, "F0002")
	a, b := personByID(tree, anna), personByID(tree, boris)
	if family == nil || refOf(family.Mother) != a.Handle || refOf(family.Father) != b.Handle || len(family.ChildRefs) != 0 ||
		!slices.Equal(handlesOf(a.ParentIn), []string{family.Handle}) || !slices.Equal(handlesOf(b.ParentIn), []string{family.Handle}) {
		t.Fatalf("new couple: %+v", family)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), anna)
	if err != nil || len(detail.Unions) != 1 || detail.Unions[0].Spouse == nil || detail.Unions[0].Spouse.ID != boris {
		t.Fatalf("union in the view: %+v %v", detail, err)
	}
	// The couple exists, from both sides.
	noChange(t, st, "the same couple", func(e *Edit) error { return e.Link(anna, AsSpouse, boris, "") })
	noChange(t, st, "the same couple from the other side", func(e *Edit) error { return e.Link(boris, AsSpouse, anna, "") })
	noChange(t, st, "the same couple with the family", func(e *Edit) error { return e.Link(anna, AsSpouse, boris, "F0002") })
	noChange(t, st, "the couple of the imported tree", func(e *Edit) error { return e.Link("I0001", AsSpouse, "I0002", "") })

	// A second spouse gets a second family.
	gleb := addPerson(t, st, "M", "Gleb", "")
	link(t, st, anna, AsSpouse, gleb, "")
	tree = checkLinks(t, st)
	if got := personByID(tree, anna).ParentIn; len(got) != 2 || len(tree.Families) != 3 {
		t.Fatalf("families of a person with two spouses: %+v", got)
	}
	refuse(t, st, ErrSlotTaken, "a third person in a full couple", func(e *Edit) error { return e.Link(anna, AsSpouse, "I0010", "F0002") })
	refuse(t, st, ErrInvalid, "a family of other people", func(e *Edit) error { return e.Link(anna, AsSpouse, "I0010", "F0001") })

	// A parent with a child and no partner: the partner joins that family when
	// the request names it.
	vera := addPerson(t, st, "F", "Vera", "")
	child := addPerson(t, st, "U", "Child", "")
	link(t, st, vera, AsChild, child, "")
	single := personByID(checkLinks(t, st), vera).ParentIn
	if len(single) != 1 {
		t.Fatalf("families of the single parent: %+v", single)
	}
	singleID := ""
	for _, f := range exported(t, st).Families {
		if f.Handle == single[0].HLink {
			singleID = f.ID
		}
	}
	dima := addPerson(t, st, "M", "Dima", "")
	link(t, st, vera, AsSpouse, dima, singleID)
	tree = checkLinks(t, st)
	family = familyByID(tree, singleID)
	if refOf(family.Mother) != personByID(tree, vera).Handle || refOf(family.Father) != personByID(tree, dima).Handle ||
		!slices.Equal(childHandles(family), []string{personByID(tree, child).Handle}) || len(personByID(tree, vera).ParentIn) != 1 {
		t.Fatalf("the family of the single parent after the link: %+v", family)
	}
}

func TestLinkChild(t *testing.T) {
	st, _ := editStore(t, editTree())
	// A person with one family: the child joins it.
	anna := addPerson(t, st, "F", "Anna", "Orlova")
	result := link(t, st, "I0001", AsChild, anna, "")
	if result.Subject != "I0001" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	family := familyByID(tree, "F0001")
	if len(tree.Families) != 1 || !slices.Equal(childHandles(family), []string{"_pyotr", personByID(tree, anna).Handle}) ||
		!slices.Equal(handlesOf(personByID(tree, anna).ChildOf), []string{"_f1"}) {
		t.Fatalf("family: %+v", family)
	}
	if graph := graphFamily(fullGraph(t, st, privacy.Full), "F0001"); !slices.Equal(graph.Children, []string{"I0003", anna}) {
		t.Fatalf("children in the graph: %+v", graph)
	}
	noChange(t, st, "the same child", func(e *Edit) error { return e.Link("I0001", AsChild, anna, "") })
	noChange(t, st, "the same child with the family", func(e *Edit) error { return e.Link("I0002", AsChild, anna, "F0001") })

	// A person with no family: a new family with one parent.
	gleb := addPerson(t, st, "M", "Gleb", "")
	son := addPerson(t, st, "M", "Son", "")
	link(t, st, gleb, AsChild, son, "")
	tree = checkLinks(t, st)
	family = familyByID(tree, "F0002")
	if family == nil || refOf(family.Father) != personByID(tree, gleb).Handle || family.Mother != nil ||
		!slices.Equal(childHandles(family), []string{personByID(tree, son).Handle}) {
		t.Fatalf("new family: %+v", family)
	}
	// The request names the family when the parent has more than one.
	vera := addPerson(t, st, "F", "Vera", "")
	link(t, st, gleb, AsSpouse, vera, "")
	daughter := addPerson(t, st, "F", "Daughter", "")
	link(t, st, gleb, AsChild, daughter, "F0003")
	tree = checkLinks(t, st)
	if got := familyByID(tree, "F0003"); !slices.Equal(childHandles(got), []string{personByID(tree, daughter).Handle}) {
		t.Fatalf("the chosen family: %+v", got)
	}
	if got := familyByID(tree, "F0002"); len(got.ChildRefs) != 1 {
		t.Fatalf("the other family changed: %+v", got)
	}
	refuse(t, st, ErrInvalid, "a family of other people", func(e *Edit) error { return e.Link(gleb, AsChild, anna, "F0001") })
	refuse(t, st, ErrInvalid, "a family in which the person is the child", func(e *Edit) error { return e.Link(son, AsChild, anna, "F0002") })
	refuse(t, st, ErrNotFound, "unknown family", func(e *Edit) error { return e.Link(gleb, AsChild, anna, "F0999") })
}

// A parent with two families gets a child without a named family. The same
// request again must change nothing.
func TestChildLinkTwiceForAParentWithTwoFamilies(t *testing.T) {
	// DOUBT: internal/store/edit_ops.go:428-440. For a parent with more than
	// one family and a request with no family, Link makes a new family with
	// one parent. It does not look whether the other person already is a child
	// of the parent, so each repeat of the request adds one more family.
	st, _ := editStore(t, editTree())
	second := addPerson(t, st, "F", "Vera", "")
	child := addPerson(t, st, "U", "Child", "")
	link(t, st, "I0001", AsSpouse, second, "")
	link(t, st, "I0001", AsChild, child, "")
	families := len(checkLinks(t, st).Families)
	noChange(t, st, "the same child again", func(e *Edit) error { return e.Link("I0001", AsChild, child, "") })
	if got := len(exported(t, st).Families); got != families {
		t.Fatalf("families: %d, want %d", got, families)
	}
}

func TestLinkSibling(t *testing.T) {
	st, _ := editStore(t, editTree())
	// The person has parents: the sibling joins their family.
	anna := addPerson(t, st, "F", "Anna", "Orlova")
	result := link(t, st, "I0003", AsSibling, anna, "")
	if result.Subject != "I0003" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	if family := familyByID(tree, "F0001"); len(tree.Families) != 1 || !slices.Equal(childHandles(family), []string{"_pyotr", personByID(tree, anna).Handle}) {
		t.Fatalf("family: %+v", family)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), anna)
	if err != nil || len(detail.Parents) != 1 || len(detail.Parents[0].Siblings) != 1 || detail.Parents[0].Siblings[0].ID != "I0003" {
		t.Fatalf("siblings in the view: %+v %v", detail, err)
	}
	noChange(t, st, "the same sibling", func(e *Edit) error { return e.Link("I0003", AsSibling, anna, "") })
	noChange(t, st, "the same sibling from the other side", func(e *Edit) error { return e.Link(anna, AsSibling, "I0003", "F0001") })

	// The person has no parents: a family with no parents holds the two.
	gleb := addPerson(t, st, "M", "Gleb", "")
	vera := addPerson(t, st, "F", "Vera", "")
	link(t, st, gleb, AsSibling, vera, "")
	tree = checkLinks(t, st)
	family := familyByID(tree, "F0002")
	if family == nil || family.Father != nil || family.Mother != nil ||
		!slices.Equal(childHandles(family), []string{personByID(tree, gleb).Handle, personByID(tree, vera).Handle}) {
		t.Fatalf("family of two siblings: %+v", family)
	}
	// A parent can join that family later.
	father := addPerson(t, st, "M", "Father", "")
	link(t, st, vera, AsParent, father, "")
	tree = checkLinks(t, st)
	if got := familyByID(tree, "F0002"); refOf(got.Father) != personByID(tree, father).Handle || len(got.ChildRefs) != 2 {
		t.Fatalf("family after the parent: %+v", got)
	}
	refuse(t, st, ErrInvalid, "a family in which the person is no child", func(e *Edit) error { return e.Link(gleb, AsSibling, anna, "F0001") })
}

func TestLinkRefusesWrongRequests(t *testing.T) {
	st, _ := editStore(t, editTree())
	for _, as := range []string{AsParent, AsSpouse, AsChild, AsSibling} {
		refuse(t, st, ErrInvalid, "a person with themself as "+as, func(e *Edit) error { return e.Link("I0001", as, "I0001", "") })
		refuse(t, st, ErrNotFound, "unknown person as "+as, func(e *Edit) error { return e.Link("I0999", as, "I0001", "") })
		refuse(t, st, ErrNotFound, "unknown other person as "+as, func(e *Edit) error { return e.Link("I0001", as, "I0999", "") })
		refuse(t, st, ErrNotFound, "unknown family as "+as, func(e *Edit) error { return e.Link("I0001", as, "I0010", "F0999") })
	}
	for _, as := range []string{"", "Parent", "partner", "cousin"} {
		refuse(t, st, ErrInvalid, "relation "+as, func(e *Edit) error { return e.Link("I0001", as, "I0010", "") })
	}
}

func TestLinkRefusesAPersonAsOwnAncestor(t *testing.T) {
	st, _ := editStore(t, editTree())
	// Ivan (I0001) -> Pyotr (I0003) -> grandchild -> great-grandchild.
	grandchild := addPerson(t, st, "F", "Grandchild", "")
	great := addPerson(t, st, "M", "Great", "")
	link(t, st, "I0003", AsChild, grandchild, "")
	link(t, st, grandchild, AsChild, great, "")
	checkLinks(t, st)
	cases := []struct {
		name                 string
		person, as, other    string
		family               string
		descendant, forebear string
	}{
		{name: "the child as a parent", person: "I0001", as: AsParent, other: "I0003"},
		{name: "the grandchild as a parent", person: "I0001", as: AsParent, other: grandchild},
		{name: "the great-grandchild as a parent", person: "I0001", as: AsParent, other: great},
		{name: "the parent as a child", person: "I0003", as: AsChild, other: "I0001"},
		{name: "the grandparent as a child", person: grandchild, as: AsChild, other: "I0001"},
		{name: "the great-grandmother as a child", person: great, as: AsChild, other: "I0002"},
		{name: "the grandparent as a child of the mother line", person: grandchild, as: AsChild, other: "I0002"},
	}
	for _, tc := range cases {
		refuse(t, st, ErrLoop, tc.name, func(e *Edit) error { return e.Link(tc.person, tc.as, tc.other, tc.family) })
	}
	// A new person with a relation is one edit: the loop cancels the person too.
	people := len(exported(t, st).People)
	refuse(t, st, ErrLoop, "a loop after a new person in the same edit", func(e *Edit) error {
		if _, err := e.CreatePerson(personInput("U", "Extra", "", "")); err != nil {
			return err
		}
		return e.Link("I0001", AsParent, great, "")
	})
	if got := len(exported(t, st).People); got != people {
		t.Fatalf("people after a cancelled edit: %d, want %d", got, people)
	}
	// Links that make no loop still work: the grandchild marries a stranger,
	// and two cousins are no ancestors of each other.
	link(t, st, grandchild, AsSpouse, "I0010", "")
	checkLinks(t, st)
}

// A sibling link puts the other person into the family of the parents. When
// the other person is one of those parents, or their ancestor, the person
// becomes an ancestor of themself. The same holds for a child who becomes a
// spouse in the family of the own parents.
func TestLinkRefusesEveryCircle(t *testing.T) {
	st, _ := editStore(t, editTree())
	grandchild := addPerson(t, st, "F", "Grandchild", "")
	link(t, st, "I0003", AsChild, grandchild, "")
	refuse(t, st, ErrLoop, "the father as a sibling", func(e *Edit) error { return e.Link("I0003", AsSibling, "I0001", "") })
	refuse(t, st, ErrLoop, "the mother as a sibling, with the family", func(e *Edit) error { return e.Link("I0003", AsSibling, "I0002", "F0001") })
	refuse(t, st, ErrLoop, "the grandfather as a sibling", func(e *Edit) error { return e.Link(grandchild, AsSibling, "I0001", "") })

	// A daughter as a spouse in the family of her own parents.
	daughter := addPerson(t, st, "F", "Daughter", "")
	father := addPerson(t, st, "M", "Father", "")
	link(t, st, father, AsChild, daughter, "")
	family := ""
	tree := exported(t, st)
	for _, f := range tree.Families {
		if refOf(f.Father) == personByID(tree, father).Handle {
			family = f.ID
		}
	}
	refuse(t, st, ErrLoop, "the daughter as a spouse in the family of her parents", func(e *Edit) error { return e.Link(father, AsSpouse, daughter, family) })
	// In a new family the two are a couple, which is no circle of parents.
	link(t, st, father, AsSpouse, daughter, "")
	checkLinks(t, st)

	// A tree that came with a circle takes other edits, but no link through it.
	looped := editTree()
	looped.Families = append(looped.Families, model.Family{Object: obj("_f2", "F0002", false), Father: &model.Ref{HLink: "_pyotr"}, ChildRefs: []model.ChildRef{{HLink: "_ivan"}}})
	looped.People[2].ParentIn = []model.Ref{{HLink: "_f2"}}
	looped.People[0].ChildOf = []model.Ref{{HLink: "_f2"}}
	other, _ := editStore(t, looped)
	addPerson(t, other, "F", "Anna", "")
	mustEdit(t, other, "link.add", func(e *Edit) error { return e.Link("I0010", AsSpouse, "I0011", "") })
	refuse(t, other, ErrLoop, "a new child of a person in a circle", func(e *Edit) error { return e.Link("I0001", AsChild, "I0011", "F0001") })
}

func TestUnlink(t *testing.T) {
	tree := editTree()
	// The marriage is also in the list of Olga, as a witness.
	tree.People[3].EventRefs = append(tree.People[3].EventRefs, model.EventRef{HLink: "_e3", Role: "Witness"})
	tree.Bookmarks = append(tree.Bookmarks, model.Bookmark{Target: "family", HLink: "_f1"})
	// The family has a second event that nobody else has.
	tree.Events = append(tree.Events, model.Event{Object: obj("_e11", "E0011", false), Type: "Engagement", Dated: dated("1874")})
	tree.Families[0].EventRefs = append(tree.Families[0].EventRefs, model.EventRef{HLink: "_e11", Role: "Family"})
	st, _ := editStore(t, tree)

	refuse(t, st, ErrNotFound, "a person outside the family", func(e *Edit) error { return e.Unlink("F0001", "I0010") })
	refuse(t, st, ErrNotFound, "unknown family", func(e *Edit) error { return e.Unlink("F0999", "I0001") })
	refuse(t, st, ErrNotFound, "unknown person", func(e *Edit) error { return e.Unlink("F0001", "I0999") })

	// The child leaves. Two people stay, so the family stays.
	result := mustEdit(t, st, "link.remove", func(e *Edit) error { return e.Unlink("F0001", "I0003") })
	if result.Subject != "I0003" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	after := checkLinks(t, st)
	family := familyByID(after, "F0001")
	if family == nil || len(family.ChildRefs) != 0 || refOf(family.Father) != "_ivan" || refOf(family.Mother) != "_maria" || len(family.EventRefs) != 2 {
		t.Fatalf("family after the child left: %+v", family)
	}
	if got := personByID(after, "I0003"); len(got.ChildOf) != 0 || len(got.EventRefs) != 2 {
		t.Fatalf("the child after the unlink: %+v", got)
	}
	if graph := graphFamily(fullGraph(t, st, privacy.Full), "F0001"); graph == nil || len(graph.Children) != 0 {
		t.Fatalf("family in the graph: %+v", graph)
	}
	refuse(t, st, ErrNotFound, "the same unlink again", func(e *Edit) error { return e.Unlink("F0001", "I0003") })

	// A parent leaves. One person stays, so the family goes away with the
	// event that only it had. The shared event stays.
	mustEdit(t, st, "link.remove", func(e *Edit) error { return e.Unlink("F0001", "I0002") })
	after = checkLinks(t, st)
	if len(after.Families) != 0 {
		t.Fatalf("a family of one person stays: %+v", after.Families)
	}
	for _, id := range []string{"I0001", "I0002"} {
		if got := personByID(after, id); len(got.ParentIn) != 0 || len(got.ChildOf) != 0 {
			t.Fatalf("%s keeps a link to the family: %+v", id, got)
		}
	}
	if eventByID(after, "E0011") != nil {
		t.Fatal("the event of the removed family stays")
	}
	if eventByID(after, "E0003") == nil {
		t.Fatal("the event that another person shares went away")
	}
	if len(after.Events) != len(tree.Events)-1 {
		t.Fatalf("events: %d, want %d", len(after.Events), len(tree.Events)-1)
	}
	// The bookmark of the family goes with the family.
	for _, bookmark := range after.Bookmarks {
		if bookmark.HLink == "_f1" {
			t.Fatal("the bookmark of the removed family stays")
		}
	}
	if graph := fullGraph(t, st, privacy.Full); len(graph.Families) != 0 {
		t.Fatalf("families in the graph: %+v", graph.Families)
	}
	if detail, err := st.View(privacy.Full).Person(context.Background(), "I0001"); err != nil || len(detail.Unions) != 0 {
		t.Fatalf("unions in the view: %+v %v", detail, err)
	}
}

// A family of one parent and one child is deleted when either leaves.
func TestUnlinkOfTheLastChildRemovesTheFamily(t *testing.T) {
	st, _ := editStore(t, editTree())
	parent := addPerson(t, st, "F", "Vera", "")
	child := addPerson(t, st, "U", "Child", "")
	link(t, st, parent, AsChild, child, "")
	if familyByID(exported(t, st), "F0002") == nil {
		t.Fatal("no family")
	}
	mustEdit(t, st, "link.remove", func(e *Edit) error { return e.Unlink("F0002", child) })
	tree := checkLinks(t, st)
	if familyByID(tree, "F0002") != nil || len(personByID(tree, parent).ParentIn) != 0 || len(personByID(tree, child).ChildOf) != 0 {
		t.Fatalf("the family stays: %+v", tree.Families)
	}
}

func TestDeletePerson(t *testing.T) {
	tree := editTree()
	st, _ := editStore(t, tree)
	refuse(t, st, ErrNotFound, "unknown person", func(e *Edit) error { return e.DeletePerson("I0999") })
	result := mustEdit(t, st, "person.delete", func(e *Edit) error { return e.DeletePerson("I0001") })
	if result.Subject != "I0001" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	after := checkLinks(t, st)
	if personByID(after, "I0001") != nil || len(after.People) != 3 {
		t.Fatalf("people: %d", len(after.People))
	}
	// The family keeps the mother and the child.
	family := familyByID(after, "F0001")
	if family == nil || family.Father != nil || refOf(family.Mother) != "_maria" || !slices.Equal(childHandles(family), []string{"_pyotr"}) || len(family.EventRefs) != 1 {
		t.Fatalf("family: %+v", family)
	}
	// The own events go, the shared event and the events of others stay.
	for _, id := range []string{"E0001", "E0002"} {
		if eventByID(after, id) != nil {
			t.Errorf("the own event %s stays", id)
		}
	}
	for _, id := range []string{"E0003", "E0004", "E0005", "E0006", "E0007", "E0008", "E0009", "E0010"} {
		if eventByID(after, id) == nil {
			t.Errorf("the event %s of other people went away", id)
		}
	}
	if got := personByID(after, "I0002"); len(got.EventRefs) != 3 || !slices.Equal(handlesOf(got.ParentIn), []string{"_f1"}) {
		t.Fatalf("the spouse: %+v", got)
	}
	// The association of another person, the home person and the bookmark.
	if got := personByID(after, "I0010"); len(got.PersonRefs) != 0 {
		t.Fatalf("an association with the removed person stays: %+v", got.PersonRefs)
	}
	if after.HomeHandle != "" {
		t.Fatalf("the home person is still %q", after.HomeHandle)
	}
	if !reflect.DeepEqual(after.Bookmarks, []model.Bookmark{{Target: "person", HLink: "_olga"}}) {
		t.Fatalf("bookmarks: %+v", after.Bookmarks)
	}
	graph := fullGraph(t, st, privacy.Full)
	if graph.Home != "" || cardByID(graph, "I0001") != nil {
		t.Fatalf("graph: home %q", graph.Home)
	}
	if got := graphFamily(graph, "F0001"); got == nil || got.Father != "" || got.Mother != "I0002" {
		t.Fatalf("family in the graph: %+v", got)
	}
	if _, err := st.View(privacy.Full).Person(context.Background(), "I0001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the view still has the person: %v", err)
	}
	if detail, err := st.View(privacy.Full).Person(context.Background(), "I0010"); err != nil || len(detail.Associations) != 0 {
		t.Fatalf("associations in the view: %+v %v", detail, err)
	}
	refuse(t, st, ErrNotFound, "the same person again", func(e *Edit) error { return e.DeletePerson("I0001") })

	// The next person leaves one person in the family, so the family goes
	// with its event.
	mustEdit(t, st, "person.delete", func(e *Edit) error { return e.DeletePerson("I0002") })
	after = checkLinks(t, st)
	if len(after.Families) != 0 || eventByID(after, "E0003") != nil || eventByID(after, "E0004") != nil {
		t.Fatalf("after the second person: families %d, events %+v", len(after.Families), after.Counts())
	}
	if got := personByID(after, "I0003"); len(got.ChildOf) != 0 || len(got.EventRefs) != 2 {
		t.Fatalf("the child: %+v", got)
	}
	if len(after.Events) != 4 {
		t.Fatalf("events: %d, want the 4 of the two people who stay", len(after.Events))
	}
}

func TestSetHomeAndFamilyType(t *testing.T) {
	st, _ := editStore(t, editTree())
	before := revision(t, st)
	result := mustEdit(t, st, "home.set", func(e *Edit) error { return e.SetHome("I0010") })
	if result.Subject != "I0010" || result.Revision <= before {
		t.Fatalf("result: %+v", result)
	}
	if tree := checkLinks(t, st); tree.HomeHandle != "_olga" {
		t.Fatalf("home: %q", tree.HomeHandle)
	}
	if graph := fullGraph(t, st, privacy.Full); graph.Home != "I0010" {
		t.Fatalf("home in the graph: %q", graph.Home)
	}
	if !describe(t, st, "I0010").Home || describe(t, st, "I0001").Home {
		t.Fatal("the form shows the wrong home person")
	}
	noChange(t, st, "the same home person", func(e *Edit) error { return e.SetHome("I0010") })
	refuse(t, st, ErrNotFound, "unknown home person", func(e *Edit) error { return e.SetHome("I0999") })

	mustEdit(t, st, "family.update", func(e *Edit) error { return e.SetFamilyType("F0001", " Civil Union\x00 ") })
	if family := familyByID(exported(t, st), "F0001"); family.Rel == nil || family.Rel.Type != "Civil Union" {
		t.Fatalf("family: %+v", family.Rel)
	}
	if graph := graphFamily(fullGraph(t, st, privacy.Full), "F0001"); graph.Rel != "Civil Union" {
		t.Fatalf("family in the graph: %+v", graph)
	}
	if form := describe(t, st, "I0001"); len(form.Families) != 1 || form.Families[0].ID != "F0001" || form.Families[0].Rel != "Civil Union" || len(form.Families[0].Events) != 1 {
		t.Fatalf("families of the form: %+v", form.Families)
	}
	noChange(t, st, "the same type", func(e *Edit) error { return e.SetFamilyType("F0001", "Civil Union") })
	refuse(t, st, ErrInvalid, "empty type", func(e *Edit) error { return e.SetFamilyType("F0001", "  ") })
	refuse(t, st, ErrInvalid, "long type", func(e *Edit) error { return e.SetFamilyType("F0001", strings.Repeat("x", maxNameLength+1)) })
	refuse(t, st, ErrNotFound, "unknown family", func(e *Edit) error { return e.SetFamilyType("F0999", "Married") })
}

func TestEventsOfAPerson(t *testing.T) {
	st, tick := editStore(t, editTree())
	olga := Owner{Kind: model.KindPerson, ID: "I0010"}
	tick()
	// A new place by name.
	result := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: " Residence ", Date: dated("1890-05-12"), Place: &PlaceInput{Name: "  New Town "}, Description: "A house\x00 by the river"})
	})
	if result.Created != "E0011" || result.Subject != "I0010" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	event := eventByID(tree, "E0011")
	places := placeByTitle(tree, "New Town")
	if len(places) != 1 || places[0].ID != "P0002" || places[0].Type != "Unknown" || !reflect.DeepEqual(places[0].Names, []model.PlaceName{{Value: "New Town"}}) {
		t.Fatalf("new place: %+v", places)
	}
	if event == nil || event.Type != "Residence" || event.Description != "A house by the river" || event.DateVal == nil || event.DateVal.Val != "1890-05-12" ||
		refOf(event.Place) != places[0].Handle || bool(event.Private) || event.Change != testBase.Add(time.Second).Unix() {
		t.Fatalf("event: %+v", event)
	}
	refs := personByID(tree, "I0010").EventRefs
	if len(refs) != 3 || !reflect.DeepEqual(refs[2], model.EventRef{HLink: event.Handle, Role: "Primary"}) {
		t.Fatalf("event links of the person: %+v", refs)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0010")
	if err != nil || len(detail.Events) != 3 {
		t.Fatalf("events in the view: %+v %v", detail, err)
	}
	var shown *EventView
	for i := range detail.Events {
		if detail.Events[i].ID == "E0011" {
			shown = &detail.Events[i]
		}
	}
	if shown == nil || shown.Type != "Residence" || shown.Place == nil || shown.Place.Name != "New Town" || shown.Place.ID != "P0002" || shown.Description != "A house by the river" {
		t.Fatalf("the new event in the view: %+v", shown)
	}

	// The same name in another case gives the same place, also for another person.
	tick()
	second := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0003"}, EventInput{Type: "Occupation", Place: &PlaceInput{Name: "NEW town"}})
	})
	// A place by its ID. The name of the request does not count then.
	third := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Education", Place: &PlaceInput{ID: "P0001", Name: "Something Else"}})
	})
	// The old place by its name.
	fourth := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Graduation", Place: &PlaceInput{Name: "old town"}})
	})
	// No place: a nil place and an empty name.
	fifth := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Retirement", Place: &PlaceInput{Name: "  "}})
	})
	tree = checkLinks(t, st)
	if len(tree.Places) != 2 {
		t.Fatalf("places: %+v", tree.Places)
	}
	if got := eventByID(tree, second.Created); refOf(got.Place) != places[0].Handle {
		t.Fatalf("the place of the second event: %+v", got.Place)
	}
	for _, id := range []string{third.Created, fourth.Created} {
		if got := eventByID(tree, id); refOf(got.Place) != "_p1" {
			t.Fatalf("the place of the event %s: %+v", id, got.Place)
		}
	}
	if got := eventByID(tree, fifth.Created); got.Place != nil {
		t.Fatalf("an event with an empty place name has a place: %+v", got.Place)
	}
	refuse(t, st, ErrInvalid, "unknown place", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Residence", Place: &PlaceInput{ID: "P0999"}})
	})
	refuse(t, st, ErrInvalid, "long place name", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Residence", Place: &PlaceInput{Name: strings.Repeat("x", maxLineLength+1)}})
	})
	refuse(t, st, ErrInvalid, "no type", func(e *Edit) error { return e.AddEvent(olga, EventInput{Type: " \x00 "}) })
	refuse(t, st, ErrInvalid, "long type", func(e *Edit) error { return e.AddEvent(olga, EventInput{Type: strings.Repeat("x", maxNameLength+1)}) })
	refuse(t, st, ErrInvalid, "long description", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Residence", Description: strings.Repeat("я", maxLineLength+1)})
	})
	refuse(t, st, ErrNotFound, "unknown person", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0999"}, EventInput{Type: "Birth"})
	})
	refuse(t, st, ErrInvalid, "an owner that is no person and no family", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindNote, ID: "N0001"}, EventInput{Type: "Birth"})
	})
	if len(exported(t, st).Places) != 2 {
		t.Fatal("a refused event left a place")
	}

	// The form reads the event back.
	form := describe(t, st, "I0010")
	var read *EditEvent
	for i := range form.Events {
		if form.Events[i].ID == "E0011" {
			read = &form.Events[i]
		}
	}
	if read == nil || read.Rev == 0 || read.Type != "Residence" || read.Role != "Primary" || read.Shared != 1 ||
		read.Place == nil || *read.Place != (PlaceInput{ID: "P0002", Name: "New Town"}) || read.Date.DateVal == nil || read.Date.DateVal.Val != "1890-05-12" {
		t.Fatalf("event of the form: %+v", read)
	}

	// Update, with the check of the state.
	tick()
	changed := EventInput{Type: "Residence", Date: model.Dated{DateRange: &model.DatePair{Start: "1890", Stop: "1895"}}, Place: &PlaceInput{ID: "P0001"}, Private: true}
	refuse(t, st, ErrConflict, "an old rev", func(e *Edit) error { return e.UpdateEvent("E0011", read.Rev-1, changed) })
	refuse(t, st, ErrNotFound, "unknown event", func(e *Edit) error { return updateEvent(e, "E0999", changed) })
	refuse(t, st, ErrInvalid, "an update without a type", func(e *Edit) error { return e.UpdateEvent("E0011", read.Rev, EventInput{}) })
	updated := mustEdit(t, st, "event.update", func(e *Edit) error { return e.UpdateEvent("E0011", read.Rev, changed) })
	if updated.ChangeID == 0 || updated.Created != "" {
		t.Fatalf("result of the update: %+v", updated)
	}
	tree = checkLinks(t, st)
	event = eventByID(tree, "E0011")
	if event.DateVal != nil || event.DateRange == nil || *event.DateRange != (model.DatePair{Start: "1890", Stop: "1895"}) || refOf(event.Place) != "_p1" ||
		!bool(event.Private) || event.Description != "" {
		t.Fatalf("event after the update: %+v", event)
	}
	// The place that nothing points to stays; see ADR 0008.
	if len(tree.Places) != 2 {
		t.Fatalf("places: %d", len(tree.Places))
	}
	refuse(t, st, ErrConflict, "the rev before the update", func(e *Edit) error { return e.UpdateEvent("E0011", read.Rev, changed) })
	noChange(t, st, "the same update again", func(e *Edit) error { return updateEvent(e, "E0011", changed) })

	// Remove.
	refuse(t, st, ErrNotFound, "an event of another person", func(e *Edit) error { return e.RemoveEvent(olga, "E0001") })
	refuse(t, st, ErrNotFound, "an event that does not exist", func(e *Edit) error { return e.RemoveEvent(olga, "E0999") })
	refuse(t, st, ErrNotFound, "an owner that does not exist", func(e *Edit) error {
		return e.RemoveEvent(Owner{Kind: model.KindPerson, ID: "I0999"}, "E0011")
	})
	removed := mustEdit(t, st, "event.remove", func(e *Edit) error { return e.RemoveEvent(olga, "E0011") })
	if removed.Subject != "I0010" || removed.ChangeID == 0 {
		t.Fatalf("result of the removal: %+v", removed)
	}
	tree = checkLinks(t, st)
	if eventByID(tree, "E0011") != nil {
		t.Fatal("the removed event stays")
	}
	for _, ref := range personByID(tree, "I0010").EventRefs {
		if ref.HLink == event.Handle {
			t.Fatal("the person keeps the link to the removed event")
		}
	}
	// A shared event leaves one person and stays with the other.
	mustEdit(t, st, "event.remove", func(e *Edit) error { return e.RemoveEvent(Owner{Kind: model.KindPerson, ID: "I0001"}, "E0004") })
	tree = checkLinks(t, st)
	if eventByID(tree, "E0004") == nil {
		t.Fatal("a shared event went away")
	}
	if got := personByID(tree, "I0001").EventRefs; len(got) != 2 {
		t.Fatalf("event links of the first person: %+v", got)
	}
	if got := personByID(tree, "I0002").EventRefs; len(got) != 3 {
		t.Fatalf("event links of the second person: %+v", got)
	}
	mustEdit(t, st, "event.remove", func(e *Edit) error { return e.RemoveEvent(Owner{Kind: model.KindPerson, ID: "I0002"}, "E0004") })
	if eventByID(checkLinks(t, st), "E0004") != nil {
		t.Fatal("the event stays after its last person removed it")
	}
}

func TestEventsOfAFamily(t *testing.T) {
	st, tick := editStore(t, editTree())
	family := Owner{Kind: model.KindFamily, ID: "F0001"}
	tick()
	result := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(family, EventInput{Type: "Divorce", Date: dated("1900"), Place: &PlaceInput{Name: "Old Town"}})
	})
	if result.Created != "E0011" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	tree := checkLinks(t, st)
	event := eventByID(tree, "E0011")
	refs := familyByID(tree, "F0001").EventRefs
	if event == nil || event.Type != "Divorce" || refOf(event.Place) != "_p1" || len(refs) != 2 || !reflect.DeepEqual(refs[1], model.EventRef{HLink: event.Handle, Role: "Family"}) {
		t.Fatalf("family event: %+v, links %+v", event, refs)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0002")
	if err != nil || len(detail.Unions) != 1 || len(detail.Unions[0].Events) != 2 || detail.Unions[0].Events[1].Type != "Divorce" {
		t.Fatalf("union in the view: %+v %v", detail, err)
	}
	form := describe(t, st, "I0001")
	if len(form.Families) != 1 || len(form.Families[0].Events) != 2 || form.Families[0].Events[1].ID != "E0011" || form.Families[0].Events[1].Role != "Family" {
		t.Fatalf("family of the form: %+v", form.Families)
	}
	// The marriage date of the graph follows an update.
	tick()
	mustEdit(t, st, "event.update", func(e *Edit) error {
		return updateEvent(e, "E0003", EventInput{Type: "Marriage", Date: dated("1876-02")})
	})
	graph := graphFamily(fullGraph(t, st, privacy.Full), "F0001")
	data, _ := json.Marshal(graph.Married)
	if graph.Married == nil || !strings.Contains(string(data), "1876") {
		t.Fatalf("marriage date in the graph: %s", data)
	}
	refuse(t, st, ErrNotFound, "unknown family", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindFamily, ID: "F0999"}, EventInput{Type: "Marriage"})
	})
	refuse(t, st, ErrNotFound, "an event of a person", func(e *Edit) error { return e.RemoveEvent(family, "E0001") })
	mustEdit(t, st, "event.remove", func(e *Edit) error { return e.RemoveEvent(family, "E0011") })
	mustEdit(t, st, "event.remove", func(e *Edit) error { return e.RemoveEvent(family, "E0003") })
	tree = checkLinks(t, st)
	if eventByID(tree, "E0011") != nil || eventByID(tree, "E0003") != nil || len(familyByID(tree, "F0001").EventRefs) != 0 {
		t.Fatalf("family events after the removal: %+v", familyByID(tree, "F0001").EventRefs)
	}
	if graph := graphFamily(fullGraph(t, st, privacy.Full), "F0001"); graph.Married != nil {
		t.Fatalf("marriage date after the removal: %+v", graph.Married)
	}
}

func TestEventDatesAreChecked(t *testing.T) {
	st, _ := editStore(t, editTree())
	olga := Owner{Kind: model.KindPerson, ID: "I0010"}
	val := func(v string) model.Dated { return model.Dated{DateVal: &model.DateVal{Val: v}} }
	valid := map[string]model.Dated{
		"no date":                  {},
		"year":                     val("1890"),
		"year and month":           val("1890-05"),
		"full date":                val("1890-05-12"),
		"unknown year":             val("????-05-12"),
		"unknown year, month only": val("????-05"),
		"unknown month and day":    val("1890-00-00"),
		"unknown day":              val("1890-05-00"),
		"last day of a year":       val("1890-12-31"),
		"year 0001":                val("0001"),
		"year 9999":                val("9999-12-31"),
		"Hebrew month 13":          {DateVal: &model.DateVal{Val: "5650-13-01", CFormat: "Hebrew"}},
		"Julian 29 February 1900":  {DateVal: &model.DateVal{Val: "1900-02-29", CFormat: "Julian"}},
		"before":                   {DateVal: &model.DateVal{Val: "1890", Type: "before"}},
		"after":                    {DateVal: &model.DateVal{Val: "1890-05", Type: "after"}},
		"about":                    {DateVal: &model.DateVal{Val: "1890", Type: "about", Quality: "estimated"}},
		"calculated":               {DateVal: &model.DateVal{Val: "1890", Quality: "calculated"}},
		"calendar":                 {DateVal: &model.DateVal{Val: "1890-05-12", CFormat: "Julian", NewYear: "Mar25", DualDated: true}},
		"range":                    {DateRange: &model.DatePair{Start: "1890", Stop: "1895-06-01"}},
		"span":                     {DateSpan: &model.DatePair{Start: "1890-01", Stop: "????-02-03", Quality: "estimated", CFormat: "Julian"}},
		"text":                     {DateStr: &model.DateStr{Val: "in the spring after the flood"}},
		"Cyrillic text":            {DateStr: &model.DateStr{Val: "весной 1890 года"}},
	}
	for name, date := range valid {
		result, err := st.Edit(context.Background(), elena, "event.add", func(e *Edit) error { return e.AddEvent(olga, EventInput{Type: "Residence", Date: date}) })
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := eventByID(exported(t, st), result.Created); got == nil || !reflect.DeepEqual(got.Dated, date) {
			t.Errorf("%s: stored date %+v", name, got)
		}
	}
	invalid := map[string]model.Dated{
		"empty value":             val(""),
		"day first":               val("12.05.1890"),
		"slashes":                 val("1890/05/12"),
		"month 14":                val("1890-14-01"),
		"Gregorian month 13":      val("1890-13-01"),
		"30 February":             val("1890-02-30"),
		"29 February 1900":        val("1900-02-29"),
		"day without a month":     val("1890-00-12"),
		"month 99":                val("1890-99"),
		"day 32":                  val("1890-05-32"),
		"month of one digit":      val("1890-5"),
		"day of one digit":        val("1890-05-1"),
		"year of three digits":    val("189"),
		"year of five digits":     val("18900"),
		"space before":            val(" 1890"),
		"space after":             val("1890 "),
		"time of day":             val("1890-05-12T10:00"),
		"words":                   val("spring 1890"),
		"three question marks":    val("???-05-12"),
		"unknown month as marks":  val("1890-??-12"),
		"minus year":              val("-1890"),
		"line break":              val("1890\n"),
		"other digits":            val("١٨٩٠"),
		"unknown type":            {DateVal: &model.DateVal{Val: "1890", Type: "maybe"}},
		"type in upper case":      {DateVal: &model.DateVal{Val: "1890", Type: "Before"}},
		"unknown quality":         {DateVal: &model.DateVal{Val: "1890", Quality: "sure"}},
		"calendar with a tag":     {DateVal: &model.DateVal{Val: "1890", CFormat: "<b>"}},
		"long calendar":           {DateVal: &model.DateVal{Val: "1890", CFormat: strings.Repeat("a", 25)}},
		"new year with a symbol":  {DateVal: &model.DateVal{Val: "1890", NewYear: "1/1"}},
		"range without a start":   {DateRange: &model.DatePair{Stop: "1895"}},
		"range without an end":    {DateRange: &model.DatePair{Start: "1890"}},
		"range with a bad end":    {DateRange: &model.DatePair{Start: "1890", Stop: "1895-5"}},
		"span with a bad start":   {DateSpan: &model.DatePair{Start: "12.05.1890", Stop: "1895"}},
		"span with a bad quality": {DateSpan: &model.DatePair{Start: "1890", Stop: "1895", Quality: "about"}},
		"empty text":              {DateStr: &model.DateStr{Val: "  "}},
		"long text":               {DateStr: &model.DateStr{Val: strings.Repeat("я", maxNameLength+1)}},
		"value and text":          {DateVal: &model.DateVal{Val: "1890"}, DateStr: &model.DateStr{Val: "spring"}},
		"value and range":         {DateVal: &model.DateVal{Val: "1890"}, DateRange: &model.DatePair{Start: "1890", Stop: "1895"}},
		"range and span":          {DateRange: &model.DatePair{Start: "1890", Stop: "1895"}, DateSpan: &model.DatePair{Start: "1890", Stop: "1895"}},
		"all four forms": {DateVal: &model.DateVal{Val: "1890"}, DateStr: &model.DateStr{Val: "spring"},
			DateRange: &model.DatePair{Start: "1890", Stop: "1895"}, DateSpan: &model.DatePair{Start: "1890", Stop: "1895"}},
	}
	for name, date := range invalid {
		refuse(t, st, ErrInvalid, "add: "+name, func(e *Edit) error { return e.AddEvent(olga, EventInput{Type: "Residence", Date: date}) })
		refuse(t, st, ErrInvalid, "update: "+name, func(e *Edit) error { return updateEvent(e, "E0009", EventInput{Type: "Birth", Date: date}) })
	}
	// A text date loses its control characters.
	result := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(olga, EventInput{Type: "Residence", Date: model.Dated{DateStr: &model.DateStr{Val: " spring\x00 1890\n"}}})
	})
	if got := eventByID(exported(t, st), result.Created); got.DateStr == nil || got.DateStr.Val != "spring 1890" {
		t.Fatalf("cleaned text date: %+v", got.DateStr)
	}
}

func TestNotes(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	text := "First line.\r\nSecond\tline with a tab.\x00\n\nПоследняя строка.  "
	result := mustEdit(t, st, "note.add", func(e *Edit) error { return e.AddNote("I0010", text, false) })
	if result.Created != "N0003" || result.Subject != "I0010" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	clean := "First line.\nSecond\tline with a tab.\n\nПоследняя строка."
	tree := checkLinks(t, st)
	note := noteByID(tree, "N0003")
	if note == nil || note.Text != clean || note.Type != "Person Note" || note.Format != 0 || len(note.Styles) != 0 || note.Change != testBase.Add(time.Second).Unix() {
		t.Fatalf("note: %+v", note)
	}
	if refs := personByID(tree, "I0010").Notes; len(refs) != 1 || refs[0].HLink != note.Handle {
		t.Fatalf("note links: %+v", refs)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0010")
	if err != nil || len(detail.Notes) != 1 || detail.Notes[0].ID != "N0003" || detail.Notes[0].Text != clean {
		t.Fatalf("notes in the view: %+v %v", detail, err)
	}
	if form := describe(t, st, "I0010"); len(form.Notes) != 1 || form.Notes[0] != (EditNote{ID: "N0003", Rev: form.Notes[0].Rev, Type: "Person Note", Text: clean}) {
		t.Fatalf("notes of the form: %+v", form.Notes)
	}
	limit := strings.Repeat("я", maxNoteLength)
	refuse(t, st, ErrInvalid, "empty note", func(e *Edit) error { return e.AddNote("I0010", " \n\t ", false) })
	refuse(t, st, ErrInvalid, "long note", func(e *Edit) error { return e.AddNote("I0010", limit+"я", false) })
	refuse(t, st, ErrNotFound, "note for an unknown person", func(e *Edit) error { return e.AddNote("I0999", "text", false) })
	mustEdit(t, st, "note.add", func(e *Edit) error { return e.AddNote("I0003", limit, false) })

	// An update of the text drops the marks for bold text, which point at
	// positions of the old text.
	tick()
	if len(noteByID(tree, "N0001").Styles) != 1 {
		t.Fatal("the test note has no styles")
	}
	noChange(t, st, "the same text", func(e *Edit) error { return updateNote(e, "N0001", "A note with a bold word.") })
	if len(noteByID(exported(t, st), "N0001").Styles) != 1 {
		t.Fatal("an update with the same text dropped the styles")
	}
	refuse(t, st, ErrConflict, "an old rev", func(e *Edit) error { return e.UpdateNote("N0001", 99, "New text.", nil) })
	refuse(t, st, ErrInvalid, "empty text", func(e *Edit) error { return updateNote(e, "N0001", "\x00") })
	refuse(t, st, ErrInvalid, "long text", func(e *Edit) error { return updateNote(e, "N0001", limit+"я") })
	refuse(t, st, ErrNotFound, "unknown note", func(e *Edit) error { return updateNote(e, "N0999", "New text.") })
	updated := mustEdit(t, st, "note.update", func(e *Edit) error { return updateNote(e, "N0001", "New text.\r\n") })
	if updated.ChangeID == 0 {
		t.Fatalf("result of the update: %+v", updated)
	}
	note = noteByID(exported(t, st), "N0001")
	if note.Text != "New text." || note.Styles != nil || note.Type != "Person Note" || note.Change == 1 {
		t.Fatalf("note after the update: %+v", note)
	}
	refuse(t, st, ErrConflict, "the rev before the update", func(e *Edit) error { return e.UpdateNote("N0001", 1, "Other text.", nil) })

	// Remove: a note of one person goes away, a shared note stays.
	refuse(t, st, ErrNotFound, "a note of another person", func(e *Edit) error { return e.RemoveNote("I0010", "N0001") })
	refuse(t, st, ErrNotFound, "a note that does not exist", func(e *Edit) error { return e.RemoveNote("I0001", "N0999") })
	refuse(t, st, ErrNotFound, "a person who does not exist", func(e *Edit) error { return e.RemoveNote("I0999", "N0001") })
	removed := mustEdit(t, st, "note.remove", func(e *Edit) error { return e.RemoveNote("I0001", "N0001") })
	if removed.Subject != "I0001" || removed.ChangeID == 0 {
		t.Fatalf("result of the removal: %+v", removed)
	}
	mustEdit(t, st, "note.remove", func(e *Edit) error { return e.RemoveNote("I0001", "N0002") })
	tree = checkLinks(t, st)
	if noteByID(tree, "N0001") != nil {
		t.Fatal("the note of one person stays")
	}
	if noteByID(tree, "N0002") == nil {
		t.Fatal("the shared note went away")
	}
	if got := personByID(tree, "I0001").Notes; len(got) != 0 {
		t.Fatalf("note links of the person: %+v", got)
	}
	if got := personByID(tree, "I0002").Notes; !slices.Equal(handlesOf(got), []string{"_n2"}) {
		t.Fatalf("note links of the other person: %+v", got)
	}
	mustEdit(t, st, "note.remove", func(e *Edit) error { return e.RemoveNote("I0002", "N0002") })
	if noteByID(checkLinks(t, st), "N0002") != nil {
		t.Fatal("the note stays after its last person removed it")
	}
}

// A note that a family or an event uses is shared too.
func TestNoteOfAnEventStaysWhenAPersonRemovesIt(t *testing.T) {
	tree := editTree()
	tree.Events[2].Notes = []model.Ref{{HLink: "_n1"}}
	st, _ := editStore(t, tree)
	mustEdit(t, st, "note.remove", func(e *Edit) error { return e.RemoveNote("I0001", "N0001") })
	if after := exported(t, st); noteByID(after, "N0001") == nil || len(eventByID(after, "E0003").Notes) != 1 {
		t.Fatal("the note of an event went away")
	}
}

func TestPhotos(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	first := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/0123abcd.jpg", "image/jpeg", "aa11") })
	if first.Created != "O0002" || first.Subject != "I0001" || first.ChangeID == 0 {
		t.Fatalf("result: %+v", first)
	}
	tree := checkLinks(t, st)
	item := mediaByID(tree, "O0002")
	if item == nil || item.File != (model.MediaFile{Src: "heirfold/0123abcd.jpg", Mime: "image/jpeg", Checksum: "aa11"}) || !strings.HasPrefix(item.Handle, "_") {
		t.Fatalf("media: %+v", item)
	}
	refs := personByID(tree, "I0001").MediaRefs
	if len(refs) != 2 || refs[0].HLink != item.Handle || refs[1].HLink != "_o1" {
		t.Fatalf("media links: %+v", refs)
	}
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0001"); card.Photo == nil || card.Photo.ID != "O0002" {
		t.Fatalf("photo of the card: %+v", card.Photo)
	}
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0001")
	if err != nil || detail.Photo == nil || detail.Photo.ID != "O0002" || len(detail.Media) != 2 || detail.Media[0].ID != "O0002" || !detail.Media[0].Image {
		t.Fatalf("media in the view: %+v %v", detail, err)
	}

	// A second picture goes to the front.
	second := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/4567.png", "image/png", "bb22") })
	if second.Created != "O0003" {
		t.Fatalf("result: %+v", second)
	}
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0001"); card.Photo == nil || card.Photo.ID != "O0003" {
		t.Fatalf("photo of the card: %+v", card.Photo)
	}
	// The same file again: no second media object. The link moves to the front.
	again := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/0123abcd.jpg", "image/jpeg", "aa11") })
	tree = checkLinks(t, st)
	if again.Created != "O0002" || len(tree.Media) != 3 {
		t.Fatalf("the same file again: %+v, media %d", again, len(tree.Media))
	}
	refs = personByID(tree, "I0001").MediaRefs
	if len(refs) != 3 || refs[0].HLink != mediaByID(tree, "O0002").Handle || refs[1].HLink != mediaByID(tree, "O0003").Handle || refs[2].HLink != "_o1" {
		t.Fatalf("media links: %+v", refs)
	}
	noChange(t, st, "the first picture again", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/0123abcd.jpg", "image/jpeg", "aa11") })
	// Another person shows the same file.
	shared := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0002", "heirfold/0123abcd.jpg", "image/jpeg", "aa11") })
	if shared.Created != "O0002" || len(exported(t, st).Media) != 3 {
		t.Fatalf("a shared file: %+v", shared)
	}

	for _, src := range []string{
		"", "photos/old.jpg", "/etc/passwd", "heirfold", "heirfold.jpg", "Heirfold/a.jpg", "heirfold/../secret.jpg", "heirfold/a/../../b.jpg",
		"../heirfold/a.jpg", "./heirfold/a.jpg", "heirfold\\a.jpg",
	} {
		refuse(t, st, ErrInvalid, "source "+src, func(e *Edit) error { return e.AddPhoto("I0010", src, "image/jpeg", "") })
	}
	refuse(t, st, ErrNotFound, "photo for an unknown person", func(e *Edit) error { return e.AddPhoto("I0999", "heirfold/a.jpg", "image/jpeg", "") })

	// Remove.
	refuse(t, st, ErrNotFound, "a file that the person does not show", func(e *Edit) error { return e.RemoveMedia("I0010", "O0002") })
	refuse(t, st, ErrNotFound, "a file that does not exist", func(e *Edit) error { return e.RemoveMedia("I0001", "O0999") })
	refuse(t, st, ErrNotFound, "a person who does not exist", func(e *Edit) error { return e.RemoveMedia("I0999", "O0002") })
	// The shared file stays in the tree.
	removed := mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", "O0002") })
	if removed.Subject != "I0001" || removed.ChangeID == 0 {
		t.Fatalf("result of the removal: %+v", removed)
	}
	tree = checkLinks(t, st)
	if mediaByID(tree, "O0002") == nil || len(personByID(tree, "I0001").MediaRefs) != 2 || len(personByID(tree, "I0002").MediaRefs) != 1 {
		t.Fatalf("after the removal of a shared file: %+v", personByID(tree, "I0001").MediaRefs)
	}
	// The last link takes the uploaded file out of the tree.
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0002", "O0002") })
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", "O0003") })
	tree = checkLinks(t, st)
	if mediaByID(tree, "O0002") != nil || mediaByID(tree, "O0003") != nil {
		t.Fatalf("uploaded files stay in the tree: %+v", tree.Media)
	}
	// The portrait left with its file. The old picture does not take its place by itself.
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0001"); card.Photo != nil {
		t.Fatalf("photo of the card: %+v", card.Photo)
	}
	// A file that came with the imported tree only loses the link.
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", "O0001") })
	tree = checkLinks(t, st)
	if mediaByID(tree, "O0001") == nil || len(personByID(tree, "I0001").MediaRefs) != 0 {
		t.Fatalf("an imported file after the removal: %+v", tree.Media)
	}
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0001"); card.Photo != nil {
		t.Fatalf("photo of the card: %+v", card.Photo)
	}
}

// A portrait that an editor added leaves the tree with the person. A file of
// the imported tree stays, and a picture that another person shows stays.
func TestDeletePersonRemovesTheUploadedPortrait(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	before := exported(t, st)
	own := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/own.jpg", "image/jpeg", "") }).Created
	shared := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/shared.jpg", "image/jpeg", "") }).Created
	mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0002", "heirfold/shared.jpg", "image/jpeg", "") })
	state, graph := exported(t, st), graphs(t, st)
	tick()
	deleted := mustEdit(t, st, "person.delete", func(e *Edit) error { return e.DeletePerson("I0001") })
	tree := checkLinks(t, st)
	if mediaByID(tree, own) != nil {
		t.Fatal("the uploaded portrait of the removed person stays in the tree")
	}
	if mediaByID(tree, shared) == nil || len(personByID(tree, "I0002").MediaRefs) != 1 {
		t.Fatal("a picture that another person shows went away")
	}
	if mediaByID(tree, "O0001") == nil {
		t.Fatal("a file of the imported tree went away")
	}
	if len(tree.Media) != len(before.Media)+1 {
		t.Fatalf("media: %d", len(tree.Media))
	}
	// The undo brings the portrait back with the person.
	tick()
	if _, err := st.UndoChange(context.Background(), elena, deleted.ChangeID); err != nil {
		t.Fatal(err)
	}
	after := checkLinks(t, st)
	// The home person and the bookmarks are no objects of the journal; see
	// TestUndoOfARemovedHomePersonRestoresTheHome.
	after.HomeHandle, after.Bookmarks = state.HomeHandle, state.Bookmarks
	if !reflect.DeepEqual(state, after) {
		t.Fatalf("the tree after the undo:\n%s", treeDiff(t, state, after))
	}
	if card := cardByID(fullGraph(t, st, privacy.Full), "I0001"); card == nil || card.Photo == nil || card.Photo.ID != shared || cardByID(graph[0], "I0001").Photo.ID != shared {
		t.Fatalf("the portrait after the undo: %+v", card)
	}
}

// A file that nothing points to has no known subject. An edit can leave the
// portrait of a living person in that state, so a guest gets no such file.
func TestGuestGetsNoMediaThatNothingPointsTo(t *testing.T) {
	st, _ := editStore(t, editTree())
	ctx := context.Background()
	guest, member := st.View(privacy.Public), st.View(privacy.Full)
	listed := func(view *View, id string) bool {
		t.Helper()
		items, err := view.MediaList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = view.MediaFile(ctx, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		inList := slices.ContainsFunc(items, func(item MediaView) bool { return item.ID == id })
		if inList != (err == nil) {
			t.Fatalf("%s: in the list %v, as a file %v", id, inList, err)
		}
		return inList
	}
	// The photo of a dead person is open.
	if !listed(guest, "O0001") || !listed(member, "O0001") {
		t.Fatal("the photo of a dead person must be open to a guest")
	}
	// The person drops the photo. The media object of the imported tree stays.
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", "O0001") })
	if mediaByID(exported(t, st), "O0001") == nil {
		t.Fatal("the media object went away")
	}
	if listed(guest, "O0001") {
		t.Fatal("a guest gets a file that nothing points to")
	}
	if !listed(member, "O0001") {
		t.Fatal("a member must still get the file")
	}
	// The same holds for a tree that comes with such a file.
	tree := editTree()
	tree.Media = append(tree.Media, model.Media{Object: obj("_o2", "O0002", false), File: model.MediaFile{Src: "photos/loose.jpg", Mime: "image/jpeg"}})
	other, _ := editStore(t, tree)
	if listed(other.View(privacy.Public), "O0002") || !listed(other.View(privacy.Full), "O0002") || !listed(other.View(privacy.Public), "O0001") {
		t.Fatal("an imported file that nothing points to: wrong access")
	}
	// A link from a dead person opens the file again.
	mustEdit(t, st, "link", func(e *Edit) error {
		person, err := e.Person("I0002")
		if err != nil {
			return err
		}
		person.MediaRefs = append(person.MediaRefs, model.MediaRef{HLink: "_o1"})
		e.touch(model.KindPerson, person.Handle)
		return nil
	})
	if !listed(guest, "O0001") {
		t.Fatal("a file of a dead person must open again")
	}
}

func TestGuestDoesNotSeeANewPerson(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	guest, member := st.View(privacy.Public), st.View(privacy.Full)
	visible := func(view *View, id string) bool {
		t.Helper()
		graph, err := view.Graph(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = view.Person(ctx, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		inGraph := cardByID(graph, id) != nil
		if inGraph != (err == nil) {
			t.Fatalf("%s: in the graph %v, as a person %v", id, inGraph, err)
		}
		return inGraph
	}
	if !visible(guest, "I0001") || !visible(guest, "I0003") {
		t.Fatal("the guest must see the dead people of the test tree")
	}

	// A new person has no dates. When in doubt, a person is living.
	before := revision(t, st)
	result := mustEdit(t, st, "person.add", func(e *Edit) error {
		person, err := e.CreatePerson(personInput("F", "CanaryNewGiven", "CanaryNewSurname", ""))
		if err != nil {
			return err
		}
		return e.Link("I0003", AsChild, person.ID, "")
	})
	id := result.Created
	if result.Revision != before+1 || revision(t, st) != before+1 {
		t.Fatalf("revision %d -> %d", before, result.Revision)
	}
	if !visible(member, id) {
		t.Fatal("a member must see the new person")
	}
	if card := cardByID(fullGraph(t, st, privacy.Full), id); !card.Living {
		t.Fatalf("a new person does not count as living: %+v", card)
	}
	if visible(guest, id) {
		t.Fatal("a guest sees a new person who counts as living")
	}
	raw, _ := json.Marshal(fullGraph(t, st, privacy.Public))
	if strings.Contains(string(raw), "CanaryNew") || strings.Contains(string(raw), id) {
		t.Fatalf("the graph of a guest holds the new person: %s", raw)
	}
	// The parent of the new person stays visible, without the child.
	parent, err := guest.Person(ctx, "I0003")
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(parent); strings.Contains(string(raw), "CanaryNew") || strings.Contains(string(raw), id) {
		t.Fatalf("the parent of the new person shows the child to a guest: %s", raw)
	}

	// A death event opens the person for guests.
	tick()
	mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: id}, EventInput{Type: "Death", Date: dated("1960")})
	})
	if !visible(guest, id) {
		t.Fatal("a guest must see a person with a death event")
	}
	// The private flag hides the person again.
	form := describe(t, st, id)
	if form.Private {
		t.Fatal("a new person is private")
	}
	tick()
	rev := revision(t, st)
	mustEdit(t, st, "person.update", func(e *Edit) error {
		return e.UpdatePerson(id, form.Rev, PersonInput{Gender: form.Gender, Private: true, Names: form.Names})
	})
	if revision(t, st) != rev+1 {
		t.Fatalf("revision %d -> %d", rev, revision(t, st))
	}
	if visible(guest, id) {
		t.Fatal("a guest sees a private person")
	}
	if !visible(member, id) || !describe(t, st, id).Private {
		t.Fatal("a member must see the private person")
	}
	detail, err := member.Person(ctx, id)
	if err != nil || !detail.Private {
		t.Fatalf("the private flag in the view: %+v %v", detail, err)
	}

	// A new private event and a new private person never reach a guest.
	mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0003"}, EventInput{Type: "Residence", Description: "CanaryNewEvent", Private: true})
	})
	mustEdit(t, st, "note.add", func(e *Edit) error { return e.AddNote(id, "CanaryNewNote", false) })
	hidden := mustEdit(t, st, "person.add", func(e *Edit) error {
		in := personInput("M", "CanaryNewPrivate", "", "")
		in.Private = true
		_, err := e.CreatePerson(in)
		return err
	})
	if visible(guest, hidden.Created) {
		t.Fatal("a guest sees a new private person")
	}
	graph, _ := json.Marshal(fullGraph(t, st, privacy.Public))
	parent, err = guest.Person(ctx, "I0003")
	if err != nil {
		t.Fatal(err)
	}
	person, _ := json.Marshal(parent)
	if strings.Contains(string(graph)+string(person), "CanaryNew") {
		t.Fatalf("a guest gets new private data: %s %s", graph, person)
	}
	if full, _ := json.Marshal(mustPerson(t, member, "I0003")); !strings.Contains(string(full), "CanaryNewEvent") {
		t.Fatalf("a member must get the private event: %s", full)
	}
}

func mustPerson(t *testing.T, view *View, id string) *PersonDetail {
	t.Helper()
	detail, err := view.Person(context.Background(), id)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return detail
}

func listChanges(t *testing.T, st *Store, subject string, limit int) []ChangeEntry {
	t.Helper()
	entries, err := st.ListChanges(context.Background(), subject, limit)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestJournalListsTheEdits(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	if entries := listChanges(t, st, "", 10); entries == nil || len(entries) != 0 {
		t.Fatalf("journal of a new tree: %#v", entries)
	}
	tick()
	first, err := st.Edit(ctx, elena, "person.add", func(e *Edit) error {
		person, err := e.CreatePerson(personInput("F", "Anna", "Orlova", ""))
		if err != nil {
			return err
		}
		return e.Link("I0003", AsChild, person.ID, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	tick()
	second, err := st.Edit(ctx, egor, "note.add", func(e *Edit) error { return e.AddNote("I0010", "A note.", false) })
	if err != nil {
		t.Fatal(err)
	}
	tick()
	third, err := st.Edit(ctx, Actor{}, "person.update", func(e *Edit) error {
		return updatePerson(e, "I0003", PersonInput{Gender: "M", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: "Petr", Surname: "Orlov", Patronymic: "Ivanovich"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	// An edit that changes nothing and an edit that fails write no entry.
	noChange(t, st, "no change", func(e *Edit) error { return e.SetHome("I0001") })
	refuse(t, st, ErrNotFound, "failure", func(e *Edit) error { return e.DeletePerson("I0999") })
	if !(first.ChangeID < second.ChangeID && second.ChangeID < third.ChangeID) {
		t.Fatalf("the numbers of the entries do not grow: %d %d %d", first.ChangeID, second.ChangeID, third.ChangeID)
	}

	base := testBase.Unix()
	want := []ChangeEntry{
		{ID: third.ChangeID, At: base + 3, Actor: "", Action: "person.update", Subject: "I0003", Objects: 1},
		// The note and the person who got it.
		{ID: second.ChangeID, At: base + 2, Actor: "Egor Editor", Action: "note.add", Subject: "I0010", Objects: 2},
		// The new person, the new family and the parent. The entry is about
		// the person of the tree who got the new relative.
		{ID: first.ChangeID, At: base + 1, Actor: "Elena Editor", Action: "person.add", Subject: "I0003", Objects: 3},
	}
	if got := listChanges(t, st, "", 10); !reflect.DeepEqual(got, want) {
		t.Fatalf("journal:\n got %+v\nwant %+v", got, want)
	}
	if got := listChanges(t, st, "", 2); !reflect.DeepEqual(got, want[:2]) {
		t.Fatalf("the two newest entries: %+v", got)
	}
	if got := listChanges(t, st, "I0010", 10); !reflect.DeepEqual(got, want[1:2]) {
		t.Fatalf("entries about one person: %+v", got)
	}
	if got := listChanges(t, st, "I0003", 10); !reflect.DeepEqual(got, []ChangeEntry{want[0], want[2]}) {
		t.Fatalf("entries about another person: %+v", got)
	}
	if first.Created != "I0011" || first.Subject != "I0003" {
		t.Fatalf("result of a new person with a relation: %+v", first)
	}
	if got := listChanges(t, st, "I0999", 10); len(got) != 0 {
		t.Fatalf("entries about nobody: %+v", got)
	}
	if n := journalSize(t, st); n != 3 {
		t.Fatalf("CountChanges: %d", n)
	}
	// The entry keeps the number of the account next to the name.
	var actorID any
	if err := st.r.QueryRowContext(ctx, `SELECT actor_id FROM change_set WHERE id = ?`, second.ChangeID).Scan(&actorID); err != nil || actorID != int64(8) {
		t.Fatalf("actor_id: %v %v", actorID, err)
	}
}

// editSteps is a series of edits that touches every kind of object.
func editSteps() []struct {
	action string
	fn     func(*Edit) error
} {
	return []struct {
		action string
		fn     func(*Edit) error
	}{
		{"person.add", func(e *Edit) error {
			person, err := e.CreatePerson(personInput("F", "Anna", "Orlova", "Petrovna"))
			if err != nil {
				return err
			}
			return e.Link("I0003", AsChild, person.ID, "")
		}},
		{"person.update", func(e *Edit) error {
			return updatePerson(e, "I0001", PersonInput{Gender: "U", Private: true, Names: []NameInput{
				{Index: 1, Type: "Also Known As", Given: "John", Surname: "Orloff"}, {Index: -1, Given: "Jan"}}})
		}},
		{"link.add", func(e *Edit) error { return e.Link("I0010", AsSpouse, "I0003", "") }},
		{"event.add", func(e *Edit) error {
			return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0010"}, EventInput{Type: "Residence", Date: dated("1890-05"), Place: &PlaceInput{Name: "New Town"}})
		}},
		{"event.add", func(e *Edit) error {
			return e.AddEvent(Owner{Kind: model.KindFamily, ID: "F0001"}, EventInput{Type: "Divorce", Place: &PlaceInput{ID: "P0001"}})
		}},
		{"event.update", func(e *Edit) error {
			return updateEvent(e, "E0001", EventInput{Type: "Baptism", Date: model.Dated{DateStr: &model.DateStr{Val: "spring"}}, Description: "changed"})
		}},
		{"event.remove", func(e *Edit) error { return e.RemoveEvent(Owner{Kind: model.KindPerson, ID: "I0001"}, "E0002") }},
		{"note.add", func(e *Edit) error { return e.AddNote("I0002", "A new note.", false) }},
		{"note.update", func(e *Edit) error { return updateNote(e, "N0001", "Other text.") }},
		{"note.remove", func(e *Edit) error { return e.RemoveNote("I0001", "N0001") }},
		{"media.add", func(e *Edit) error { return e.AddPhoto("I0002", "heirfold/aa.png", "image/png", "c1") }},
		{"media.remove", func(e *Edit) error { return e.RemoveMedia("I0002", "O0002") }},
		{"family.update", func(e *Edit) error { return e.SetFamilyType("F0001", "Unmarried") }},
		{"link.remove", func(e *Edit) error { return e.Unlink("F0001", "I0003") }},
		{"link.remove", func(e *Edit) error { return e.Unlink("F0001", "I0002") }},
		{"person.delete", func(e *Edit) error { return e.DeletePerson("I0003") }},
		{"person.delete", func(e *Edit) error { return e.DeletePerson("I0002") }},
	}
}

// treeDiff names the parts of two trees that differ, with their JSON form.
func treeDiff(t *testing.T, want, got *model.Tree) string {
	t.Helper()
	var out []string
	a, b := reflect.ValueOf(*want), reflect.ValueOf(*got)
	for i := 0; i < a.NumField(); i++ {
		if reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			continue
		}
		x, y := asJSON(t, a.Field(i).Interface()), asJSON(t, b.Field(i).Interface())
		// The common start of the two texts is of no interest.
		start := 0
		for start < len(x) && start < len(y) && x[start] == y[start] {
			start++
		}
		start = max(0, start-80)
		out = append(out, fmt.Sprintf("%s:\n  want ...%.600s\n   got ...%.600s", a.Type().Field(i).Name, x[start:], y[start:]))
	}
	return strings.Join(out, "\n")
}

// sameState compares two trees and the graphs that the views give.
func sameState(t *testing.T, step string, wantTree, gotTree *model.Tree, wantGraph, gotGraph []*Graph) {
	t.Helper()
	if !reflect.DeepEqual(wantTree, gotTree) {
		t.Fatalf("%s: the tree differs:\n%s", step, treeDiff(t, wantTree, gotTree))
	}
	if !reflect.DeepEqual(wantGraph, gotGraph) {
		t.Fatalf("%s: the graph differs:\nwant %.1500s\n got %.1500s", step, asJSON(t, wantGraph), asJSON(t, gotGraph))
	}
}

func graphs(t *testing.T, st *Store) []*Graph {
	t.Helper()
	return []*Graph{fullGraph(t, st, privacy.Full), fullGraph(t, st, privacy.Public)}
}

// Every step is undone right after it ran, then the undo is undone. The tree
// must go back to the exact state before the step, and forward again.
func TestUndoRestoresTheExactState(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	for i, step := range editSteps() {
		name := fmt.Sprintf("step %d (%s)", i+1, step.action)
		tick()
		beforeTree, beforeGraph, beforeRev := exported(t, st), graphs(t, st), revision(t, st)
		result := mustEdit(t, st, step.action, step.fn)
		if result.ChangeID == 0 {
			t.Fatalf("%s: no journal entry", name)
		}
		afterTree, afterGraph := checkLinks(t, st), graphs(t, st)
		if reflect.DeepEqual(beforeTree, afterTree) {
			t.Fatalf("%s: the edit changed nothing", name)
		}

		tick()
		undo, err := st.UndoChange(ctx, egor, result.ChangeID)
		if err != nil {
			t.Fatalf("%s: undo: %v", name, err)
		}
		if undo.ChangeID <= result.ChangeID || undo.Subject != result.Subject || undo.Revision != beforeRev+2 || revision(t, st) != beforeRev+2 {
			t.Fatalf("%s: result of the undo: %+v after %+v", name, undo, result)
		}
		sameState(t, name+", after the undo", beforeTree, checkLinks(t, st), beforeGraph, graphs(t, st))
		entries := listChanges(t, st, "", 2)
		if entries[0].ID != undo.ChangeID || entries[0].Action != "undo" || entries[0].Undoes != result.ChangeID || entries[0].Undone ||
			entries[0].Actor != "Egor Editor" || entries[0].Subject != result.Subject || entries[0].Objects != entries[1].Objects ||
			entries[1].ID != result.ChangeID || !entries[1].Undone || entries[1].Undoes != 0 {
			t.Fatalf("%s: journal after the undo: %+v", name, entries)
		}
		// An entry is undone one time.
		if _, err := st.UndoChange(ctx, egor, result.ChangeID); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s: the second undo: %v", name, err)
		}
		sameState(t, name+", after the refused second undo", beforeTree, exported(t, st), beforeGraph, graphs(t, st))

		// The undo of the undo brings the edit back.
		tick()
		redo, err := st.UndoChange(ctx, elena, undo.ChangeID)
		if err != nil {
			t.Fatalf("%s: undo of the undo: %v", name, err)
		}
		sameState(t, name+", after the undo of the undo", afterTree, checkLinks(t, st), afterGraph, graphs(t, st))
		entries = listChanges(t, st, "", 3)
		if entries[0].ID != redo.ChangeID || entries[0].Action != "undo" || entries[0].Undoes != undo.ChangeID || !entries[1].Undone || !entries[2].Undone {
			t.Fatalf("%s: journal after the undo of the undo: %+v", name, entries)
		}
		for _, id := range []int64{result.ChangeID, undo.ChangeID} {
			if _, err := st.UndoChange(ctx, egor, id); !errors.Is(err, ErrConflict) {
				t.Fatalf("%s: an entry that is already undone: %v", name, err)
			}
		}
	}
	if n := journalSize(t, st); n != 3*len(editSteps()) {
		t.Fatalf("journal entries: %d", n)
	}
}

// All steps run, then they are undone from the newest to the oldest.
func TestUndoOfAllStepsGivesTheFirstTree(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	start, startGraph := exported(t, st), graphs(t, st)
	var ids []int64
	var states []*model.Tree
	for _, step := range editSteps() {
		tick()
		states = append(states, exported(t, st))
		ids = append(ids, mustEdit(t, st, step.action, step.fn).ChangeID)
	}
	for i := len(ids) - 1; i >= 0; i-- {
		tick()
		if _, err := st.UndoChange(ctx, elena, ids[i]); err != nil {
			t.Fatalf("undo of step %d: %v", i+1, err)
		}
		if got := checkLinks(t, st); !reflect.DeepEqual(states[i], got) {
			t.Fatalf("after the undo of step %d the tree differs:\n%s", i+1, treeDiff(t, states[i], got))
		}
	}
	sameState(t, "after all undos", start, exported(t, st), startGraph, graphs(t, st))
}

func TestUndoNeedsTheStateThatTheEntryLeft(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	rename := func(given string) func(*Edit) error {
		return func(e *Edit) error {
			return updatePerson(e, "I0002", PersonInput{Gender: "F", Names: []NameInput{{Index: 0, Type: "Birth Name", Given: given, Surname: "Orlova"}}})
		}
	}
	tick()
	first := mustEdit(t, st, "person.update", rename("Marya"))
	tick()
	second := mustEdit(t, st, "person.update", rename("Masha"))
	state := exported(t, st)
	rev := revision(t, st)
	tick()
	if _, err := st.UndoChange(ctx, elena, first.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of an entry with a later change of the same person: %v", err)
	}
	if !reflect.DeepEqual(state, exported(t, st)) || revision(t, st) != rev || journalSize(t, st) != 2 {
		t.Fatal("a refused undo changed the store")
	}
	if entries := listChanges(t, st, "", 5); entries[0].Undone || entries[1].Undone {
		t.Fatalf("a refused undo marked an entry: %+v", entries)
	}
	// The later edit goes first.
	if _, err := st.UndoChange(ctx, elena, second.ChangeID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UndoChange(ctx, elena, first.ChangeID); err != nil {
		t.Fatalf("undo after the later edit was undone: %v", err)
	}
	if got := personByID(exported(t, st), "I0002").Names[0].First; got != "Maria" {
		t.Fatalf("name after both undos: %q", got)
	}

	// An entry that made a person: a later link of that person blocks the undo.
	tick()
	created := mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(personInput("F", "Anna", "", ""))
		return err
	})
	tick()
	linked := link(t, st, "I0010", AsChild, created.Created, "")
	if _, err := st.UndoChange(ctx, elena, created.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of a new person who got a link later: %v", err)
	}
	checkLinks(t, st)
	// An entry that removed a person: the ID is taken again, so the undo stops.
	if _, err := st.UndoChange(ctx, elena, linked.ChangeID); err != nil {
		t.Fatal(err)
	}
	tick()
	deleted := mustEdit(t, st, "person.delete", func(e *Edit) error { return e.DeletePerson(created.Created) })
	tick()
	if again := addPerson(t, st, "M", "Boris", ""); again != created.Created {
		t.Fatalf("the test expects that the free ID %s is used again, got %s", created.Created, again)
	}
	if _, err := st.UndoChange(ctx, elena, deleted.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of a removal whose ID is taken again: %v", err)
	}
	checkLinks(t, st)

	for _, id := range []int64{0, -1, 999999} {
		if _, err := st.UndoChange(ctx, elena, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("undo of the unknown entry %d: %v", id, err)
		}
	}
}

// An undo that removes an object must not leave links to that object.
func TestUndoIsRefusedWhileALaterObjectUsesWhatTheEntryMade(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	// Entry 1 makes an event with a new place. Entry 2 uses that place again.
	tick()
	first := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0010"}, EventInput{Type: "Residence", Place: &PlaceInput{Name: "Kazan"}})
	})
	tick()
	second := mustEdit(t, st, "event.add", func(e *Edit) error {
		return e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0003"}, EventInput{Type: "Residence", Place: &PlaceInput{Name: "kazan"}})
	})
	state, rev := exported(t, st), revision(t, st)
	if len(placeByTitle(state, "Kazan")) != 1 {
		t.Fatalf("places: %+v", state.Places)
	}
	tick()
	if _, err := st.UndoChange(ctx, elena, first.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of the entry that made a place in use: %v", err)
	}
	if !reflect.DeepEqual(state, checkLinks(t, st)) || revision(t, st) != rev || listChanges(t, st, "", 1)[0].ID != second.ChangeID {
		t.Fatal("a refused undo changed the store")
	}
	// In the other order both undos work, and the place goes with its entry.
	if _, err := st.UndoChange(ctx, elena, second.ChangeID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UndoChange(ctx, elena, first.ChangeID); err != nil {
		t.Fatalf("undo after the later entry was undone: %v", err)
	}
	if after := checkLinks(t, st); len(placeByTitle(after, "Kazan")) != 0 || !reflect.DeepEqual(after, editTree()) {
		t.Fatalf("the tree after both undos: %+v", after.Counts())
	}

	// Entry 3 adds a picture. Entry 4 gives the same file to another person.
	tick()
	photo := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/aa.jpg", "image/jpeg", "") })
	tick()
	shared := mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0002", "heirfold/aa.jpg", "image/jpeg", "") })
	if _, err := st.UndoChange(ctx, elena, photo.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of the entry that made a shared picture: %v", err)
	}
	checkLinks(t, st)
	if _, err := st.UndoChange(ctx, elena, shared.ChangeID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UndoChange(ctx, elena, photo.ChangeID); err != nil {
		t.Fatal(err)
	}

	// Entry 5 makes a person. The person becomes the centre of the tree, which
	// is a link from the data of the tree and from no object.
	tick()
	created := mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(personInput("F", "Anna", "", ""))
		return err
	})
	tick()
	mustEdit(t, st, "home.set", func(e *Edit) error { return e.SetHome(created.Created) })
	if _, err := st.UndoChange(ctx, elena, created.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo of the entry that made the home person: %v", err)
	}
	if tree := checkLinks(t, st); personByID(tree, created.Created) == nil || tree.HomeHandle != personByID(tree, created.Created).Handle {
		t.Fatal("a refused undo removed the home person")
	}
	mustEdit(t, st, "home.set", func(e *Edit) error { return e.SetHome("I0001") })
	if _, err := st.UndoChange(ctx, elena, created.ChangeID); err != nil {
		t.Fatalf("undo after the home person changed: %v", err)
	}
	checkLinks(t, st)
}

// The home person is part of the state that an editor sees. An undo of the
// removal of that person must bring the person back as the home person.
func TestUndoOfARemovedHomePersonRestoresTheHome(t *testing.T) {
	// BUG: internal/store/edit_ops.go:608-617 with internal/store/edit.go:UndoChange.
	// DeletePerson clears Tree.HomeHandle and the bookmarks of the person, and
	// deleteFamily clears the bookmarks of a family. The journal keeps only
	// objects, so UndoChange brings the person back but not the home person
	// and not the bookmarks. ADR 0008 says that an undo takes the entry back.
	tree := editTree()
	tree.Bookmarks = append(tree.Bookmarks, model.Bookmark{Target: "family", HLink: "_f1"})
	tree.Families[0].ChildRefs = nil
	tree.People[2].ChildOf = nil
	st, tick := editStore(t, tree)
	before, beforeGraph := exported(t, st), graphs(t, st)
	tick()
	// The family of two people goes away with the person.
	deleted := mustEdit(t, st, "person.delete", func(e *Edit) error { return e.DeletePerson("I0001") })
	tick()
	if _, err := st.UndoChange(context.Background(), elena, deleted.ChangeID); err != nil {
		t.Fatal(err)
	}
	after := exported(t, st)
	if after.HomeHandle != before.HomeHandle {
		t.Errorf("home person after the undo: %q, want %q", after.HomeHandle, before.HomeHandle)
	}
	if !reflect.DeepEqual(after.Bookmarks, before.Bookmarks) {
		t.Errorf("bookmarks after the undo: %+v, want %+v", after.Bookmarks, before.Bookmarks)
	}
	sameState(t, "after the undo", before, after, beforeGraph, graphs(t, st))
}

// A change of the home person is an action of an editor, so it has an entry
// in the journal and the editor can take it back.
func TestUndoOfANewHomePerson(t *testing.T) {
	// DOUBT: internal/store/edit.go:362-367 and :539. SetHome changes only the
	// metadata of the tree. Store.Edit writes a journal entry with no objects
	// for it, and UndoChange answers ErrConflict for an entry with no objects.
	// The history shows an action that can never be undone, and the answer
	// says that the record changed in the meantime.
	st, tick := editStore(t, editTree())
	before := exported(t, st)
	tick()
	result := mustEdit(t, st, "home.set", func(e *Edit) error { return e.SetHome("I0010") })
	if result.ChangeID == 0 {
		t.Fatal("no journal entry")
	}
	if _, err := st.UndoChange(context.Background(), elena, result.ChangeID); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if after := exported(t, st); after.HomeHandle != before.HomeHandle {
		t.Fatalf("home person after the undo: %q", after.HomeHandle)
	}
}

func TestReplaceTreeClearsTheJournal(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	for _, step := range editSteps()[:6] {
		tick()
		mustEdit(t, st, step.action, step.fn)
	}
	last := listChanges(t, st, "", 1)[0]
	if _, err := st.UndoChange(ctx, elena, last.ID); err != nil {
		t.Fatal(err)
	}
	if n := journalSize(t, st); n != 7 {
		t.Fatalf("journal before the replacement: %d", n)
	}
	if _, err := st.ReplaceTree(ctx, editTree()); err != nil {
		t.Fatal(err)
	}
	if n := journalSize(t, st); n != 0 {
		t.Fatalf("CountChanges after the replacement: %d", n)
	}
	if entries := listChanges(t, st, "", 10); len(entries) != 0 {
		t.Fatalf("journal after the replacement: %+v", entries)
	}
	for _, table := range []string{"change_set", "change_item"} {
		if n := countRows(t, st, table); n != 0 {
			t.Fatalf("%s keeps %d rows", table, n)
		}
	}
	if _, err := st.UndoChange(ctx, elena, last.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("undo of an entry of the old tree: %v", err)
	}
	if !reflect.DeepEqual(editTree(), exported(t, st)) {
		t.Fatal("the new tree is not the imported one")
	}
	// The new tree takes edits again, and the numbers of the people start
	// from the new tree.
	if id := addPerson(t, st, "F", "Anna", ""); id != "I0011" {
		t.Fatalf("the first new person after the replacement: %s", id)
	}
}

func TestInspectStoresNothing(t *testing.T) {
	st, _ := editStore(t, editTree())
	before, rev := exported(t, st), revision(t, st)
	var options []PlaceOption
	err := st.Inspect(context.Background(), func(e *Edit) error {
		if _, err := e.CreatePerson(personInput("F", "Anna", "", "")); err != nil {
			return err
		}
		if err := e.AddEvent(Owner{Kind: model.KindPerson, ID: "I0001"}, EventInput{Type: "Residence", Place: &PlaceInput{Name: "alpha Town"}}); err != nil {
			return err
		}
		options = e.Places()
		return e.DeletePerson("I0002")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, exported(t, st)) || revision(t, st) != rev || journalSize(t, st) != 0 {
		t.Fatal("Inspect changed the store")
	}
	// The places come by name, with no difference of case.
	if len(options) != 2 || options[0].Name != "alpha Town" || options[1] != (PlaceOption{ID: "P0001", Name: "Old Town"}) {
		t.Fatalf("places: %+v", options)
	}
	if err := st.Inspect(context.Background(), func(e *Edit) error {
		_, err := e.Describe("I0999")
		return err
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("form of an unknown person: %v", err)
	}
	want := errors.New("stop")
	if err := st.Inspect(context.Background(), func(*Edit) error { return want }); !errors.Is(err, want) {
		t.Fatalf("the error of the function: %v", err)
	}
}

// After a series of edits the tree goes to Gramps XML and back with no loss
// and no warning.
func TestEditedTreeSurvivesTheGrampsFormat(t *testing.T) {
	tree := fixture.Generate(fixture.DefaultOptions("en"))
	st, tick := editStore(t, tree)
	home := ""
	for i := range tree.People {
		if tree.People[i].Handle == tree.HomeHandle {
			home = tree.People[i].ID
		}
	}
	other := tree.People[len(tree.People)-1].ID
	if home == "" || other == home {
		t.Fatalf("home %q, other %q", home, other)
	}
	var child, spouse string
	steps := []struct {
		action string
		fn     func(*Edit) error
	}{
		{"person.add", func(e *Edit) error {
			person, err := e.CreatePerson(PersonInput{Gender: "F", Names: []NameInput{
				{Index: -1, Given: "Анна", Surname: "Орлова", Patronymic: "Ивановна", Nick: "Аня", Title: "Dr.", Suffix: "Jr.", Call: "Анна"},
				{Index: -1, Type: "Married Name", Given: "Anna", Surname: "O'Brien & <Sons>"},
			}})
			if err != nil {
				return err
			}
			child = person.ID
			return e.Link(home, AsChild, child, "")
		}},
		{"person.add", func(e *Edit) error {
			in := personInput("M", "Boris", "", "")
			in.Private = true
			person, err := e.CreatePerson(in)
			if err != nil {
				return err
			}
			spouse = person.ID
			return e.Link(child, AsSpouse, spouse, "")
		}},
		{"link.add", func(e *Edit) error { return e.Link(child, AsSibling, other, "") }},
		{"person.add", func(e *Edit) error {
			person, err := e.CreatePerson(personInput("U", "", "", ""))
			if err != nil {
				return err
			}
			return e.Link(spouse, AsParent, person.ID, "")
		}},
		{"event.add", func(e *Edit) error {
			return e.AddEvent(Owner{Kind: model.KindPerson, ID: child}, EventInput{
				Type: "Birth", Date: model.Dated{DateVal: &model.DateVal{Val: "1990-02-03", Type: "about", Quality: "estimated"}},
				Place: &PlaceInput{Name: "Новый город \"у реки\""}, Description: "a < b & c > d", Private: true})
		}},
		{"event.add", func(e *Edit) error {
			return e.AddEvent(Owner{Kind: model.KindPerson, ID: child}, EventInput{Type: "Residence", Date: model.Dated{DateSpan: &model.DatePair{Start: "2000", Stop: "2010-05"}}})
		}},
		{"event.add", func(e *Edit) error {
			return e.AddEvent(Owner{Kind: model.KindPerson, ID: spouse}, EventInput{Type: "Custom type", Date: model.Dated{DateStr: &model.DateStr{Val: "после войны"}}})
		}},
		{"note.add", func(e *Edit) error {
			return e.AddNote(child, "Первая строка.\nSecond line with a tab:\there.\n\n<b>not a tag</b> & more", false)
		}},
		{"media.add", func(e *Edit) error {
			return e.AddPhoto(child, "heirfold/00112233445566778899aabbccddeeff.jpg", "image/jpeg", "d41d8cd98f00b204e9800998ecf8427e")
		}},
		{"media.add", func(e *Edit) error {
			return e.AddPhoto(spouse, "heirfold/ffeeddccbbaa99887766554433221100.png", "image/png", "")
		}},
		{"home.set", func(e *Edit) error { return e.SetHome(child) }},
		{"person.delete", func(e *Edit) error { return e.DeletePerson(home) }},
	}
	for i, step := range steps {
		tick()
		if result := mustEdit(t, st, step.action, step.fn); result.ChangeID == 0 {
			t.Fatalf("step %d (%s) changed nothing", i+1, step.action)
		}
	}
	// Every person and every event gets the form saved once more, with a
	// small change, so the update paths run over real data.
	tick()
	for _, id := range []string{child, spouse, other} {
		form := describe(t, st, id)
		form.Names[0].Nick = "Edited"
		mustEdit(t, st, "person.update", func(e *Edit) error {
			return e.UpdatePerson(id, form.Rev, PersonInput{Gender: form.Gender, Private: form.Private, Names: form.Names})
		})
		for _, event := range form.Events {
			in := EventInput{Type: event.Type, Date: event.Date, Place: event.Place, Description: event.Description + " (checked)", Private: event.Private}
			mustEdit(t, st, "event.update", func(e *Edit) error { return e.UpdateEvent(event.ID, event.Rev, in) })
		}
		for _, family := range form.Families {
			mustEdit(t, st, "event.add", func(e *Edit) error {
				return e.AddEvent(Owner{Kind: model.KindFamily, ID: family.ID}, EventInput{Type: "Marriage", Date: dated("2015-06-07")})
			})
		}
	}

	out := checkLinks(t, st)
	if len(out.People) != len(tree.People)+2 || out.HomeHandle != personByID(out, child).Handle {
		t.Fatalf("people after the edits: %d, was %d", len(out.People), len(tree.People))
	}
	var plain bytes.Buffer
	if err := gramps.Write(&plain, out); err != nil {
		t.Fatal(err)
	}
	parsed, err := gramps.Parse(bytes.NewReader(plain.Bytes()), gramps.DefaultLimits)
	if err != nil {
		t.Fatalf("Gramps XML of the edited tree does not parse: %v", err)
	}
	if parsed.WarningsTotal != 0 || len(parsed.Warnings) != 0 {
		t.Fatalf("warnings: %+v", parsed.Warnings)
	}
	if !reflect.DeepEqual(out, parsed.Tree) {
		for _, kind := range []struct {
			name string
			a, b any
		}{
			{"people", out.People, parsed.Tree.People}, {"families", out.Families, parsed.Tree.Families}, {"events", out.Events, parsed.Tree.Events},
			{"places", out.Places, parsed.Tree.Places}, {"notes", out.Notes, parsed.Tree.Notes}, {"media", out.Media, parsed.Tree.Media},
		} {
			if !reflect.DeepEqual(kind.a, kind.b) {
				t.Errorf("the %s changed after a round trip", kind.name)
			}
		}
		t.Fatal("the edited tree changed after a round trip through Gramps XML")
	}
	// The parsed tree imports again and gives the same export.
	second := open(t)
	if _, err := second.ReplaceTree(context.Background(), parsed.Tree); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, exported(t, second)) {
		t.Fatal("the tree changed after an import of its own export")
	}
}

func backupNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func TestBackup(t *testing.T) {
	st, tick := editStore(t, editTree())
	ctx := context.Background()
	tick()
	mustEdit(t, st, "person.add", func(e *Edit) error {
		_, err := e.CreatePerson(personInput("F", "Anna", "Orlova", ""))
		return err
	})
	dir := filepath.Join(t.TempDir(), "data", "backups")
	path, err := st.Backup(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// The clock of the test shows 2026-01-01 12:00:01 UTC.
	if want := filepath.Join(dir, "tree-20260101-120001.gramps"); path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("file mode %v, size %d", info.Mode(), info.Size())
	}
	if dirInfo, err := os.Stat(dir); err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode: %v %v", dirInfo.Mode(), err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the test made this path.
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("the file is no gzip: % x", data[:min(len(data), 4)])
	}
	parsed, err := gramps.Parse(bytes.NewReader(data), gramps.DefaultLimits)
	if err != nil {
		t.Fatalf("the backup does not parse: %v", err)
	}
	if parsed.WarningsTotal != 0 {
		t.Fatalf("warnings: %+v", parsed.Warnings)
	}
	want := exported(t, st)
	if len(parsed.Tree.People) != 5 || personByID(parsed.Tree, "I0011") == nil || !reflect.DeepEqual(want.People, parsed.Tree.People) ||
		!reflect.DeepEqual(want.Families, parsed.Tree.Families) || !reflect.DeepEqual(want.Events, parsed.Tree.Events) ||
		!reflect.DeepEqual(want.Notes, parsed.Tree.Notes) || parsed.Tree.HomeHandle != want.HomeHandle {
		t.Fatalf("the backup differs from the tree: %+v", parsed.Tree.Counts())
	}
	// A backup changes nothing in the store.
	if !reflect.DeepEqual(want, exported(t, st)) || journalSize(t, st) != 1 {
		t.Fatal("the backup changed the store")
	}

	// Other files of the directory stay, also with a similar name.
	for _, name := range []string{"notes.txt", "tree-manual.gramps.bak", "old-tree.gramps"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "tree-folder.gramps"), 0o700); err != nil {
		t.Fatal(err)
	}
	var written []string
	for i := 0; i < 13; i++ {
		tick()
		path, err := st.Backup(ctx, dir)
		if err != nil {
			t.Fatalf("backup %d: %v", i+2, err)
		}
		written = append(written, filepath.Base(path))
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup %d: %v %v", i+2, info, err)
		}
	}
	if backupsKept != 10 {
		t.Fatalf("backupsKept is %d, ADR 0008 says 10", backupsKept)
	}
	want10 := append([]string{"notes.txt", "old-tree.gramps"}, written[len(written)-backupsKept:]...)
	want10 = append(want10, "tree-folder.gramps", "tree-manual.gramps.bak")
	sort.Strings(want10)
	if got := backupNames(t, dir); !slices.Equal(got, want10) {
		t.Fatalf("files after 14 backups:\n got %v\nwant %v", got, want10)
	}
	if written[len(written)-1] != "tree-20260101-120014.gramps" {
		t.Fatalf("the newest backup: %s", written[len(written)-1])
	}
}

// A backup of an empty store is still a file that Gramps XML readers accept.
func TestBackupOfAnEmptyTree(t *testing.T) {
	st, _ := editStore(t, nil)
	path, err := st.Backup(context.Background(), filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path) // #nosec G304 -- the test made this path.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	parsed, err := gramps.Parse(file, gramps.DefaultLimits)
	if err != nil || len(parsed.Tree.People) != 0 {
		t.Fatalf("backup of an empty tree: %v", err)
	}
}

// Two replacements of a tree can fall into one second, for example two
// imports in a row. The second backup must not fail and must not replace the
// first one.
func TestTwoBackupsInOneSecond(t *testing.T) {
	// DOUBT: internal/store/backup.go:29-30. The file name has a resolution of
	// one second and the file opens with O_EXCL, so the second backup of the
	// same second fails with "file exists". handleImportCommit then answers
	// 500 and the import does not happen.
	st, _ := editStore(t, editTree())
	dir := filepath.Join(t.TempDir(), "backups")
	first, err := st.Backup(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Backup(context.Background(), dir)
	if err != nil {
		t.Fatalf("the second backup of the same second: %v", err)
	}
	if first == second || len(backupNames(t, dir)) != 2 {
		t.Fatalf("backups: %q %q", first, second)
	}
}

func TestEditsFromManyGoroutines(t *testing.T) {
	st, _ := editStore(t, editTree())
	// The clock of this test is the real one: the goroutines read it.
	st.now = time.Now
	ctx := context.Background()
	const workers = 20
	var wg sync.WaitGroup
	results := make([]*EditResult, workers)
	created := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = st.Edit(ctx, Actor{ID: int64(i + 1), Name: fmt.Sprintf("Editor %d", i)}, "person.add", func(e *Edit) error {
				person, err := e.CreatePerson(personInput("U", fmt.Sprintf("Worker %d", i), "", ""))
				if err != nil {
					return err
				}
				created[i] = person.ID
				if err := e.AddEvent(Owner{Kind: model.KindPerson, ID: person.ID}, EventInput{Type: "Birth", Place: &PlaceInput{Name: "Shared Town"}}); err != nil {
					return err
				}
				return e.Link("I0010", AsChild, person.ID, "")
			})
		}()
	}
	// Readers work at the same time.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := st.View(privacy.Full).Graph(ctx); err != nil {
					t.Errorf("read during the edits: %v", err)
					return
				}
				if _, err := st.ListChanges(ctx, "", 50); err != nil {
					t.Errorf("journal during the edits: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(stop)
	readers.Wait()

	ids := map[string]bool{}
	changes := map[int64]bool{}
	revisions := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
		ids[created[i]] = true
		changes[results[i].ChangeID] = true
		revisions[results[i].Revision] = true
	}
	if len(ids) != workers || len(changes) != workers || len(revisions) != workers {
		t.Fatalf("unique people %d, journal entries %d, revisions %d, want %d of each", len(ids), len(changes), len(revisions), workers)
	}
	tree := checkLinks(t, st)
	if len(tree.People) != 4+workers || len(tree.Events) != 10+workers {
		t.Fatalf("after the edits: %+v", tree.Counts())
	}
	for id := range ids {
		person := personByID(tree, id)
		if person == nil || len(person.ChildOf) != 1 || len(person.EventRefs) != 1 {
			t.Fatalf("person %s: %+v", id, person)
		}
	}
	// One family holds all the children, and one place serves all the events.
	if len(tree.Families) != 2 || len(familyByID(tree, "F0002").ChildRefs) != workers || len(placeByTitle(tree, "Shared Town")) != 1 {
		t.Fatalf("families %d, places %+v", len(tree.Families), placeByTitle(tree, "Shared Town"))
	}
	if n := journalSize(t, st); n != workers {
		t.Fatalf("journal entries: %d", n)
	}
	if rev := revision(t, st); rev != int64(1+workers) {
		t.Fatalf("revision: %d", rev)
	}
}
