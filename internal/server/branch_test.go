package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/store"
)

// branchTree makes a small tree for the tests of the own branch:
//
//	Pavel ── (F2) ── Polina          Sam ── (F3) ── Yana
//	        │                               │
//	  Rita ─┴─ Boris                       Zoya
//	  Rita ── (F1) ── Sam
//	        │
//	      Clara
//
// and Xenia, who has no relatives.
type branchTree struct {
	rita, sam, clara, pavel, boris, yana, zoya, xenia string
	ownUnion, parents, otherUnion                     string
}

func makeBranchTree(t *testing.T, editor *client) branchTree {
	t.Helper()
	var tree branchTree
	tree.rita = editor.create("F", "Rita", "Orlova", nil)
	tree.sam = editor.create("M", "Sam", "Orlov", map[string]string{"person": tree.rita, "as": "spouse"})
	tree.ownUnion = editor.form(tree.rita).Families[0].ID
	tree.clara = editor.create("F", "Clara", "Orlova", map[string]string{"person": tree.rita, "as": "child", "family": tree.ownUnion})
	tree.pavel = editor.create("M", "Pavel", "Sokolov", map[string]string{"person": tree.rita, "as": "parent"})
	tree.boris = editor.create("M", "Boris", "Sokolov", map[string]string{"person": tree.rita, "as": "sibling"})
	tree.yana = editor.create("F", "Yana", "Lisina", map[string]string{"person": tree.sam, "as": "spouse"})
	for _, family := range editor.form(tree.sam).Families {
		if family.ID != tree.ownUnion {
			tree.otherUnion = family.ID
		}
	}
	tree.zoya = editor.create("F", "Zoya", "Orlova", map[string]string{"person": tree.sam, "as": "child", "family": tree.otherUnion})
	tree.xenia = editor.create("F", "Xenia", "Dalnaya", nil)
	detail := decode[store.PersonDetail](t, editor.expect(http.StatusOK, "GET", "/api/v1/people/"+tree.rita, nil))
	if len(detail.Parents) != 1 || tree.otherUnion == "" {
		t.Fatalf("the tree of the test: parents %+v, other union %q", detail.Parents, tree.otherUnion)
	}
	tree.parents = detail.Parents[0].FamilyID
	return tree
}

var someEvent = map[string]any{"event": map[string]any{"type": "Residence", "date": map[string]any{"dateval": map[string]string{"val": "1990"}}}}

