package store

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Renset/heirfold/internal/model"
)

// Limits of the text that an editor can store.
const (
	maxNameLength = 120
	maxLineLength = 500
	maxNoteLength = 50_000
	maxNames      = 12
)

// cleanLine removes control characters and outer spaces from one line of text.
// The second result is false if the text is too long.
func cleanLine(text string, limit int) (string, bool) {
	text = strings.TrimSpace(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, text))
	return text, utf8.RuneCountInString(text) <= limit
}

// cleanText does the same for text with several lines.
func cleanText(text string, limit int) (string, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return -1
		}
		return c
	}, text))
	return text, utf8.RuneCountInString(text) <= limit
}

// NameInput is one name of a person as an editor types it.
type NameInput struct {
	// Index is the position of the name in the stored list, or -1 for a new
	// name. A stored name keeps the parts that the form does not show.
	Index      int    `json:"index"`
	Type       string `json:"type"`
	Given      string `json:"given"`
	Surname    string `json:"surname"`
	Patronymic string `json:"patronymic"`
	Suffix     string `json:"suffix"`
	Title      string `json:"title"`
	Nick       string `json:"nick"`
	Call       string `json:"call"`
}

// PersonInput holds the facts of a person that the main form changes.
type PersonInput struct {
	Gender  string      `json:"gender"`
	Private bool        `json:"private"`
	Names   []NameInput `json:"names"`
}

// PlaceInput names a place: an existing one by its ID, or a new one by name.
type PlaceInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EventInput holds the facts of an event that the form changes.
type EventInput struct {
	Type        string      `json:"type"`
	Date        model.Dated `json:"date"`
	Place       *PlaceInput `json:"place"`
	Description string      `json:"description"`
	Private     bool        `json:"private"`
}

func validGender(gender string) bool {
	return gender == "M" || gender == "F" || gender == "U" || gender == "X"
}

func isPatronymic(s model.Surname) bool {
	return s.Derivation == "Patronymic" || s.Derivation == "Matronymic"
}

// applyName writes the form fields into a name and keeps all other parts.
func applyName(name *model.Name, in NameInput) bool {
	fields := []*string{&in.Type, &in.Given, &in.Surname, &in.Patronymic, &in.Suffix, &in.Title, &in.Nick, &in.Call}
	for _, field := range fields {
		clean, ok := cleanLine(*field, maxNameLength)
		if !ok {
			return false
		}
		*field = clean
	}
	if in.Type == "" {
		in.Type = "Birth Name"
	}
	name.Type, name.First, name.Suffix, name.Title, name.Nick, name.Call = in.Type, in.Given, in.Suffix, in.Title, in.Nick, in.Call
	// A changed name must not keep an old ready-made display form.
	name.Display, name.Sort = "", ""

	// The form shows the surname that model.SplitName chooses: the primary
	// part, else the first part that is not a patronymic. The form writes
	// back to the same part.
	surname, patronymic := -1, -1
	for i, part := range name.Surnames {
		if isPatronymic(part) {
			if patronymic < 0 {
				patronymic = i
			}
			continue
		}
		if surname < 0 && (part.Primary == nil || bool(*part.Primary)) {
			surname = i
		}
	}
	if surname < 0 {
		for i, part := range name.Surnames {
			if !isPatronymic(part) {
				surname = i
				break
			}
		}
	}
	primary := model.Flag(true)
	switch {
	case surname >= 0 && in.Surname != "":
		part := &name.Surnames[surname]
		full := part.Value
		if part.Prefix != "" {
			full = part.Prefix + " " + part.Value
		}
		switch {
		case in.Surname == full:
		case part.Prefix != "" && strings.HasPrefix(in.Surname, part.Prefix+" "):
			part.Value = strings.TrimSpace(strings.TrimPrefix(in.Surname, part.Prefix+" "))
		default:
			part.Prefix, part.Value = "", in.Surname
		}
	case surname >= 0 && name.Surnames[surname].Value == "" && name.Surnames[surname].Prefix == "":
		// Gramps stores a name without a surname as one empty part. The form
		// did not change it, so it stays.
	case surname >= 0:
		name.Surnames = slices.Delete(name.Surnames, surname, surname+1)
		if patronymic > surname {
			patronymic--
		}
	case in.Surname != "":
		// The new surname is the only primary part.
		for i := range name.Surnames {
			no := model.Flag(false)
			name.Surnames[i].Primary = &no
		}
		name.Surnames = slices.Insert(name.Surnames, 0, model.Surname{Value: in.Surname, Primary: &primary})
		if patronymic >= 0 {
			patronymic++
		}
	}
	switch {
	case patronymic >= 0 && in.Patronymic != "":
		name.Surnames[patronymic].Value = in.Patronymic
	case patronymic >= 0:
		name.Surnames = slices.Delete(name.Surnames, patronymic, patronymic+1)
	case in.Patronymic != "":
		no := model.Flag(false)
		name.Surnames = append(name.Surnames, model.Surname{Value: in.Patronymic, Derivation: "Patronymic", Primary: &no})
	}
	// Gramps wants one primary surname when a name has surnames.
	hasPrimary := false
	for _, part := range name.Surnames {
		if part.Primary == nil || bool(*part.Primary) {
			hasPrimary = true
		}
	}
	if !hasPrimary && len(name.Surnames) > 0 {
		name.Surnames[0].Primary = &primary
	}
	return true
}

