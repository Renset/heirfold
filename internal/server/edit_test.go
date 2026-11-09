package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// editRoutes lists the patterns of all routes that only an editor can use.
// TestEditRouteListIsComplete compares the list with server.go.
var editRoutes = [][2]string{
	{"GET", "/api/v1/edit/people/{id}"},
	{"GET", "/api/v1/edit/places"},
	{"POST", "/api/v1/edit/people"},
	{"PUT", "/api/v1/edit/people/{id}"},
	{"DELETE", "/api/v1/edit/people/{id}"},
	{"POST", "/api/v1/edit/links"},
	{"DELETE", "/api/v1/edit/families/{id}/members/{person}"},
	{"PUT", "/api/v1/edit/families/{id}"},
	{"POST", "/api/v1/edit/people/{id}/events"},
	{"POST", "/api/v1/edit/families/{id}/events"},
	{"PUT", "/api/v1/edit/events/{id}"},
	{"DELETE", "/api/v1/edit/people/{id}/events/{event}"},
	{"DELETE", "/api/v1/edit/families/{id}/events/{event}"},
	{"POST", "/api/v1/edit/people/{id}/notes"},
	{"PUT", "/api/v1/edit/notes/{id}"},
	{"DELETE", "/api/v1/edit/people/{id}/notes/{note}"},
	{"POST", "/api/v1/edit/people/{id}/photo"},
	{"PUT", "/api/v1/edit/people/{id}/media/{media}"},
	{"DELETE", "/api/v1/edit/people/{id}/media/{media}"},
	{"DELETE", "/api/v1/edit/people/{id}/portrait"},
	{"POST", "/api/v1/edit/gallery"},
	{"PUT", "/api/v1/edit/gallery/{id}"},
	{"DELETE", "/api/v1/edit/gallery/{id}"},
	{"PUT", "/api/v1/edit/gallery/{id}/faces/{person}"},
	{"DELETE", "/api/v1/edit/gallery/{id}/faces/{person}"},
	{"PUT", "/api/v1/edit/home"},
	{"POST", "/api/v1/history/{id}/undo"},
}

// historyRoute reads the journal. It changes nothing, so it also works while
// the tree follows a Gramps database.
var historyRoute = [2]string{"GET", "/api/v1/history"}

// fillRoute puts IDs of the usual form into the pattern of a route.
func fillRoute(pattern string) string {
	return strings.NewReplacer(
		"people/{id}", "people/I0001", "families/{id}", "families/F0001", "events/{id}", "events/E0001",
		"notes/{id}", "notes/N0001", "history/{id}", "history/1", "gallery/{id}", "gallery/O0001",
		"{person}", "I0001", "{event}", "E0001", "{note}", "N0001", "{media}", "O0001",
	).Replace(pattern)
}

// editPaths returns the edit routes with real paths, and the journal.
func editPaths() [][2]string {
	out := make([][2]string, 0, len(editRoutes)+1)
	for _, route := range editRoutes {
		out = append(out, [2]string{route[0], fillRoute(route[1])})
	}
	return append(out, historyRoute)
}

