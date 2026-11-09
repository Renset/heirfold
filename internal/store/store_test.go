package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// canaries are strings that a guest must never receive.
const (
	canaryLivingName  = "CanaryLivingName"
	canaryPrivateName = "CanaryPrivatePerson"
	canaryPrivateAlt  = "CanaryPrivateAltName"
	canaryEvent       = "CanaryPrivateEvent"
	canaryRefEvent    = "CanaryPrivateRefEvent"
	canaryNote        = "CanaryPrivateNote"
	canaryAttr        = "CanaryPrivateAttribute"
	canaryPlace       = "CanaryPrivatePlace"
	canarySource      = "CanaryPrivateSource"
	canaryCitation    = "CanaryPrivateCitation"
	canaryMarriage    = "CanaryMarriagePlace"
	canaryFamilyNote  = "CanaryFamilyNote"
	canaryURL         = "https://canary.example/private"
)

func ptr(o model.Object) model.Object { return o }

func obj(handle, id string, private bool) model.Object {
	return ptr(model.Object{Handle: handle, ID: id, Private: model.Flag(private), Change: 1})
}

func dated(y string) model.Dated { return model.Dated{DateVal: &model.DateVal{Val: y}} }

func name(given, surname string) []model.Name {
	return []model.Name{{Type: "Birth Name", First: given, Surnames: []model.Surname{{Value: surname}}}}
}