// applyPerson writes the form into a person. The first name of the form
// becomes the main name.
func applyPerson(person *model.Person, in PersonInput) error {
	if !validGender(in.Gender) || len(in.Names) == 0 || len(in.Names) > maxNames {
		return ErrInvalid
	}
	names := make([]model.Name, 0, len(in.Names))
	used := map[int]bool{}
	for i, input := range in.Names {
		var name model.Name
		if input.Index >= 0 {
			if input.Index >= len(person.Names) || used[input.Index] {
				return ErrInvalid
			}
			used[input.Index] = true
			name = person.Names[input.Index]
			name.Surnames = slices.Clone(name.Surnames)
		}
		if !applyName(&name, input) {
			return ErrInvalid
		}
		name.Alt = i > 0
		names = append(names, name)
	}
	person.Names = names
	person.Gender = in.Gender
	person.Private = model.Flag(in.Private)
	return nil
}

// CreatePerson adds a person and returns it.
func (e *Edit) CreatePerson(in PersonInput) (*model.Person, error) {
	person := &model.Person{}
	if err := applyPerson(person, in); err != nil {
		return nil, err
	}
	e.register(model.KindPerson, &person.Object)
	e.people[person.Handle] = person
	e.Created = person.ID
	if e.Subject == "" {
		e.Subject = person.ID
	}
	return person, nil
}

// UpdatePerson changes the names, the gender and the privacy of a person.
func (e *Edit) UpdatePerson(gid string, rev int64, in PersonInput) error {
	person, err := e.Person(gid)
	if err != nil {
		return err
	}
	if err := e.checkRev(model.KindPerson, person.Handle, rev); err != nil {
		return err
	}
	if err := applyPerson(person, in); err != nil {
		return err
	}
	e.touch(model.KindPerson, person.Handle)
	e.Subject = gid
	return nil
}

func dropRef(refs []model.Ref, handle string) []model.Ref {
	return slices.DeleteFunc(refs, func(ref model.Ref) bool { return ref.HLink == handle })
}

func hasRef(refs []model.Ref, handle string) bool {
	return slices.ContainsFunc(refs, func(ref model.Ref) bool { return ref.HLink == handle })
}

func (e *Edit) newFamily() *model.Family {
	family := &model.Family{Rel: &model.FamilyRel{Type: "Unknown"}}
	e.register(model.KindFamily, &family.Object)
	e.families[family.Handle] = family
	return family
}

// setParent puts a person into a free parent place of a family. The gender
// chooses the place when it is free.
func (e *Edit) setParent(family *model.Family, person *model.Person) error {
	if (family.Father != nil && family.Father.HLink == person.Handle) || (family.Mother != nil && family.Mother.HLink == person.Handle) {
		return nil
	}
	ref := &model.Ref{HLink: person.Handle}
	switch {
	case person.Gender == "F" && family.Mother == nil:
		family.Mother = ref
	case family.Father == nil:
		family.Father = ref
	case family.Mother == nil:
		family.Mother = ref
	default:
		return ErrSlotTaken
	}
	if !hasRef(person.ParentIn, family.Handle) {
		person.ParentIn = append(person.ParentIn, model.Ref{HLink: family.Handle})
	}
	e.touch(model.KindFamily, family.Handle)
	e.touch(model.KindPerson, person.Handle)
	return nil
}

func (e *Edit) addChild(family *model.Family, person *model.Person) {
	if !slices.ContainsFunc(family.ChildRefs, func(ref model.ChildRef) bool { return ref.HLink == person.Handle }) {
		family.ChildRefs = append(family.ChildRefs, model.ChildRef{HLink: person.Handle})
		e.touch(model.KindFamily, family.Handle)
	}
	if !hasRef(person.ChildOf, family.Handle) {
		person.ChildOf = append(person.ChildOf, model.Ref{HLink: family.Handle})
		e.touch(model.KindPerson, person.Handle)
	}
}

func isParent(family *model.Family, handle string) bool {
	return (family.Father != nil && family.Father.HLink == handle) || (family.Mother != nil && family.Mother.HLink == handle)
}

func isChild(family *model.Family, handle string) bool {
	return slices.ContainsFunc(family.ChildRefs, func(ref model.ChildRef) bool { return ref.HLink == handle })
}

// descends reports whether the person `low` comes from the person `high`.
func (e *Edit) descends(low, high string) bool {
	seen := map[string]bool{}
	queue := []string{high}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == low {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		person := e.people[current]
		if person == nil {
			continue
		}
		for _, ref := range person.ParentIn {
			if family := e.families[ref.HLink]; family != nil && !e.deleted[recordKey(model.KindFamily, family.Handle)] {
				for _, child := range family.ChildRefs {
					queue = append(queue, child.HLink)
				}
			}
		}
	}
	return false
}

