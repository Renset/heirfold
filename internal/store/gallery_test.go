package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

func addGalleryPhoto(t *testing.T, st *Store, name string) string {
	t.Helper()
	result := mustEdit(t, st, "gallery.add", func(e *Edit) error {
		return e.AddGalleryPhoto("heirfold/"+name, "image/jpeg", "0123")
	})
	if result.Created == "" {
		t.Fatalf("result of a new photo: %+v", result)
	}
	return result.Created
}

func gallery(t *testing.T, st *Store, scope privacy.Scope) []GalleryPhoto {
	t.Helper()
	photos, err := st.View(scope).Gallery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return photos
}

func galleryPhoto(t *testing.T, st *Store, scope privacy.Scope, id string) *GalleryPhoto {
	t.Helper()
	for _, photo := range gallery(t, st, scope) {
		if photo.ID == id {
			return &photo
		}
	}
	return nil
}

func TestGalleryPhotoStartsWithoutPeopleAndHiddenFromGuests(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	first := addGalleryPhoto(t, st, "a.jpg")
	tick()
	second := addGalleryPhoto(t, st, "b.jpg")

	photos := gallery(t, st, privacy.Full)
	// The newest photo is first. The old picture of the tree is in the gallery too.
	if len(photos) != 3 || photos[0].ID != second || photos[1].ID != first || photos[2].ID != "O0001" {
		t.Fatalf("gallery: %+v", photos)
	}
	if !photos[0].Hidden || !photos[0].Uploaded || photos[0].Faces == nil || len(photos[0].Faces) != 0 {
		t.Fatalf("new photo: %+v", photos[0])
	}
	if photos[2].Hidden || photos[2].Uploaded {
		t.Fatalf("old picture: %+v", photos[2])
	}
	// A guest sees no photo without a person, and no hidden photo. The old
	// picture of a dead person stays visible, as before.
	if public := gallery(t, st, privacy.Public); len(public) != 1 || public[0].ID != "O0001" {
		t.Fatalf("gallery of a guest: %+v", public)
	}
	if _, err := st.View(privacy.Public).MediaFile(context.Background(), first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("file for a guest: %v", err)
	}
	// The same file is one photo.
	again := mustEdit(t, st, "gallery.add", func(e *Edit) error { return e.AddGalleryPhoto("heirfold/a.jpg", "image/jpeg", "0123") })
	if again.Created != first || again.ChangeID != 0 || len(gallery(t, st, privacy.Full)) != 3 {
		t.Fatalf("the same file again: %+v", again)
	}
	for _, src := range []string{"photos/a.jpg", "heirfold/../a.jpg", "/etc/passwd", ""} {
		refuse(t, st, ErrInvalid, "source "+src, func(e *Edit) error { return e.AddGalleryPhoto(src, "image/jpeg", "") })
	}
}

func TestMarksOnFaces(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}

	// Ivan has a portrait already. The mark goes after it.
	mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/ivan.jpg", "image/jpeg", "") })
	result := mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0001", face) })
	if result.Subject != "I0001" || result.ChangeID == 0 {
		t.Fatalf("result: %+v", result)
	}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error {
		return e.MarkFace(photo, "I0002", model.Region{X1: 50, Y1: 20, X2: 70, Y2: 45})
	})
	tree := exported(t, st)
	ivan := tree.People[0]
	// The new portrait is first, the old picture of the tree is second, the mark is last.
	if len(ivan.MediaRefs) != 3 || ivan.MediaRefs[2].Region == nil || *ivan.MediaRefs[2].Region != face || ivan.MediaRefs[0].Region != nil {
		t.Fatalf("links of the person: %+v", ivan.MediaRefs)
	}
	got := galleryPhoto(t, st, privacy.Full, photo)
	if got == nil || len(got.Faces) != 2 || got.Faces[0].Person.ID != "I0001" || *got.Faces[0].Region != face || got.Faces[1].Person.ID != "I0002" {
		t.Fatalf("faces: %+v", got)
	}
	// The photo shows in the card of the person, cut to the face.
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0002")
	if err != nil || len(detail.Media) != 1 || detail.Media[0].ID != photo || detail.Media[0].Region == nil {
		t.Fatalf("card of the person: %v %+v", err, detail)
	}

	// A second mark of the same person moves the first.
	moved := model.Region{X1: 12, Y1: 22, X2: 32, Y2: 47}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0001", moved) })
	if got := galleryPhoto(t, st, privacy.Full, photo); len(got.Faces) != 2 || *got.Faces[0].Region != moved {
		t.Fatalf("moved mark: %+v", got.Faces)
	}

	refuse(t, st, ErrInvalid, "a mark outside the picture", func(e *Edit) error {
		return e.MarkFace(photo, "I0001", model.Region{X1: 90, Y1: 10, X2: 120, Y2: 40})
	})
	refuse(t, st, ErrInvalid, "an empty mark", func(e *Edit) error {
		return e.MarkFace(photo, "I0001", model.Region{X1: 30, Y1: 10, X2: 30, Y2: 40})
	})
	refuse(t, st, ErrNotFound, "an unknown person", func(e *Edit) error { return e.MarkFace(photo, "I9999", face) })
	refuse(t, st, ErrNotFound, "an unknown photo", func(e *Edit) error { return e.MarkFace("O9999", "I0001", face) })

	// A removed mark leaves the photo in the gallery.
	mustEdit(t, st, "gallery.unmark", func(e *Edit) error { return e.UnmarkFace(photo, "I0002") })
	if got := galleryPhoto(t, st, privacy.Full, photo); got == nil || len(got.Faces) != 1 {
		t.Fatalf("after a removed mark: %+v", got)
	}
	refuse(t, st, ErrNotFound, "a mark that is not there", func(e *Edit) error { return e.UnmarkFace(photo, "I0002") })
}