// privacyTree holds one dead public person with a mix of private details, and
// the relatives that a guest must not see.
func privacyTree() *model.Tree {
	t := &model.Tree{Namespace: "http://gramps-project.org/xml/1.7.2/"}
	t.HomeHandle = "_living"
	t.Places = []model.Place{
		{Object: obj("_pl1", "P1", false), Type: "City", Title: "Public Town"},
		{Object: obj("_pl2", "P2", true), Type: "City", Title: canaryPlace},
		{Object: obj("_pl3", "P3", false), Type: "City", Title: canaryMarriage},
	}
	t.Events = []model.Event{
		{Object: obj("_eb", "E1", false), Type: "Birth", Dated: dated("1900"), Place: &model.Ref{HLink: "_pl1"}},
		{Object: obj("_ed", "E2", false), Type: "Death", Dated: dated("1970")},
		{Object: obj("_ep", "E3", true), Type: "Residence", Description: canaryEvent},
		{Object: obj("_er", "E4", false), Type: "Residence", Description: canaryRefEvent},
		{Object: obj("_ev", "E5", false), Type: "Occupation", Place: &model.Ref{HLink: "_pl2"}, Description: "public occupation"},
		{Object: obj("_em", "E6", false), Type: "Marriage", Dated: dated("1925"), Place: &model.Ref{HLink: "_pl3"}},
		{Object: obj("_lb", "E7", false), Type: "Birth", Dated: dated("1990")},
		{Object: obj("_cb", "E8", false), Type: "Birth", Dated: dated("1930")},
		{Object: obj("_cd", "E9", false), Type: "Death", Dated: dated("2000")},
	}
	t.Notes = []model.Note{
		{Object: obj("_np", "N1", true), Type: "General", Text: canaryNote},
		{Object: obj("_nf", "N2", false), Type: "General", Text: canaryFamilyNote},
		{Object: obj("_nu", "N3", false), Type: "General", Text: "public note"},
	}
	t.Sources = []model.Source{
		{Object: obj("_sp", "S1", true), Title: canarySource},
		{Object: obj("_su", "S2", false), Title: "Public Source"},
	}
	t.Citations = []model.Citation{
		{Object: obj("_cp", "C1", false), Page: canaryCitation, Source: model.Ref{HLink: "_sp"}},
		{Object: obj("_cu", "C2", false), Page: "public page", Source: model.Ref{HLink: "_su"}},
	}
	dead := model.Person{
		Object: obj("_dead", "I1", false), Gender: "M",
		Names: append(name("Public", "Ancestor"), model.Name{Alt: true, Private: true, Type: "Other", First: canaryPrivateAlt}),
		EventRefs: []model.EventRef{
			{HLink: "_eb", Role: "Primary"}, {HLink: "_ed", Role: "Primary"},
			{HLink: "_ep", Role: "Primary"}, {HLink: "_er", Role: "Primary", Private: true}, {HLink: "_ev", Role: "Primary"},
		},
		Attributes: []model.Attribute{{Type: "Note", Value: canaryAttr, Private: true}, {Type: "Note", Value: "public attribute"}},
		URLs:       []model.URL{{Href: canaryURL, Private: true}, {Href: "javascript:alert(1)"}, {Href: "https://public.example/"}},
		Notes:      []model.Ref{{HLink: "_np"}, {HLink: "_nu"}},
		Citations:  []model.Ref{{HLink: "_cp"}, {HLink: "_cu"}},
		ParentIn:   []model.Ref{{HLink: "_fam"}, {HLink: "_fam2"}},
		PersonRefs: []model.PersonRef{{HLink: "_living", Rel: "Godparent"}},
	}
	t.People = []model.Person{
		dead,
		{Object: obj("_living", "I2", false), Gender: "F", Names: name(canaryLivingName, "Young"), EventRefs: []model.EventRef{{HLink: "_lb", Role: "Primary"}}, ChildOf: []model.Ref{{HLink: "_fam"}}},
		{Object: obj("_priv", "I3", true), Gender: "F", Names: name(canaryPrivateName, "Hidden"), ChildOf: []model.Ref{{HLink: "_fam"}}, EventRefs: []model.EventRef{{HLink: "_cd", Role: "Primary"}}},
		{Object: obj("_child", "I4", false), Gender: "M", Names: name("Public", "Child"), EventRefs: []model.EventRef{{HLink: "_cb", Role: "Primary"}, {HLink: "_cd", Role: "Primary"}}, ChildOf: []model.Ref{{HLink: "_fam"}}},
		{Object: obj("_wife", "I5", false), Gender: "F", Names: name(canaryLivingName+"Wife", "Young"), ParentIn: []model.Ref{{HLink: "_fam2"}}},
	}
	t.Families = []model.Family{
		{
			Object: obj("_fam", "F1", false), Rel: &model.FamilyRel{Type: "Married"},
			Father:    &model.Ref{HLink: "_dead"},
			ChildRefs: []model.ChildRef{{HLink: "_living"}, {HLink: "_priv"}, {HLink: "_child"}},
		},
		{
			Object: obj("_fam2", "F2", false), Rel: &model.FamilyRel{Type: "Married"},
			Father: &model.Ref{HLink: "_dead"}, Mother: &model.Ref{HLink: "_wife"},
			EventRefs: []model.EventRef{{HLink: "_em", Role: "Family"}},
			Notes:     []model.Ref{{HLink: "_nf"}},
		},
	}
	return t
}