// The relations that Link accepts. Each names what the other person becomes
// for the first person.
const (
	AsParent  = "parent"
	AsSpouse  = "spouse"
	AsChild   = "child"
	AsSibling = "sibling"
)

// Link connects two people. other becomes the parent, the spouse, the child or
// the sibling of person. familyID chooses the family when the person has more
// than one that fits; it can be empty.
func (e *Edit) Link(personID, as, otherID, familyID string) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	other, err := e.Person(otherID)
	if err != nil {
		return err
	}
	if person.Handle == other.Handle {
		return ErrInvalid
	}
	var family *model.Family
	if familyID != "" {
		if family, err = e.Family(familyID); err != nil {
			return err
		}
	}
	e.Subject = personID
	if err := e.link(person, as, other, family); err != nil {
		return err
	}
	// A new link must not close a circle of parents and children. A circle
	// that the link makes passes through one of the two people.
	if e.reachesSelf(person.Handle) || e.reachesSelf(other.Handle) {
		return ErrLoop
	}
	return nil
}

// reachesSelf reports whether a person is among their own descendants.
func (e *Edit) reachesSelf(start string) bool {
	seen := map[string]bool{}
	queue := e.childrenOf(start)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == start {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, e.childrenOf(current)...)
	}
	return false
}

func (e *Edit) childrenOf(handle string) []string {
	person := e.people[handle]
	if person == nil || e.deleted[recordKey(model.KindPerson, handle)] {
		return nil
	}
	var out []string
	for _, ref := range person.ParentIn {
		family := e.families[ref.HLink]
		if family == nil || e.deleted[recordKey(model.KindFamily, family.Handle)] || !isParent(family, handle) {
			continue
		}
		for _, child := range family.ChildRefs {
			out = append(out, child.HLink)
		}
	}
	return out
}

func (e *Edit) link(person *model.Person, as string, other *model.Person, family *model.Family) error {
	// A link that exists already changes nothing, so a repeat of a request
	// makes no second family.
	fits := func(candidate *model.Family) bool {
		return candidate != nil && !e.deleted[recordKey(model.KindFamily, candidate.Handle)] && (family == nil || family.Handle == candidate.Handle)
	}
	for _, ref := range person.ParentIn {
		candidate := e.families[ref.HLink]
		if fits(candidate) && isParent(candidate, person.Handle) &&
			((as == AsChild && isChild(candidate, other.Handle)) || (as == AsSpouse && isParent(candidate, other.Handle))) {
			return nil
		}
	}
	for _, ref := range person.ChildOf {
		candidate := e.families[ref.HLink]
		if fits(candidate) && isChild(candidate, person.Handle) &&
			((as == AsParent && isParent(candidate, other.Handle)) || (as == AsSibling && isChild(candidate, other.Handle))) {
			return nil
		}
	}
	switch as {
	case AsParent:
		if e.descends(other.Handle, person.Handle) {
			return ErrLoop
		}
		if family != nil && !isChild(family, person.Handle) {
			return ErrInvalid
		}
		if family == nil && len(person.ChildOf) > 0 {
			family = e.families[person.ChildOf[0].HLink]
		}
		if family == nil {
			family = e.newFamily()
			e.addChild(family, person)
		}
		return e.setParent(family, other)
	case AsSpouse:
		if family != nil && !isParent(family, person.Handle) {
			return ErrInvalid
		}
		if family == nil {
			// The two people can already be a couple.
			for _, ref := range person.ParentIn {
				if candidate := e.families[ref.HLink]; candidate != nil && isParent(candidate, other.Handle) {
					return nil
				}
			}
			family = e.newFamily()
			if err := e.setParent(family, person); err != nil {
				return err
			}
		}
		return e.setParent(family, other)
	case AsChild:
		if e.descends(person.Handle, other.Handle) {
			return ErrLoop
		}
		if family != nil && !isParent(family, person.Handle) {
			return ErrInvalid
		}
		if family == nil && len(person.ParentIn) == 1 {
			family = e.families[person.ParentIn[0].HLink]
		}
		if family == nil {
			family = e.newFamily()
			if err := e.setParent(family, person); err != nil {
				return err
			}
		}
		e.addChild(family, other)
		return nil
	case AsSibling:
		if family != nil && !isChild(family, person.Handle) {
			return ErrInvalid
		}
		if family == nil && len(person.ChildOf) > 0 {
			family = e.families[person.ChildOf[0].HLink]
		}
		if family == nil {
			family = e.newFamily()
			e.addChild(family, person)
		}
		e.addChild(family, other)
		return nil
	}
	return ErrInvalid
}

// eventUsed reports whether a person or a family still points to the event.
func (e *Edit) eventUsed(handle string) bool {
	for key, person := range e.people {
		if e.deleted[recordKey(model.KindPerson, key)] {
			continue
		}
		if slices.ContainsFunc(person.EventRefs, func(ref model.EventRef) bool { return ref.HLink == handle }) {
			return true
		}
	}
	for key, family := range e.families {
		if e.deleted[recordKey(model.KindFamily, key)] {
			continue
		}
		if slices.ContainsFunc(family.EventRefs, func(ref model.EventRef) bool { return ref.HLink == handle }) {
			return true
		}
	}
	return false
}

