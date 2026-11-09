package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/faces"
	"github.com/Renset/heirfold/internal/store"
)

type galleryAnswer struct {
	Photos []store.GalleryPhoto `json:"photos"`
	Files  bool                 `json:"files"`
}

func eventBody(kind, year string) map[string]any {
	return map[string]any{"event": map[string]any{"type": kind, "date": map[string]any{"dateval": map[string]string{"val": year}}}}
}

// The gallery is for family members and above. A guest gets no list, and no
// file of a photo until an editor opens the photo and nobody on it is living.
func TestGalleryAndGuests(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)

	dead := editor.create("M", "Ivan", "Orlov", nil)
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+dead+"/events", eventBody("Birth", "1850"))
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+dead+"/events", eventBody("Death", "1910"))
	living := editor.create("F", "Anna", "Orlova", nil)

	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 80, 60))).Created
	if photo == "" {
		t.Fatal("no ID for the new photo")
	}
	image := "/api/v1/media/" + photo + "/image?size=s"
	file := "/api/v1/media/" + photo + "/file"
	guestSees := func(want bool, when string) {
		t.Helper()
		status := http.StatusNotFound
		if want {
			status = http.StatusOK
		}
		for _, path := range []string{image, file} {
			if res, body := guest.do("GET", path, nil, nil); res.StatusCode != status {
				t.Fatalf("%s: guest, %s: status %d, want %d: %.80s", when, path, res.StatusCode, status, body)
			}
		}
		card := decode[store.PersonDetail](t, guest.expect(http.StatusOK, "GET", "/api/v1/people/"+dead, nil))
		if got := len(card.Media) == 1; got != want {
			t.Fatalf("%s: the card of the dead person shows the photo to a guest: %v, want %v", when, got, want)
		}
		// A mark is no portrait, for nobody.
		if card.Photo != nil {
			t.Fatalf("%s: the portrait for a guest: %+v", when, card.Photo)
		}
	}

	// The roles.
	guest.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/gallery", nil)
	h.client().refuse(http.StatusUnauthorized, "unauthorized", "GET", "/api/v1/gallery", nil)
	for _, c := range []*client{member, editor} {
		got := decode[galleryAnswer](t, c.expect(http.StatusOK, "GET", "/api/v1/gallery", nil))
		if len(got.Photos) != 1 || got.Photos[0].ID != photo || !got.Photos[0].Hidden || !got.Photos[0].Uploaded || !got.Files {
			t.Fatalf("gallery: %+v", got)
		}
		c.expect(http.StatusOK, "GET", image, nil)
	}
	for client, want := range map[*client]bool{guest: false, member: true, editor: true} {
		if me := decode[accountDTO](t, client.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.Gallery != want {
			t.Fatalf("the gallery right of %s: %v", me.User.Role, me.Gallery)
		}
	}
	guestSees(false, "a new photo")

	// A mark needs a part of the picture.
	face := map[string]any{"region": map[string]int{"x1": 10, "y1": 10, "x2": 40, "y2": 55}}
	editor.refuse(http.StatusBadRequest, "invalid", "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+dead, map[string]any{})
	editor.refuse(http.StatusBadRequest, "invalid", "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+dead,
		map[string]any{"region": map[string]int{"x1": 50, "y1": 10, "x2": 40, "y2": 55}})
	editor.refuse(http.StatusNotFound, "not_found", "PUT", "/api/v1/edit/gallery/"+photo+"/faces/I9999", face)
	editor.refuse(http.StatusNotFound, "not_found", "PUT", "/api/v1/edit/gallery/O9999/faces/"+dead, face)

	// A dead person is marked. The photo stays hidden: other people can be on it.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+dead, face)
	guestSees(false, "a hidden photo with a dead person")
	got := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil))
	if len(got.Photos[0].Faces) != 1 || got.Photos[0].Faces[0].Person.ID != dead || got.Photos[0].Faces[0].Region == nil {
		t.Fatalf("faces: %+v", got.Photos[0].Faces)
	}
	// The photo is in the card of the person, cut to the face. It is no portrait.
	card := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+dead, nil))
	if len(card.Media) != 1 || card.Media[0].ID != photo || card.Media[0].Region == nil || card.Photo != nil {
		t.Fatalf("card for a member: %+v", card)
	}

	// An editor opens the photo for guests.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo, map[string]any{"description": "At the lake", "hidden": false})
	guestSees(true, "an open photo with a dead person")

	// A living person is marked. The photo closes for guests.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+living,
		map[string]any{"region": map[string]int{"x1": 50, "y1": 10, "x2": 80, "y2": 55}})
	guestSees(false, "a photo with a living person")
	editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/gallery/"+photo+"/faces/"+living, nil)
	guestSees(true, "the mark of the living person is off")

	// The photo leaves the gallery. The journal can bring it back.
	removed := decode[store.EditResult](t, editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/gallery/"+photo, nil))
	if got := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)); len(got.Photos) != 0 {
		t.Fatalf("gallery after the removal: %+v", got)
	}
	guestSees(false, "a removed photo")
	editor.expect(http.StatusOK, "POST", "/api/v1/history/"+itoa(removed.ChangeID)+"/undo", nil)
	if got := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)); len(got.Photos) != 1 || len(got.Photos[0].Faces) != 1 {
		t.Fatalf("gallery after the undo: %+v", got)
	}
}

