package store

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// ErrOutsideBranch means that a person who can edit only the own branch asked
// for a change outside it, or for a change that such a person cannot make.
var ErrOutsideBranch = errors.New("store: the edit goes outside the branch of the user")

// branch is the part of the tree that a family member can edit: the person,
// the descendants of the person, and the partners of all of them.
type branch struct {
	// line holds the person and the descendants.
	line map[string]bool
	// people holds the line and the partners.
	people map[string]bool
	// families holds the families where a person of the line is a parent.
	families map[string]bool
}

func (e *Edit) has(kind model.Kind, handle string) bool {
	if e.deleted[recordKey(kind, handle)] {
		return false
	}
	switch kind {
	case model.KindPerson:
		return e.people[handle] != nil
	case model.KindFamily:
		return e.families[handle] != nil
	}
	return false
}

// branchOf returns the branch of the person with this ID in the current
// state of the edit. The second result is false if the person does not exist.
func (e *Edit) branchOf(rootID string) (*branch, bool) {
	root, ok := e.handleOf(model.KindPerson, rootID)
	if !ok || !e.has(model.KindPerson, root) {
		return nil, false
	}
	asParent := map[string][]*model.Family{}
	for handle, family := range e.families {
		if !e.has(model.KindFamily, handle) {
			continue
		}
		for _, parent := range []*model.Ref{family.Father, family.Mother} {
			if parent != nil {
				asParent[parent.HLink] = append(asParent[parent.HLink], family)
			}
		}
	}
	b := &branch{line: map[string]bool{root: true}, people: map[string]bool{}, families: map[string]bool{}}
	queue := []string{root}
	for len(queue) > 0 {
		person := queue[0]
		queue = queue[1:]
		b.people[person] = true
		for _, family := range asParent[person] {
			b.families[family.Handle] = true
			for _, parent := range []*model.Ref{family.Father, family.Mother} {
				if parent != nil && e.has(model.KindPerson, parent.HLink) {
					b.people[parent.HLink] = true
				}
			}
			for _, child := range family.ChildRefs {
				if !b.line[child.HLink] && e.has(model.KindPerson, child.HLink) {
					b.line[child.HLink] = true
					queue = append(queue, child.HLink)
				}
			}
		}
	}
	return b, true
}

// InBranch reports whether a person is in the branch of another person: the
// person, a descendant, or a partner of one of them.
func (e *Edit) InBranch(rootID, personID string) bool {
	b, ok := e.branchOf(rootID)
	if !ok {
		return false
	}
	handle, ok := e.handleOf(model.KindPerson, personID)
	return ok && b.people[handle]
}

// branchGuard checks an edit of a person who can change only the own branch.
// It does not look at the request. It compares the tree before and after the
// edit, so no operation can go around it.
type branchGuard struct {
	rootID string
	pre    *branch
}

// guard reads the branch before the edit starts.
func (e *Edit) guard(rootID string) (*branchGuard, bool) {
	pre, ok := e.branchOf(rootID)
	if !ok {
		return nil, false
	}
	return &branchGuard{rootID: rootID, pre: pre}, true
}

func refsKept(before, after []model.Ref) bool {
	for _, ref := range before {
		if !slices.Contains(after, ref) {
			return false
		}
	}
	return true
}

func sameRef(a, b *model.Ref) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.HLink == b.HLink)
}

// users returns the records whose document names the handle.
func users(docs map[string]string, self, handle string) []string {
	needle := `"` + handle + `"`
	var out []string
	for key, doc := range docs {
		if key != self && strings.Contains(doc, needle) {
			out = append(out, key)
		}
	}
	return out
}

