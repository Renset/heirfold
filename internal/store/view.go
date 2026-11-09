package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// View is read access to the tree, limited to one privacy scope.
type View struct {
	db    *sql.DB
	scope privacy.Scope
}

// sees reports whether the scope can see a record or a link with this flag.
func (v *View) sees(private model.Flag) bool {
	return !bool(private) || v.scope.Private
}

// restricted reports whether the scope hides anything.
func (v *View) restricted() bool {
	return !v.scope.Private || !v.scope.Living
}

// allows reports whether the scope can see a record with these flags.
func (v *View) allows(private, living bool) bool {
	return (!private || v.scope.Private) && (!living || v.scope.Living)
}

// GraphFamily is one family in the tree graph.
type GraphFamily struct {
	ID       string          `json:"id"`
	Father   string          `json:"father,omitempty"`
	Mother   string          `json:"mother,omitempty"`
	Children []string        `json:"children,omitempty"`
	Rel      string          `json:"rel,omitempty"`
	Married  *model.DateView `json:"married,omitempty"`
}

// Graph is the complete visible structure of the tree.
type Graph struct {
	Home     string        `json:"home,omitempty"`
	People   []Card        `json:"people"`
	Families []GraphFamily `json:"families"`
}

type personRow struct {
	gid     string
	private bool
	living  bool
}

// visiblePeople returns the visible people by handle.
func (v *View) visiblePeople(ctx context.Context) (map[string]personRow, []Card, error) {
	column := "card_public"
	if v.scope.Private {
		column = "card_full"
	}
	// The column name comes from the two constants above.
	rows, err := v.db.QueryContext(ctx,
		`SELECT handle, gid, private, living, `+column+` FROM person_index WHERE (private = 0 OR ?1) AND (living = 0 OR ?2) ORDER BY gid`, // #nosec G202
		v.scope.Private, v.scope.Living)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	people := make(map[string]personRow)
	var cards []Card
	for rows.Next() {
		var handle, card string
		var row personRow
		if err := rows.Scan(&handle, &row.gid, &row.private, &row.living, &card); err != nil {
			return nil, nil, err
		}
		var c Card
		if err := json.Unmarshal([]byte(card), &c); err != nil {
			return nil, nil, err
		}
		people[handle] = row
		cards = append(cards, c)
	}
	return people, cards, rows.Err()
}