func TestGuestSeesAGalleryPhotoOnlyWhenAnEditorOpensItAndNobodyOnItIsLiving(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}
	guestSees := func() bool {
		_, err := st.View(privacy.Public).MediaFile(context.Background(), photo)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		return err == nil
	}
	guestCard := func(id string) int {
		detail, err := st.View(privacy.Public).Person(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return len(detail.Media)
	}

	// Only a dead person is marked. The photo stays hidden: other people can be on it.
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	if guestSees() || guestCard("I0002") != 0 {
		t.Fatal("a guest sees a hidden photo")
	}
	if member := galleryPhoto(t, st, privacy.Full, photo); member == nil || !member.Hidden {
		t.Fatalf("a member must see the hidden photo: %+v", member)
	}

	// An editor opens the photo for guests.
	mustEdit(t, st, "gallery.update", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Description: "  Wedding, 1875 ", Hidden: false})
	})
	if !guestSees() || guestCard("I0002") != 1 {
		t.Fatal("a guest must see an open photo of a dead person")
	}
	if got := galleryPhoto(t, st, privacy.Public, photo); got == nil || got.Description != "Wedding, 1875" || got.Hidden {
		t.Fatalf("open photo: %+v", got)
	}

	// A living person is marked. The photo is closed for guests again.
	living := addPerson(t, st, "F", "Anna", "Orlova")
	mustEdit(t, st, "gallery.mark", func(e *Edit) error {
		return e.MarkFace(photo, living, model.Region{X1: 50, Y1: 20, X2: 70, Y2: 45})
	})
	if guestSees() || guestCard("I0002") != 0 || galleryPhoto(t, st, privacy.Public, photo) != nil {
		t.Fatal("a guest sees a photo with a living person")
	}
	// The mark comes off. No living person leads to the photo, and it is open.
	mustEdit(t, st, "gallery.unmark", func(e *Edit) error { return e.UnmarkFace(photo, living) })
	if !guestSees() {
		t.Fatal("the photo must be open again")
	}
	refuse(t, st, ErrInvalid, "a title that is too long", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Description: strings.Repeat("a", 121)})
	})
}

func TestRemoveGalleryPhoto(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0001", face) })
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	before := exported(t, st)

	tick()
	removed := mustEdit(t, st, "gallery.remove", func(e *Edit) error { return e.RemoveGalleryPhoto(photo) })
	tree := exported(t, st)
	if len(tree.Media) != 1 || len(tree.People[0].MediaRefs) != 1 || len(tree.People[1].MediaRefs) != 0 {
		t.Fatalf("after the removal: %d media, %+v", len(tree.Media), tree.People[0].MediaRefs)
	}
	if galleryPhoto(t, st, privacy.Full, photo) != nil {
		t.Fatal("the photo is still in the gallery")
	}
	// An undo brings the photo and the marks back.
	tick()
	if _, err := st.UndoChange(context.Background(), elena, removed.ChangeID); err != nil {
		t.Fatal(err)
	}
	after := exported(t, st)
	if len(after.Media) != len(before.Media) || len(after.People[0].MediaRefs) != 2 || *after.People[0].MediaRefs[1].Region != face {
		t.Fatalf("after the undo: %+v", after.People[0].MediaRefs)
	}

	// A picture that came from Gramps stays.
	refuse(t, st, ErrInvalid, "a picture of the tree", func(e *Edit) error { return e.RemoveGalleryPhoto("O0001") })
	refuse(t, st, ErrNotFound, "an unknown photo", func(e *Edit) error { return e.RemoveGalleryPhoto("O9999") })
}