// Each person chooses the own portrait. An editor chooses for everybody, so a
// dead relative gets a portrait too. A mark alone gives none.
func TestPortraitIsAChoice(t *testing.T) {
	h, _ := editHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	other := h.user("other@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)

	anna := editor.create("F", "Anna", "Orlova", nil)
	boris := editor.create("M", "Boris", "Orlov", nil)
	dead := editor.create("M", "Ivan", "Orlov", nil)
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+dead+"/events", eventBody("Death", "1910"))
	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 80, 60))).Created
	face := map[string]any{"region": map[string]int{"x1": 10, "y1": 10, "x2": 40, "y2": 55}}
	for _, person := range []string{anna, boris, dead} {
		editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+person, face)
	}
	portrait := func(c *client, person string) string {
		t.Helper()
		card := decode[store.PersonDetail](t, c.expect(http.StatusOK, "GET", "/api/v1/people/"+person, nil))
		if card.Photo == nil {
			return ""
		}
		return card.Photo.ID
	}
	for _, person := range []string{anna, boris, dead} {
		if got := portrait(member, person); got != "" {
			t.Fatalf("a mark became the portrait of %s: %s", person, got)
		}
	}
	choose := map[string]any{"region": map[string]int{"x1": 5, "y1": 5, "x2": 45, "y2": 58}, "primary": true}
	own := "/api/v1/edit/people/" + anna

	// An account without a person in the tree chooses nothing.
	member.refuse(http.StatusForbidden, "forbidden", "PUT", own+"/media/"+photo, choose)
	member.refuse(http.StatusForbidden, "forbidden", "DELETE", own+"/portrait", nil)
	account := h.userByEmail("member@example.org")
	admin.expect(http.StatusOK, "PATCH", "/api/v1/users/"+itoa(account.ID), map[string]any{"person": anna})

	// The person of the account chooses the own portrait among the own pictures.
	member.expect(http.StatusOK, "PUT", own+"/media/"+photo, choose)
	if got := portrait(member, anna); got != photo {
		t.Fatalf("own portrait: %q", got)
	}
	// The portrait leaves, the picture stays in the card.
	member.expect(http.StatusOK, "DELETE", own+"/portrait", nil)
	card := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/"+anna, nil))
	if card.Photo != nil || len(card.Media) != 1 {
		t.Fatalf("after the portrait left: %+v %+v", card.Photo, card.Media)
	}
	// An own new picture is the portrait.
	added := decode[store.EditResult](t, member.expect(http.StatusOK, "POST", own+"/photo", testPNG(t, 64, 64))).Created
	if got := portrait(member, anna); added == "" || got != added {
		t.Fatalf("own new picture: %q, portrait %q", added, got)
	}
	// The gallery shows it among the photos, next to the photo that came through the gallery.
	if list := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)).Photos; len(list) != 2 {
		t.Fatalf("gallery with an own picture: %+v", list)
	}
	member.expect(http.StatusOK, "DELETE", own+"/media/"+added, nil)
	if got := portrait(member, anna); got != "" {
		t.Fatalf("after the own picture left: %q", got)
	}

	// Nothing else is open: another person, the rest of the own page, the gallery.
	member.refuse(http.StatusForbidden, "forbidden", "PUT", "/api/v1/edit/people/"+boris+"/media/"+photo, choose)
	member.refuse(http.StatusForbidden, "forbidden", "DELETE", "/api/v1/edit/people/"+dead+"/portrait", nil)
	member.refuse(http.StatusForbidden, "forbidden", "POST", "/api/v1/edit/people/"+boris+"/photo", testPNG(t, 64, 64))
	member.refuse(http.StatusForbidden, "forbidden", "GET", own, nil)
	member.refuse(http.StatusForbidden, "forbidden", "POST", own+"/events", eventBody("Birth", "1990"))
	member.refuse(http.StatusForbidden, "forbidden", "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+anna, face)
	member.refuse(http.StatusForbidden, "forbidden", "DELETE", "/api/v1/edit/gallery/"+photo, nil)
	other.refuse(http.StatusForbidden, "forbidden", "PUT", own+"/media/"+photo, choose)
	guest.refuse(http.StatusForbidden, "forbidden", "PUT", own+"/media/"+photo, choose)
	if got := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)); len(got.Photos) != 1 || len(got.Photos[0].Faces) != 3 {
		t.Fatalf("the gallery after the refused edits: %+v", got.Photos)
	}

	// An editor chooses for a dead relative, and takes the choice back.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/people/"+dead+"/media/"+photo, choose)
	if got := portrait(member, dead); got != photo {
		t.Fatalf("portrait of the dead person: %q", got)
	}
	editor.expect(http.StatusOK, "DELETE", "/api/v1/edit/people/"+dead+"/portrait", nil)
	if got := portrait(member, dead); got != "" {
		t.Fatalf("portrait of the dead person after it left: %q", got)
	}
}