// Graph returns every visible person and family. Families keep only links
// between visible people.
func (v *View) Graph(ctx context.Context) (*Graph, error) {
	people, cards, err := v.visiblePeople(ctx)
	if err != nil {
		return nil, err
	}
	graph := &Graph{People: cards, Families: []GraphFamily{}}
	if graph.People == nil {
		graph.People = []Card{}
	}
	column := "married_public"
	if v.scope.Private {
		column = "married_full"
	}
	// The column name comes from the two constants above.
	rows, err := v.db.QueryContext(ctx,
		`SELECT handle, gid, rel, father, mother, `+column+` FROM family_index WHERE private = 0 OR ?1 ORDER BY gid`, // #nosec G202
		v.scope.Private)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type familyRow struct {
		GraphFamily
		handle string
	}
	var families []familyRow
	index := make(map[string]int)
	for rows.Next() {
		var family familyRow
		var father, mother, married string
		if err := rows.Scan(&family.handle, &family.ID, &family.Rel, &father, &mother, &married); err != nil {
			return nil, err
		}
		spouseHidden := false
		if father != "" {
			if p, ok := people[father]; ok {
				family.Father = p.gid
			} else {
				spouseHidden = true
			}
		}
		if mother != "" {
			if p, ok := people[mother]; ok {
				family.Mother = p.gid
			} else {
				spouseHidden = true
			}
		}
		// Without two visible parents the kind of the relationship is removed,
		// so a family with a hidden spouse looks like one with no known spouse.
		if v.restricted() && (family.Father == "" || family.Mother == "") {
			family.Rel = ""
		}
		if married != "" && !spouseHidden {
			var date model.DateView
			if err := json.Unmarshal([]byte(married), &date); err != nil {
				return nil, err
			}
			family.Married = &date
		}
		index[family.handle] = len(families)
		families = append(families, family)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	children, err := v.db.QueryContext(ctx,
		`SELECT family, child FROM family_child WHERE private = 0 OR ?1 ORDER BY family, ord`, v.scope.Private)
	if err != nil {
		return nil, err
	}
	defer func() { _ = children.Close() }()
	for children.Next() {
		var family, child string
		if err := children.Scan(&family, &child); err != nil {
			return nil, err
		}
		i, ok := index[family]
		if !ok {
			continue
		}
		if p, ok := people[child]; ok {
			families[i].Children = append(families[i].Children, p.gid)
		}
	}
	if err := children.Err(); err != nil {
		return nil, err
	}
	for i := range families {
		f := &families[i].GraphFamily
		visible := len(f.Children)
		if f.Father != "" {
			visible++
		}
		if f.Mother != "" {
			visible++
		}
		// One person alone is not a family. Showing that person's family would
		// reveal that a hidden relative exists.
		if visible < 2 {
			continue
		}
		graph.Families = append(graph.Families, *f)
	}

	var metaText string
	err = v.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'tree'`).Scan(&metaText)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if metaText != "" {
		var meta model.Meta
		if err := json.Unmarshal([]byte(metaText), &meta); err != nil {
			return nil, err
		}
		if p, ok := people[meta.HomeHandle]; ok {
			graph.Home = p.gid
		}
	}
	return graph, nil
}

// PlaceBrief is the short form of a place.
type PlaceBrief struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Lat  *float64 `json:"lat,omitempty"`
	Long *float64 `json:"long,omitempty"`
}

// NoteView is a note that the viewer can read.
type NoteView struct {
	ID     string            `json:"id"`
	Type   string            `json:"type,omitempty"`
	Text   string            `json:"text"`
	Styles []model.NoteStyle `json:"styles,omitempty"`
	// Hidden is true for a note that only editors and administrators read.
	Hidden bool `json:"hidden,omitempty"`
}

// CitationView is a citation with its source.
type CitationView struct {
	ID         string          `json:"id"`
	Page       string          `json:"page,omitempty"`
	Confidence int             `json:"confidence"`
	Date       *model.DateView `json:"date,omitempty"`
	SourceID   string          `json:"sourceId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Author     string          `json:"author,omitempty"`
	PubInfo    string          `json:"pubinfo,omitempty"`
	Notes      []NoteView      `json:"notes,omitempty"`
	Media      []MediaView     `json:"media,omitempty"`
}

// EventView is an event that the viewer can see.
type EventView struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Role string          `json:"role,omitempty"`
	Date *model.DateView `json:"date,omitempty"`
	// Sort orders events in time: year, month and day as one number, in the
	// Gregorian calendar. Zero means that the event has no date.
	Sort        int64       `json:"sort,omitempty"`
	Place       *PlaceBrief `json:"place,omitempty"`
	Description string      `json:"description,omitempty"`
	Cause       string      `json:"cause,omitempty"`
	Notes       []NoteView  `json:"notes,omitempty"`
	Citations   []string    `json:"citations,omitempty"`
	Media       []MediaView `json:"media,omitempty"`
}

// The places of the events that have a fixed place in a life.
const (
	rankBirth = -1
	rankDeath = 1
	rankAfter = 2
)

// eventRank says where an event stands among events of the same time: a
// birth first, a death last, a burial after the death.
func eventRank(kind string) int {
	switch kind {
	case "Birth":
		return rankBirth
	case "Death":
		return rankDeath
	case "Burial", "Cremation":
		return rankAfter
	}
	return 0
}