// The file of a picture says when and where the picture was made. An editor
// can write the date that the family knows, and that date wins.
func TestGalleryShowsWhatTheFileSaysAndWhoAddedThePhoto(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	ctx := context.Background()

	jobs, err := st.InfoJobs(ctx, 10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("pictures without a record: %v %+v", err, jobs)
	}
	for _, job := range jobs {
		info := MediaInfo{}
		if job.ID == photo {
			info = MediaInfo{Taken: "2019-07-14T15:42:10", HasPlace: true, Lat: 48.8584, Lon: 2.2945,
				Town: "Paris", TownRu: "Париж", Country: "FR"}
		}
		if err := st.SaveInfo(ctx, job, info); err != nil {
			t.Fatal(err)
		}
	}
	if jobs, err := st.InfoJobs(ctx, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("pictures without a record after the reading: %v %+v", err, jobs)
	}

	got := galleryPhoto(t, st, privacy.Full, photo)
	if got.Taken != "2019-07-14T15:42:10" || got.Year != 2019 ||
		got.Place == nil || *got.Place != (PhotoPlace{Lat: 48.8584, Lon: 2.2945, Name: "Paris", NameRu: "Париж", Country: "FR"}) {
		t.Fatalf("what the file says: %+v", got)
	}
	if got.AddedBy != elena.Name || got.AddedAt == 0 || got.Dated != nil {
		t.Fatalf("who added the photo: %+v", got)
	}
	// The picture from Gramps has no time in the file and nobody who added it.
	if old := galleryPhoto(t, st, privacy.Full, "O0001"); old.Taken != "" || old.Year != 0 || old.Place != nil || old.AddedBy != "" {
		t.Fatalf("picture of the tree: %+v", old)
	}

	// A place far from every settlement has no name. The point is rounded to about ten metres.
	if err := st.SaveInfo(ctx, jobs[0], MediaInfo{HasPlace: true, Lat: -12.5149267, Lon: -45.2551312}); err != nil {
		t.Fatal(err)
	}
	if far := galleryPhoto(t, st, privacy.Full, jobs[0].ID); far.Place == nil || *far.Place != (PhotoPlace{Lat: -12.5149, Lon: -45.2551}) {
		t.Fatalf("a place at sea: %+v", far.Place)
	}
	if err := st.SaveInfo(ctx, jobs[0], MediaInfo{}); err != nil {
		t.Fatal(err)
	}

	// The family knows that the photo is older than the scan.
	year := model.Dated{DateVal: &model.DateVal{Val: "1975", Type: "about"}}
	mustEdit(t, st, "gallery.update", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Description: "At the lake", Hidden: true, Date: &year})
	})
	got = galleryPhoto(t, st, privacy.Full, photo)
	if got.Year != 1975 || got.Taken != "2019-07-14T15:42:10" || got.Dated == nil || got.Dated.DateVal == nil || got.Dated.DateVal.Val != "1975" || got.Date == nil {
		t.Fatalf("after the date of the family: %+v", got)
	}
	// An edit of the title alone keeps the date. An empty date takes it away.
	mustEdit(t, st, "gallery.update", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Description: "By the lake", Hidden: true})
	})
	if got = galleryPhoto(t, st, privacy.Full, photo); got.Year != 1975 {
		t.Fatalf("after an edit of the title: %+v", got)
	}
	mustEdit(t, st, "gallery.update", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Description: "By the lake", Hidden: true, Date: &model.Dated{}})
	})
	if got = galleryPhoto(t, st, privacy.Full, photo); got.Year != 2019 || got.Dated != nil || got.Date != nil {
		t.Fatalf("after the date left: %+v", got)
	}
	refuse(t, st, ErrInvalid, "a date that is none", func(e *Edit) error {
		return e.UpdateGalleryPhoto(photo, GalleryInput{Date: &model.Dated{DateVal: &model.DateVal{Val: "1975-13-40"}}})
	})

	// A changed file is read again. The record of a removed photo leaves.
	mustEdit(t, st, "gallery.remove", func(e *Edit) error { return e.RemoveGalleryPhoto(photo) })
	if jobs, err := st.InfoJobs(ctx, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("after the photo left: %v %+v", err, jobs)
	}
	var left int
	if err := st.r.QueryRowContext(ctx, `SELECT count(*) FROM media_info`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("records after the photo left: %v %d", err, left)
	}
}