var muxRoute = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/api/v1/(?:edit|history)[^"]*)"`)

// A new edit route must get into the access tests.
func TestEditRouteListIsComplete(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	var inCode []string
	for _, match := range muxRoute.FindAllStringSubmatch(string(source), -1) {
		inCode = append(inCode, match[1]+" "+match[2])
	}
	var inTest []string
	for _, route := range append(slices.Clone(editRoutes), historyRoute) {
		inTest = append(inTest, route[0]+" "+route[1])
	}
	sort.Strings(inCode)
	sort.Strings(inTest)
	if len(inCode) < 20 || !slices.Equal(inCode, inTest) {
		t.Fatalf("the routes of server.go and of the test differ:\nserver.go %v\n     test %v", inCode, inTest)
	}
	for _, route := range editPaths() {
		if strings.ContainsAny(route[1], "{}") {
			t.Errorf("no test value for %s", route[1])
		}
	}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 5 % 256), G: uint8(y * 7 % 256), B: 90, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black, color.White})
	var out bytes.Buffer
	if err := gif.Encode(&out, img, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// pngWithSize returns a PNG file that holds only the header chunk, with a
// right checksum. No reader can decode a picture from it.
func pngWithSize(width, height uint32) []byte {
	// 8 bits for each of red, green and blue.
	return pngWithDepth(width, height, 8, 2)
}

// pngWithDepth does the same with a chosen bit depth and colour type.
func pngWithDepth(width, height uint32, depth, colorType byte) []byte {
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], width)
	binary.BigEndian.PutUint32(header[4:], height)
	header[8], header[9] = depth, colorType
	chunk := append([]byte("IHDR"), header...)
	_ = binary.Write(&out, binary.BigEndian, uint32(len(header)))
	out.Write(chunk)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

// claimSize changes the size in the header of a real PNG file and repairs the
// checksum of the header.
func claimSize(t *testing.T, file []byte, width, height uint32) []byte {
	t.Helper()
	out := slices.Clone(file)
	if string(out[12:16]) != "IHDR" {
		t.Fatal("the file has no PNG header")
	}
	binary.BigEndian.PutUint32(out[16:], width)
	binary.BigEndian.PutUint32(out[20:], height)
	binary.BigEndian.PutUint32(out[29:], crc32.ChecksumIEEE(out[12:29]))
	return out
}

// claimJPEGSize changes the size in the frame header of a JPEG file.
func claimJPEGSize(t *testing.T, file []byte, width, height uint16) []byte {
	t.Helper()
	out := slices.Clone(file)
	at := bytes.Index(out, []byte{0xff, 0xc0})
	if at < 0 {
		t.Fatal("the file has no baseline frame header")
	}
	binary.BigEndian.PutUint16(out[at+5:], height)
	binary.BigEndian.PutUint16(out[at+7:], width)
	return out
}

// editHarness has an empty tree and a media directory.
func editHarness(t *testing.T, mutate func(*Config)) (*harness, string) {
	t.Helper()
	media := filepath.Join(t.TempDir(), "media")
	if err := os.Mkdir(media, 0o700); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(cfg *Config) {
		cfg.MediaDir = media
		if mutate != nil {
			mutate(cfg)
		}
	})
	return h, filepath.Join(h.app.mediaDir, "heirfold")
}

func personBody(gender, given, surname string) map[string]any {
	return map[string]any{"gender": gender, "names": []map[string]any{{"index": -1, "given": given, "surname": surname}}}
}

// create adds a person and returns the ID.
func (c *client) create(gender, given, surname string, relation map[string]string) string {
	c.h.t.Helper()
	body := map[string]any{"person": personBody(gender, given, surname)}
	if relation != nil {
		body["relation"] = relation
	}
	result := decode[store.EditResult](c.h.t, c.expect(http.StatusOK, "POST", "/api/v1/edit/people", body))
	if result.Created == "" || result.ChangeID == 0 {
		c.h.t.Fatalf("result of a new person: %+v", result)
	}
	return result.Created
}

func (c *client) form(id string) store.EditPerson {
	c.h.t.Helper()
	return decode[store.EditPerson](c.h.t, c.expect(http.StatusOK, "GET", "/api/v1/edit/people/"+id, nil))
}

type historyDTO struct {
	Entries []store.ChangeEntry `json:"entries"`
}

func (c *client) history(query string) []store.ChangeEntry {
	c.h.t.Helper()
	return decode[historyDTO](c.h.t, c.expect(http.StatusOK, "GET", "/api/v1/history"+query, nil)).Entries
}

func (h *harness) journalSize() int {
	h.t.Helper()
	n, err := h.store.CountChanges(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *harness) revision() int64 {
	h.t.Helper()
	rev, err := h.store.Revision(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return rev
}

func filesIn(t *testing.T, dir string) []os.FileInfo {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []os.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, info)
	}
	return out
}

// nextSecond waits until the clock shows a new second. The state number of a
// record is its time stamp in seconds, so two changes within one second look
// like one state; see TestStaleRevIsRefusedWithinOneSecond in the store package.
func nextSecond() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(time.Second + 10*time.Millisecond)))
}

// useEveryEditRoute runs one request with a good answer on every edit route
// and returns the patterns that it used.
func useEveryEditRoute(t *testing.T, c *client) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	call := func(method, pattern, path string, body any) store.EditResult {
		t.Helper()
		used[method+" "+pattern] = true
		res, data := c.do(method, path, body, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s %s: status %d: %s", method, path, res.StatusCode, data)
		}
		if method == "GET" {
			return store.EditResult{}
		}
		result := decode[store.EditResult](t, data)
		if result.ChangeID == 0 || result.Revision == 0 {
			t.Fatalf("%s %s: result %+v", method, path, result)
		}
		return result
	}
	anna := call("POST", "/api/v1/edit/people", "/api/v1/edit/people", map[string]any{"person": personBody("F", "Anna", "Orlova")}).Created
	boris := call("POST", "/api/v1/edit/people", "/api/v1/edit/people",
		map[string]any{"person": personBody("M", "Boris", "Orlov"), "relation": map[string]string{"person": anna, "as": "spouse"}}).Created
	child := call("POST", "/api/v1/edit/people", "/api/v1/edit/people", map[string]any{"person": personBody("U", "Child", "Orlov")}).Created
	if anna != "I0001" || boris != "I0002" || child != "I0003" {
		t.Fatalf("new people: %s %s %s", anna, boris, child)
	}
	call("GET", "/api/v1/edit/people/{id}", "/api/v1/edit/people/"+anna, nil)
	form := c.form(anna)
	if len(form.Families) != 1 || form.Families[0].ID != "F0001" || len(form.Names) != 1 || form.Names[0].Given != "Anna" {
		t.Fatalf("form: %+v", form)
	}
	form.Names[0].Nick = "Anya"
	call("PUT", "/api/v1/edit/people/{id}", "/api/v1/edit/people/"+anna,
		map[string]any{"rev": form.Rev, "person": map[string]any{"gender": form.Gender, "private": form.Private, "names": form.Names}})
	call("POST", "/api/v1/edit/links", "/api/v1/edit/links", map[string]string{"person": anna, "as": "child", "other": child})
	call("PUT", "/api/v1/edit/families/{id}", "/api/v1/edit/families/F0001", map[string]string{"rel": "Married"})
	event := call("POST", "/api/v1/edit/people/{id}/events", "/api/v1/edit/people/"+anna+"/events",
		map[string]any{"event": map[string]any{"type": "Birth", "date": map[string]any{"dateval": map[string]string{"val": "1950-05-12"}}, "place": map[string]string{"name": "New Town"}}}).Created
	call("GET", "/api/v1/edit/places", "/api/v1/edit/places", nil)
	places := decode[struct {
		Places []store.PlaceOption `json:"places"`
	}](t, c.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil))
	if len(places.Places) != 1 || places.Places[0] != (store.PlaceOption{ID: "P0001", Name: "New Town"}) {
		t.Fatalf("places: %+v", places)
	}
	// A change needs the state number that the form showed.
	form = c.form(anna)
	call("PUT", "/api/v1/edit/events/{id}", "/api/v1/edit/events/"+event+"?person="+anna,
		map[string]any{"rev": form.Events[0].Rev, "event": map[string]any{"type": "Baptism", "place": map[string]string{"id": "P0001"}, "description": "in the old church"}})
	marriage := call("POST", "/api/v1/edit/families/{id}/events", "/api/v1/edit/families/F0001/events",
		map[string]any{"event": map[string]any{"type": "Marriage", "date": map[string]any{"dateval": map[string]string{"val": "1975"}}}}).Created
	call("DELETE", "/api/v1/edit/families/{id}/events/{event}", "/api/v1/edit/families/F0001/events/"+marriage, nil)
	call("DELETE", "/api/v1/edit/people/{id}/events/{event}", "/api/v1/edit/people/"+anna+"/events/"+event, nil)
	note := call("POST", "/api/v1/edit/people/{id}/notes", "/api/v1/edit/people/"+anna+"/notes", map[string]string{"text": "A first note."}).Created
	form = c.form(anna)
	call("PUT", "/api/v1/edit/notes/{id}", "/api/v1/edit/notes/"+note+"?person="+anna, map[string]any{"rev": form.Notes[0].Rev, "text": "A better note."})
	call("DELETE", "/api/v1/edit/people/{id}/notes/{note}", "/api/v1/edit/people/"+anna+"/notes/"+note, nil)
	photo := call("POST", "/api/v1/edit/people/{id}/photo", "/api/v1/edit/people/"+anna+"/photo", testPNG(t, 40, 30)).Created
	call("PUT", "/api/v1/edit/people/{id}/media/{media}", "/api/v1/edit/people/"+anna+"/media/"+photo,
		map[string]any{"region": map[string]int{"x1": 10, "y1": 10, "x2": 60, "y2": 70}, "primary": true})
	call("DELETE", "/api/v1/edit/people/{id}/portrait", "/api/v1/edit/people/"+anna+"/portrait", nil)
	call("DELETE", "/api/v1/edit/people/{id}/media/{media}", "/api/v1/edit/people/"+anna+"/media/"+photo, nil)
	shared := call("POST", "/api/v1/edit/gallery", "/api/v1/edit/gallery", testPNG(t, 60, 40)).Created
	call("PUT", "/api/v1/edit/gallery/{id}", "/api/v1/edit/gallery/"+shared, map[string]any{"description": "At the lake", "hidden": true})
	call("PUT", "/api/v1/edit/gallery/{id}/faces/{person}", "/api/v1/edit/gallery/"+shared+"/faces/"+anna,
		map[string]any{"region": map[string]int{"x1": 10, "y1": 10, "x2": 40, "y2": 55}})
	call("DELETE", "/api/v1/edit/gallery/{id}/faces/{person}", "/api/v1/edit/gallery/"+shared+"/faces/"+anna, nil)
	call("DELETE", "/api/v1/edit/gallery/{id}", "/api/v1/edit/gallery/"+shared, nil)
	call("PUT", "/api/v1/edit/home", "/api/v1/edit/home", map[string]string{"person": anna})
	unlinked := call("DELETE", "/api/v1/edit/families/{id}/members/{person}", "/api/v1/edit/families/F0001/members/"+child, nil)
	used["GET /api/v1/history"] = true
	entries := c.history("")
	if len(entries) == 0 || entries[0].ID != unlinked.ChangeID || entries[0].Action != "link.remove" {
		t.Fatalf("history: %+v", entries)
	}
	call("POST", "/api/v1/history/{id}/undo", "/api/v1/history/"+itoa(unlinked.ChangeID)+"/undo", nil)
	call("DELETE", "/api/v1/edit/people/{id}", "/api/v1/edit/people/"+child, nil)
	return used
}

func TestEditRoutesNeedAnEditor(t *testing.T) {
	// Without a session, and with a role below the editor.
	h, uploads := editHarness(t, nil)
	h.loadFixture()
	before := h.revision()
	anonymous := h.client()
	for _, route := range editPaths() {
		res, body := anonymous.do(route[0], route[1], nil, nil)
		if res.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), `"unauthorized"`) {
			t.Errorf("no session, %s %s: %d %s", route[0], route[1], res.StatusCode, body)
		}
	}
	bodies := []any{nil, map[string]any{"person": personBody("F", "Anna", "Orlova")}, testPNG(t, 8, 8)}
	for _, role := range []auth.Role{auth.RoleGuest, auth.RoleMember} {
		c := h.user(role.String()+"@example.org", role)
		for _, route := range editPaths() {
			for _, body := range bodies {
				res, data := c.do(route[0], route[1], body, nil)
				if res.StatusCode != http.StatusForbidden || !strings.Contains(string(data), `"forbidden"`) {
					t.Errorf("%s, %s %s: %d %s", role, route[0], route[1], res.StatusCode, data)
				}
			}
		}
		me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
		if me.Edit || me.Source != "heirfold" {
			t.Errorf("%s: rights %+v", role, me)
		}
	}
	if h.journalSize() != 0 || h.revision() != before || len(filesIn(t, uploads)) != 0 {
		t.Fatal("a refused request changed the tree or stored a file")
	}

	// The editor and the administrator use every route.
	for _, role := range []auth.Role{auth.RoleEditor, auth.RoleAdmin} {
		h, _ := editHarness(t, nil)
		c := h.user(role.String()+"@example.org", role)
		me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
		if !me.Edit || me.Source != "heirfold" {
			t.Fatalf("%s: rights %+v", role, me)
		}
		used := useEveryEditRoute(t, c)
		for _, route := range append(slices.Clone(editRoutes), historyRoute) {
			if !used[route[0]+" "+route[1]] {
				t.Errorf("%s: the test did not use %s %s", role, route[0], route[1])
			}
		}
		if h.journalSize() == 0 {
			t.Fatalf("%s: no journal entries", role)
		}
	}
}

func TestEditIsOffWhileTheTreeFollowsGramps(t *testing.T) {
	h, uploads := editHarness(t, func(cfg *Config) { cfg.Sync = &fakeSync{} })
	h.loadFixture()
	before := h.revision()
	bodies := []any{
		nil, map[string]any{"person": personBody("F", "Anna", "Orlova")}, map[string]string{"person": "I0001"},
		map[string]string{"text": "A note."}, []byte("not json"), testPNG(t, 8, 8),
	}
	for _, role := range []auth.Role{auth.RoleEditor, auth.RoleAdmin} {
		c := h.user(role.String()+"@example.org", role)
		for _, route := range editRoutes {
			for _, body := range bodies {
				res, data := c.do(route[0], fillRoute(route[1]), body, nil)
				if res.StatusCode != http.StatusConflict || decode[map[string]string](t, data)["error"] != "managed_by_gramps" {
					t.Errorf("%s, %s %s: %d %s", role, route[0], route[1], res.StatusCode, data)
				}
			}
		}
		me := decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil))
		if me.Edit || me.Source != "gramps" || me.ImportTree || !me.Invite || !me.ViewLiving || !me.ViewPrivate {
			t.Errorf("%s: rights %+v", role, me)
		}
		// The journal is a read. It has no entries for a tree from Gramps.
		if entries := c.history(""); entries == nil || len(entries) != 0 {
			t.Errorf("%s: history %#v", role, entries)
		}
	}
	// The other rules stay as they are.
	guest := h.user("guest@example.org", auth.RoleGuest)
	anonymous := h.client()
	for _, route := range editPaths() {
		if res, _ := guest.do(route[0], route[1], nil, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest, %s %s: %d", route[0], route[1], res.StatusCode)
		}
		if res, _ := anonymous.do(route[0], route[1], nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("no session, %s %s: %d", route[0], route[1], res.StatusCode)
		}
	}
	if h.journalSize() != 0 || h.revision() != before || len(filesIn(t, uploads)) != 0 {
		t.Fatal("a refused request changed the tree or stored a file")
	}
}

func TestEditFlow(t *testing.T) {
	h := newHarness(t, nil)
	h.loadFixture()
	h.account("editor@example.org", "Elena Editor", auth.RoleEditor)
	h.account("second@example.org", "Egor Second", auth.RoleEditor)
	editor, second := h.client(), h.client()
	editor.signIn("editor@example.org")
	second.signIn("second@example.org")
	guest := h.user("guest@example.org", auth.RoleGuest)
	member := h.user("member@example.org", auth.RoleMember)

	graph := decode[store.Graph](t, editor.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	home := graph.Home
	if home == "" {
		t.Fatal("the fixture has no home person")
	}
	people := len(graph.People)
	first, _ := editor.do("GET", "/api/v1/tree", nil, nil)
	etag := first.Header.Get("ETag")
	revision := h.revision()

	// A new person with a relation.
	created := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people", map[string]any{
		"person":   map[string]any{"gender": "F", "names": []map[string]any{{"index": -1, "given": "Zinaida", "surname": "Canarysurname", "patronymic": "Canarovna"}}},
		"relation": map[string]string{"person": home, "as": "child"},
	}))
	id := created.Created
	if id == "" || created.Subject != home || created.ChangeID == 0 || created.Revision != revision+1 {
		t.Fatalf("result: %+v, revision before %d", created, revision)
	}
	// The tree of the browser is out of date now.
	if res, _ := editor.do("GET", "/api/v1/tree", nil, map[string]string{"If-None-Match": etag}); res.StatusCode != http.StatusOK || res.Header.Get("ETag") == etag {
		t.Fatalf("the tree after an edit: status %d, the same ETag: %v", res.StatusCode, res.Header.Get("ETag") == etag)
	}
	detail := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+id, nil))
	if detail.Given != "Zinaida" || detail.Surname != "Canarysurname" || detail.Patronymic != "Canarovna" || !detail.Living || len(detail.Parents) != 1 {
		t.Fatalf("the new person for a member: %+v", detail)
	}
	parent := detail.Parents[0]
	if (parent.Father == nil || parent.Father.ID != home) && (parent.Mother == nil || parent.Mother.ID != home) {
		t.Fatalf("the parents of the new person: %+v", parent)
	}

	// Read the form, change the person.
	form := editor.form(id)
	if form.ID != id || form.Rev == 0 || form.Gender != "F" || form.Private || form.Home || len(form.Names) != 1 ||
		form.Names[0] != (store.NameInput{Index: 0, Type: "Birth Name", Given: "Zinaida", Surname: "Canarysurname", Patronymic: "Canarovna"}) ||
		form.Events == nil || form.Notes == nil || form.Families == nil {
		t.Fatalf("form: %+v", form)
	}
	stale := second.form(id)
	nextSecond()
	update := func(rev int64, given string) map[string]any {
		return map[string]any{"rev": rev, "person": map[string]any{"gender": "F", "names": []map[string]any{
			{"index": 0, "type": "Birth Name", "given": given, "surname": "Canarysurname", "patronymic": "Canarovna"}}}}
	}
	updated := decode[store.EditResult](t, editor.expect(http.StatusOK, "PUT", "/api/v1/edit/people/"+id, update(form.Rev, "Zina")))
	if updated.Subject != id || updated.ChangeID <= created.ChangeID || updated.Revision != created.Revision+1 {
		t.Fatalf("result of the update: %+v", updated)
	}
	// The second editor still has the old state.
	second.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/people/"+id, update(stale.Rev, "Zinochka"))
	fresh := second.form(id)
	if fresh.Rev == stale.Rev || fresh.Names[0].Given != "Zina" {
		t.Fatalf("form after the update: %+v", fresh)
	}
	nextSecond()
	second.expect(http.StatusOK, "PUT", "/api/v1/edit/people/"+id, update(fresh.Rev, "Zinochka"))

	// An event: add, read, change.
	event := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+id+"/events", map[string]any{
		"event": map[string]any{"type": "Residence", "date": map[string]any{"dateval": map[string]string{"val": "2001-02-03"}},
			"place": map[string]string{"name": "Canarytown"}, "description": "Canarydescription"},
	}))
	if event.Created == "" || event.Subject != id {
		t.Fatalf("result of the event: %+v", event)
	}
	form = editor.form(id)
	if len(form.Events) != 1 || form.Events[0].ID != event.Created || form.Events[0].Type != "Residence" || form.Events[0].Place == nil ||
		form.Events[0].Place.Name != "Canarytown" || form.Events[0].Date.DateVal == nil || form.Events[0].Date.DateVal.Val != "2001-02-03" || form.Events[0].Shared != 1 {
		t.Fatalf("events of the form: %+v", form.Events)
	}
	nextSecond()
	change := map[string]any{"rev": form.Events[0].Rev, "event": map[string]any{
		"type": "Residence", "date": map[string]any{"daterange": map[string]string{"start": "2001", "stop": "2005"}}, "place": map[string]string{"id": form.Events[0].Place.ID}}}
	changed := decode[store.EditResult](t, editor.expect(http.StatusOK, "PUT", "/api/v1/edit/events/"+event.Created+"?person="+id, change))
	if changed.Subject != id || changed.ChangeID == 0 {
		t.Fatalf("result of the event update: %+v", changed)
	}
	second.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/events/"+event.Created+"?person="+id, change)
	form = editor.form(id)
	if form.Events[0].Date.DateRange == nil || form.Events[0].Date.DateRange.Stop != "2005" || form.Events[0].Date.DateVal != nil || form.Events[0].Description != "" {
		t.Fatalf("the event after the update: %+v", form.Events[0])
	}

	// A note.
	note := decode[store.EditResult](t, second.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+id+"/notes", map[string]string{"text": "Canarynote, line one.\nLine two."}))
	if note.Created == "" || note.Subject != id {
		t.Fatalf("result of the note: %+v", note)
	}
	if form = editor.form(id); len(form.Notes) != 1 || form.Notes[0].ID != note.Created || form.Notes[0].Text != "Canarynote, line one.\nLine two." {
		t.Fatalf("notes of the form: %+v", form.Notes)
	}

	// The journal names each action with the editor who did it.
	entries := editor.history("?person=" + id)
	type line struct{ actor, action string }
	var got []line
	for _, entry := range entries {
		if entry.Subject != id || entry.At == 0 || entry.Objects == 0 || entry.Undone || entry.Undoes != 0 {
			t.Errorf("entry: %+v", entry)
		}
		got = append(got, line{entry.Actor, entry.Action})
	}
	want := []line{
		{"Egor Second", "note.add"}, {"Elena Editor", "event.update"}, {"Elena Editor", "event.add"},
		{"Egor Second", "person.update"}, {"Elena Editor", "person.update"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("journal of the person:\n got %+v\nwant %+v", got, want)
	}
	all := editor.history("")
	if len(all) != 6 || all[5].Action != "person.add" || all[5].Subject != home || all[5].Actor != "Elena Editor" || all[5].ID != created.ChangeID || all[0].ID != note.ChangeID {
		t.Fatalf("the whole journal: %+v", all)
	}
	// The journal holds no address.
	if raw := string(editor.expect(http.StatusOK, "GET", "/api/v1/history", nil)); strings.Contains(raw, "@") || strings.Contains(raw, "example.org") {
		t.Fatalf("the journal holds an address: %s", raw)
	}

	// Undo: the note goes away. An older entry of the same person waits.
	editor.refuse(http.StatusConflict, "changed", "POST", "/api/v1/history/"+itoa(updated.ChangeID)+"/undo", nil)
	undone := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(note.ChangeID)+"/undo", nil))
	if undone.ChangeID <= note.ChangeID || undone.Subject != id {
		t.Fatalf("result of the undo: %+v", undone)
	}
	if form = editor.form(id); len(form.Notes) != 0 || len(form.Events) != 1 {
		t.Fatalf("form after the undo: %+v", form)
	}
	all = editor.history("")
	if all[0].Action != "undo" || all[0].Undoes != note.ChangeID || all[0].Actor != "Elena Editor" || !all[1].Undone || all[1].ID != note.ChangeID {
		t.Fatalf("journal after the undo: %+v", all[:2])
	}
	editor.refuse(http.StatusConflict, "changed", "POST", "/api/v1/history/"+itoa(note.ChangeID)+"/undo", nil)
	editor.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/history/999999/undo", nil)
	for _, bad := range []string{"abc", "1.5", "9999999999999999999999"} {
		editor.refuse(http.StatusBadRequest, "bad_request", "POST", "/api/v1/history/"+bad+"/undo", nil)
	}
	// The undo of the undo brings the note back.
	second.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(undone.ChangeID)+"/undo", nil)
	if form = editor.form(id); len(form.Notes) != 1 || form.Notes[0].ID != note.Created {
		t.Fatalf("form after the undo of the undo: %+v", form.Notes)
	}

	// The new person counts as living, so a guest gets nothing about them.
	guest.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/people/"+id, nil)
	guestTree := string(guest.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	guestHome, _ := guest.do("GET", "/api/v1/people/"+home, nil, nil)
	_, guestHomeBody := guest.do("GET", "/api/v1/people/"+home, nil, nil)
	for name, text := range map[string]string{"tree": guestTree, "page of the parent": string(guestHomeBody)} {
		for _, secret := range []string{"Zin", "Canary", `"` + id + `"`} {
			if strings.Contains(text, secret) {
				t.Errorf("the %s of a guest holds %q", name, secret)
			}
		}
	}
	// The home person of the fixture is living, so the guest does not get it.
	if guestHome.StatusCode != http.StatusNotFound && guestHome.StatusCode != http.StatusOK {
		t.Fatalf("the parent for a guest: %d", guestHome.StatusCode)
	}
	memberGraph := decode[store.Graph](t, member.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	if len(memberGraph.People) != people+1 {
		t.Fatalf("people for a member: %d, want %d", len(memberGraph.People), people+1)
	}
	found := false
	for _, card := range memberGraph.People {
		if card.ID == id {
			found = card.Given == "Zinochka" && card.Living
		}
	}
	if !found {
		t.Fatal("the member does not see the new person with the last name")
	}

	// Remove the person. The journal keeps the action, and the undo works.
	removed := decode[store.EditResult](t, editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/people/"+id, nil))
	editor.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/edit/people/"+id, nil)
	member.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/people/"+id, nil)
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(removed.ChangeID)+"/undo", nil)
	if form = editor.form(id); form.Names[0].Given != "Zinochka" || len(form.Events) != 1 || len(form.Notes) != 1 {
		t.Fatalf("the person after the undo of the removal: %+v", form)
	}
	guest.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/people/"+id, nil)
}

func TestEditRequestsAreChecked(t *testing.T) {
	h := newHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	anna := editor.create("F", "Anna", "Orlova", nil)
	father := editor.create("M", "Boris", "Orlov", map[string]string{"person": anna, "as": "parent"})
	mother := editor.create("F", "Vera", "Orlova", map[string]string{"person": anna, "as": "parent"})
	child := editor.create("U", "Child", "Orlov", map[string]string{"person": anna, "as": "child"})
	other := editor.create("M", "Gleb", "Sokolov", nil)
	event := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/events", map[string]any{"event": map[string]string{"type": "Birth"}})).Created
	note := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/notes", map[string]string{"text": "A note."})).Created
	if anna != "I0001" || father != "I0002" || mother != "I0003" || child != "I0004" || other != "I0005" {
		t.Fatalf("people: %s %s %s %s %s", anna, father, mother, child, other)
	}
	form := editor.form(anna)
	personRev, eventRev, noteRev := form.Rev, form.Events[0].Rev, form.Notes[0].Rev
	revision, entries := h.revision(), h.journalSize()

	// Routes that read a JSON body.
	withBody := [][2]string{
		{"POST", "/api/v1/edit/people"}, {"PUT", "/api/v1/edit/people/" + anna}, {"POST", "/api/v1/edit/links"},
		{"PUT", "/api/v1/edit/families/F0001"}, {"POST", "/api/v1/edit/people/" + anna + "/events"}, {"POST", "/api/v1/edit/families/F0001/events"},
		{"PUT", "/api/v1/edit/events/" + event}, {"POST", "/api/v1/edit/people/" + anna + "/notes"}, {"PUT", "/api/v1/edit/notes/" + note},
		{"PUT", "/api/v1/edit/home"},
	}
	// One byte more than the limit, in a string of a known field.
	large := func(field string) []byte {
		head := `{"` + field + `":"`
		return []byte(head + strings.Repeat("a", maxEditBody-len(head)-1) + `"}`)
	}
	for _, route := range withBody {
		// One account has an allowance of 120 writes; see
		// TestEditsOfOnePersonAreRateLimited.
		h.resetLimits()
		for name, body := range map[string][]byte{
			"unknown field":             []byte(`{"extra":1}`),
			"unknown inner field":       []byte(`{"person":{"gender":"F","names":[{"index":-1,"given":"A","handle":"_x"}]},"event":{"type":"Birth","handle":"_x"},"text":"x","rel":"y","as":"child","admin":true}`),
			"no JSON":                   []byte(`not json`),
			"empty body":                {},
			"an array":                  []byte(`[]`),
			"a field of the wrong type": []byte(`{"rev":"1","person":"I0001","text":5,"rel":5,"as":5,"event":5}`),
		} {
			res, data := editor.do(route[0], route[1], body, nil)
			if res.StatusCode != http.StatusBadRequest || decode[map[string]string](t, data)["error"] != "bad_request" {
				t.Errorf("%s %s, %s: %d %s", route[0], route[1], name, res.StatusCode, data)
			}
		}
		for _, field := range []string{"text", "rel", "person", "as"} {
			res, data := editor.do(route[0], route[1], large(field), nil)
			// A request whose first field is unknown for the route stops at
			// that field. Both answers refuse the request.
			if res.StatusCode != http.StatusRequestEntityTooLarge && res.StatusCode != http.StatusBadRequest {
				t.Errorf("%s %s, a large body in %q: %d %s", route[0], route[1], field, res.StatusCode, data)
			}
		}
	}
	if len(large("text")) != maxEditBody+1 {
		t.Fatalf("the large body has %d bytes", len(large("text")))
	}
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "POST", "/api/v1/edit/people/"+anna+"/notes", large("text"))
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "PUT", "/api/v1/edit/notes/"+note, large("text"))
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "PUT", "/api/v1/edit/families/F0001", large("rel"))
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "PUT", "/api/v1/edit/home", large("person"))
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "POST", "/api/v1/edit/links", large("as"))

	// The error codes of the store.
	invalid := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/edit/people", map[string]any{"person": personBody("Z", "A", "B")}},
		{"POST", "/api/v1/edit/people", map[string]any{"person": map[string]any{"gender": "F"}}},
		{"POST", "/api/v1/edit/people", map[string]any{}},
		{"POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", strings.Repeat("я", 121), "")}},
		{"PUT", "/api/v1/edit/people/" + anna, map[string]any{"rev": personRev, "person": map[string]any{"gender": "F", "names": []map[string]any{{"index": 7, "given": "A"}}}}},
		{"PUT", "/api/v1/edit/people/" + anna, map[string]any{"rev": personRev, "person": personBody("Z", "A", "B")}},
		{"POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "cousin", "other": other}},
		{"POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "spouse", "other": anna}},
		{"PUT", "/api/v1/edit/families/F0001", map[string]string{"rel": ""}},
		{"POST", "/api/v1/edit/people/" + anna + "/events", map[string]any{"event": map[string]any{"type": ""}}},
		{"POST", "/api/v1/edit/people/" + anna + "/events", map[string]any{"event": map[string]any{"type": "Birth", "date": map[string]any{"dateval": map[string]string{"val": "12.05.1890"}}}}},
		{"POST", "/api/v1/edit/people/" + anna + "/events", map[string]any{"event": map[string]any{"type": "Birth", "place": map[string]string{"id": "P0999"}}}},
		{"PUT", "/api/v1/edit/events/" + event, map[string]any{"rev": eventRev, "event": map[string]any{"type": "Birth", "date": map[string]any{
			"dateval": map[string]string{"val": "1890"}, "datestr": map[string]string{"val": "spring"}}}}},
		{"POST", "/api/v1/edit/people/" + anna + "/notes", map[string]string{"text": "   "}},
		{"POST", "/api/v1/edit/people/" + anna + "/notes", map[string]string{"text": strings.Repeat("я", 50_001)}},
		{"PUT", "/api/v1/edit/notes/" + note, map[string]any{"rev": noteRev, "text": ""}},
	}
	h.resetLimits()
	for _, tc := range invalid {
		editor.refuse(http.StatusBadRequest, "invalid", tc.method, tc.path, tc.body)
	}
	editor.refuse(http.StatusConflict, "parents_full", "POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "parent", "other": other})
	editor.refuse(http.StatusConflict, "parents_full", "POST", "/api/v1/edit/people",
		map[string]any{"person": personBody("M", "Third", "Parent"), "relation": map[string]string{"person": anna, "as": "parent"}})
	editor.refuse(http.StatusConflict, "loop", "POST", "/api/v1/edit/links", map[string]string{"person": father, "as": "parent", "other": child})
	editor.refuse(http.StatusConflict, "loop", "POST", "/api/v1/edit/links", map[string]string{"person": child, "as": "child", "other": mother})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/people/"+anna, map[string]any{"rev": 5, "person": personBody("F", "A", "B")})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/events/"+event, map[string]any{"rev": 5, "event": map[string]string{"type": "Birth"}})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/notes/"+note, map[string]any{"rev": 5, "text": "Other."})
	// A request without the state number is refused too, also when the rest
	// of it is wrong.
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/people/"+anna, map[string]any{"person": personBody("F", "A", "B")})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/people/"+anna, map[string]any{"rev": 0, "person": personBody("Z", "A", "B")})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/events/"+event, map[string]any{"event": map[string]string{"type": "Birth"}})
	editor.refuse(http.StatusConflict, "changed", "PUT", "/api/v1/edit/notes/"+note, map[string]any{"text": "Other."})
	// A circle through a sibling link or a spouse link.
	editor.refuse(http.StatusConflict, "loop", "POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "sibling", "other": father})
	editor.refuse(http.StatusConflict, "loop", "POST", "/api/v1/edit/links", map[string]string{"person": child, "as": "sibling", "other": mother})
	missing := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/edit/people/I0999", nil},
		{"PUT", "/api/v1/edit/people/I0999", map[string]any{"person": personBody("F", "A", "B")}},
		{"DELETE", "/api/v1/edit/people/I0999", nil},
		{"POST", "/api/v1/edit/people", map[string]any{"person": personBody("F", "A", "B"), "relation": map[string]string{"person": "I0999", "as": "child"}}},
		{"POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "child", "other": "I0999"}},
		{"POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "child", "other": other, "family": "F0999"}},
		{"DELETE", "/api/v1/edit/families/F0999/members/" + anna, nil},
		{"DELETE", "/api/v1/edit/families/F0001/members/" + other, nil},
		{"PUT", "/api/v1/edit/families/F0999", map[string]string{"rel": "Married"}},
		{"POST", "/api/v1/edit/people/I0999/events", map[string]any{"event": map[string]string{"type": "Birth"}}},
		{"POST", "/api/v1/edit/families/F0999/events", map[string]any{"event": map[string]string{"type": "Marriage"}}},
		{"PUT", "/api/v1/edit/events/E0999", map[string]any{"event": map[string]string{"type": "Birth"}}},
		{"DELETE", "/api/v1/edit/people/" + other + "/events/" + event, nil},
		{"DELETE", "/api/v1/edit/families/F0001/events/" + event, nil},
		{"POST", "/api/v1/edit/people/I0999/notes", map[string]string{"text": "A note."}},
		{"PUT", "/api/v1/edit/notes/N0999", map[string]any{"text": "A note."}},
		{"DELETE", "/api/v1/edit/people/" + other + "/notes/" + note, nil},
		{"DELETE", "/api/v1/edit/people/" + anna + "/media/O0999", nil},
		{"PUT", "/api/v1/edit/home", map[string]string{"person": "I0999"}},
		{"PUT", "/api/v1/edit/home", map[string]string{}},
		// An ID is only an ID: it is no path and no query.
		{"GET", "/api/v1/edit/people/..%2F..%2Fetc%2Fpasswd", nil},
		{"GET", "/api/v1/edit/people/I0001%27%20OR%201=1", nil},
	}
	h.resetLimits()
	for _, tc := range missing {
		editor.refuse(http.StatusNotFound, "not_found", tc.method, tc.path, tc.body)
	}
	// A method that a route does not have.
	for _, route := range [][2]string{{"PATCH", "/api/v1/edit/people/" + anna}, {"DELETE", "/api/v1/edit/events/" + event}, {"DELETE", "/api/v1/history/1"}, {"POST", "/api/v1/history"}} {
		if res, _ := editor.do(route[0], route[1], nil, nil); res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: %d", route[0], route[1], res.StatusCode)
		}
	}
	// A write from another site does not pass.
	res, _ := editor.do("DELETE", "/api/v1/edit/people/"+other, nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("a request from another site: %d", res.StatusCode)
	}
	if h.revision() != revision || h.journalSize() != entries {
		t.Fatalf("a refused request changed the tree: revision %d -> %d, journal %d -> %d", revision, h.revision(), entries, h.journalSize())
	}
	// A note at the limit of 50 000 letters fits into the body limit.
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/notes", map[string]string{"text": strings.Repeat("я", 50_000)})
	// The error answers hold nothing of the request.
	_, data := editor.do("POST", "/api/v1/edit/people", map[string]any{"person": personBody("Z", "Canarygiven", "Canarysurname")}, nil)
	if strings.Contains(string(data), "Canary") {
		t.Fatalf("an error answer repeats the input: %s", data)
	}
}

func TestEditsOfOnePersonAreRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	other := h.user("other@example.org", auth.RoleEditor)
	limited := 0
	for i := 0; i < int(ruleEdit.capacity)+40; i++ {
		res, body := editor.do("PUT", "/api/v1/edit/home", map[string]string{"person": "I0999"}, nil)
		switch res.StatusCode {
		case http.StatusNotFound:
		case http.StatusTooManyRequests:
			if decode[map[string]string](t, body)["error"] != "rate_limited" {
				t.Fatalf("answer: %s", body)
			}
			limited++
		default:
			t.Fatalf("status %d: %s", res.StatusCode, body)
		}
	}
	if limited == 0 {
		t.Fatalf("%d writes in a row passed the limit of %v", int(ruleEdit.capacity)+40, ruleEdit.capacity)
	}
	// Reads do not count, and another editor has an own allowance.
	editor.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil)
	editor.expect(http.StatusOK, "GET", "/api/v1/history", nil)
	other.create("F", "Anna", "Orlova", nil)
}

func TestPhotoUpload(t *testing.T) {
	h, uploads := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	anna := editor.create("F", "Anna", "Orlova", nil)
	path := "/api/v1/edit/people/" + anna + "/photo"
	hexName := regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg)$`)

	// The header and the file name of the request say nothing.
	lies := map[string]string{
		"Content-Type":        "text/html",
		"Content-Disposition": `form-data; name="file"; filename="../../canary-evil.html"`,
		"X-File-Name":         "../canary-evil.svg",
	}
	pngFile := testPNG(t, 40, 30)
	res, body := editor.do("POST", path+"?name=canary-evil.php", pngFile, lies)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PNG: status %d: %s", res.StatusCode, body)
	}
	first := decode[store.EditResult](t, body)
	if first.Created != "O0001" || first.Subject != anna || first.ChangeID == 0 {
		t.Fatalf("result: %+v", first)
	}
	files := filesIn(t, uploads)
	if len(files) != 1 || !hexName.MatchString(files[0].Name()) || !strings.HasSuffix(files[0].Name(), ".png") ||
		files[0].Mode().Perm() != 0o600 || !files[0].Mode().IsRegular() || files[0].Size() != int64(len(pngFile)) {
		t.Fatalf("stored file: %+v", files)
	}
	if info, err := os.Stat(uploads); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the directory of the uploads: %v %v", info, err)
	}
	stored, err := os.ReadFile(filepath.Join(uploads, files[0].Name())) // #nosec G304 -- a path of the test.
	if err != nil || !bytes.Equal(stored, pngFile) {
		t.Fatalf("the stored bytes differ: %v", err)
	}
	// Nothing lies outside the directory of the uploads.
	if outside := filesIn(t, h.app.mediaDir); len(outside) != 1 || outside[0].Name() != "heirfold" {
		t.Fatalf("the media directory: %+v", outside)
	}
	res, body = member.do("GET", "/api/v1/media/"+first.Created+"/image?size=s", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("picture of the PNG: status %d, type %q: %.200s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if img, err := jpeg.Decode(bytes.NewReader(body)); err != nil || img.Bounds().Dx() != 40 || img.Bounds().Dy() != 30 {
		t.Fatalf("the picture is no JPEG of 40x30: %v", err)
	}
	detail := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil))
	if detail.Photo == nil || detail.Photo.ID != first.Created || len(detail.Media) != 1 || !detail.Media[0].Image || detail.Media[0].Mime != "image/png" {
		t.Fatalf("the person after the upload: photo %+v, media %+v", detail.Photo, detail.Media)
	}

	// A JPEG, with a header that names another type.
	jpegFile := testJPEG(t, 64, 48)
	res, body = editor.do("POST", path, jpegFile, map[string]string{"Content-Type": "image/svg+xml"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("JPEG: status %d: %s", res.StatusCode, body)
	}
	second := decode[store.EditResult](t, body)
	files = filesIn(t, uploads)
	if second.Created != "O0002" || len(files) != 2 {
		t.Fatalf("result: %+v, files %d", second, len(files))
	}
	for _, file := range files {
		if !hexName.MatchString(file.Name()) || file.Mode().Perm() != 0o600 || strings.Contains(file.Name(), "canary") {
			t.Errorf("stored file %q with mode %v", file.Name(), file.Mode())
		}
	}
	res, body = member.do("GET", "/api/v1/media/"+second.Created+"/image?size=s", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("picture of the JPEG: status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	// The new picture is the portrait.
	if detail := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil)); detail.Photo == nil || detail.Photo.ID != second.Created || len(detail.Media) != 2 {
		t.Fatalf("the person after the second upload: %+v", detail.Photo)
	}
	// The original file is a download with its real type.
	res, body = member.do("GET", "/api/v1/media/"+second.Created+"/file", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(body, jpegFile) ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("the original file: status %d, type %q, disposition %q", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
	}

	// The same picture again is the same file and the same media object.
	again := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", path, pngFile))
	if again.Created != first.Created || len(filesIn(t, uploads)) != 2 {
		t.Fatalf("the same picture again: %+v, files %d", again, len(filesIn(t, uploads)))
	}
	// Another person can show it too.
	boris := editor.create("M", "Boris", "Orlov", nil)
	shared := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+boris+"/photo", pngFile))
	if shared.Created != first.Created || shared.Subject != boris || len(filesIn(t, uploads)) != 2 {
		t.Fatalf("a shared picture: %+v", shared)
	}

	// Files that are no JPEG and no PNG.
	revision, entries := h.revision(), h.journalSize()
	large := claimSize(t, testPNG(t, 100, 100), 20000, 20000)
	refused := map[string][]byte{
		"HTML":                                  []byte("<!doctype html><html><body><script>alert(1)</script></body></html>"),
		"HTML with a picture type":              []byte("<html><img src=x onerror=alert(1)></html>"),
		"SVG":                                   []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><script>alert(1)</script></svg>`),
		"GIF":                                   testGIF(t),
		"empty body":                            {},
		"text":                                  []byte("just some text"),
		"PDF":                                   []byte("%PDF-1.4 invented"),
		"only the PNG signature":                []byte("\x89PNG\r\n\x1a\n"),
		"the start of a JPEG":                   jpegFile[:20],
		"a PNG header with a bad sum":           append(pngWithSize(10, 10)[:29], 0, 0, 0, 0),
		"a PNG header with no size":             pngWithSize(0, 0),
		"a PNG header of 100 megapixels":        pngWithSize(10000, 10000),
		"a PNG header just over the limit":      pngWithSize(7746, 7746),
		"a PNG header of the largest size":      pngWithSize(0x7fffffff, 0x7fffffff),
		"a real PNG that claims 400 megapixels": large,
		"a real JPEG that claims 4000 megapixels": claimJPEGSize(t, jpegFile, 0xffff, 0xffff),
		// Complete headers with no picture after them.
		"a PNG header alone":          pngWithSize(100, 100),
		"a PNG header before a page":  append(pngWithSize(100, 100), []byte("<html><script>alert(1)</script></html>")...),
		"a JPEG that stops too early": testJPEG(t, 200, 200)[:400],
		"a PNG that stops too early":  testPNG(t, 200, 200)[:300],
		// 36 million pixels with 8 bytes each need more than 256 MiB.
		"a 16-bit PNG header over the memory limit": pngWithDepth(6000, 6000, 16, 6),
		"a multipart form":                          []byte("--x\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n" + string(pngFile) + "\r\n--x--\r\n"),
	}
	if maxImagePixels != 60_000_000 || 7746*7746 <= maxImagePixels {
		t.Fatalf("the test expects a limit of 60 million pixels, got %d", maxImagePixels)
	}
	for name, data := range refused {
		for _, contentType := range []string{"image/png", "image/jpeg", "application/octet-stream", ""} {
			// One account has a small allowance of uploads; see
			// TestPhotoUploadsOfOnePersonAreRateLimited.
			h.resetLimits()
			res, body := editor.do("POST", path, data, map[string]string{"Content-Type": contentType, "X-File-Name": "photo.png"})
			if res.StatusCode != http.StatusUnprocessableEntity || decode[map[string]string](t, body)["error"] != "bad_image" {
				t.Errorf("%s as %q: status %d: %s", name, contentType, res.StatusCode, body)
			}
		}
	}
	// A body over the limit, also when it starts as a real picture.
	if maxPhotoBytes != 25<<20 {
		t.Fatalf("the test expects a limit of 25 MB, got %d", maxPhotoBytes)
	}
	h.resetLimits()
	tooLarge := make([]byte, maxPhotoBytes+1)
	copy(tooLarge, pngFile)
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "POST", path, tooLarge)
	copy(tooLarge, jpegFile)
	editor.refuse(http.StatusRequestEntityTooLarge, "too_large", "POST", path, tooLarge)
	if len(filesIn(t, uploads)) != 2 || h.revision() != revision || h.journalSize() != entries {
		t.Fatalf("a refused upload left a file or changed the tree: files %d", len(filesIn(t, uploads)))
	}
	// The person is checked before the body: no size and no content matter.
	editor.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/edit/people/I0999/photo", pngFile)
	editor.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/edit/people/I0999/photo", tooLarge)
	editor.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/edit/people/I0999/photo", []byte("<html>"))
	if len(filesIn(t, uploads)) != 2 {
		t.Fatal("an upload for an unknown person left a file")
	}

	// A removed picture leaves the person. The file stays, so the undo works.
	removed := decode[store.EditResult](t, editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/people/"+anna+"/media/"+second.Created, nil))
	member.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/media/"+second.Created+"/image?size=s", nil)
	if detail := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil)); detail.Photo == nil || detail.Photo.ID != first.Created {
		t.Fatalf("the portrait after the removal: %+v", detail.Photo)
	}
	if len(filesIn(t, uploads)) != 2 {
		t.Fatal("the file of a removed picture went away")
	}
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(removed.ChangeID)+"/undo", nil)
	member.expect(http.StatusOK, "GET", "/api/v1/media/"+second.Created+"/image?size=s", nil)

	// The journal names the uploads.
	actions := map[string]int{}
	for _, entry := range editor.history("?person=" + anna) {
		actions[entry.Action]++
	}
	if actions["media.add"] != 3 || actions["media.remove"] != 1 || actions["undo"] != 1 {
		t.Fatalf("journal: %+v", actions)
	}
}