// OrderEvents puts events in the order of a life. A date that names only the
// year or the month stands at the start of that time, but a death and a
// burial stand at its end. An event without a date keeps its place after the
// event that it followed; a birth without a date goes first and a death
// without a date goes last. The web page uses the same rule to join the
// events of a person and of the families.
func OrderEvents(events []EventView) {
	type item struct {
		view  EventView
		key   int64
		rank  int
		index int
	}
	const last = int64(1) << 60
	items := make([]item, len(events))
	// The place of the last event that has a date or that follows such an event.
	previous, previousRank := -last+1, 0
	for i, event := range events {
		rank := eventRank(event.Type)
		key := event.Sort
		switch {
		case key != 0:
			if rank > 0 {
				// The end of the year or of the month.
				if key/100%100 == 0 {
					key += 1300
				}
				if key%100 == 0 {
					key += 32
				}
			}
			previous, previousRank = key, rank
		case rank < 0:
			key = -last
		case rank > 0:
			key = last
		default:
			key, rank = previous, previousRank
		}
		items[i] = item{view: event, key: key, rank: rank, index: i}
	}
	slices.SortStableFunc(items, func(a, b item) int {
		return cmp.Or(cmp.Compare(a.key, b.key), cmp.Compare(a.rank, b.rank), cmp.Compare(a.index, b.index))
	})
	for i := range items {
		events[i] = items[i].view
	}
}

// NameView is a personal name.
type NameView struct {
	Type       string `json:"type,omitempty"`
	Given      string `json:"given,omitempty"`
	Surname    string `json:"surname,omitempty"`
	Patronymic string `json:"patronymic,omitempty"`
	Nick       string `json:"nick,omitempty"`
	Title      string `json:"title,omitempty"`
	Suffix     string `json:"suffix,omitempty"`
}

// KV is a typed value.
type KV struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// LinkView is a web address.
type LinkView struct {
	Href        string `json:"href"`
	Description string `json:"description,omitempty"`
}

// TagView is a tag.
type TagView struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Parents is one family in which the person is a child.
type Parents struct {
	FamilyID string `json:"familyId"`
	Father   *Card  `json:"father,omitempty"`
	Mother   *Card  `json:"mother,omitempty"`
	Siblings []Card `json:"siblings,omitempty"`
}

// Union is one family in which the person is a parent.
type Union struct {
	FamilyID string      `json:"familyId"`
	Rel      string      `json:"rel,omitempty"`
	Spouse   *Card       `json:"spouse,omitempty"`
	Events   []EventView `json:"events,omitempty"`
	Children []Card      `json:"children,omitempty"`
	Notes    []NoteView  `json:"notes,omitempty"`
	Media    []MediaView `json:"media,omitempty"`
}

// Association links the person to someone outside the family.
type Association struct {
	Rel    string `json:"rel,omitempty"`
	Person Card   `json:"person"`
}

// PersonDetail holds all facts of one person that the viewer can see.
type PersonDetail struct {
	Card
	Private      bool           `json:"private,omitempty"`
	Names        []NameView     `json:"names,omitempty"`
	Events       []EventView    `json:"events,omitempty"`
	Parents      []Parents      `json:"parents,omitempty"`
	Unions       []Union        `json:"unions,omitempty"`
	Associations []Association  `json:"associations,omitempty"`
	Attributes   []KV           `json:"attributes,omitempty"`
	Links        []LinkView     `json:"links,omitempty"`
	Notes        []NoteView     `json:"notes,omitempty"`
	Citations    []CitationView `json:"citations,omitempty"`
	Media        []MediaView    `json:"media,omitempty"`
	Tags         []TagView      `json:"tags,omitempty"`
}