// An editor chooses the place of a photo. It wins over the place from the
// file, and it lives in the tree, so an export carries it.
func TestEditorChoosesThePlaceOfAPhoto(t *testing.T) {
	tree := editTree()
	tree.Places[0].Coord = &model.Coord{Lat: "54,25", Long: "56.5"}
	tree.Places = append(tree.Places,
		model.Place{Object: obj("_p2", "P0002", false), Type: "Village", Title: "No Point"},
		model.Place{Object: obj("_p3", "P0003", false), Type: "Village", Title: "Words", Coord: &model.Coord{Lat: "54°15'N", Long: "56°30'E"}},
	)
	st, tick := editStore(t, tree)
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	ctx := context.Background()
	set := func(in *PhotoPlaceInput) {
		t.Helper()
		mustEdit(t, st, "gallery.update", func(e *Edit) error {
			return e.UpdateGalleryPhoto(photo, GalleryInput{Hidden: true, Place: in})
		})
	}

	set(&PhotoPlaceInput{Lat: 54.74306, Lon: 55.96779, Name: "Ufa", NameRu: "Уфа", Country: "ru"})
	want := PhotoPlace{Lat: 54.7431, Lon: 55.9678, Name: "Ufa", NameRu: "Уфа", Country: "RU", Manual: true}
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || *got.Place != want {
		t.Fatalf("the chosen place: %+v", got.Place)
	}
	// The attribute is plain text that a person can read in Gramps.
	if item := mediaByID(exported(t, st), photo); len(item.Attributes) != 2 || item.Attributes[1].Type != attrPlace || item.Attributes[1].Value != "54.7431,55.9678|RU|Ufa|Уфа" {
		t.Fatalf("attributes: %+v", item.Attributes)
	}
	// The file says another place. The choice of the editor wins.
	jobs, err := st.InfoJobs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == photo {
			if err := st.SaveInfo(ctx, job, MediaInfo{HasPlace: true, Lat: 48.8584, Lon: 2.2945, Town: "Paris", TownRu: "Париж", Country: "FR"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || *got.Place != want {
		t.Fatalf("the chosen place with a place in the file: %+v", got.Place)
	}
	// An edit of the title keeps the place. A new choice replaces it.
	set(nil)
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || *got.Place != want {
		t.Fatalf("after an edit without a place: %+v", got.Place)
	}
	set(&PhotoPlaceInput{Lat: 54.25, Lon: 56.5, Name: "Old Town"})
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || *got.Place != (PhotoPlace{Lat: 54.25, Lon: 56.5, Name: "Old Town", Manual: true}) {
		t.Fatalf("a place of the tree: %+v", got.Place)
	}
	if item := mediaByID(exported(t, st), photo); len(item.Attributes) != 2 {
		t.Fatalf("attributes after a second choice: %+v", item.Attributes)
	}
	// A point far from every settlement has no name.
	set(&PhotoPlaceInput{Lat: -12.5149267, Lon: -45.2551312})
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || *got.Place != (PhotoPlace{Lat: -12.5149, Lon: -45.2551, Manual: true}) {
		t.Fatalf("a point without a name: %+v", got.Place)
	}
	// Without the choice, the place of the file shows again.
	set(&PhotoPlaceInput{Remove: true})
	if got := galleryPhoto(t, st, privacy.Full, photo); got.Place == nil || got.Place.Name != "Paris" || got.Place.Manual {
		t.Fatalf("after the choice left: %+v", got.Place)
	}

	for name, in := range map[string]PhotoPlaceInput{
		"a second name alone": {Lat: 1, Lon: 1, NameRu: "Имя"},
		"a bar in the name":   {Lat: 1, Lon: 1, Name: "A|B"},
		"too far north":       {Lat: 91, Lon: 1, Name: "X"},
		"too far east":        {Lat: 1, Lon: 181, Name: "X"},
		"a country of words":  {Lat: 1, Lon: 1, Name: "X", Country: "Russia"},
		"a country of signs":  {Lat: 1, Lon: 1, Name: "X", Country: "1|"},
	} {
		refuse(t, st, ErrInvalid, name, func(e *Edit) error {
			return e.UpdateGalleryPhoto(photo, GalleryInput{Hidden: true, Place: &in})
		})
	}

	// The places of the tree with a point as two numbers.
	var own []TreePlace
	if err := st.Inspect(ctx, func(e *Edit) error { own = e.PlacesWithPoints(); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(own) != 1 || own[0] != (TreePlace{Name: "Old Town", Lat: 54.25, Lon: 56.5}) {
		t.Fatalf("places of the tree: %+v", own)
	}
}