// A family member with the right for the own branch changes the person of
// the account, the descendants and the partners, and nothing else.
func TestMemberEditsTheOwnBranch(t *testing.T) {
	h, uploads := editHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	tree := makeBranchTree(t, editor)
	account := h.userByEmail("member@example.org")
	path := "/api/v1/users/" + itoa(account.ID)

	// Without the right, a family member changes nothing.
	member.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/edit/people/"+tree.rita, nil)
	member.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/edit/people/"+tree.rita+"/events", someEvent)
	member.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/history", nil)

	// The right needs a person in the tree and the role of a family member.
	admin.refuse(http.StatusBadRequest, "needs_person", "PATCH", path, map[string]any{"ownBranch": true})
	granted := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]any{"person": tree.rita, "ownBranch": true}))
	if !granted.OwnBranch || granted.Person != tree.rita {
		t.Fatalf("after the grant: %+v", granted)
	}
	me := decode[accountDTO](t, member.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if !me.EditOwn || me.Edit || me.Invite {
		t.Fatalf("the account of the member: %+v", me)
	}
	// The link now decides what the member can change, so the member cannot move it.
	member.refuse(http.StatusForbidden, "person_locked", "PATCH", "/api/v1/me", map[string]string{"person": tree.pavel})
	member.refuse(http.StatusForbidden, "person_locked", "PATCH", "/api/v1/me", map[string]string{"person": ""})

	// The member opens the forms of the own branch only.
	for _, id := range []string{tree.rita, tree.sam, tree.clara} {
		member.expect(http.StatusOK, "GET", "/api/v1/edit/people/"+id, nil)
	}
	for _, id := range []string{tree.pavel, tree.boris, tree.yana, tree.zoya, tree.xenia, "I0999"} {
		member.refuse(http.StatusForbidden, "outside_branch", "GET", "/api/v1/edit/people/"+id, nil)
	}
	member.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil)

	size := h.journalSize()
	allowed := func(method, path string, body any) store.EditResult {
		t.Helper()
		result := decode[store.EditResult](t, member.expect(http.StatusOK, method, path, body))
		size++
		if result.ChangeID == 0 || h.journalSize() != size {
			t.Fatalf("%s %s: result %+v, journal %d, want %d", method, path, result, h.journalSize(), size)
		}
		return result
	}
	refused := func(method, path string, body any) {
		t.Helper()
		res, data := member.do(method, path, body, nil)
		if res.StatusCode != http.StatusForbidden || h.journalSize() != size {
			t.Fatalf("%s %s: status %d, journal %d, want %d: %.200s", method, path, res.StatusCode, h.journalSize(), size, data)
		}
	}

	// Names, events, notes and pictures in the branch.
	form := member.form(tree.rita)
	form.Names[0].Nick = "Rita the First"
	allowed("PUT", "/api/v1/edit/people/"+tree.rita, map[string]any{"rev": form.Rev, "person": map[string]any{"gender": form.Gender, "private": form.Private, "names": form.Names}})
	own := allowed("POST", "/api/v1/edit/people/"+tree.rita+"/events", someEvent).Created
	allowed("POST", "/api/v1/edit/people/"+tree.sam+"/events", someEvent)
	allowed("POST", "/api/v1/edit/people/"+tree.clara+"/events", someEvent)
	wedding := allowed("POST", "/api/v1/edit/families/"+tree.ownUnion+"/events", map[string]any{"event": map[string]any{"type": "Marriage"}}).Created
	form = member.form(tree.rita)
	allowed("PUT", "/api/v1/edit/events/"+own+"?person="+tree.rita, map[string]any{"rev": form.Events[0].Rev, "event": map[string]any{"type": "Residence", "place": map[string]string{"name": "New Town"}}})
	allowed("DELETE", "/api/v1/edit/families/"+tree.ownUnion+"/events/"+wedding, nil)
	note := allowed("POST", "/api/v1/edit/people/"+tree.clara+"/notes", map[string]string{"text": "A note about Clara."}).Created
	form = member.form(tree.clara)
	allowed("PUT", "/api/v1/edit/notes/"+note+"?person="+tree.clara, map[string]any{"rev": form.Notes[0].Rev, "text": "A better note."})
	allowed("PUT", "/api/v1/edit/families/"+tree.ownUnion, map[string]string{"rel": "Married"})
	photo := allowed("POST", "/api/v1/edit/people/"+tree.rita+"/photo", testPNG(t, 30, 30)).Created
	allowed("PUT", "/api/v1/edit/people/"+tree.rita+"/media/"+photo, map[string]any{"region": map[string]int{"x1": 10, "y1": 10, "x2": 60, "y2": 60}})

	// New people who join the branch: a child, a grandchild, a partner of a child.
	newChild := map[string]any{"person": personBody("M", "Denis", "Orlov"), "relation": map[string]string{"person": tree.rita, "as": "child", "family": tree.ownUnion}}
	denis := allowed("POST", "/api/v1/edit/people", newChild).Created
	allowed("POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", "Eva", "Orlova"), "relation": map[string]string{"person": tree.clara, "as": "child"}})
	partner := allowed("POST", "/api/v1/edit/people", map[string]any{"person": personBody("M", "Gleb", "Novikov"), "relation": map[string]string{"person": tree.clara, "as": "spouse"}}).Created
	// The new people are in the branch at once.
	allowed("POST", "/api/v1/edit/people/"+denis+"/events", someEvent)
	allowed("POST", "/api/v1/edit/people/"+partner+"/events", someEvent)

	// Outside the branch: parents, brothers and sisters, and unrelated people.
	files := len(filesIn(t, uploads))
	for _, id := range []string{tree.pavel, tree.boris, tree.yana, tree.zoya, tree.xenia} {
		refused("POST", "/api/v1/edit/people/"+id+"/events", someEvent)
		refused("POST", "/api/v1/edit/people/"+id+"/notes", map[string]string{"text": "A note."})
		refused("POST", "/api/v1/edit/people/"+id+"/photo", testPNG(t, 31, 31))
	}
	if got := len(filesIn(t, uploads)); got != files {
		t.Fatalf("a refused upload left a file: %d files, want %d", got, files)
	}
	refused("POST", "/api/v1/edit/families/"+tree.parents+"/events", map[string]any{"event": map[string]any{"type": "Marriage"}})
	refused("POST", "/api/v1/edit/families/"+tree.otherUnion+"/events", map[string]any{"event": map[string]any{"type": "Marriage"}})
	refused("PUT", "/api/v1/edit/families/"+tree.parents, map[string]string{"rel": "Married"})
	// An event and a note of a person outside the branch.
	foreign := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+tree.pavel+"/events", someEvent)).Created
	foreignNote := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+tree.pavel+"/notes", map[string]string{"text": "About Pavel."})).Created
	size += 2
	pavel := editor.form(tree.pavel)
	refused("PUT", "/api/v1/edit/events/"+foreign+"?person="+tree.rita, map[string]any{"rev": pavel.Events[0].Rev, "event": map[string]any{"type": "Death"}})
	refused("PUT", "/api/v1/edit/notes/"+foreignNote+"?person="+tree.rita, map[string]any{"rev": pavel.Notes[0].Rev, "text": "Changed."})
	refused("DELETE", "/api/v1/edit/people/"+tree.pavel+"/events/"+foreign, nil)

	// New people outside the branch, and links that pull people into it.
	refused("POST", "/api/v1/edit/people", map[string]any{"person": personBody("U", "Nobody", "Alone")})
	refused("POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", "Polina", "Sokolova"), "relation": map[string]string{"person": tree.rita, "as": "parent"}})
	refused("POST", "/api/v1/edit/people", map[string]any{"person": personBody("M", "Brat", "Sokolov"), "relation": map[string]string{"person": tree.rita, "as": "sibling"}})
	refused("POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", "Third", "Wife"), "relation": map[string]string{"person": tree.sam, "as": "spouse"}})
	refused("POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", "Other", "Child"), "relation": map[string]string{"person": tree.sam, "as": "child", "family": tree.otherUnion}})
	refused("POST", "/api/v1/edit/links", map[string]string{"person": tree.rita, "as": "child", "other": tree.xenia})
	refused("POST", "/api/v1/edit/links", map[string]string{"person": tree.clara, "as": "spouse", "other": tree.boris})

	// The privacy mark, removals and the settings of the tree.
	form = member.form(tree.rita)
	refused("PUT", "/api/v1/edit/people/"+tree.rita, map[string]any{"rev": form.Rev, "person": map[string]any{"gender": form.Gender, "private": true, "names": form.Names}})
	// A new event can be private. Only an old mark stays as it is.
	allowed("POST", "/api/v1/edit/people/"+tree.rita+"/events", map[string]any{"event": map[string]any{"type": "Residence", "private": true}})
	form = member.form(tree.rita)
	hidden := form.Events[len(form.Events)-1]
	if !hidden.Private {
		t.Fatalf("the new private event: %+v", hidden)
	}
	refused("PUT", "/api/v1/edit/events/"+hidden.ID+"?person="+tree.rita, map[string]any{"rev": hidden.Rev, "event": map[string]any{"type": "Residence", "private": false}})
	refused("DELETE", "/api/v1/edit/people/"+tree.clara, nil)
	refused("DELETE", "/api/v1/edit/families/"+tree.ownUnion+"/members/"+tree.clara, nil)
	refused("DELETE", "/api/v1/edit/families/"+tree.ownUnion+"/members/"+tree.sam, nil)
	refused("PUT", "/api/v1/edit/home", map[string]string{"person": tree.rita})

	// The member sees and takes back the own actions only.
	entries := member.history("")
	if len(entries) == 0 {
		t.Fatal("the member sees no own actions")
	}
	for _, entry := range entries {
		if entry.Actor != account.DisplayName {
			t.Fatalf("the member sees an action of %q", entry.Actor)
		}
	}
	all := editor.history("")
	var foreignEntry int64
	for _, entry := range all {
		if entry.Actor != account.DisplayName {
			foreignEntry = entry.ID
			break
		}
	}
	member.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/history/"+itoa(foreignEntry)+"/undo", nil)
	member.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/history/999999/undo", nil)
	member.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(entries[0].ID)+"/undo", nil)

	// The right goes away with the role, and with the person.
	if got := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]any{"person": ""})); got.OwnBranch {
		t.Fatalf("the right stayed without a person: %+v", got)
	}
	member.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/edit/people/"+tree.rita+"/events", someEvent)
	admin.expect(http.StatusOK, "PATCH", path, map[string]any{"person": tree.rita, "ownBranch": true})
	member.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+tree.rita+"/events", someEvent)
	if got := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]any{"role": "guest"})); got.OwnBranch {
		t.Fatalf("the right stayed with a guest: %+v", got)
	}
	if text := h.auditText(); !strings.Contains(text, "user.branch") {
		t.Fatalf("the audit log has no line for the right: %s", text)
	}
}