// load reads one object. It returns false if the object does not exist or the
// scope hides it.
func load[T any](ctx context.Context, v *View, kind model.Kind, handle string, out *T) (string, bool, error) {
	var gid, doc string
	var private bool
	err := v.db.QueryRowContext(ctx, `SELECT gid, private, doc FROM object WHERE kind = ? AND handle = ?`, string(kind), handle).Scan(&gid, &private, &doc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if private && !v.scope.Private {
		return "", false, nil
	}
	if err := json.Unmarshal([]byte(doc), out); err != nil {
		return "", false, err
	}
	return gid, true, nil
}

// card returns the short form of a person, or nil if the scope hides that person.
func (v *View) card(ctx context.Context, handle string) (*Card, error) {
	column := "card_public"
	if v.scope.Private {
		column = "card_full"
	}
	var private, living bool
	var text string
	// The column name comes from the two constants above.
	err := v.db.QueryRowContext(ctx,
		`SELECT private, living, `+column+` FROM person_index WHERE handle = ?`, handle).Scan(&private, &living, &text) // #nosec G202
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !v.allows(private, living) {
		return nil, nil
	}
	var c Card
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (v *View) notes(ctx context.Context, refs []model.Ref) ([]NoteView, error) {
	var out []NoteView
	for _, ref := range refs {
		var note model.Note
		gid, ok, err := load(ctx, v, model.KindNote, ref.HLink, &note)
		if err != nil {
			return nil, err
		}
		// A hidden note is for the people who can change the whole tree.
		if ok && bool(note.Private) && !v.scope.Notes {
			continue
		}
		if ok {
			view := NoteView{ID: gid, Type: note.Type, Text: note.Text, Hidden: bool(note.Private)}
			// A style can link to a hidden record, so only full scopes get styles.
			if v.scope.Private {
				view.Styles = note.Styles
			}
			out = append(out, view)
		}
	}
	return out, nil
}

func (v *View) place(ctx context.Context, ref *model.Ref) (*PlaceBrief, error) {
	if ref == nil {
		return nil, nil
	}
	var place model.Place
	gid, ok, err := load(ctx, v, model.KindPlace, ref.HLink, &place)
	if err != nil || !ok {
		return nil, err
	}
	brief := &PlaceBrief{ID: gid, Name: strings.TrimSpace(place.Title)}
	if lat, long, valid := model.PlaceCoord(place.Coord); valid {
		brief.Lat, brief.Long = &lat, &long
	}
	if brief.Name != "" {
		return brief, nil
	}
	// Without a title, join the names of the enclosing places.
	var parts []string
	current := place
	for depth := 0; depth < 8; depth++ {
		if len(current.Names) > 0 {
			parts = append(parts, current.Names[0].Value)
		}
		if len(current.PlaceRefs) == 0 {
			break
		}
		var next model.Place
		_, found, err := load(ctx, v, model.KindPlace, current.PlaceRefs[0].HLink, &next)
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		current = next
	}
	brief.Name = strings.Join(parts, ", ")
	return brief, nil
}

// citationSet collects citations and removes duplicates.
type citationSet struct {
	byHandle map[string]string
	list     []CitationView
}

func (c *citationSet) add(ctx context.Context, v *View, refs []model.Ref) ([]string, error) {
	var ids []string
	for _, ref := range refs {
		if id, seen := c.byHandle[ref.HLink]; seen {
			if id != "" {
				ids = append(ids, id)
			}
			continue
		}
		var citation model.Citation
		gid, ok, err := load(ctx, v, model.KindCitation, ref.HLink, &citation)
		if err != nil {
			return nil, err
		}
		if !ok {
			c.byHandle[ref.HLink] = ""
			continue
		}
		var source model.Source
		sourceID, sourceOK, err := load(ctx, v, model.KindSource, citation.Source.HLink, &source)
		if err != nil {
			return nil, err
		}
		if !sourceOK {
			// A citation of a hidden source would reveal that source.
			c.byHandle[ref.HLink] = ""
			continue
		}
		c.byHandle[ref.HLink] = gid
		view := CitationView{
			ID: gid, Page: citation.Page, Confidence: citation.Confidence, Date: citation.View(),
			SourceID: sourceID, Title: source.Title, Author: source.Author, PubInfo: source.PubInfo,
		}
		// The scans of the cited page come first, then the files of the whole source.
		if view.Media, err = v.media(ctx, append(slices.Clone(citation.MediaRefs), source.MediaRefs...)); err != nil {
			return nil, err
		}
		if view.Notes, err = v.notes(ctx, citation.Notes); err != nil {
			return nil, err
		}
		c.list = append(c.list, view)
		ids = append(ids, gid)
	}
	return ids, nil
}

func (v *View) events(ctx context.Context, refs []model.EventRef, citations *citationSet) ([]EventView, error) {
	var out []EventView
	for _, ref := range refs {
		if !v.sees(ref.Private) {
			continue
		}
		var event model.Event
		gid, ok, err := load(ctx, v, model.KindEvent, ref.HLink, &event)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		view := EventView{
			ID: gid, Type: event.Type, Role: ref.Role, Date: event.View(),
			Description: event.Description, Cause: event.Cause,
		}
		view.Sort = view.Date.SortKey()
		if view.Place, err = v.place(ctx, event.Place); err != nil {
			return nil, err
		}
		if view.Notes, err = v.notes(ctx, append(slices.Clone(ref.Notes), event.Notes...)); err != nil {
			return nil, err
		}
		if view.Citations, err = citations.add(ctx, v, append(slices.Clone(ref.Citations), event.Citations...)); err != nil {
			return nil, err
		}
		if view.Media, err = v.media(ctx, event.MediaRefs); err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	OrderEvents(out)
	return out, nil
}

// Person returns the details of one person.
func (v *View) Person(ctx context.Context, gid string) (*PersonDetail, error) {
	var handle string
	var private, living bool
	err := v.db.QueryRowContext(ctx, `SELECT handle, private, living FROM person_index WHERE gid = ?`, gid).Scan(&handle, &private, &living)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !v.allows(private, living) {
		return nil, ErrNotFound
	}
	var person model.Person
	if _, ok, err := load(ctx, v, model.KindPerson, handle, &person); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	card, err := v.card(ctx, handle)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, ErrNotFound
	}
	detail := &PersonDetail{Card: *card, Private: private}
	citations := &citationSet{byHandle: map[string]string{}}

	for _, name := range person.Names {
		if !v.sees(name.Private) {
			continue
		}
		surname, patronymic := model.SplitName(name)
		detail.Names = append(detail.Names, NameView{
			Type: name.Type, Given: strings.TrimSpace(name.First), Surname: surname, Patronymic: patronymic,
			Nick: name.Nick, Title: name.Title, Suffix: name.Suffix,
		})
	}
	if detail.Events, err = v.events(ctx, person.EventRefs, citations); err != nil {
		return nil, err
	}
	for _, ref := range person.ChildOf {
		parents, err := v.parents(ctx, ref.HLink, handle)
		if err != nil {
			return nil, err
		}
		if parents != nil {
			detail.Parents = append(detail.Parents, *parents)
		}
	}
	for _, ref := range person.ParentIn {
		union, err := v.union(ctx, ref.HLink, handle, citations)
		if err != nil {
			return nil, err
		}
		if union != nil {
			detail.Unions = append(detail.Unions, *union)
		}
	}
	for _, ref := range person.PersonRefs {
		if !v.sees(ref.Private) {
			continue
		}
		other, err := v.card(ctx, ref.HLink)
		if err != nil {
			return nil, err
		}
		if other != nil {
			detail.Associations = append(detail.Associations, Association{Rel: ref.Rel, Person: *other})
		}
	}
	for _, attr := range person.Attributes {
		if !v.sees(attr.Private) {
			continue
		}
		detail.Attributes = append(detail.Attributes, KV{Type: attr.Type, Value: attr.Value})
	}
	for _, link := range person.URLs {
		if !v.sees(link.Private) {
			continue
		}
		if safe := safeLink(link.Href); safe != "" {
			detail.Links = append(detail.Links, LinkView{Href: safe, Description: link.Description})
		}
	}
	if detail.Notes, err = v.notes(ctx, person.Notes); err != nil {
		return nil, err
	}
	if _, err := citations.add(ctx, v, person.Citations); err != nil {
		return nil, err
	}
	detail.Citations = citations.list
	if detail.Media, err = v.media(ctx, person.MediaRefs); err != nil {
		return nil, err
	}
	for _, ref := range person.Tags {
		var tag model.Tag
		if _, ok, err := load(ctx, v, model.KindTag, ref.HLink, &tag); err != nil {
			return nil, err
		} else if ok {
			detail.Tags = append(detail.Tags, TagView{Name: tag.Name, Color: tag.Color})
		}
	}
	return detail, nil
}

// safeLink keeps only web and mail links. Other schemes can run code.
func safeLink(href string) string {
	href = strings.TrimSpace(href)
	lower := strings.ToLower(href)
	for _, prefix := range []string{"https://", "http://", "mailto:"} {
		if strings.HasPrefix(lower, prefix) {
			return href
		}
	}
	return ""
}

func (v *View) parents(ctx context.Context, familyHandle, self string) (*Parents, error) {
	var family model.Family
	gid, ok, err := load(ctx, v, model.KindFamily, familyHandle, &family)
	if err != nil || !ok {
		return nil, err
	}
	for _, child := range family.ChildRefs {
		if child.HLink == self && !v.sees(child.Private) {
			return nil, nil
		}
	}
	out := &Parents{FamilyID: gid}
	if family.Father != nil {
		if out.Father, err = v.card(ctx, family.Father.HLink); err != nil {
			return nil, err
		}
	}
	if family.Mother != nil {
		if out.Mother, err = v.card(ctx, family.Mother.HLink); err != nil {
			return nil, err
		}
	}
	for _, child := range family.ChildRefs {
		if child.HLink == self || !v.sees(child.Private) {
			continue
		}
		c, err := v.card(ctx, child.HLink)
		if err != nil {
			return nil, err
		}
		if c != nil {
			out.Siblings = append(out.Siblings, *c)
		}
	}
	if out.Father == nil && out.Mother == nil && len(out.Siblings) == 0 {
		return nil, nil
	}
	return out, nil
}

func (v *View) union(ctx context.Context, familyHandle, self string, citations *citationSet) (*Union, error) {
	var family model.Family
	gid, ok, err := load(ctx, v, model.KindFamily, familyHandle, &family)
	if err != nil || !ok {
		return nil, err
	}
	out := &Union{FamilyID: gid}
	if family.Rel != nil {
		out.Rel = family.Rel.Type
	}
	spouseHidden := false
	for _, ref := range []*model.Ref{family.Father, family.Mother} {
		if ref == nil || ref.HLink == self {
			continue
		}
		card, err := v.card(ctx, ref.HLink)
		if err != nil {
			return nil, err
		}
		if card == nil {
			spouseHidden = true
			continue
		}
		out.Spouse = card
	}
	if v.restricted() && out.Spouse == nil {
		out.Rel = ""
	}
	// The events and notes of a couple describe both people, so a hidden spouse
	// hides them too.
	if !spouseHidden {
		if out.Events, err = v.events(ctx, family.EventRefs, citations); err != nil {
			return nil, err
		}
		if out.Notes, err = v.notes(ctx, family.Notes); err != nil {
			return nil, err
		}
		if out.Media, err = v.media(ctx, family.MediaRefs); err != nil {
			return nil, err
		}
	}
	childHidden := false
	for _, child := range family.ChildRefs {
		if !v.sees(child.Private) {
			childHidden = true
			continue
		}
		c, err := v.card(ctx, child.HLink)
		if err != nil {
			return nil, err
		}
		if c == nil {
			childHidden = true
			continue
		}
		out.Children = append(out.Children, *c)
	}
	// With only hidden relatives left, the family would reveal that they exist.
	if out.Spouse == nil && len(out.Children) == 0 && (spouseHidden || childHidden) {
		return nil, nil
	}
	return out, nil
}