// check refuses an edit that does one of these things:
//   - changes a person or a family outside the branch;
//   - removes a person, a family, or a link between people;
//   - links a person who was outside the branch into it;
//   - changes the privacy mark of a record;
//   - changes an event, a note or a file that somebody outside the branch uses;
//   - changes any other kind of record, or the settings of the tree.
func (g *branchGuard) check(e *Edit, changes []docChange) error {
	post, ok := e.branchOf(g.rootID)
	if !ok {
		return ErrOutsideBranch
	}
	created := map[string]bool{}
	after := make(map[string]string, len(e.before)+len(changes))
	for key, doc := range e.before {
		after[key] = doc
	}
	for _, change := range changes {
		key := recordKey(change.kind, change.handle)
		if change.before == nil {
			created[key] = true
		}
		if change.after == nil {
			delete(after, key)
		} else {
			after[key] = string(change.after)
		}
	}
	personOK := func(handle string) bool {
		return g.pre.people[handle] || (created[recordKey(model.KindPerson, handle)] && post.people[handle])
	}
	familyOK := func(handle string) bool {
		return g.pre.families[handle] || (created[recordKey(model.KindFamily, handle)] && post.families[handle])
	}
	// usersOK reports whether only people and families of the branch use a record.
	usersOK := func(key, handle string, families bool) bool {
		all := append(users(e.before, key, handle), users(after, key, handle)...)
		for _, user := range all {
			kind, owner, _ := strings.Cut(user, "\x00")
			switch {
			case model.Kind(kind) == model.KindPerson && personOK(owner):
			case families && model.Kind(kind) == model.KindFamily && familyOK(owner):
			default:
				return false
			}
		}
		return true
	}
	for _, change := range changes {
		key := recordKey(change.kind, change.handle)
		switch change.kind {
		case model.KindPerson:
			if change.after == nil {
				return ErrOutsideBranch
			}
			var now model.Person
			if err := json.Unmarshal(change.after, &now); err != nil {
				return err
			}
			if change.before == nil {
				if !post.people[change.handle] || bool(now.Private) {
					return ErrOutsideBranch
				}
				continue
			}
			var old model.Person
			if err := json.Unmarshal(change.before, &old); err != nil {
				return err
			}
			if !g.pre.people[change.handle] || old.Private != now.Private ||
				!refsKept(old.ChildOf, now.ChildOf) || !refsKept(old.ParentIn, now.ParentIn) {
				return ErrOutsideBranch
			}
		case model.KindFamily:
			if change.after == nil {
				return ErrOutsideBranch
			}
			var now model.Family
			if err := json.Unmarshal(change.after, &now); err != nil {
				return err
			}
			if change.before == nil {
				if !post.families[change.handle] || bool(now.Private) {
					return ErrOutsideBranch
				}
				continue
			}
			var old model.Family
			if err := json.Unmarshal(change.before, &old); err != nil {
				return err
			}
			if !g.pre.families[change.handle] || old.Private != now.Private ||
				(old.Father != nil && !sameRef(old.Father, now.Father)) || (old.Mother != nil && !sameRef(old.Mother, now.Mother)) {
				return ErrOutsideBranch
			}
			for _, child := range old.ChildRefs {
				if !slices.ContainsFunc(now.ChildRefs, func(ref model.ChildRef) bool { return ref.HLink == child.HLink }) {
					return ErrOutsideBranch
				}
			}
		case model.KindEvent, model.KindNote:
			if change.before != nil && change.after != nil {
				var old, now model.Object
				if err := json.Unmarshal(change.before, &old); err != nil {
					return err
				}
				if err := json.Unmarshal(change.after, &now); err != nil {
					return err
				}
				if old.Private != now.Private {
					return ErrOutsideBranch
				}
			}
			// A hidden note is for the people who can change the whole
			// tree. A family member makes, changes and removes none.
			if change.kind == model.KindNote {
				for _, doc := range [][]byte{change.before, change.after} {
					if doc == nil {
						continue
					}
					var note model.Object
					if err := json.Unmarshal(doc, &note); err != nil {
						return err
					}
					if note.Private {
						return ErrOutsideBranch
					}
				}
			}
			// A couple has events. Only a person has notes that Heirfold edits.
			if !usersOK(key, change.handle, change.kind == model.KindEvent) {
				return ErrOutsideBranch
			}
		case model.KindPlace:
			// A new place comes with a new event. An old place stays as it is.
			if change.before != nil {
				return ErrOutsideBranch
			}
		case model.KindMedia:
			if (change.before != nil && change.after != nil) || !usersOK(key, change.handle, false) {
				return ErrOutsideBranch
			}
		default:
			return ErrOutsideBranch
		}
	}
	return nil
}