func load2(t *testing.T, st *Store, tree *model.Tree) {
	t.Helper()
	if _, err := st.ReplaceTree(context.Background(), tree); err != nil {
		t.Fatal(err)
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoCanary(t *testing.T, text string) {
	t.Helper()
	for _, canary := range []string{canaryLivingName, canaryPrivateName, canaryPrivateAlt, canaryEvent, canaryRefEvent, canaryNote, canaryAttr, canaryPlace, canarySource, canaryCitation, canaryMarriage, canaryFamilyNote, canaryURL} {
		if strings.Contains(text, canary) {
			t.Errorf("guest output holds %q", canary)
		}
	}
}

func TestGuestSeesNoLivingOrPrivateData(t *testing.T) {
	st := open(t)
	load2(t, st, privacyTree())
	ctx := context.Background()
	guest := st.View(privacy.Public)

	graph, err := guest.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCanary(t, asJSON(t, graph))
	if graph.Home != "" {
		t.Fatalf("the home person is living and must be hidden, got %q", graph.Home)
	}
	ids := map[string]bool{}
	for _, c := range graph.People {
		ids[c.ID] = true
	}
	if !ids["I1"] || !ids["I4"] || ids["I2"] || ids["I3"] || ids["I5"] {
		t.Fatalf("wrong people: %v", ids)
	}

	detail, err := guest.Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	text := asJSON(t, detail)
	assertNoCanary(t, text)
	if !strings.Contains(text, "public note") || !strings.Contains(text, "public attribute") || !strings.Contains(text, "public page") {
		t.Fatalf("public facts are missing: %s", text)
	}
	if strings.Contains(text, "javascript:") {
		t.Fatalf("a dangerous link passed: %s", text)
	}
	for _, id := range []string{"I2", "I3", "I5"} {
		if _, err := guest.Person(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: want ErrNotFound, got %v", id, err)
		}
	}
}

func TestPrivatePlaceIsRemovedFromPublicEvent(t *testing.T) {
	st := open(t)
	load2(t, st, privacyTree())
	detail, err := st.View(privacy.Public).Person(context.Background(), "I1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range detail.Events {
		if event.Description == "public occupation" {
			found = true
			if event.Place != nil {
				t.Fatalf("private place leaked: %+v", event.Place)
			}
		}
	}
	if !found {
		t.Fatal("public event is missing")
	}
}

func TestCoupleEventsHideWithSpouse(t *testing.T) {
	st := open(t)
	load2(t, st, privacyTree())
	ctx := context.Background()
	guest, err := st.View(privacy.Public).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	for _, union := range guest.Unions {
		if union.FamilyID == "F2" && (len(union.Events) > 0 || len(union.Notes) > 0 || union.Spouse != nil) {
			t.Fatalf("couple data leaked: %+v", union)
		}
	}
	graph, err := st.View(privacy.Public).Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range graph.Families {
		if family.ID == "F2" && family.Married != nil {
			t.Fatal("marriage date leaked")
		}
	}
	full, err := st.View(privacy.Full).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	text := asJSON(t, full)
	for _, want := range []string{canaryMarriage, canaryFamilyNote, canaryEvent, canaryNote} {
		if !strings.Contains(text, want) {
			t.Errorf("full view lacks %q", want)
		}
	}
}

func TestPublicCardSkipsPrivateFacts(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{
		{Object: obj("_b", "E1", true), Type: "Birth", Dated: dated("1800")},
		{Object: obj("_d", "E2", false), Type: "Death", Dated: dated("1850")},
	}
	tree.People = []model.Person{{
		Object: obj("_p", "I1", false),
		Names: []model.Name{
			{Private: true, Type: "Birth Name", First: "Secret"},
			{Alt: true, Type: "Other", First: "Known"},
		},
		EventRefs: []model.EventRef{{HLink: "_b", Role: "Primary"}, {HLink: "_d", Role: "Primary"}},
	}}
	load2(t, st, tree)
	graph, err := st.View(privacy.Public).Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	card := graph.People[0]
	if card.Birth != nil || card.Given != "Known" {
		t.Fatalf("private facts in the public card: %+v", card)
	}
	fullGraph, err := st.View(privacy.Full).Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fullGraph.People[0].Birth == nil || fullGraph.People[0].Given != "Secret" {
		t.Fatalf("full card is wrong: %+v", fullGraph.People[0])
	}
}

func TestFixtureGuestViewHasNoLivingPeople(t *testing.T) {
	st := open(t)
	tree := fixture.Generate(fixture.DefaultOptions("en"))
	living, err := st.ReplaceTree(context.Background(), tree)
	if err != nil {
		t.Fatal(err)
	}
	if living == 0 {
		t.Fatal("the fixture needs living people")
	}
	ctx := context.Background()
	guest, err := st.View(privacy.Public).Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	full, err := st.View(privacy.Full).Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(guest.People) >= len(full.People) {
		t.Fatalf("guest sees %d of %d people", len(guest.People), len(full.People))
	}
	for _, card := range guest.People {
		if card.Living {
			t.Fatalf("guest sees living person %s", card.ID)
		}
	}
}

func TestExportKeepsAllData(t *testing.T) {
	st := open(t)
	tree := privacyTree()
	load2(t, st, tree)
	exported, err := st.ExportTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.People) != len(tree.People) || len(exported.Notes) != len(tree.Notes) {
		t.Fatalf("export lost objects: %+v", exported.Counts())
	}
	if !reflect.DeepEqual(findPerson(tree, "_dead"), findPerson(exported, "_dead")) {
		t.Fatal("export changed a person")
	}
}

func findPerson(tree *model.Tree, handle string) *model.Person {
	for i := range tree.People {
		if tree.People[i].Handle == handle {
			return &tree.People[i]
		}
	}
	return nil
}

func TestDuplicateIDsBecomeUnique(t *testing.T) {
	st := open(t)
	tree := &model.Tree{People: []model.Person{
		{Object: obj("_a", "I1", false), Names: name("A", "X")},
		{Object: obj("_b", "I1", false), Names: name("B", "X")},
		{Object: obj("_c", "", false), Names: name("C", "X")},
	}}
	load2(t, st, tree)
	graph, err := st.View(privacy.Full).Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, card := range graph.People {
		if card.ID == "" || seen[card.ID] {
			t.Fatalf("bad id %q", card.ID)
		}
		seen[card.ID] = true
	}
}

func TestReplaceIsAtomic(t *testing.T) {
	st := open(t)
	load2(t, st, privacyTree())
	bad := privacyTree()
	bad.Events = append(bad.Events, bad.Events[0]) // A duplicate primary key.
	ctx := context.Background()
	if _, err := st.ReplaceTree(ctx, bad); err == nil {
		t.Fatal("want an error")
	}
	counts, err := st.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Events != len(privacyTree().Events) {
		t.Fatalf("the old tree was damaged: %+v", counts)
	}
}

func TestRefreshLivingFollowsTheCalendar(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{{Object: obj("_b", "E1", false), Type: "Birth", Dated: dated("1950")}}
	tree.People = []model.Person{{Object: obj("_p", "I1", false), Names: name("A", "B"), EventRefs: []model.EventRef{{HLink: "_b", Role: "Primary"}}}}
	st.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	load2(t, st, tree)
	ctx := context.Background()
	if _, err := st.View(privacy.Public).Person(ctx, "I1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a person of 76 must be hidden, got %v", err)
	}
	st.now = func() time.Time { return time.Date(2070, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := st.RefreshLiving(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := st.View(privacy.Public).Person(ctx, "I1")
	if err != nil {
		t.Fatalf("a person of 120 must be visible: %v", err)
	}
	if detail.Living {
		t.Fatal("the card still says living")
	}
}

func TestDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x", "test.db")
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestPrivateChildLinkHidesParents(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{
		{Object: obj("_d1", "E1", false), Type: "Death", Dated: dated("1950")},
		{Object: obj("_d2", "E2", false), Type: "Death", Dated: dated("1990")},
	}
	tree.People = []model.Person{
		{Object: obj("_father", "I1", false), Names: name("Father", "X"), ParentIn: []model.Ref{{HLink: "_fam"}}, EventRefs: []model.EventRef{{HLink: "_d1", Role: "Primary"}}},
		{Object: obj("_child", "I2", false), Names: name("Child", "X"), ChildOf: []model.Ref{{HLink: "_fam"}}, EventRefs: []model.EventRef{{HLink: "_d2", Role: "Primary"}}},
	}
	tree.Families = []model.Family{{Object: obj("_fam", "F1", false), Father: &model.Ref{HLink: "_father"}, ChildRefs: []model.ChildRef{{HLink: "_child", Private: true}}}}
	load2(t, st, tree)
	ctx := context.Background()
	guest, err := st.View(privacy.Public).Person(ctx, "I2")
	if err != nil {
		t.Fatal(err)
	}
	if len(guest.Parents) != 0 {
		t.Fatalf("a private child link leaked the parents: %+v", guest.Parents)
	}
	full, err := st.View(privacy.Full).Person(ctx, "I2")
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Parents) != 1 {
		t.Fatalf("full view lost the parents: %+v", full.Parents)
	}
}

func TestHiddenSpouseLeavesNoTrace(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{
		{Object: obj("_d1", "E1", false), Type: "Death", Dated: dated("1950")},
		{Object: obj("_d2", "E2", false), Type: "Death", Dated: dated("1990")},
	}
	dead := func(handle, id, event string, parentIn ...model.Ref) model.Person {
		return model.Person{Object: obj(handle, id, false), Names: name(id, "X"), ParentIn: parentIn, EventRefs: []model.EventRef{{HLink: event, Role: "Primary"}}}
	}
	tree.People = []model.Person{
		dead("_man", "I1", "_d1", model.Ref{HLink: "_lone"}, model.Ref{HLink: "_kids"}),
		{Object: obj("_wife", "I2", false), Names: name(canaryLivingName, "X"), ParentIn: []model.Ref{{HLink: "_lone"}, {HLink: "_kids"}}},
		dead("_kid", "I3", "_d2"),
	}
	tree.People[2].ChildOf = []model.Ref{{HLink: "_kids"}}
	family := func(handle, id string, children ...model.ChildRef) model.Family {
		return model.Family{Object: obj(handle, id, false), Rel: &model.FamilyRel{Type: "Married"}, Father: &model.Ref{HLink: "_man"}, Mother: &model.Ref{HLink: "_wife"}, ChildRefs: children}
	}
	tree.Families = []model.Family{family("_lone", "F1"), family("_kids", "F2", model.ChildRef{HLink: "_kid"})}
	load2(t, st, tree)
	ctx := context.Background()
	guest := st.View(privacy.Public)
	graph, err := guest.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Families) != 1 || graph.Families[0].ID != "F2" || graph.Families[0].Rel != "" {
		t.Fatalf("graph shows a trace of the hidden spouse: %+v", graph.Families)
	}
	detail, err := guest.Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Unions) != 1 || detail.Unions[0].FamilyID != "F2" || detail.Unions[0].Rel != "" {
		t.Fatalf("person page shows a trace of the hidden spouse: %+v", detail.Unions)
	}
}

