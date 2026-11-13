package store

import (
	"context"
	"errors"
	"testing"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// portraitOf returns the ID of the portrait in the card of a person, or "".
func portraitOf(t *testing.T, st *Store, id string) string {
	t.Helper()
	detail, err := st.View(privacy.Full).Person(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Photo == nil {
		return ""
	}
	return detail.Photo.ID
}

func picturesOf(t *testing.T, st *Store, id string) int {
	t.Helper()
	detail, err := st.View(privacy.Full).Person(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return len(detail.Media)
}

func TestAMarkIsNoPortraitUntilSomebodyChoosesIt(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}

	// A tree from Gramps keeps the rule of Gramps: the first picture is the portrait.
	if got := portraitOf(t, st, "I0001"); got != "O0001" {
		t.Fatalf("portrait from the tree: %q", got)
	}
	// Olga has no picture. A mark gives her a picture and no portrait.
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	if got := portraitOf(t, st, "I0002"); got != "" || picturesOf(t, st, "I0002") != 1 {
		t.Fatalf("after a mark: portrait %q, pictures %d", got, picturesOf(t, st, "I0002"))
	}
	// A moved mark stays a mark.
	moved := model.Region{X1: 12, Y1: 22, X2: 32, Y2: 47}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", moved) })
	if got := portraitOf(t, st, "I0002"); got != "" {
		t.Fatalf("after a moved mark: portrait %q", got)
	}

	// Somebody chooses the marked face as the portrait.
	mustEdit(t, st, "media.update", func(e *Edit) error {
		return e.SetPhoto("I0002", photo, PhotoInput{Region: &moved, Primary: true})
	})
	detail, err := st.View(privacy.Full).Person(context.Background(), "I0002")
	if err != nil || detail.Photo == nil || detail.Photo.ID != photo || detail.Photo.Region == nil || *detail.Photo.Region != moved {
		t.Fatalf("chosen portrait: %v %+v", err, detail.Photo)
	}
	// The mark on the photo stays where it was.
	if got := galleryPhoto(t, st, privacy.Full, photo); len(got.Faces) != 1 || *got.Faces[0].Region != moved {
		t.Fatalf("mark after the choice: %+v", got.Faces)
	}
	// A new frame of the chosen portrait keeps it the portrait.
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	if got := portraitOf(t, st, "I0002"); got != photo {
		t.Fatalf("portrait after a new frame: %q", got)
	}
}

func TestClearPortraitKeepsThePictures(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/ivan.jpg", "image/jpeg", "") })
	added := exported(t, st).Media[1].ID
	// A picture that somebody adds for the person is the portrait: that is a choice.
	if got := portraitOf(t, st, "I0001"); got != added || picturesOf(t, st, "I0001") != 2 {
		t.Fatalf("after a new picture: portrait %q, pictures %d", got, picturesOf(t, st, "I0001"))
	}

	mustEdit(t, st, "portrait.clear", func(e *Edit) error { return e.ClearPortrait("I0001") })
	if got := portraitOf(t, st, "I0001"); got != "" || picturesOf(t, st, "I0001") != 2 {
		t.Fatalf("after the portrait left: portrait %q, pictures %d", got, picturesOf(t, st, "I0001"))
	}
	noChange(t, st, "a person without a portrait", func(e *Edit) error { return e.ClearPortrait("I0001") })
	noChange(t, st, "a person without pictures", func(e *Edit) error { return e.ClearPortrait("I0002") })
	refuse(t, st, ErrNotFound, "an unknown person", func(e *Edit) error { return e.ClearPortrait("I9999") })

	// Each picture can become the portrait again. The other stays out.
	mustEdit(t, st, "media.update", func(e *Edit) error { return e.SetPhoto("I0001", "O0001", PhotoInput{Primary: true}) })
	if got := portraitOf(t, st, "I0001"); got != "O0001" {
		t.Fatalf("portrait after a new choice: %q", got)
	}
	// When the portrait leaves the person, the next picture does not take its place.
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", "O0001") })
	if got := portraitOf(t, st, "I0001"); got != "" || picturesOf(t, st, "I0001") != 1 {
		t.Fatalf("after the portrait was removed: portrait %q, pictures %d", got, picturesOf(t, st, "I0001"))
	}
}

func TestARemovedPortraitDoesNotPassToTheNextPicture(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/ivan.jpg", "image/jpeg", "") })
	added := exported(t, st).Media[1].ID
	// The tree from Gramps has a second picture without any mark.
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0001", added) })
	if got := portraitOf(t, st, "I0001"); got != "" || picturesOf(t, st, "I0001") != 1 {
		t.Fatalf("portrait %q, pictures %d", got, picturesOf(t, st, "I0001"))
	}
}

func TestGalleryPhotoIsToldFromAPictureOfOnePerson(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	mustEdit(t, st, "media.add", func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/ivan.jpg", "image/jpeg", "") })
	var own string
	for _, item := range gallery(t, st, privacy.Full) {
		switch item.ID {
		case photo, "O0001":
		default:
			own = item.ID
			if !item.Uploaded {
				t.Errorf("picture of one person: %+v", item)
			}
		}

	}
	// The mark passes through an export, so Gramps keeps it.
	if tree := exported(t, st); !inGallery(mediaByID(tree, photo)) || inGallery(mediaByID(tree, own)) {
		t.Fatalf("marks in the export: %+v", tree.Media)
	}

	// The last link to a photo of the gallery leaves, and the photo stays.
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	mustEdit(t, st, "media.remove", func(e *Edit) error { return e.RemoveMedia("I0002", photo) })
	if got := galleryPhoto(t, st, privacy.Full, photo); got == nil || len(got.Faces) != 0 {
		t.Fatalf("photo after its last link left: %+v", got)
	}
	// A picture of one person that comes again through the gallery becomes a photo of the gallery.
	again := mustEdit(t, st, "gallery.add", func(e *Edit) error { return e.AddGalleryPhoto("heirfold/ivan.jpg", "image/jpeg", "") })
	if again.Created != own || !inGallery(mediaByID(exported(t, st), own)) {
		t.Fatalf("the same file through the gallery: %+v", again)
	}
}

// The gallery holds photos. The scan of a document is a picture that a source
// or a citation shows, and it stays out of the gallery.
func TestGalleryHoldsNoScansOfDocuments(t *testing.T) {
	tree := editTree()
	tree.Media = append(tree.Media,
		model.Media{Object: obj("_o2", "O0002", false), File: model.MediaFile{Src: "scans/page.jpg", Mime: "image/jpeg"}},
		model.Media{Object: obj("_o3", "O0003", false), File: model.MediaFile{Src: "scans/book.jpg", Mime: "image/jpeg"}},
		// Somebody added this photo in the gallery, and a citation shows it too.
		model.Media{Object: obj("_o4", "O0004", false), File: model.MediaFile{Src: "heirfold/group.jpg", Mime: "image/jpeg"},
			Attributes: []model.Attribute{{Type: attrGallery, Value: "yes"}}},
	)
	tree.Citations[0].MediaRefs = []model.MediaRef{{HLink: "_o2"}, {HLink: "_o4"}}
	tree.Sources[0].MediaRefs = []model.MediaRef{{HLink: "_o3"}}
	st, tick := editStore(t, tree)
	tick()
	added := addGalleryPhoto(t, st, "new.jpg")
	want := map[string]bool{"O0001": true, "O0004": true, added: true}
	photos := gallery(t, st, privacy.Full)
	if len(photos) != len(want) {
		t.Fatalf("gallery: %+v", photos)
	}
	for _, item := range photos {
		if !want[item.ID] {
			t.Errorf("%s is in the gallery", item.ID)
		}
	}
	// The scan stays a file of the tree: its source shows it.
	if _, err := st.View(privacy.Full).MediaFile(context.Background(), "O0002"); err != nil {
		t.Errorf("the file of a scan: %v", err)
	}
}

func TestFamilyMemberChangesOnlyTheOwnPictures(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	photo := addGalleryPhoto(t, st, "group.jpg")
	face := model.Region{X1: 10, Y1: 20, X2: 30, Y2: 45}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(photo, "I0002", face) })
	olga := Actor{ID: 7, Name: "Olga", Pictures: "I0002"}
	as := func(action string, fn func(*Edit) error) error {
		t.Helper()
		before := exported(t, st)
		_, err := st.Edit(context.Background(), olga, action, fn)
		if err != nil && len(before.People) != len(exported(t, st).People) {
			t.Fatalf("%s: a refused edit changed the store", action)
		}
		return err
	}

	if err := as("media.update", func(e *Edit) error {
		return e.SetPhoto("I0002", photo, PhotoInput{Region: &face, Primary: true})
	}); err != nil || portraitOf(t, st, "I0002") != photo {
		t.Fatalf("own portrait from a mark: %v, %q", err, portraitOf(t, st, "I0002"))
	}
	if err := as("portrait.clear", func(e *Edit) error { return e.ClearPortrait("I0002") }); err != nil || portraitOf(t, st, "I0002") != "" {
		t.Fatalf("own portrait off: %v", err)
	}
	if err := as("media.add", func(e *Edit) error { return e.AddPhoto("I0002", "heirfold/olga.jpg", "image/jpeg", "") }); err != nil {
		t.Fatalf("own new picture: %v", err)
	}
	own := portraitOf(t, st, "I0002")
	if own == "" || own == photo {
		t.Fatalf("portrait after the own picture: %q", own)
	}
	if err := as("media.remove", func(e *Edit) error { return e.RemoveMedia("I0002", own) }); err != nil || picturesOf(t, st, "I0002") != 1 {
		t.Fatalf("own picture removed: %v, pictures %d", err, picturesOf(t, st, "I0002"))
	}

	for name, fn := range map[string]func(*Edit) error{
		"a picture of another person":  func(e *Edit) error { return e.AddPhoto("I0001", "heirfold/x.jpg", "image/jpeg", "") },
		"the portrait of another":      func(e *Edit) error { return e.ClearPortrait("I0001") },
		"the own name":                 func(e *Edit) error { return updatePerson(e, "I0002", personInput("F", "Other", "Name", "")) },
		"a photo of the gallery":       func(e *Edit) error { return e.AddGalleryPhoto("heirfold/y.jpg", "image/jpeg", "") },
		"the title of a photo":         func(e *Edit) error { return e.UpdateGalleryPhoto(photo, GalleryInput{Description: "x"}) },
		"a photo out of the gallery":   func(e *Edit) error { return e.RemoveGalleryPhoto(photo) },
		"a mark of another person":     func(e *Edit) error { return e.MarkFace(photo, "I0001", face) },
		"a picture and something else": func(e *Edit) error { _ = e.ClearPortrait("I0002"); return e.MarkFace(photo, "I0001", face) },
	} {
		if err := as("test.refused", fn); !errors.Is(err, ErrOutsideBranch) {
			t.Errorf("%s: got %v, want %v", name, err, ErrOutsideBranch)
		}
	}
	if _, err := st.Edit(context.Background(), Actor{ID: 8, Pictures: "I9999"}, "media.add", func(e *Edit) error {
		return e.AddPhoto("I0002", "heirfold/z.jpg", "image/jpeg", "")
	}); !errors.Is(err, ErrOutsideBranch) {
		t.Errorf("an account of an unknown person: %v", err)
	}
}