// The server reads the time, the place and the camera from an uploaded file
// and says who added the photo.
func TestGalleryShowsWhatTheFileSays(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	file, err := os.ReadFile(filepath.Join("..", "imaging", "testdata", "exif-camera.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", file)).Created
	plain := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 80, 60))).Created
	find := func(id string) store.GalleryPhoto {
		t.Helper()
		for _, item := range decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)).Photos {
			if item.ID == id {
				return item
			}
		}
		t.Fatalf("no photo %s in the gallery", id)
		return store.GalleryPhoto{}
	}
	got := find(photo)
	// The place of this picture is at sea: a point without a settlement.
	if got.Taken != "2019-07-14T15:42:10" || got.Year != 2019 ||
		got.Place == nil || *got.Place != (store.PhotoPlace{Lat: -12.51, Lon: -45.25}) {
		t.Fatalf("what the file says: %+v %+v", got, got.Place)
	}
	// A place in a city gets the name of the city. The point stays the point of the file.
	inTown, err := os.ReadFile(filepath.Join("..", "imaging", "testdata", "exif-town.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	town := find(decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", inTown)).Created)
	if town.Place == nil || town.Place.Name != "Paris" || town.Place.NameRu != "Париж" || town.Place.Country != "FR" ||
		town.Place.Lat != 48.8583 || town.Place.Lon != 2.2944 || town.Year != 2022 {
		t.Fatalf("a picture from a city: %+v %+v", town, town.Place)
	}
	if got.AddedBy == "" || got.AddedAt == 0 {
		t.Fatalf("who added the photo: %+v", got)
	}
	if other := find(plain); other.Taken != "" || other.Year != 0 || other.Place != nil || other.AddedBy == "" {
		t.Fatalf("a picture without marks: %+v", other)
	}
	// An editor writes the date that the family knows.
	body := map[string]any{"description": "At the lake", "hidden": true, "date": map[string]any{"dateval": map[string]string{"val": "1975-06"}}}
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo, body)
	if got = find(photo); got.Year != 1975 || got.Dated == nil || got.Taken == "" {
		t.Fatalf("after the date of the family: %+v", got)
	}
	body["date"] = map[string]any{"dateval": map[string]string{"val": "not a date"}}
	editor.refuse(http.StatusBadRequest, "invalid", "PUT", "/api/v1/edit/gallery/"+photo, body)
}