// A guest gets no picture of a person who counts as living.
func TestGuestGetsNoUploadedPhotoOfALivingPerson(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	guest := h.user("guest@example.org", auth.RoleGuest)
	anna := editor.create("F", "Anna", "Orlova", nil)
	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/photo", testPNG(t, 20, 20))).Created
	for _, suffix := range []string{"/image?size=s", "/file"} {
		guest.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/media/"+photo+suffix, nil)
	}
	list := string(guest.expect(http.StatusOK, "GET", "/api/v1/media", nil))
	if strings.Contains(list, photo) {
		t.Fatalf("the media list of a guest: %s", list)
	}
	// With a death event the person and the picture open.
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/events", map[string]any{"event": map[string]any{"type": "Death", "date": map[string]any{"dateval": map[string]string{"val": "1950"}}}})
	guest.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil)
	guest.expect(http.StatusOK, "GET", "/api/v1/media/"+photo+"/image?size=s", nil)
}

func TestPhotoUploadNeedsAMediaDirectory(t *testing.T) {
	h := newHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	anna := editor.create("F", "Anna", "Orlova", nil)
	entries := h.journalSize()
	for _, body := range [][]byte{testPNG(t, 40, 30), testJPEG(t, 40, 30), []byte("<html>"), {}} {
		editor.refuse(http.StatusConflict, "media_off", "POST", "/api/v1/edit/people/"+anna+"/photo", body)
	}
	editor.refuse(http.StatusConflict, "media_off", "POST", "/api/v1/edit/people/I0999/photo", testPNG(t, 40, 30))
	if h.journalSize() != entries {
		t.Fatal("an upload without a media directory changed the tree")
	}
	for _, dir := range []string{"heirfold", "media"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(h.dbPath), dir)); !os.IsNotExist(err) {
			t.Fatalf("the data directory got %q: %v", dir, err)
		}
	}
}