func TestGuestGetsNoNoteStyles(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{{Object: obj("_d", "E1", false), Type: "Death", Dated: dated("1950")}}
	style := model.NoteStyle{Name: "link", Value: "gramps://Person/handle/_living", Ranges: []model.StyleRange{{Start: 0, End: 4}}}
	tree.Notes = []model.Note{{Object: obj("_n", "N1", false), Type: "General", Text: "text", Styles: []model.NoteStyle{style}}}
	tree.People = []model.Person{{Object: obj("_p", "I1", false), Names: name("A", "B"), Notes: []model.Ref{{HLink: "_n"}}, EventRefs: []model.EventRef{{HLink: "_d", Role: "Primary"}}}}
	load2(t, st, tree)
	ctx := context.Background()
	guest, err := st.View(privacy.Public).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(asJSON(t, guest), "_living") {
		t.Fatal("a guest received a style with a link to a record")
	}
	full, err := st.View(privacy.Full).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asJSON(t, full), "_living") {
		t.Fatal("the full view lost the style")
	}
}

func TestManyDuplicateIDsStayFast(t *testing.T) {
	records := make([]model.Record, 50_000)
	for i := range records {
		records[i] = model.Record{Kind: model.KindPerson, Handle: "_h" + strconv.Itoa(i), ID: "I1"}
	}
	start := time.Now()
	ids := assignIDs(records)
	if len(ids) != len(records) {
		t.Fatalf("ids: %d", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("too slow: %s", elapsed)
	}
}

func TestAuditIsPruned(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return base }
	if err := st.AddAudit(ctx, 0, "", "old", "", "", ""); err != nil {
		t.Fatal(err)
	}
	st.now = func() time.Time { return base.Add(2 * auditRetention) }
	if err := st.AddAudit(ctx, 0, "", "new", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := st.ListAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "new" {
		t.Fatalf("entries: %+v", entries)
	}
}

// A family with one known parent must look the same as a family whose second
// parent is hidden.
func TestKnownAndHiddenSpouseLookTheSame(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{
		{Object: obj("_d1", "E1", false), Type: "Death", Dated: dated("1950")},
		{Object: obj("_d2", "E2", false), Type: "Death", Dated: dated("1990")},
	}
	dead := func(handle, id, event string) model.Person {
		return model.Person{Object: obj(handle, id, false), Names: name(id, "X"), EventRefs: []model.EventRef{{HLink: event, Role: "Primary"}}}
	}
	single, kid := dead("_single", "I1", "_d1"), dead("_kid", "I2", "_d2")
	single.ParentIn, kid.ChildOf = []model.Ref{{HLink: "_f"}}, []model.Ref{{HLink: "_f"}}
	tree.People = []model.Person{single, kid}
	tree.Families = []model.Family{{Object: obj("_f", "F1", false), Rel: &model.FamilyRel{Type: "Married"}, Father: &model.Ref{HLink: "_single"}, ChildRefs: []model.ChildRef{{HLink: "_kid"}}}}
	load2(t, st, tree)
	ctx := context.Background()
	graph, err := st.View(privacy.Public).Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Families) != 1 || graph.Families[0].Rel != "" {
		t.Fatalf("graph: %+v", graph.Families)
	}
	detail, err := st.View(privacy.Public).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Unions) != 1 || detail.Unions[0].Rel != "" {
		t.Fatalf("unions: %+v", detail.Unions)
	}
	full, err := st.View(privacy.Full).Person(ctx, "I1")
	if err != nil {
		t.Fatal(err)
	}
	if full.Unions[0].Rel != "Married" {
		t.Fatalf("the full view lost the relation: %+v", full.Unions)
	}
}