// An editor looks for a place by the start of its name and sets it for a photo.
func TestEditorSetsThePlaceOfAPhoto(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 80, 60))).Created
	type answer struct {
		Places []foundPlace `json:"places"`
	}
	search := func(text string) []foundPlace {
		t.Helper()
		return decode[answer](t, editor.expect(http.StatusOK, "GET", "/api/v1/places/search?q="+url.QueryEscape(text), nil)).Places
	}
	found := search("Каза")
	if len(found) == 0 || len(found) > maxFoundPlaces || found[0].Name != "Kazan" || found[0].NameRu != "Казань" || found[0].Country != "RU" || found[0].Tree {
		t.Fatalf("search by a Russian name: %+v", found)
	}
	if got := search("pari"); len(got) == 0 || got[0].Name != "Paris" {
		t.Fatalf("search by an international name: %+v", got)
	}
	for _, text := range []string{"", "к", "qqqqzzzz"} {
		if got := search(text); got == nil || len(got) != 0 {
			t.Fatalf("search for %q: %+v", text, got)
		}
	}
	member.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/places/search?q=pari", nil)
	h.client().refuse(http.StatusUnauthorized, "unauthorized", "GET", "/api/v1/places/search?q=pari", nil)

	// The map asks for the towns of the part of the world that it shows.
	around := decode[answer](t, member.expect(http.StatusOK, "GET", "/api/v1/places/around?south=54.2&west=55.4&north=55.2&east=56.6", nil)).Places
	if len(around) < 5 || len(around) > maxMapPlaces || around[0].Name != "Ufa" || around[0].People < 1000000 {
		t.Fatalf("towns around a city: %d, first %+v", len(around), around[:min(len(around), 1)])
	}
	for _, query := range []string{"", "south=54&west=55&north=53&east=56", "south=0&west=0&north=60&east=10", "south=a&west=1&north=2&east=3"} {
		member.refuse(http.StatusBadRequest, "invalid", "GET", "/api/v1/places/around?"+query, nil)
	}
	h.client().refuse(http.StatusUnauthorized, "unauthorized", "GET", "/api/v1/places/around?south=54.2&west=55.4&north=55.2&east=56.6", nil)
	// A press on the map gives the settlement of the point, or none.
	type nearest struct {
		Place *foundPlace `json:"place"`
	}
	if got := decode[nearest](t, editor.expect(http.StatusOK, "GET", "/api/v1/places/nearest?lat=48.8584&lon=2.2945", nil)).Place; got == nil || got.Name != "Paris" {
		t.Fatalf("the settlement of a point: %+v", got)
	}
	if got := decode[nearest](t, editor.expect(http.StatusOK, "GET", "/api/v1/places/nearest?lat=-12.51&lon=-45.25", nil)).Place; got != nil {
		t.Fatalf("the settlement of a point at sea: %+v", got)
	}
	editor.refuse(http.StatusBadRequest, "invalid", "GET", "/api/v1/places/nearest?lat=95&lon=0", nil)
	member.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/places/nearest?lat=48.8584&lon=2.2945", nil)

	town := found[0]
	body := map[string]any{"description": "", "hidden": true, "place": map[string]any{
		"lat": town.Lat, "lon": town.Lon, "name": town.Name, "nameRu": town.NameRu, "country": town.Country}}
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo, body)
	list := decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)).Photos
	if len(list) != 1 || list[0].Place == nil || list[0].Place.Name != "Kazan" || list[0].Place.NameRu != "Казань" || !list[0].Place.Manual {
		t.Fatalf("the place of the photo: %+v", list)
	}
	body["place"] = map[string]any{"remove": true}
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo, body)
	if list = decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)).Photos; list[0].Place != nil {
		t.Fatalf("after the place left: %+v", list[0].Place)
	}
	// A point without a settlement is a place too.
	body["place"] = map[string]any{"lat": -12.51, "lon": -45.25}
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo, body)
	if list = decode[galleryAnswer](t, member.expect(http.StatusOK, "GET", "/api/v1/gallery", nil)).Photos; list[0].Place == nil || list[0].Place.Name != "" || list[0].Place.Lat != -12.51 || !list[0].Place.Manual {
		t.Fatalf("a point without a settlement: %+v", list[0].Place)
	}
	body["place"] = map[string]any{"lat": 95, "lon": 0, "name": "Nowhere"}
	editor.refuse(http.StatusBadRequest, "invalid", "PUT", "/api/v1/edit/gallery/"+photo, body)
}