// The server checks the person before it reads the body, so a request for a
// person who does not exist leaves no file.
func TestPhotoForAnUnknownPersonLeavesNoFile(t *testing.T) {
	h, uploads := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	editor.refuse(http.StatusNotFound, "not_found", "POST", "/api/v1/edit/people/I0999/photo", testPNG(t, 40, 30))
	if files := filesIn(t, uploads); len(files) != 0 {
		t.Fatalf("%d files stay after a refused upload", len(files))
	}
	if h.journalSize() != 0 {
		t.Fatal("a refused upload wrote to the journal")
	}
}

// One account uploads few pictures in a short time.
func TestPhotoUploadsOfOnePersonAreRateLimited(t *testing.T) {
	h, uploads := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	other := h.user("other@example.org", auth.RoleEditor)
	anna := editor.create("F", "Anna", "Orlova", nil)
	path := "/api/v1/edit/people/" + anna + "/photo"
	picture := testPNG(t, 12, 12)
	if ruleUpload.capacity != 30 {
		t.Fatalf("the test expects an allowance of 30 uploads, got %v", ruleUpload.capacity)
	}
	for i := 0; i < int(ruleUpload.capacity); i++ {
		// Refused uploads count too.
		body := picture
		if i%2 == 1 {
			body = []byte("no picture")
		}
		if res, data := editor.do("POST", path, body, nil); res.StatusCode != http.StatusOK && res.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("upload %d: status %d: %s", i+1, res.StatusCode, data)
		}
	}
	entries := h.journalSize()
	editor.refuse(http.StatusTooManyRequests, "rate_limited", "POST", path, testPNG(t, 14, 14))
	editor.refuse(http.StatusTooManyRequests, "rate_limited", "POST", "/api/v1/edit/people/I0999/photo", picture)
	if len(filesIn(t, uploads)) != 1 || h.journalSize() != entries {
		t.Fatalf("a refused upload stored something: files %d", len(filesIn(t, uploads)))
	}
	// Other edits and the uploads of another editor go on.
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/notes", map[string]string{"text": "A note."})
	other.expect(http.StatusOK, "POST", path, testPNG(t, 14, 14))
}