func (e *Edit) dropUnusedEvents(refs []model.EventRef) {
	for _, ref := range refs {
		if e.events[ref.HLink] != nil && !e.eventUsed(ref.HLink) {
			e.remove(model.KindEvent, ref.HLink)
		}
	}
}

// deleteFamily removes a family and every link to it.
func (e *Edit) deleteFamily(family *model.Family) {
	members := []string{}
	if family.Father != nil {
		members = append(members, family.Father.HLink)
	}
	if family.Mother != nil {
		members = append(members, family.Mother.HLink)
	}
	for _, child := range family.ChildRefs {
		members = append(members, child.HLink)
	}
	for _, handle := range members {
		if person := e.people[handle]; person != nil {
			person.ParentIn = dropRef(person.ParentIn, family.Handle)
			person.ChildOf = dropRef(person.ChildOf, family.Handle)
			e.touch(model.KindPerson, handle)
		}
	}
	e.remove(model.KindFamily, family.Handle)
	e.dropUnusedEvents(family.EventRefs)
	before := len(e.tree.Bookmarks)
	e.tree.Bookmarks = slices.DeleteFunc(e.tree.Bookmarks, func(b model.Bookmark) bool { return b.HLink == family.Handle })
	if len(e.tree.Bookmarks) != before {
		e.metaChanged = true
	}
}

func familySize(family *model.Family) int {
	n := len(family.ChildRefs)
	if family.Father != nil {
		n++
	}
	if family.Mother != nil {
		n++
	}
	return n
}

// detach takes a person out of a family. A family with fewer than two people
// links nobody, so it goes away.
func (e *Edit) detach(family *model.Family, person *model.Person) {
	if family.Father != nil && family.Father.HLink == person.Handle {
		family.Father = nil
	}
	if family.Mother != nil && family.Mother.HLink == person.Handle {
		family.Mother = nil
	}
	family.ChildRefs = slices.DeleteFunc(family.ChildRefs, func(ref model.ChildRef) bool { return ref.HLink == person.Handle })
	person.ParentIn = dropRef(person.ParentIn, family.Handle)
	person.ChildOf = dropRef(person.ChildOf, family.Handle)
	e.touch(model.KindFamily, family.Handle)
	e.touch(model.KindPerson, person.Handle)
	if familySize(family) < 2 {
		e.deleteFamily(family)
	}
}

// Unlink takes a person out of a family.
func (e *Edit) Unlink(familyID, personID string) error {
	family, err := e.Family(familyID)
	if err != nil {
		return err
	}
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	if !isParent(family, person.Handle) && !isChild(family, person.Handle) {
		return ErrNotFound
	}
	e.detach(family, person)
	e.Subject = personID
	return nil
}

// SetFamilyType changes the relationship type of a couple.
func (e *Edit) SetFamilyType(familyID, rel string) error {
	family, err := e.Family(familyID)
	if err != nil {
		return err
	}
	rel, ok := cleanLine(rel, maxNameLength)
	if !ok || rel == "" {
		return ErrInvalid
	}
	family.Rel = &model.FamilyRel{Type: rel}
	e.touch(model.KindFamily, family.Handle)
	return nil
}

// DeletePerson removes a person, the links to them and the events that only
// they had.
func (e *Edit) DeletePerson(gid string) error {
	person, err := e.Person(gid)
	if err != nil {
		return err
	}
	for _, ref := range slices.Concat(slices.Clone(person.ParentIn), slices.Clone(person.ChildOf)) {
		if family := e.families[ref.HLink]; family != nil && !e.deleted[recordKey(model.KindFamily, family.Handle)] {
			e.detach(family, person)
		}
	}
	// Other people can name this person in an association.
	for handle, other := range e.people {
		before := len(other.PersonRefs)
		other.PersonRefs = slices.DeleteFunc(other.PersonRefs, func(ref model.PersonRef) bool { return ref.HLink == person.Handle })
		if len(other.PersonRefs) != before {
			e.touch(model.KindPerson, handle)
		}
	}
	e.remove(model.KindPerson, person.Handle)
	e.dropUnusedEvents(person.EventRefs)
	// A portrait that an editor added leaves with the person.
	for _, ref := range person.MediaRefs {
		if item := e.media[ref.HLink]; item != nil && e.ownPicture(item) && !e.mediaUsed(ref.HLink) {
			e.remove(model.KindMedia, ref.HLink)
		}
	}
	if e.tree.HomeHandle == person.Handle {
		e.tree.HomeHandle = ""
		e.metaChanged = true
	}
	before := len(e.tree.Bookmarks)
	e.tree.Bookmarks = slices.DeleteFunc(e.tree.Bookmarks, func(b model.Bookmark) bool { return b.HLink == person.Handle })
	if len(e.tree.Bookmarks) != before {
		e.metaChanged = true
	}
	e.Subject = gid
	return nil
}

