package store

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/Renset/heirfold/internal/model"
)

// The portrait of a person is a choice of a human. In Gramps the first
// picture of a person is the portrait, and Heirfold reads a tree from Gramps
// in that way. But Heirfold itself never makes a picture the portrait as a
// side effect: a mark on a photo of the gallery gives no portrait, and a
// removed portrait does not pass to the next picture.
//
// Gramps has no field for "this picture is not a portrait" and none for
// "this file came through the gallery". Heirfold keeps each fact in an
// attribute. Gramps keeps attributes that it does not know, so both facts
// pass through a Gramps XML file and through a Gramps database.
const (
	// attrPortrait stands on a link from a person to a picture. The value
	// "no" means that the picture is not the portrait until somebody chooses it.
	attrPortrait = "Heirfold portrait"
	// attrGallery stands on a media object. The value "yes" means that the
	// file came through the photo gallery.
	attrGallery = "Heirfold gallery"
	// attrPlace stands on a media object. It holds the place of the picture
	// that an editor chose: see encodePlace.
	attrPlace = "Heirfold place"
)

// notPortrait reports whether the link says that the picture is no portrait.
func notPortrait(ref *model.MediaRef) bool {
	return slices.ContainsFunc(ref.Attributes, func(a model.Attribute) bool {
		return a.Type == attrPortrait && a.Value == "no"
	})
}

// setNotPortrait changes the mark on a link. It reports whether the link changed.
func setNotPortrait(ref *model.MediaRef, on bool) bool {
	if notPortrait(ref) == on {
		return false
	}
	ref.Attributes = slices.DeleteFunc(ref.Attributes, func(a model.Attribute) bool { return a.Type == attrPortrait })
	if on {
		ref.Attributes = append(ref.Attributes, model.Attribute{Type: attrPortrait, Value: "no"})
	}
	if len(ref.Attributes) == 0 {
		ref.Attributes = nil
	}
	return true
}

// inGallery reports whether the file came through the photo gallery.
func inGallery(item *model.Media) bool {
	return slices.ContainsFunc(item.Attributes, func(a model.Attribute) bool {
		return a.Type == attrGallery && a.Value == "yes"
	})
}

// setInGallery marks a file as a photo of the gallery. It reports whether the file changed.
func setInGallery(item *model.Media) bool {
	if inGallery(item) {
		return false
	}
	item.Attributes = append(item.Attributes, model.Attribute{Type: attrGallery, Value: "yes"})
	return true
}

// portraitAt returns the place of the portrait among the pictures of a
// person, or -1 if the person has no portrait.
func (e *Edit) portraitAt(person *model.Person) int {
	return slices.IndexFunc(person.MediaRefs, func(ref model.MediaRef) bool {
		item := e.media[ref.HLink]
		return item != nil && IsImage(item.File.Mime) && !notPortrait(&ref)
	})
}

// clearPortrait leaves a person without a portrait. The pictures stay.
func (e *Edit) clearPortrait(person *model.Person) bool {
	changed := false
	for i := range person.MediaRefs {
		if item := e.media[person.MediaRefs[i].HLink]; item != nil && IsImage(item.File.Mime) {
			changed = setNotPortrait(&person.MediaRefs[i], true) || changed
		}
	}
	return changed
}

// dropPicture takes a file out of the pictures of a person. If the file was
// the portrait, the person has no portrait after that: the next picture does
// not take the place by itself. It reports whether the person changed.
func (e *Edit) dropPicture(person *model.Person, handle string) bool {
	at := slices.IndexFunc(person.MediaRefs, func(ref model.MediaRef) bool { return ref.HLink == handle })
	if at < 0 {
		return false
	}
	wasPortrait := e.portraitAt(person) == at
	person.MediaRefs = slices.DeleteFunc(person.MediaRefs, func(ref model.MediaRef) bool { return ref.HLink == handle })
	if wasPortrait {
		e.clearPortrait(person)
	}
	return true
}

// ClearPortrait leaves a person without a portrait. The pictures of the
// person stay where they are, and each of them can become the portrait again.
func (e *Edit) ClearPortrait(personID string) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	if e.clearPortrait(person) {
		e.touch(model.KindPerson, person.Handle)
	}
	e.Subject = personID
	return nil
}

// onlyPictures checks the edit of a family member who chooses the own
// portrait. Like the guard of a branch, it does not look at the request: it
// compares the tree before and after the edit. The edit can change only the
// links from that one person to pictures. It can add a new file, and it can
// remove a file that the person had.
func (e *Edit) onlyPictures(personID string, changes []docChange) error {
	owner, ok := e.handleOf(model.KindPerson, personID)
	if !ok {
		return ErrOutsideBranch
	}
	var had []model.MediaRef
	if doc, ok := e.before[recordKey(model.KindPerson, owner)]; ok {
		var old model.Person
		if err := json.Unmarshal([]byte(doc), &old); err != nil {
			return err
		}
		had = old.MediaRefs
	}
	for _, change := range changes {
		switch change.kind {
		case model.KindPerson:
			if change.handle != owner || change.before == nil || change.after == nil {
				return ErrOutsideBranch
			}
			var old, now model.Person
			if err := json.Unmarshal(change.before, &old); err != nil {
				return err
			}
			if err := json.Unmarshal(change.after, &now); err != nil {
				return err
			}
			// Without the pictures and the time of the change, the person must be the same.
			old.MediaRefs, now.MediaRefs = nil, nil
			old.Change = now.Change
			a, err := json.Marshal(&old)
			if err != nil {
				return err
			}
			b, err := json.Marshal(&now)
			if err != nil {
				return err
			}
			if !bytes.Equal(a, b) {
				return ErrOutsideBranch
			}
		case model.KindMedia:
			switch {
			case change.before == nil && change.after != nil:
				var item model.Media
				if err := json.Unmarshal(change.after, &item); err != nil {
					return err
				}
				if bool(item.Private) || inGallery(&item) {
					return ErrOutsideBranch
				}
			case change.before != nil && change.after == nil:
				var item model.Media
				if err := json.Unmarshal(change.before, &item); err != nil {
					return err
				}
				// A photo of the gallery is not a picture of one person.
				if inGallery(&item) || !slices.ContainsFunc(had, func(ref model.MediaRef) bool { return ref.HLink == change.handle }) {
					return ErrOutsideBranch
				}
			default:
				return ErrOutsideBranch
			}
		default:
			return ErrOutsideBranch
		}
	}
	return nil
}