func TestImportOverATreeWritesABackup(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("root@example.org", auth.RoleAdmin)
	backups := filepath.Join(filepath.Dir(h.dbPath), "backups")
	tree := fixture.Generate(fixture.DefaultOptions("en"))
	data, err := gramps.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	type preview struct {
		ID string `json:"id"`
	}
	upload := func(replace bool) {
		t.Helper()
		id := decode[preview](t, admin.expect(http.StatusOK, "POST", "/api/v1/imports", data)).ID
		admin.expect(http.StatusOK, "POST", "/api/v1/imports/"+id+"/commit", map[string]bool{"replace": replace})
	}
	// The first import has nothing to save.
	upload(false)
	if files := filesIn(t, backups); len(files) != 0 {
		t.Fatalf("a backup of no tree: %+v", files)
	}
	// An edit, so that the stored tree differs from the file.
	created := admin.create("F", "Zinaida", "Canarysurname", nil)
	if h.journalSize() != 1 {
		t.Fatalf("journal: %d", h.journalSize())
	}
	// A refused commit writes no backup.
	id := decode[preview](t, admin.expect(http.StatusOK, "POST", "/api/v1/imports", data)).ID
	admin.refuse(http.StatusConflict, "tree_exists", "POST", "/api/v1/imports/"+id+"/commit", map[string]bool{"replace": false})
	admin.refuse(http.StatusNotFound, "import_expired", "POST", "/api/v1/imports/unknown/commit", map[string]bool{"replace": true})
	if files := filesIn(t, backups); len(files) != 0 {
		t.Fatalf("a refused commit wrote a backup: %+v", files)
	}
	admin.expect(http.StatusOK, "POST", "/api/v1/imports/"+id+"/commit", map[string]bool{"replace": true})

	files := filesIn(t, backups)
	if len(files) != 1 || !regexp.MustCompile(`^tree-\d{8}-\d{6}\.gramps$`).MatchString(files[0].Name()) || files[0].Mode().Perm() != 0o600 || files[0].Size() == 0 {
		t.Fatalf("backups: %+v", files)
	}
	if info, err := os.Stat(backups); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the directory of the backups: %v %v", info, err)
	}
	file, err := os.Open(filepath.Join(backups, files[0].Name())) // #nosec G304 -- a path of the test.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	saved, err := gramps.Parse(file, gramps.DefaultLimits)
	if err != nil {
		t.Fatalf("the backup does not parse: %v", err)
	}
	// The backup holds the tree before the replacement, with the edit.
	if len(saved.Tree.People) != len(tree.People)+1 || saved.WarningsTotal != 0 {
		t.Fatalf("people in the backup: %d, want %d", len(saved.Tree.People), len(tree.People)+1)
	}
	found := false
	for _, person := range saved.Tree.People {
		if person.ID == created {
			found = person.Names[0].First == "Zinaida"
		}
	}
	if !found {
		t.Fatal("the backup lacks the person of the edit")
	}
	// A replacement of the tree needs a recent sign-in code.
	h.makeStale(31 * time.Minute)
	stale := decode[preview](t, admin.expect(http.StatusOK, "POST", "/api/v1/imports", data)).ID
	admin.refuse(http.StatusForbidden, "reauth_required", "POST", "/api/v1/imports/"+stale+"/commit", map[string]bool{"replace": true})
	if n := len(filesIn(t, backups)); n != 1 {
		t.Fatalf("a refused commit wrote a backup: %d files", n)
	}
	// After a new code the same upload goes in. The second backup must have
	// another name, which holds the time in seconds.
	nextSecond()
	admin.signIn("root@example.org")
	admin.expect(http.StatusOK, "POST", "/api/v1/imports/"+stale+"/commit", map[string]bool{"replace": true})
	if n := len(filesIn(t, backups)); n != 2 {
		t.Fatalf("backups after the second replacement: %d", n)
	}
	// The new tree is the file, and the journal of the old tree is gone.
	graph := decode[store.Graph](t, admin.expect(http.StatusOK, "GET", "/api/v1/tree", nil))
	if len(graph.People) != len(tree.People) || h.journalSize() != 0 || len(admin.history("")) != 0 {
		t.Fatalf("after the replacement: people %d, journal %d", len(graph.People), h.journalSize())
	}
	admin.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/edit/people/"+created, nil)
	// The backup is no file of the web server.
	for _, path := range []string{"/backups/" + files[0].Name(), "/api/v1/backups/" + files[0].Name()} {
		if res, body := admin.do("GET", path, nil, nil); bytes.HasPrefix(body, []byte{0x1f, 0x8b}) || strings.Contains(res.Header.Get("Content-Type"), "gzip") {
			t.Fatalf("%s serves the backup", path)
		}
	}
}