// SetHome makes a person the centre of the tree.
func (e *Edit) SetHome(gid string) error {
	person, err := e.Person(gid)
	if err != nil {
		return err
	}
	if e.tree.HomeHandle != person.Handle {
		e.tree.HomeHandle = person.Handle
		e.metaChanged = true
	}
	e.Subject = gid
	return nil
}

var (
	datePoint   = regexp.MustCompile(`^(\d{4}|\?{4})(-\d{2}(-\d{2})?)?$`)
	dateWord    = regexp.MustCompile(`^[A-Za-z ]{0,24}$`)
	dateNewYear = regexp.MustCompile(`^[A-Za-z0-9 ]{0,12}$`)
)

// validPoint checks one point of a date in the form year-month-day. A 0 or
// "????" marks an unknown part. The Gregorian and the Julian calendar check
// the day against the month. Other calendars of Gramps, such as the Hebrew
// one, have up to 13 months.
func validPoint(text, calendar string) bool {
	if !datePoint.MatchString(text) {
		return false
	}
	parts := strings.Split(text, "-")
	if len(parts) < 2 {
		return true
	}
	month, _ := strconv.Atoi(parts[1])
	day := 0
	if len(parts) > 2 {
		day, _ = strconv.Atoi(parts[2])
	}
	if calendar != "" && calendar != "Gregorian" && calendar != "Julian" {
		return month <= 13 && day <= 31
	}
	if month > 12 || (month == 0 && day > 0) {
		return false
	}
	if month == 0 || day == 0 {
		return true
	}
	// A year that nobody knows can be a leap year.
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		year = 2000
	}
	last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if calendar == "Julian" && month == 2 && year%4 == 0 {
		last = 29
	}
	return day <= last
}

// validDate checks a date that came from an editor. A date has at most one form.
func validDate(d *model.Dated) bool {
	forms := 0
	if d.DateVal != nil {
		forms++
		v := d.DateVal
		if !validPoint(v.Val, v.CFormat) || !slices.Contains([]string{"", "before", "after", "about"}, v.Type) ||
			!slices.Contains([]string{"", "estimated", "calculated"}, v.Quality) || !dateWord.MatchString(v.CFormat) || !dateNewYear.MatchString(v.NewYear) {
			return false
		}
	}
	for _, pair := range []*model.DatePair{d.DateRange, d.DateSpan} {
		if pair == nil {
			continue
		}
		forms++
		if !validPoint(pair.Start, pair.CFormat) || !validPoint(pair.Stop, pair.CFormat) || !slices.Contains([]string{"", "estimated", "calculated"}, pair.Quality) ||
			!dateWord.MatchString(pair.CFormat) || !dateNewYear.MatchString(pair.NewYear) {
			return false
		}
	}
	if d.DateStr != nil {
		forms++
		text, ok := cleanLine(d.DateStr.Val, maxNameLength)
		if !ok || text == "" {
			return false
		}
		d.DateStr.Val = text
	}
	return forms <= 1
}

// placeRef finds or makes the place of an event.
func (e *Edit) placeRef(in *PlaceInput) (*model.Ref, error) {
	if in == nil {
		return nil, nil
	}
	if in.ID != "" {
		handle, ok := e.handleOf(model.KindPlace, in.ID)
		if !ok {
			return nil, ErrInvalid
		}
		return &model.Ref{HLink: handle}, nil
	}
	name, ok := cleanLine(in.Name, maxLineLength)
	if !ok {
		return nil, ErrInvalid
	}
	if name == "" {
		return nil, nil
	}
	for handle, place := range e.places {
		if e.deleted[recordKey(model.KindPlace, handle)] {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(place.Title), name) {
			return &model.Ref{HLink: handle}, nil
		}
	}
	place := &model.Place{Type: "Unknown", Title: name, Names: []model.PlaceName{{Value: name}}}
	e.register(model.KindPlace, &place.Object)
	e.places[place.Handle] = place
	return &model.Ref{HLink: place.Handle}, nil
}

func (e *Edit) applyEvent(event *model.Event, in EventInput) error {
	kind, ok := cleanLine(in.Type, maxNameLength)
	if !ok || kind == "" {
		return ErrInvalid
	}
	description, ok := cleanLine(in.Description, maxLineLength)
	if !ok || !validDate(&in.Date) {
		return ErrInvalid
	}
	place, err := e.placeRef(in.Place)
	if err != nil {
		return err
	}
	event.Type, event.Dated, event.Place, event.Description, event.Private = kind, in.Date, place, description, model.Flag(in.Private)
	return nil
}

// Owner names the person or the family that an event belongs to.
type Owner struct {
	Kind model.Kind
	ID   string
}