func TestGalleryUploadChecksTheFile(t *testing.T) {
	h, uploads := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	editor.refuse(http.StatusUnprocessableEntity, "bad_image", "POST", "/api/v1/edit/gallery", []byte("<svg onload=alert(1)>"))
	editor.refuse(http.StatusUnprocessableEntity, "bad_image", "POST", "/api/v1/edit/gallery", testGIF(t))
	if len(filesIn(t, uploads)) != 0 || h.journalSize() != 0 {
		t.Fatal("a refused upload left a file or a journal entry")
	}
	// The same picture twice is one photo and one file.
	first := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 30, 30))).Created
	second := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", testPNG(t, 30, 30))).Created
	if first == "" || first != second || len(filesIn(t, uploads)) != 1 {
		t.Fatalf("photos %q and %q, files %v", first, second, filesIn(t, uploads))
	}
}

func drawnPicture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "faces", "testdata", "drawn.png"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// shifted returns the drawn picture with a band of another colour at the
// top, so it is another file with the same faces at other places.
func shifted(t *testing.T) []byte {
	t.Helper()
	src, err := png.Decode(bytes.NewReader(drawnPicture(t)))
	if err != nil {
		t.Fatal(err)
	}
	bounds := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()+64))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{R: 90, G: 120, B: 150, A: 255}), image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 64, bounds.Dx(), bounds.Dy()+64), src, bounds.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type foundAnswer struct {
	Scanned     bool                  `json:"scanned"`
	Faces       []store.SuggestedFace `json:"faces"`
	On          bool                  `json:"on"`
	Recognition bool                  `json:"recognition"`
}

// The server finds the faces of a new photo. An editor marks them or
// dismisses them. Only an editor sees what the server found.
func TestServerFindsFaces(t *testing.T) {
	h, _ := editHarness(t, nil)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)
	anna := editor.create("F", "Anna", "Orlova", nil)

	photo := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", drawnPicture(t))).Created
	found := "/api/v1/gallery/" + photo + "/found"
	for _, c := range []*client{member, guest} {
		c.refuse(http.StatusForbidden, "forbidden", "GET", found, nil)
		c.refuse(http.StatusForbidden, "forbidden", "DELETE", found+"/1", nil)
	}
	h.client().refuse(http.StatusUnauthorized, "unauthorized", "GET", found, nil)
	before := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", found, nil))
	if before.Scanned || !before.On || len(before.Faces) != 0 {
		t.Fatalf("before the search: %+v", before)
	}

	if n, err := h.app.searchFaces(context.Background(), 0); err != nil || n != 1 {
		t.Fatalf("search: %d pictures, %v", n, err)
	}
	got := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", found, nil))
	if !got.Scanned || len(got.Faces) != 7 || got.Recognition {
		t.Fatalf("after the search: scanned %v, %d faces, recognition %v", got.Scanned, len(got.Faces), got.Recognition)
	}
	for _, face := range got.Faces {
		r := face.Region
		// The faces stand in a row in the upper half of the picture of 640 by 320 pixels.
		if r.X1 < 0 || r.X2 > 100 || r.Y1 < 0 || r.Y2 > 75 || r.X2-r.X1 < 4 || r.X2-r.X1 > 14 || (r.Y2-r.Y1) != 2*(r.X2-r.X1) && abs((r.Y2-r.Y1)-2*(r.X2-r.X1)) > 2 {
			t.Errorf("region %+v", r)
		}
	}

	// A mark with the square of the server closes that face.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+photo+"/faces/"+anna, map[string]any{"region": got.Faces[0].Region})
	left := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", found, nil))
	if len(left.Faces) != 6 || left.Faces[0].ID == got.Faces[0].ID {
		t.Fatalf("after the mark: %d faces", len(left.Faces))
	}
	// A dismissed face goes away.
	editor.expect(http.StatusOK, "DELETE", found+"/"+itoa(left.Faces[0].ID), nil)
	editor.refuse(http.StatusNotFound, "not_found", "DELETE", found+"/999999", nil)
	editor.refuse(http.StatusNotFound, "not_found", "DELETE", found+"/abc", nil)
	if after := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", found, nil)); len(after.Faces) != 5 {
		t.Fatalf("after the dismissal: %d faces", len(after.Faces))
	}
	// The second look finds no new work.
	if n, err := h.app.searchFaces(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("second search: %d pictures, %v", n, err)
	}
	editor.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/gallery/O9999/found", nil)

	// The page asks how the camera held a picture.
	info := decode[map[string]int](t, member.expect(http.StatusOK, "GET", "/api/v1/media/"+photo+"/info", nil))
	if info["orientation"] != 1 {
		t.Fatalf("orientation: %v", info)
	}
	guest.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/media/"+photo+"/info", nil)
}