// TestEditLogsHoldNoNames runs an edit flow with requests that pass and
// requests that fail, and then reads the server log and the audit log.
func TestEditLogsHoldNoNames(t *testing.T) {
	h, _ := editHarness(t, nil)
	// The audit log names the actor by the display name of the account, so
	// the accounts carry no canary. The addresses must not appear at all.
	h.account("e.editor@example.org", "Elena Editor", auth.RoleEditor)
	editor := h.client()
	editor.signIn("e.editor@example.org")
	h.account("g.guest@example.org", "Gleb", auth.RoleGuest)
	guest := h.client()
	guest.signIn("g.guest@example.org")

	body := func(given, surname string) map[string]any {
		return map[string]any{"gender": "F", "names": []map[string]any{{
			"index": -1, "given": given, "surname": surname, "patronymic": "Canarovna", "nick": "Canarynick", "title": "Canarytitle", "suffix": "Canarysuffix", "call": "Canarycall"}}}
	}
	anna := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people", map[string]any{"person": body("Canarygiven", "Canarysurname")})).Created
	boris := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people",
		map[string]any{"person": body("Canaryspouse", "Canarysurname"), "relation": map[string]string{"person": anna, "as": "spouse"}})).Created
	form := editor.form(anna)
	form.Names[0].Given = "Canaryrenamed"
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/people/"+anna, map[string]any{"rev": form.Rev, "person": map[string]any{"gender": "F", "names": form.Names}})
	event := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/events", map[string]any{"event": map[string]any{
		"type": "Canarytype", "date": map[string]any{"datestr": map[string]string{"val": "Canarydate"}}, "place": map[string]string{"name": "Canarytown"}, "description": "Canarydescription"}})).Created
	form = editor.form(anna)
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/events/"+event+"?person="+anna, map[string]any{"rev": form.Events[0].Rev, "event": map[string]any{"type": "Canarytype", "description": "Canarychanged"}})
	note := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/notes", map[string]string{"text": "Canarynote"})).Created
	form = editor.form(anna)
	noteUpdate := decode[store.EditResult](t, editor.expect(http.StatusOK, "PUT", "/api/v1/edit/notes/"+note+"?person="+anna, map[string]any{"rev": form.Notes[0].Rev, "text": "Canarynote, changed"}))
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(noteUpdate.ChangeID)+"/undo", nil)
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/families/F0001", map[string]string{"rel": "Canaryrel"})
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/photo?name=canaryquery.png", testPNG(t, 16, 16))
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/home", map[string]string{"person": anna})
	editor.expect(http.StatusOK, "GET", "/api/v1/edit/places", nil)
	editor.expect(http.StatusOK, "GET", "/api/v1/history?person="+anna, nil)
	editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/families/F0001/members/"+boris, nil)
	editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/people/"+boris, nil)

	// Requests that fail, with names in the body, in the path and in the query.
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/edit/people", map[string]any{"person": map[string]any{"gender": "Canarygender", "names": []map[string]any{{"index": -1, "given": "Canaryinvalid"}}}})
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/edit/people", []byte(`{"canaryfield":"Canaryvalue"}`))
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/edit/people", []byte(`Canary that is no JSON`))
	editor.expect(http.StatusNotFound, "GET", "/api/v1/edit/people/Canarypath", nil)
	editor.expect(http.StatusNotFound, "PUT", "/api/v1/edit/events/E0999?person=Canaryquery", map[string]any{"event": map[string]string{"type": "Canarytype"}})
	editor.expect(http.StatusNotFound, "POST", "/api/v1/edit/links", map[string]string{"person": anna, "as": "child", "other": "Canaryother"})
	editor.expect(http.StatusConflict, "PUT", "/api/v1/edit/people/"+anna, map[string]any{"rev": 5, "person": body("Canarystale", "Canarysurname")})
	editor.expect(http.StatusUnprocessableEntity, "POST", "/api/v1/edit/people/"+anna+"/photo", []byte("<html>Canaryhtml</html>"))
	editor.expect(http.StatusBadRequest, "POST", "/api/v1/history/Canaryundo/undo", nil)
	guest.expect(http.StatusForbidden, "POST", "/api/v1/edit/people", map[string]any{"person": body("Canaryguest", "Canarysurname")})
	guest.expect(http.StatusNotFound, "GET", "/api/v1/people/"+anna, nil)
	h.client().expect(http.StatusUnauthorized, "POST", "/api/v1/edit/people", map[string]any{"person": body("Canaryanonymous", "Canarysurname")})
	h.idle()

	log := h.log.String()
	for _, want := range []string{
		`route="POST /api/v1/edit/people"`, `route="PUT /api/v1/edit/people/{id}"`, `route="POST /api/v1/edit/people/{id}/photo"`,
		`route="PUT /api/v1/edit/events/{id}"`, `route="POST /api/v1/history/{id}/undo"`, "status=409", "status=422", "status=404",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the server log lacks %q", want)
		}
	}
	audit := h.auditText()
	for name, text := range map[string]string{"server log": log, "audit log": audit} {
		lower := strings.ToLower(text)
		for _, part := range []string{"canary", "@", "example.org", "e.editor", "g.guest", "/" + strings.ToLower(anna), "people/i0", "f0001", strings.ToLower(event), strings.ToLower(note)} {
			if strings.Contains(lower, part) {
				t.Errorf("the %s holds %q:\n%s", name, part, text)
				break
			}
		}
	}
	if strings.Contains(log, "level=ERROR") {
		t.Errorf("the server log has an error:\n%s", log)
	}
}

// Each new picture form costs a decode. One account has an allowance of new
// decodes; a picture from the cache costs nothing.
func TestNewPictureFormsAreRateLimited(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	other := h.user("other@example.org", auth.RoleMember)
	anna := editor.create("F", "Anna", "Orlova", nil)
	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+anna+"/photo", testPNG(t, 64, 64))).Created
	path := "/api/v1/media/" + photo + "/image?size=s"
	member.expect(http.StatusOK, "GET", path, nil)
	limited := 0
	for i := 0; i < int(ruleRender.capacity)+40; i++ {
		res, body := member.do("GET", path+"&region=0,0,"+itoa(int64(10+i%90))+","+itoa(int64(10+i/90)), nil, nil)
		switch res.StatusCode {
		case http.StatusOK:
		case http.StatusTooManyRequests:
			if decode[map[string]string](t, body)["error"] != "rate_limited" {
				t.Fatalf("answer: %s", body)
			}
			limited++
		default:
			t.Fatalf("status %d: %s", res.StatusCode, body)
		}
	}
	if limited == 0 {
		t.Fatalf("%d new picture forms in a row passed the limit", int(ruleRender.capacity)+40)
	}
	// The cached form and another account still work.
	member.expect(http.StatusOK, "GET", path, nil)
	other.expect(http.StatusOK, "GET", path+"&region=1,1,50,50", nil)
}