func (e *Edit) ownerRefs(owner Owner) (*[]model.EventRef, string, error) {
	switch owner.Kind {
	case model.KindPerson:
		person, err := e.Person(owner.ID)
		if err != nil {
			return nil, "", err
		}
		e.Subject = owner.ID
		return &person.EventRefs, person.Handle, nil
	case model.KindFamily:
		family, err := e.Family(owner.ID)
		if err != nil {
			return nil, "", err
		}
		return &family.EventRefs, family.Handle, nil
	}
	return nil, "", ErrInvalid
}

// AddEvent adds an event to a person or to a family.
func (e *Edit) AddEvent(owner Owner, in EventInput) error {
	refs, handle, err := e.ownerRefs(owner)
	if err != nil {
		return err
	}
	event := &model.Event{}
	if err := e.applyEvent(event, in); err != nil {
		return err
	}
	e.register(model.KindEvent, &event.Object)
	e.events[event.Handle] = event
	role := "Primary"
	if owner.Kind == model.KindFamily {
		role = "Family"
	}
	*refs = append(*refs, model.EventRef{HLink: event.Handle, Role: role})
	e.touch(owner.Kind, handle)
	e.Created = event.ID
	return nil
}

// UpdateEvent changes an event. Every person who shares the event sees the change.
func (e *Edit) UpdateEvent(gid string, rev int64, in EventInput) error {
	event, err := e.Event(gid)
	if err != nil {
		return err
	}
	if err := e.checkRev(model.KindEvent, event.Handle, rev); err != nil {
		return err
	}
	if err := e.applyEvent(event, in); err != nil {
		return err
	}
	e.touch(model.KindEvent, event.Handle)
	return nil
}

// RemoveEvent takes an event away from a person or a family. The event itself
// goes away when nobody else has it.
func (e *Edit) RemoveEvent(owner Owner, gid string) error {
	refs, handle, err := e.ownerRefs(owner)
	if err != nil {
		return err
	}
	event, err := e.Event(gid)
	if err != nil {
		return err
	}
	before := len(*refs)
	*refs = slices.DeleteFunc(*refs, func(ref model.EventRef) bool { return ref.HLink == event.Handle })
	if len(*refs) == before {
		return ErrNotFound
	}
	e.touch(owner.Kind, handle)
	if !e.eventUsed(event.Handle) {
		e.remove(model.KindEvent, event.Handle)
	}
	return nil
}

// AddNote adds a note to a person.
func (e *Edit) AddNote(personID, text string, hidden bool) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	text, ok := cleanText(text, maxNoteLength)
	if !ok || text == "" {
		return ErrInvalid
	}
	note := &model.Note{Type: "Person Note", Text: text}
	note.Private = model.Flag(hidden)
	e.register(model.KindNote, &note.Object)
	e.notes[note.Handle] = note
	person.Notes = append(person.Notes, model.Ref{HLink: note.Handle})
	e.touch(model.KindPerson, person.Handle)
	e.Subject, e.Created = personID, note.ID
	return nil
}

// UpdateNote replaces the text of a note and says whether the note is hidden.
// The marks for bold and italic text point at positions of the old text, so
// they go away with a new text.
// Without hidden the note stays as it is: hidden or open.
func (e *Edit) UpdateNote(gid string, rev int64, text string, hidden *bool) error {
	note, err := e.Note(gid)
	if err != nil {
		return err
	}
	if err := e.checkRev(model.KindNote, note.Handle, rev); err != nil {
		return err
	}
	text, ok := cleanText(text, maxNoteLength)
	if !ok || text == "" {
		return ErrInvalid
	}
	if note.Text != text {
		note.Text, note.Styles = text, nil
		e.touch(model.KindNote, note.Handle)
	}
	if hidden != nil && bool(note.Private) != *hidden {
		note.Private = model.Flag(*hidden)
		e.touch(model.KindNote, note.Handle)
	}
	return nil
}

// noteUsed reports whether a person, a family or an event still points to the note.
func (e *Edit) noteUsed(handle string) bool {
	for key, person := range e.people {
		if !e.deleted[recordKey(model.KindPerson, key)] && hasRef(person.Notes, handle) {
			return true
		}
	}
	for key, family := range e.families {
		if !e.deleted[recordKey(model.KindFamily, key)] && hasRef(family.Notes, handle) {
			return true
		}
	}
	for key, event := range e.events {
		if !e.deleted[recordKey(model.KindEvent, key)] && hasRef(event.Notes, handle) {
			return true
		}
	}
	return false
}

// RemoveNote takes a note away from a person. The note itself goes away when
// no person, family or event has it.
func (e *Edit) RemoveNote(personID, gid string) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	note, err := e.Note(gid)
	if err != nil {
		return err
	}
	if !hasRef(person.Notes, note.Handle) {
		return ErrNotFound
	}
	person.Notes = dropRef(person.Notes, note.Handle)
	e.touch(model.KindPerson, person.Handle)
	if !e.noteUsed(note.Handle) {
		e.remove(model.KindNote, note.Handle)
	}
	e.Subject = personID
	return nil
}