// A family with nobody else in it keeps its events when nothing is hidden.
func TestFamilyOfOneKeepsItsEvents(t *testing.T) {
	st := open(t)
	tree := &model.Tree{}
	tree.Events = []model.Event{
		{Object: obj("_d", "E1", false), Type: "Death", Dated: dated("1950")},
		{Object: obj("_m", "E2", false), Type: "Marriage", Dated: dated("1920")},
	}
	tree.Notes = []model.Note{{Object: obj("_n", "N1", false), Type: "General", Text: "family note"}}
	person := model.Person{Object: obj("_p", "I1", false), Names: name("A", "B"), ParentIn: []model.Ref{{HLink: "_f"}}, EventRefs: []model.EventRef{{HLink: "_d", Role: "Primary"}}}
	tree.People = []model.Person{person}
	tree.Families = []model.Family{{Object: obj("_f", "F1", false), Father: &model.Ref{HLink: "_p"}, EventRefs: []model.EventRef{{HLink: "_m", Role: "Family"}}, Notes: []model.Ref{{HLink: "_n"}}}}
	load2(t, st, tree)
	for _, scope := range []privacy.Scope{privacy.Full, privacy.Public} {
		detail, err := st.View(scope).Person(context.Background(), "I1")
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Unions) != 1 || len(detail.Unions[0].Events) != 1 || len(detail.Unions[0].Notes) != 1 {
			t.Fatalf("scope %+v: %+v", scope, detail.Unions)
		}
	}
}