// Only an administrator gives the right for the own branch, and only a short
// time after a sign-in code.
func TestOwnBranchIsASettingOfAnAdministrator(t *testing.T) {
	h, _ := editHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	h.user("member@example.org", auth.RoleMember)
	h.user("guest@example.org", auth.RoleGuest)
	anna := editor.create("F", "Anna", "Orlova", nil)
	member := "/api/v1/users/" + itoa(h.userByEmail("member@example.org").ID)
	guest := "/api/v1/users/" + itoa(h.userByEmail("guest@example.org").ID)

	editor.refuse(http.StatusForbidden, "forbidden", "PATCH", member, map[string]any{"person": anna, "ownBranch": true})
	admin.refuse(http.StatusBadRequest, "needs_person", "PATCH", guest, map[string]any{"person": anna, "ownBranch": true})
	admin.refuse(http.StatusBadRequest, "needs_person", "PATCH", member, map[string]any{"role": "editor", "person": anna, "ownBranch": true})

	// An invitation can carry the right, from an administrator.
	invite := map[string]any{"email": "new@example.org", "displayName": "Anna", "role": "member", "person": anna, "ownBranch": true}
	editor.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/users", invite)
	admin.refuse(http.StatusBadRequest, "needs_person", "POST", "/api/v1/users",
		map[string]any{"email": "new@example.org", "displayName": "Anna", "role": "member", "ownBranch": true})
	admin.refuse(http.StatusBadRequest, "needs_person", "POST", "/api/v1/users",
		map[string]any{"email": "new@example.org", "displayName": "Anna", "role": "guest", "person": anna, "ownBranch": true})
	if got := decode[personDTO](t, admin.expect(http.StatusCreated, "POST", "/api/v1/users", invite)); !got.OwnBranch || got.Person != anna {
		t.Fatalf("the invitation: %+v", got)
	}

	h.makeStale(time.Hour)
	admin.refuse(http.StatusForbidden, "reauth_required", "PATCH", member, map[string]any{"person": anna, "ownBranch": true})
	invite["email"] = "late@example.org"
	admin.refuse(http.StatusForbidden, "reauth_required", "POST", "/api/v1/users", invite)
	// To take the right away needs no new code.
	admin.expect(http.StatusOK, "PATCH", "/api/v1/users/"+itoa(h.userByEmail("new@example.org").ID), map[string]any{"ownBranch": false})
	if h.userByEmail("new@example.org").OwnBranch {
		t.Fatal("the right stayed")
	}
}