// EditEvent is a stored event in the form of the edit page.
type EditEvent struct {
	ID   string      `json:"id"`
	Rev  int64       `json:"rev"`
	Type string      `json:"type"`
	Role string      `json:"role,omitempty"`
	Date model.Dated `json:"date"`
	// Sort orders events in time, as in EventView.
	Sort        int64       `json:"sort,omitempty"`
	Place       *PlaceInput `json:"place,omitempty"`
	Description string      `json:"description,omitempty"`
	Private     bool        `json:"private,omitempty"`
	// Shared is the number of people and families that have this event.
	Shared int `json:"shared"`
}

// EditNote is a stored note in the form of the edit page.
type EditNote struct {
	ID   string `json:"id"`
	Rev  int64  `json:"rev"`
	Type string `json:"type,omitempty"`
	Text string `json:"text"`
	// Hidden is true for a note that only editors and administrators read.
	Hidden bool `json:"hidden,omitempty"`
}

// EditFamily holds the events of one couple.
type EditFamily struct {
	ID     string      `json:"id"`
	Rel    string      `json:"rel,omitempty"`
	Events []EditEvent `json:"events"`
}

// EditPerson is what the edit page of a person needs.
type EditPerson struct {
	ID       string       `json:"id"`
	Rev      int64        `json:"rev"`
	Gender   string       `json:"gender"`
	Private  bool         `json:"private"`
	Home     bool         `json:"home"`
	Names    []NameInput  `json:"names"`
	Events   []EditEvent  `json:"events"`
	Notes    []EditNote   `json:"notes"`
	Families []EditFamily `json:"families"`
}

func (e *Edit) eventUses(handle string) int {
	n := 0
	for _, person := range e.people {
		for _, ref := range person.EventRefs {
			if ref.HLink == handle {
				n++
			}
		}
	}
	for _, family := range e.families {
		for _, ref := range family.EventRefs {
			if ref.HLink == handle {
				n++
			}
		}
	}
	return n
}

func (e *Edit) editEvents(refs []model.EventRef) []EditEvent {
	out := []EditEvent{}
	for _, ref := range refs {
		event := e.events[ref.HLink]
		if event == nil {
			continue
		}
		item := EditEvent{
			ID: e.ID(model.KindEvent, event.Handle), Rev: e.Rev(model.KindEvent, event.Handle), Type: event.Type, Role: ref.Role, Date: event.Dated,
			Description: event.Description, Private: bool(event.Private), Shared: e.eventUses(event.Handle),
			Sort: event.View().SortKey(),
		}
		if event.Place != nil {
			if place := e.places[event.Place.HLink]; place != nil {
				name := strings.TrimSpace(place.Title)
				if name == "" && len(place.Names) > 0 {
					name = place.Names[0].Value
				}
				item.Place = &PlaceInput{ID: e.ID(model.KindPlace, place.Handle), Name: name}
			}
		}
		out = append(out, item)
	}
	return out
}

// Describe returns a person in the form of the edit page.
func (e *Edit) Describe(gid string) (*EditPerson, error) {
	person, err := e.Person(gid)
	if err != nil {
		return nil, err
	}
	out := &EditPerson{
		ID: gid, Rev: e.Rev(model.KindPerson, person.Handle), Gender: person.Gender, Private: bool(person.Private),
		Home: e.tree.HomeHandle == person.Handle, Names: []NameInput{}, Notes: []EditNote{}, Families: []EditFamily{},
	}
	for i, name := range person.Names {
		surname, patronymic := model.SplitName(name)
		out.Names = append(out.Names, NameInput{
			Index: i, Type: name.Type, Given: name.First, Surname: surname, Patronymic: patronymic,
			Suffix: name.Suffix, Title: name.Title, Nick: name.Nick, Call: name.Call,
		})
	}
	// The main name comes first, as the form expects.
	slices.SortStableFunc(out.Names, func(a, b NameInput) int {
		altA, altB := bool(person.Names[a.Index].Alt), bool(person.Names[b.Index].Alt)
		switch {
		case altA == altB:
			return 0
		case altB:
			return -1
		}
		return 1
	})
	out.Events = e.editEvents(person.EventRefs)
	for _, ref := range person.Notes {
		if note := e.notes[ref.HLink]; note != nil {
			out.Notes = append(out.Notes, EditNote{
				ID: e.ID(model.KindNote, note.Handle), Rev: e.Rev(model.KindNote, note.Handle),
				Type: note.Type, Text: note.Text, Hidden: bool(note.Private),
			})
		}
	}
	for _, ref := range person.ParentIn {
		if family := e.families[ref.HLink]; family != nil {
			item := EditFamily{ID: e.ID(model.KindFamily, family.Handle), Events: e.editEvents(family.EventRefs)}
			if family.Rel != nil {
				item.Rel = family.Rel.Type
			}
			out.Families = append(out.Families, item)
		}
	}
	return out, nil
}