// With the recognition model the server says who a face is like.
func TestServerSuggestsPeople(t *testing.T) {
	model, err := filepath.Abs(filepath.Join("..", "faces", "models", faces.RecognizerFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(model); err != nil {
		t.Skip("the recognition model is not here: it is large and is not in the repository")
	}
	h, _ := editHarness(t, nil)
	if err := os.MkdirAll(filepath.Join(h.app.cfg.DataDir, "models"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(model, filepath.Join(h.app.cfg.DataDir, "models", faces.RecognizerFile)); err != nil {
		t.Fatal(err)
	}
	editor := h.user("editor@example.org", auth.RoleEditor)
	anna := editor.create("F", "Anna", "Orlova", nil)
	boris := editor.create("M", "Boris", "Orlov", nil)

	first := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", drawnPicture(t))).Created
	second := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/gallery", shifted(t))).Created
	if n, err := h.app.searchFaces(context.Background(), 0); err != nil || n != 2 {
		t.Fatalf("search: %d pictures, %v", n, err)
	}
	known := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", "/api/v1/gallery/"+first+"/found", nil))
	if !known.Recognition || len(known.Faces) != 7 {
		t.Fatalf("first photo: recognition %v, %d faces", known.Recognition, len(known.Faces))
	}
	// Nobody is marked yet, so no face has a suggestion.
	for _, face := range known.Faces {
		if len(face.People) != 0 {
			t.Fatalf("a suggestion without any mark: %+v", face)
		}
	}
	// Anna is the first face from the left and Boris is the last.
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+first+"/faces/"+anna, map[string]any{"region": known.Faces[0].Region})
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/gallery/"+first+"/faces/"+boris, map[string]any{"region": known.Faces[6].Region})

	other := decode[foundAnswer](t, editor.expect(http.StatusOK, "GET", "/api/v1/gallery/"+second+"/found", nil))
	if len(other.Faces) != 7 {
		t.Fatalf("second photo: %d faces", len(other.Faces))
	}
	// The same faces are on the second photo. The first one must name Anna and the last one Boris.
	if people := other.Faces[0].People; len(people) == 0 || people[0].ID != anna {
		t.Fatalf("the first face of the second photo: %+v", people)
	}
	if people := other.Faces[6].People; len(people) == 0 || people[0].ID != boris {
		t.Fatalf("the last face of the second photo: %+v", people)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// A hidden note is for the people who can change the whole tree: an editor
// and an administrator. A family member and a guest get no word of it: not
// on the page, not in the edit form of the own branch, and not in the file
// of the tree.
func TestHiddenNoteIsForEditorsOnly(t *testing.T) {
	h, _ := editHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	editor := h.user("editor@example.org", auth.RoleEditor)
	member := h.user("member@example.org", auth.RoleMember)
	relative := h.user("relative@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)

	person := editor.create("M", "Ivan", "Orlov", nil)
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+person+"/events", eventBody("Death", "1910"))
	const open, secret = "She kept bees.", "Nobody checked this story about the lost ring."
	editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+person+"/notes", map[string]any{"rev": 0, "text": open})
	hidden := decode[store.EditResult](t, editor.expect(http.StatusOK, "POST", "/api/v1/edit/people/"+person+"/notes",
		map[string]any{"rev": 0, "text": secret, "hidden": true})).Created

	notes := func(c *client) (texts []string, hiddenCount int) {
		t.Helper()
		card := decode[store.PersonDetail](t, c.expect(http.StatusOK, "GET", "/api/v1/people/"+person, nil))
		for _, note := range card.Notes {
			texts = append(texts, note.Text)
			if note.Hidden {
				hiddenCount++
			}
		}
		return texts, hiddenCount
	}
	for name, c := range map[string]*client{"administrator": admin, "editor": editor} {
		if texts, count := notes(c); len(texts) != 2 || texts[1] != secret || count != 1 {
			t.Fatalf("%s: %v, hidden %d", name, texts, count)
		}
	}
	for name, c := range map[string]*client{"family member": member, "guest": guest} {
		if texts, count := notes(c); len(texts) != 1 || texts[0] != open || count != 0 {
			t.Fatalf("%s: %v, hidden %d", name, texts, count)
		}
	}

	// The file of the tree for a family member has no text of the hidden note.
	export := func(c *client) string {
		t.Helper()
		_, body := c.do("GET", "/api/v1/export.gramps", nil, nil)
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		text, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(text)
	}
	if file := export(member); !strings.Contains(file, open) || strings.Contains(file, secret) || strings.Contains(file, "lost ring") {
		t.Fatal("the file of a family member holds the hidden note, or loses the open one")
	}
	if file := export(editor); !strings.Contains(file, secret) {
		t.Fatal("the file of an editor has no hidden note")
	}
	guest.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/export.gramps", nil)

	// A family member with the right for the own branch edits this person and still reads no hidden note.
	account := h.userByEmail("relative@example.org")
	admin.expect(http.StatusOK, "PATCH", "/api/v1/users/"+itoa(account.ID), map[string]any{"person": person, "ownBranch": true})
	form := decode[store.EditPerson](t, relative.expect(http.StatusOK, "GET", "/api/v1/edit/people/"+person, nil))
	if len(form.Notes) != 1 || form.Notes[0].Text != open {
		t.Fatalf("the edit form of a family member: %+v", form.Notes)
	}
	// For a family member the hidden note does not exist, so no answer tells that it is there.
	relative.refuse(http.StatusNotFound, "not_found", "PUT", "/api/v1/edit/notes/"+hidden+"?person="+person, map[string]any{"rev": 0, "text": "changed"})
	relative.refuse(http.StatusNotFound, "not_found", "DELETE", "/api/v1/edit/people/"+person+"/notes/"+hidden, nil)
	relative.refuse(http.StatusForbidden, "outside_branch", "POST", "/api/v1/edit/people/"+person+"/notes", map[string]any{"rev": 0, "text": "mine", "hidden": true})
	relative.refuse(http.StatusForbidden, "outside_branch", "PUT", "/api/v1/edit/notes/"+form.Notes[0].ID+"?person="+person,
		map[string]any{"rev": form.Notes[0].Rev, "text": open, "hidden": true})
	if texts, _ := notes(relative); len(texts) != 1 {
		t.Fatalf("a family member with the own branch: %v", texts)
	}
	if texts, count := notes(editor); len(texts) != 2 || count != 1 {
		t.Fatalf("after the refused edits: %v, hidden %d", texts, count)
	}

	// A new text without a word about the mark keeps the note hidden. An editor opens it on purpose.
	full := decode[store.EditPerson](t, editor.expect(http.StatusOK, "GET", "/api/v1/edit/people/"+person, nil))
	if len(full.Notes) != 2 || !full.Notes[1].Hidden || full.Notes[0].Hidden {
		t.Fatalf("the edit form of an editor: %+v", full.Notes)
	}
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/notes/"+hidden+"?person="+person, map[string]any{"rev": full.Notes[1].Rev, "text": secret + " More."})
	if texts, _ := notes(member); len(texts) != 1 {
		t.Fatalf("after a new text: %v", texts)
	}
	full = decode[store.EditPerson](t, editor.expect(http.StatusOK, "GET", "/api/v1/edit/people/"+person, nil))
	editor.expect(http.StatusOK, "PUT", "/api/v1/edit/notes/"+hidden+"?person="+person, map[string]any{"rev": full.Notes[1].Rev, "text": secret, "hidden": false})
	if texts, _ := notes(member); len(texts) != 2 {
		t.Fatalf("after the editor opened the note: %v", texts)
	}
}