// An editor chooses the portrait among the pictures of a person and the part
// of the picture that the portrait shows. The file stays as it is.
func TestPortraitChoiceAndRegion(t *testing.T) {
	h, uploads := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	anna := editor.create("F", "Anna", "Orlova", nil)
	boris := editor.create("M", "Boris", "Orlov", nil)
	upload := func(person string, file []byte) string {
		t.Helper()
		return decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+person+"/photo", file)).Created
	}
	first := upload(anna, testPNG(t, 40, 30))
	second := upload(anna, testJPEG(t, 64, 48))
	other := upload(boris, testPNG(t, 20, 20))
	before := filesIn(t, uploads)
	person := func() store.PersonDetail {
		t.Helper()
		return decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil))
	}
	if detail := person(); detail.Photo == nil || detail.Photo.ID != second || detail.Photo.Region != nil {
		t.Fatalf("portrait before the choice: %+v", detail.Photo)
	}
	path := "/api/v1/edit/people/" + anna + "/media/"
	region := model.Region{X1: 10, Y1: 20, X2: 60, Y2: 70}
	change := decode[store.EditResult](t, editor.expect(http.StatusOK, "PUT", path+first, map[string]any{"region": region, "primary": true}))
	detail := person()
	if detail.Photo == nil || detail.Photo.ID != first || detail.Photo.Region == nil || *detail.Photo.Region != region {
		t.Fatalf("portrait after the choice: %+v", detail.Photo)
	}
	if len(detail.Media) != 2 || detail.Media[0].ID != first || detail.Media[1].ID != second || detail.Media[1].Region != nil {
		t.Fatalf("pictures after the choice: %+v", detail.Media)
	}
	if entries := editor.history("?person=" + anna); entries[0].ID != change.ChangeID || entries[0].Action != "media.update" {
		t.Fatalf("journal: %+v", entries[0])
	}
	// The picture of the region has the form of the region: 50% of 40 by 50% of 30.
	res, body := member.do("GET", "/api/v1/media/"+first+"/image?size=s&region=10,20,60,70", nil, nil)
	if img, err := jpeg.Decode(bytes.NewReader(body)); res.StatusCode != http.StatusOK || err != nil || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 15 {
		t.Fatalf("picture of the region: status %d, %v", res.StatusCode, err)
	}
	// A change of the region alone keeps the order.
	editor.expect(http.StatusOK, "PUT", path+second, map[string]any{"region": map[string]int{"x1": 0, "y1": 0, "x2": 50, "y2": 50}})
	if detail := person(); detail.Photo.ID != first || detail.Media[1].Region == nil || detail.Media[1].Region.X2 != 50 {
		t.Fatalf("after a region for the second picture: %+v", detail.Media)
	}
	// The whole picture is the same as no region.
	editor.expect(http.StatusOK, "PUT", path+first, map[string]any{"region": map[string]int{"x1": 0, "y1": 0, "x2": 100, "y2": 100}})
	if detail := person(); detail.Photo.ID != first || detail.Photo.Region != nil {
		t.Fatalf("after the whole picture: %+v", detail.Photo)
	}
	editor.expect(http.StatusOK, "PUT", path+first, map[string]any{"region": region})
	editor.expect(http.StatusOK, "PUT", path+first, map[string]any{})
	if detail := person(); detail.Photo.Region != nil {
		t.Fatalf("after a request without a region: %+v", detail.Photo)
	}

	for name, bad := range map[string]map[string]int{
		"empty":    {"x1": 10, "y1": 10, "x2": 10, "y2": 60},
		"reversed": {"x1": 60, "y1": 10, "x2": 10, "y2": 60},
		"too far":  {"x1": 0, "y1": 0, "x2": 101, "y2": 50},
		"negative": {"x1": -1, "y1": 0, "x2": 50, "y2": 50},
	} {
		res, body := editor.do("PUT", path+second, map[string]any{"region": bad}, nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("region %s: status %d: %s", name, res.StatusCode, body)
		}
	}
	editor.refuse(http.StatusNotFound, "not_found", "PUT", path+"O0999", map[string]any{"primary": true})
	// A picture of another person is not a picture of this person.
	editor.refuse(http.StatusNotFound, "not_found", "PUT", path+other, map[string]any{"primary": true})
	member.refuse(http.StatusForbidden, "forbidden", "PUT", path+second, map[string]any{"primary": true})

	// The undo of the choice brings the old portrait back.
	editor.expect(http.StatusOK, "PUT", path+second, map[string]any{"primary": true})
	if detail := person(); detail.Photo.ID != second {
		t.Fatalf("after the second choice: %+v", detail.Photo)
	}
	entries := editor.history("?person=" + anna)
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(entries[0].ID)+"/undo", nil)
	if detail := person(); detail.Photo.ID != first {
		t.Fatalf("after the undo: %+v", detail.Photo)
	}
	after := filesIn(t, uploads)
	if len(after) != len(before) {
		t.Fatalf("files: %d before, %d after", len(before), len(after))
	}
	for i := range after {
		if after[i].Name() != before[i].Name() || after[i].Size() != before[i].Size() {
			t.Fatalf("file %s changed", before[i].Name())
		}
	}
}

// An account can name the person in the tree who owns it.
func TestAccountLinksToAPerson(t *testing.T) {
	h, _ := editHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)
	anna := editor.create("F", "Anna", "Orlova", nil)
	boris := editor.create("M", "Boris", "Orlov", nil)
	me := func(c *client) string {
		t.Helper()
		return decode[accountDTO](t, c.expect(http.StatusOK, "GET", "/api/v1/me", nil)).User.Person
	}
	if got := me(member); got != "" {
		t.Fatalf("a new account has the person %q", got)
	}
	// Each user sets the own link.
	account := decode[accountDTO](t, member.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"person": anna}))
	if account.User.Person != anna || me(member) != anna {
		t.Fatalf("after the link: %q, %q", account.User.Person, me(member))
	}
	member.refuse(http.StatusBadRequest, "bad_person", "PATCH", "/api/v1/me", map[string]string{"person": "I0999"})
	if me(member) != anna {
		t.Fatal("a refused request changed the link")
	}
	// Another part of the profile keeps the link.
	member.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"displayName": "Anna O."})
	if me(member) != anna {
		t.Fatal("a new name removed the link")
	}
	// A guest sees no living person, so a guest cannot name one.
	guest.refuse(http.StatusBadRequest, "bad_person", "PATCH", "/api/v1/me", map[string]string{"person": boris})
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+boris+"/events", map[string]any{"event": map[string]any{"type": "Death", "date": map[string]any{"dateval": map[string]string{"val": "1950"}}}})
	guest.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"person": boris})
	if me(guest) != boris {
		t.Fatalf("the guest has the person %q", me(guest))
	}

	// The list of accounts shows the link.
	linked := false
	for _, person := range editor.people() {
		linked = linked || person.Person == anna
	}
	if !linked {
		t.Fatal("the list of accounts has no link")
	}

	// An invitation can carry the link.
	data := editor.expect(http.StatusCreated, "POST", "/api/v1/users",
		map[string]string{"email": "new@example.org", "displayName": "Anna", "role": "member", "person": anna})
	invited := decode[personDTO](t, data)
	if invited.Person != anna || h.userByEmail("new@example.org").Person != anna {
		t.Fatalf("the invitation: %+v", invited)
	}
	editor.refuse(http.StatusBadRequest, "bad_person", "POST", "/api/v1/users",
		map[string]string{"email": "other@example.org", "displayName": "Other", "role": "member", "person": "I0999"})
	if _, err := h.app.store.UserByEmail(context.Background(), "other@example.org"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused invitation made an account: %v", err)
	}

	// An administrator changes and removes the link of another account.
	path := "/api/v1/users/" + itoa(invited.ID)
	if got := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]string{"person": boris})); got.Person != boris {
		t.Fatalf("after the change: %+v", got)
	}
	admin.refuse(http.StatusBadRequest, "bad_person", "PATCH", path, map[string]string{"person": "I0999"})
	if got := decode[personDTO](t, admin.expect(http.StatusOK, "PATCH", path, map[string]string{"person": ""})); got.Person != "" {
		t.Fatalf("after the removal: %+v", got)
	}
	editor.refuse(http.StatusForbidden, "forbidden", "PATCH", path, map[string]string{"person": anna})

	// The link goes away with the person, and comes back with an undo.
	removed := decode[store.EditResult](t, editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/people/"+anna, nil))
	if got := me(member); got != "" {
		t.Fatalf("the link to a removed person: %q", got)
	}
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(removed.ChangeID)+"/undo", nil)
	if got := me(member); got != anna {
		t.Fatalf("the link after the undo: %q", got)
	}
	member.expect(http.StatusOK, "PATCH", "/api/v1/me", map[string]string{"person": ""})
	if got := me(member); got != "" {
		t.Fatalf("the link after the removal: %q", got)
	}
	if text := h.auditText(); !strings.Contains(text, "user.person") {
		t.Fatalf("the audit log has no line for the link: %s", text)
	}
}