// PlaceOption is one place in the list that the event form offers.
type PlaceOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Places returns all places by name, for the event form.
func (e *Edit) Places() []PlaceOption {
	out := make([]PlaceOption, 0, len(e.places))
	for handle, place := range e.places {
		name := strings.TrimSpace(place.Title)
		if name == "" && len(place.Names) > 0 {
			name = place.Names[0].Value
		}
		if name != "" {
			out = append(out, PlaceOption{ID: e.ID(model.KindPlace, handle), Name: name})
		}
	}
	slices.SortFunc(out, func(a, b PlaceOption) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

// uploadDir is the folder inside the media directory for the files that
// people add in Heirfold.
const uploadDir = "heirfold"

// AddPhoto links a stored picture to a person. The picture becomes the first
// media item, so it is the portrait of the person.
func (e *Edit) AddPhoto(personID, src, mime, checksum string) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(src, uploadDir+"/") || strings.Contains(src, "..") {
		return ErrInvalid
	}
	var item *model.Media
	for handle, candidate := range e.media {
		if candidate.File.Src == src && !e.deleted[recordKey(model.KindMedia, handle)] {
			item = candidate
			break
		}
	}
	if item == nil {
		item = &model.Media{File: model.MediaFile{Src: src, Mime: mime, Checksum: checksum, Description: ""}}
		e.register(model.KindMedia, &item.Object)
		e.media[item.Handle] = item
	}
	person.MediaRefs = slices.DeleteFunc(person.MediaRefs, func(ref model.MediaRef) bool { return ref.HLink == item.Handle })
	person.MediaRefs = slices.Insert(person.MediaRefs, 0, model.MediaRef{HLink: item.Handle})
	e.touch(model.KindPerson, person.Handle)
	e.Subject, e.Created = personID, item.ID
	return nil
}

// PhotoInput says how a person shows one picture.
type PhotoInput struct {
	// Region is the part of the picture that shows the person, in percent.
	// Without it the person shows the whole picture.
	Region *model.Region `json:"region"`
	// Primary makes the picture the portrait. The portrait is the first
	// picture of the person, as in Gramps.
	Primary bool `json:"primary"`
}

func validRegion(r *model.Region) bool {
	return r.X1 >= 0 && r.Y1 >= 0 && r.X2 <= 100 && r.Y2 <= 100 && r.X1 < r.X2 && r.Y1 < r.Y2
}

// SetPhoto changes the visible part of a picture of a person, and can make
// the picture the portrait. The file itself does not change.
func (e *Edit) SetPhoto(personID, gid string, in PhotoInput) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	handle, ok := e.handleOf(model.KindMedia, gid)
	if !ok {
		return ErrNotFound
	}
	at := slices.IndexFunc(person.MediaRefs, func(ref model.MediaRef) bool { return ref.HLink == handle })
	if at < 0 {
		return ErrNotFound
	}
	region := in.Region
	if region != nil {
		if !validRegion(region) {
			return ErrInvalid
		}
		if *region == (model.Region{X2: 100, Y2: 100}) {
			region = nil
		} else {
			copied := *region
			region = &copied
		}
	}
	ref := person.MediaRefs[at]
	ref.Region = region
	person.MediaRefs[at] = ref
	if in.Primary {
		setNotPortrait(&ref, false)
		person.MediaRefs = slices.Delete(person.MediaRefs, at, at+1)
		person.MediaRefs = slices.Insert(person.MediaRefs, 0, ref)
	}
	e.touch(model.KindPerson, person.Handle)
	e.Subject = personID
	return nil
}

// ownPicture reports whether a file is a picture that somebody added for one
// person. Such a file leaves the tree with its last link. A photo of the
// gallery stays: it is in the gallery without any person.
func (e *Edit) ownPicture(item *model.Media) bool {
	return strings.HasPrefix(item.File.Src, uploadDir+"/") && !inGallery(item)
}

// mediaUsed reports whether a person, a family or an event points to the file.
func (e *Edit) mediaUsed(handle string) bool {
	has := func(refs []model.MediaRef) bool {
		return slices.ContainsFunc(refs, func(ref model.MediaRef) bool { return ref.HLink == handle })
	}
	for key, person := range e.people {
		if !e.deleted[recordKey(model.KindPerson, key)] && has(person.MediaRefs) {
			return true
		}
	}
	for key, family := range e.families {
		if !e.deleted[recordKey(model.KindFamily, key)] && has(family.MediaRefs) {
			return true
		}
	}
	for key, event := range e.events {
		if !e.deleted[recordKey(model.KindEvent, key)] && has(event.MediaRefs) {
			return true
		}
	}
	return false
}

// RemoveMedia takes a file away from a person. A file that a person added in
// Heirfold leaves the tree when nobody else shows it. The file stays on disk,
// so an undo brings the picture back.
func (e *Edit) RemoveMedia(personID, gid string) error {
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	handle, ok := e.handleOf(model.KindMedia, gid)
	if !ok {
		return ErrNotFound
	}
	if !e.dropPicture(person, handle) {
		return ErrNotFound
	}
	e.touch(model.KindPerson, person.Handle)
	if item := e.media[handle]; item != nil && e.ownPicture(item) && !e.mediaUsed(handle) {
		e.remove(model.KindMedia, handle)
	}
	e.Subject = personID
	return nil
}
