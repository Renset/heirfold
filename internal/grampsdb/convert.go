package grampsdb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// The raw types mirror the JSON documents that Gramps 6 keeps in its SQLite
// tables. The converters below give the same result as the XML export of
// Gramps, so a tree reads the same from a database and from a .gramps file.

type rawType struct {
	Value  int    `json:"value"`
	String string `json:"string"`
}

// name returns the text that Gramps XML uses for a type.
func (t typeTable) name(r rawType) string {
	if r.Value == t.custom {
		return r.String
	}
	if name, ok := t.names[r.Value]; ok {
		return name
	}
	return "Unknown"
}

type rawDate struct {
	Calendar int             `json:"calendar"`
	Modifier int             `json:"modifier"`
	Quality  int             `json:"quality"`
	Dateval  []any           `json:"dateval"`
	Text     string          `json:"text"`
	NewYear  json.RawMessage `json:"newyear"`
}

type rawBase struct {
	Handle   string `json:"handle"`
	Change   int64  `json:"change"`
	Private  bool   `json:"private"`
	GrampsID string `json:"gramps_id"`
}

type rawAttribute struct {
	Private   bool     `json:"private"`
	Type      rawType  `json:"type"`
	Value     string   `json:"value"`
	Citations []string `json:"citation_list"`
	Notes     []string `json:"note_list"`
}

type rawSrcAttribute struct {
	Private bool    `json:"private"`
	Type    rawType `json:"type"`
	Value   string  `json:"value"`
}

type rawSurname struct {
	Surname   string  `json:"surname"`
	Prefix    string  `json:"prefix"`
	Primary   bool    `json:"primary"`
	Origin    rawType `json:"origintype"`
	Connector string  `json:"connector"`
}

type rawName struct {
	Private   bool         `json:"private"`
	Surnames  []rawSurname `json:"surname_list"`
	Citations []string     `json:"citation_list"`
	Notes     []string     `json:"note_list"`
	Date      *rawDate     `json:"date"`
	First     string       `json:"first_name"`
	Suffix    string       `json:"suffix"`
	Title     string       `json:"title"`
	Type      rawType      `json:"type"`
	GroupAs   string       `json:"group_as"`
	SortAs    int          `json:"sort_as"`
	DisplayAs int          `json:"display_as"`
	Call      string       `json:"call"`
	Nick      string       `json:"nick"`
	FamNick   string       `json:"famnick"`
}

type rawEventRef struct {
	Private    bool           `json:"private"`
	Citations  []string       `json:"citation_list"`
	Notes      []string       `json:"note_list"`
	Attributes []rawAttribute `json:"attribute_list"`
	Ref        string         `json:"ref"`
	Role       rawType        `json:"role"`
}

type rawLDS struct {
	Citations []string `json:"citation_list"`
	Notes     []string `json:"note_list"`
	Date      *rawDate `json:"date"`
	Place     string   `json:"place"`
	Private   bool     `json:"private"`
	Type      int      `json:"type"`
	Family    *string  `json:"famc"`
	Temple    string   `json:"temple"`
	Status    int      `json:"status"`
}

type rawMediaRef struct {
	Private    bool           `json:"private"`
	Citations  []string       `json:"citation_list"`
	Notes      []string       `json:"note_list"`
	Attributes []rawAttribute `json:"attribute_list"`
	Ref        string         `json:"ref"`
	Rect       []*int         `json:"rect"`
}

type rawAddress struct {
	Private   bool     `json:"private"`
	Citations []string `json:"citation_list"`
	Notes     []string `json:"note_list"`
	Date      *rawDate `json:"date"`
	Street    string   `json:"street"`
	Locality  string   `json:"locality"`
	City      string   `json:"city"`
	County    string   `json:"county"`
	State     string   `json:"state"`
	Country   string   `json:"country"`
	Postal    string   `json:"postal"`
	Phone     string   `json:"phone"`
}

type rawURL struct {
	Private bool    `json:"private"`
	Path    string  `json:"path"`
	Desc    string  `json:"desc"`
	Type    rawType `json:"type"`
}

type rawPersonRef struct {
	Private   bool     `json:"private"`
	Citations []string `json:"citation_list"`
	Notes     []string `json:"note_list"`
	Ref       string   `json:"ref"`
	Rel       string   `json:"rel"`
}

type rawPerson struct {
	rawBase
	Tags           []string       `json:"tag_list"`
	Citations      []string       `json:"citation_list"`
	Notes          []string       `json:"note_list"`
	Media          []rawMediaRef  `json:"media_list"`
	EventRefs      []rawEventRef  `json:"event_ref_list"`
	Attributes     []rawAttribute `json:"attribute_list"`
	Addresses      []rawAddress   `json:"address_list"`
	URLs           []rawURL       `json:"urls"`
	LDS            []rawLDS       `json:"lds_ord_list"`
	PrimaryName    rawName        `json:"primary_name"`
	Families       []string       `json:"family_list"`
	ParentFamilies []string       `json:"parent_family_list"`
	AlternateNames []rawName      `json:"alternate_names"`
	PersonRefs     []rawPersonRef `json:"person_ref_list"`
	Gender         int            `json:"gender"`
}

type rawChildRef struct {
	Private   bool     `json:"private"`
	Citations []string `json:"citation_list"`
	Notes     []string `json:"note_list"`
	Ref       string   `json:"ref"`
	FRel      rawType  `json:"frel"`
	MRel      rawType  `json:"mrel"`
}

type rawFamily struct {
	rawBase
	Tags       []string       `json:"tag_list"`
	Citations  []string       `json:"citation_list"`
	Notes      []string       `json:"note_list"`
	Media      []rawMediaRef  `json:"media_list"`
	EventRefs  []rawEventRef  `json:"event_ref_list"`
	Attributes []rawAttribute `json:"attribute_list"`
	LDS        []rawLDS       `json:"lds_ord_list"`
	Father     *string        `json:"father_handle"`
	Mother     *string        `json:"mother_handle"`
	ChildRefs  []rawChildRef  `json:"child_ref_list"`
	Type       rawType        `json:"type"`
}

type rawEvent struct {
	rawBase
	Tags        []string       `json:"tag_list"`
	Citations   []string       `json:"citation_list"`
	Notes       []string       `json:"note_list"`
	Media       []rawMediaRef  `json:"media_list"`
	Attributes  []rawAttribute `json:"attribute_list"`
	Date        *rawDate       `json:"date"`
	Place       string         `json:"place"`
	Type        rawType        `json:"type"`
	Description string         `json:"description"`
}

type rawCitation struct {
	rawBase
	Tags       []string          `json:"tag_list"`
	Media      []rawMediaRef     `json:"media_list"`
	Notes      []string          `json:"note_list"`
	Date       *rawDate          `json:"date"`
	Source     string            `json:"source_handle"`
	Page       string            `json:"page"`
	Confidence int               `json:"confidence"`
	Attributes []rawSrcAttribute `json:"attribute_list"`
}

type rawRepoRef struct {
	Private    bool     `json:"private"`
	Notes      []string `json:"note_list"`
	Ref        string   `json:"ref"`
	CallNumber string   `json:"call_number"`
	MediaType  rawType  `json:"media_type"`
}

type rawSource struct {
	rawBase
	Tags       []string          `json:"tag_list"`
	Media      []rawMediaRef     `json:"media_list"`
	Notes      []string          `json:"note_list"`
	Attributes []rawSrcAttribute `json:"attribute_list"`
	Title      string            `json:"title"`
	Author     string            `json:"author"`
	PubInfo    string            `json:"pubinfo"`
	Abbrev     string            `json:"abbrev"`
	RepoRefs   []rawRepoRef      `json:"reporef_list"`
}

type rawPlaceName struct {
	Date  *rawDate `json:"date"`
	Value string   `json:"value"`
	Lang  string   `json:"lang"`
}

type rawPlaceRef struct {
	Ref  string   `json:"ref"`
	Date *rawDate `json:"date"`
}

type rawLocation struct {
	Street   string `json:"street"`
	Locality string `json:"locality"`
	City     string `json:"city"`
	Parish   string `json:"parish"`
	County   string `json:"county"`
	State    string `json:"state"`
	Country  string `json:"country"`
	Postal   string `json:"postal"`
	Phone    string `json:"phone"`
}

type rawPlace struct {
	rawBase
	Tags      []string       `json:"tag_list"`
	Citations []string       `json:"citation_list"`
	Notes     []string       `json:"note_list"`
	Media     []rawMediaRef  `json:"media_list"`
	URLs      []rawURL       `json:"urls"`
	Long      string         `json:"long"`
	Lat       string         `json:"lat"`
	Title     string         `json:"title"`
	Name      rawPlaceName   `json:"name"`
	AltNames  []rawPlaceName `json:"alt_names"`
	PlaceRefs []rawPlaceRef  `json:"placeref_list"`
	Type      rawType        `json:"place_type"`
	Code      string         `json:"code"`
	AltLoc    []rawLocation  `json:"alt_loc"`
}

type rawMedia struct {
	rawBase
	Tags       []string       `json:"tag_list"`
	Citations  []string       `json:"citation_list"`
	Notes      []string       `json:"note_list"`
	Date       *rawDate       `json:"date"`
	Attributes []rawAttribute `json:"attribute_list"`
	Path       string         `json:"path"`
	Mime       string         `json:"mime"`
	Desc       string         `json:"desc"`
	Checksum   string         `json:"checksum"`
}

type rawRepository struct {
	rawBase
	Tags      []string     `json:"tag_list"`
	Notes     []string     `json:"note_list"`
	Addresses []rawAddress `json:"address_list"`
	URLs      []rawURL     `json:"urls"`
	Type      rawType      `json:"type"`
	Name      string       `json:"name"`
}

type rawStyle struct {
	Name   rawType `json:"name"`
	Value  any     `json:"value"`
	Ranges [][]int `json:"ranges"`
}

type rawNote struct {
	rawBase
	Tags []string `json:"tag_list"`
	Text struct {
		Tags   []rawStyle `json:"tags"`
		String string     `json:"string"`
	} `json:"text"`
	Format int     `json:"format"`
	Type   rawType `json:"type"`
}

type rawTag struct {
	Handle   string `json:"handle"`
	Change   int64  `json:"change"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Priority int    `json:"priority"`
}

// clean removes the control characters that XML cannot hold.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

// fix removes control characters and the space around a value.
func fix(s string) string {
	return strings.TrimSpace(clean(s))
}

// link turns a Gramps handle into the form that Gramps XML uses.
func link(handle string) string {
	return "_" + handle
}

func refs(handles []string) []model.Ref {
	var out []model.Ref
	for _, handle := range handles {
		if handle != "" {
			out = append(out, model.Ref{HLink: link(handle)})
		}
	}
	return out
}

func ref(handle string) *model.Ref {
	if handle == "" {
		return nil
	}
	return &model.Ref{HLink: link(handle)}
}

func (b rawBase) object() model.Object {
	return model.Object{Handle: link(b.Handle), ID: b.GrampsID, Private: model.Flag(b.Private), Change: b.Change}
}

func datePart(values []any, i int) int {
	if i < len(values) {
		if number, ok := values[i].(float64); ok {
			return int(number)
		}
	}
	return 0
}

func dateFlag(values []any, i int) bool {
	if i < len(values) {
		switch value := values[i].(type) {
		case bool:
			return value
		case float64:
			return value != 0
		}
	}
	return false
}

// isoDate writes day, month and year the way Gramps XML does. Unknown parts
// become question marks.
func isoDate(values []any, offset int) string {
	day, month, year := datePart(values, offset), datePart(values, offset+1), datePart(values, offset+2)
	out := "????"
	if year != 0 {
		out = fmt.Sprintf("%04d", year)
	}
	switch {
	case month != 0:
		out += fmt.Sprintf("-%02d", month)
	case day != 0:
		out += "-??"
	}
	if day != 0 {
		out += fmt.Sprintf("-%02d", day)
	}
	if strings.Trim(out, "-?") == "" {
		return ""
	}
	return out
}

var calendarNames = []string{"Gregorian", "Julian", "Hebrew", "French Republican", "Persian", "Islamic", "Swedish"}

// The date modifiers of Gramps.
const (
	modBefore   = 1
	modAfter    = 2
	modAbout    = 3
	modRange    = 4
	modSpan     = 5
	modTextOnly = 6
	modFrom     = 7
	modTo       = 8
)

func newYear(raw json.RawMessage) string {
	var code int
	if err := json.Unmarshal(raw, &code); err == nil {
		switch code {
		case 0:
			return ""
		case 1:
			return "Mar1"
		case 2:
			return "Mar25"
		case 3:
			return "Sep1"
		}
		return "Err"
	}
	var pair []int
	if err := json.Unmarshal(raw, &pair); err == nil && len(pair) == 2 {
		return strconv.Itoa(pair[0]) + "-" + strconv.Itoa(pair[1])
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return "Err"
}

func dated(d *rawDate) model.Dated {
	if d == nil {
		return model.Dated{}
	}
	calendar := ""
	if d.Calendar > 0 && d.Calendar < len(calendarNames) {
		calendar = calendarNames[d.Calendar]
	}
	quality := ""
	switch d.Quality {
	case 1:
		quality = "estimated"
	case 2:
		quality = "calculated"
	}
	dual := model.Flag(dateFlag(d.Dateval, 3))
	year := newYear(d.NewYear)
	switch d.Modifier {
	case modRange, modSpan:
		start, stop := isoDate(d.Dateval, 0), isoDate(d.Dateval, 4)
		if start == "" && stop == "" {
			return model.Dated{}
		}
		pair := &model.DatePair{Start: start, Stop: stop, Quality: quality, CFormat: calendar, DualDated: dual, NewYear: year}
		if d.Modifier == modRange {
			return model.Dated{DateRange: pair}
		}
		return model.Dated{DateSpan: pair}
	case modTextOnly:
		return model.Dated{DateStr: &model.DateStr{Val: fix(d.Text)}}
	}
	value := isoDate(d.Dateval, 0)
	if value == "" {
		return model.Dated{}
	}
	kind := ""
	switch d.Modifier {
	case modBefore:
		kind = "before"
	case modAfter:
		kind = "after"
	case modAbout:
		kind = "about"
	case modFrom:
		kind = "from"
	case modTo:
		kind = "to"
	}
	return model.Dated{DateVal: &model.DateVal{Val: value, Type: kind, Quality: quality, CFormat: calendar, DualDated: dual, NewYear: year}}
}

func attributes(list []rawAttribute) []model.Attribute {
	var out []model.Attribute
	for _, a := range list {
		out = append(out, model.Attribute{
			Private: model.Flag(a.Private), Type: attributeTypes.name(a.Type), Value: fix(a.Value),
			Citations: refs(a.Citations), Notes: refs(a.Notes),
		})
	}
	return out
}

func srcAttributes(list []rawSrcAttribute) []model.SrcAttribute {
	var out []model.SrcAttribute
	for _, a := range list {
		out = append(out, model.SrcAttribute{Private: model.Flag(a.Private), Type: srcAttributeTypes.name(a.Type), Value: fix(a.Value)})
	}
	return out
}

func name(n rawName, alternative bool) model.Name {
	out := model.Name{
		Alt: model.Flag(alternative), Type: nameTypes.name(n.Type), Private: model.Flag(n.Private),
		First: fix(strings.Join(strings.FieldsFunc(n.First, func(r rune) bool { return r == '\n' || r == '\r' }), "")),
		Call:  fix(n.Call), Suffix: fix(n.Suffix), Title: fix(n.Title), Nick: fix(n.Nick),
		FamilyNick: fix(n.FamNick), Group: fix(n.GroupAs),
		Dated: dated(n.Date), Notes: refs(n.Notes), Citations: refs(n.Citations),
	}
	if n.SortAs != 0 {
		out.Sort = strconv.Itoa(n.SortAs)
	}
	if n.DisplayAs != 0 {
		out.Display = strconv.Itoa(n.DisplayAs)
	}
	for _, s := range n.Surnames {
		surname := model.Surname{
			Prefix: s.Prefix, Connector: s.Connector, Derivation: nameOriginTypes.name(s.Origin), Value: fix(s.Surname),
		}
		if !s.Primary {
			no := model.Flag(false)
			surname.Primary = &no
		}
		out.Surnames = append(out.Surnames, surname)
	}
	return out
}

func eventRefs(list []rawEventRef) []model.EventRef {
	var out []model.EventRef
	for _, r := range list {
		if r.Ref == "" {
			continue
		}
		out = append(out, model.EventRef{
			HLink: link(r.Ref), Private: model.Flag(r.Private), Role: eventRoleTypes.name(r.Role),
			Attributes: attributes(r.Attributes), Notes: refs(r.Notes), Citations: refs(r.Citations),
		})
	}
	return out
}

var ldsTypes = map[int]string{
	0: "baptism", 1: "endowment", 2: "sealed_to_parents", 3: "sealed_to_spouse", 4: "confirmation", 5: "initiatory",
}

var ldsStatuses = map[int]string{
	1: "BIC", 2: "Canceled", 3: "Child", 4: "Cleared", 5: "Completed", 6: "DNS", 7: "Infant",
	8: "Pre-1970", 9: "Qualified", 10: "DNS/CAN", 11: "Stillborn", 12: "Submitted", 13: "Uncleared",
}

func ordinances(list []rawLDS) []model.LDSOrd {
	var out []model.LDSOrd
	for _, o := range list {
		ord := model.LDSOrd{
			Private: model.Flag(o.Private), Type: ldsTypes[o.Type], Dated: dated(o.Date),
			Place: ref(o.Place), Notes: refs(o.Notes), Citations: refs(o.Citations),
		}
		if temple := fix(o.Temple); temple != "" {
			ord.Temple = &model.Val{Val: temple}
		}
		if o.Status != 0 {
			ord.Status = &model.Val{Val: ldsStatuses[o.Status]}
		}
		if o.Family != nil {
			ord.SealedTo = ref(*o.Family)
		}
		out = append(out, ord)
	}
	return out
}

func corner(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func mediaRefs(list []rawMediaRef) []model.MediaRef {
	var out []model.MediaRef
	for _, r := range list {
		item := model.MediaRef{
			HLink: link(r.Ref), Private: model.Flag(r.Private),
			Attributes: attributes(r.Attributes), Citations: refs(r.Citations), Notes: refs(r.Notes),
		}
		if len(r.Rect) == 4 {
			region := model.Region{X1: corner(r.Rect[0], 0), Y1: corner(r.Rect[1], 0), X2: corner(r.Rect[2], 100), Y2: corner(r.Rect[3], 100)}
			empty := region == model.Region{}
			whole := region == model.Region{X2: 100, Y2: 100}
			if !empty && !whole {
				item.Region = &region
			}
		}
		out = append(out, item)
	}
	return out
}

func addresses(list []rawAddress) []model.Address {
	var out []model.Address
	for _, a := range list {
		out = append(out, model.Address{
			Private: model.Flag(a.Private), Dated: dated(a.Date),
			Street: fix(a.Street), Locality: fix(a.Locality), City: fix(a.City), County: fix(a.County),
			State: fix(a.State), Country: fix(a.Country), Postal: fix(a.Postal), Phone: fix(a.Phone),
			Notes: refs(a.Notes), Citations: refs(a.Citations),
		})
	}
	return out
}

func urls(list []rawURL) []model.URL {
	var out []model.URL
	for _, u := range list {
		out = append(out, model.URL{
			Private: model.Flag(u.Private), Type: urlTypes.name(u.Type), Href: fix(u.Path), Description: fix(u.Desc),
		})
	}
	return out
}

var genders = map[int]string{0: "F", 1: "M", 3: "X"}

func person(p *rawPerson) model.Person {
	gender, ok := genders[p.Gender]
	if !ok {
		gender = "U"
	}
	out := model.Person{
		Object: p.object(), Gender: gender,
		Names:     []model.Name{name(p.PrimaryName, false)},
		EventRefs: eventRefs(p.EventRefs), LDS: ordinances(p.LDS), MediaRefs: mediaRefs(p.Media),
		Addresses: addresses(p.Addresses), Attributes: attributes(p.Attributes), URLs: urls(p.URLs),
		ChildOf: refs(p.ParentFamilies), ParentIn: refs(p.Families),
		Notes: refs(p.Notes), Citations: refs(p.Citations), Tags: refs(p.Tags),
	}
	for _, alt := range p.AlternateNames {
		out.Names = append(out.Names, name(alt, true))
	}
	for _, r := range p.PersonRefs {
		if r.Ref == "" {
			continue
		}
		out.PersonRefs = append(out.PersonRefs, model.PersonRef{
			HLink: link(r.Ref), Private: model.Flag(r.Private), Rel: r.Rel,
			Citations: refs(r.Citations), Notes: refs(r.Notes),
		})
	}
	return out
}

func family(f *rawFamily) model.Family {
	out := model.Family{
		Object:    f.object(),
		EventRefs: eventRefs(f.EventRefs), LDS: ordinances(f.LDS), MediaRefs: mediaRefs(f.Media),
		Attributes: attributes(f.Attributes), Notes: refs(f.Notes), Citations: refs(f.Citations), Tags: refs(f.Tags),
	}
	if rel := familyRelTypes.name(f.Type); rel != "" {
		out.Rel = &model.FamilyRel{Type: rel}
	}
	if f.Father != nil {
		out.Father = ref(*f.Father)
	}
	if f.Mother != nil {
		out.Mother = ref(*f.Mother)
	}
	for _, c := range f.ChildRefs {
		if c.Ref == "" {
			continue
		}
		child := model.ChildRef{
			HLink: link(c.Ref), Private: model.Flag(c.Private), Citations: refs(c.Citations), Notes: refs(c.Notes),
		}
		if c.MRel.Value != childRefTypes.initial {
			child.MRel = childRefTypes.name(c.MRel)
		}
		if c.FRel.Value != childRefTypes.initial {
			child.FRel = childRefTypes.name(c.FRel)
		}
		out.ChildRefs = append(out.ChildRefs, child)
	}
	return out
}

func event(e *rawEvent) model.Event {
	return model.Event{
		Object: e.object(), Type: fix(eventTypes.name(e.Type)), Dated: dated(e.Date), Place: ref(e.Place),
		Description: fix(e.Description), Attributes: attributes(e.Attributes),
		Notes: refs(e.Notes), Citations: refs(e.Citations), MediaRefs: mediaRefs(e.Media), Tags: refs(e.Tags),
	}
}

func citation(c *rawCitation) model.Citation {
	return model.Citation{
		Object: c.object(), Dated: dated(c.Date), Page: fix(c.Page), Confidence: c.Confidence,
		Notes: refs(c.Notes), MediaRefs: mediaRefs(c.Media), Attributes: srcAttributes(c.Attributes),
		Source: model.Ref{HLink: link(c.Source)}, Tags: refs(c.Tags),
	}
}

func source(s *rawSource) model.Source {
	out := model.Source{
		Object: s.object(), Title: fix(s.Title), Author: fix(s.Author), PubInfo: fix(s.PubInfo), Abbrev: fix(s.Abbrev),
		Notes: refs(s.Notes), MediaRefs: mediaRefs(s.Media), Attributes: srcAttributes(s.Attributes), Tags: refs(s.Tags),
	}
	for _, r := range s.RepoRefs {
		if r.Ref == "" {
			continue
		}
		out.RepoRefs = append(out.RepoRefs, model.RepoRef{
			HLink: link(r.Ref), Private: model.Flag(r.Private), CallNo: r.CallNumber,
			Medium: sourceMediaTypes.name(r.MediaType), Notes: refs(r.Notes),
		})
	}
	return out
}

func placeName(n rawPlaceName) model.PlaceName {
	return model.PlaceName{Value: fix(n.Value), Lang: fix(n.Lang), Dated: dated(n.Date)}
}

func place(p *rawPlace) model.Place {
	out := model.Place{
		Object: p.object(), Type: fix(placeTypes.name(p.Type)), Title: fix(p.Title), Code: fix(p.Code),
		Names:     []model.PlaceName{placeName(p.Name)},
		MediaRefs: mediaRefs(p.Media), URLs: urls(p.URLs),
		Notes: refs(p.Notes), Citations: refs(p.Citations), Tags: refs(p.Tags),
	}
	for _, alt := range p.AltNames {
		out.Names = append(out.Names, placeName(alt))
	}
	if long, lat := fix(p.Long), fix(p.Lat); long != "" || lat != "" {
		out.Coord = &model.Coord{Long: long, Lat: lat}
	}
	for _, r := range p.PlaceRefs {
		if r.Ref == "" {
			continue
		}
		out.PlaceRefs = append(out.PlaceRefs, model.PlaceRef{HLink: link(r.Ref), Dated: dated(r.Date)})
	}
	for _, l := range p.AltLoc {
		location := model.Location{
			Street: fix(l.Street), Locality: fix(l.Locality), City: fix(l.City), Parish: fix(l.Parish),
			County: fix(l.County), State: fix(l.State), Country: fix(l.Country), Postal: fix(l.Postal), Phone: fix(l.Phone),
		}
		if location != (model.Location{}) {
			out.Locations = append(out.Locations, location)
		}
	}
	return out
}

func media(m *rawMedia) model.Media {
	return model.Media{
		Object:     m.object(),
		File:       model.MediaFile{Src: fix(m.Path), Mime: fix(m.Mime), Checksum: m.Checksum, Description: fix(m.Desc)},
		Attributes: attributes(m.Attributes), Notes: refs(m.Notes), Dated: dated(m.Date),
		Citations: refs(m.Citations), Tags: refs(m.Tags),
	}
}

func repository(r *rawRepository) model.Repository {
	return model.Repository{
		Object: r.object(), Name: fix(r.Name), Type: fix(repositoryTypes.name(r.Type)),
		Addresses: addresses(r.Addresses), URLs: urls(r.URLs), Notes: refs(r.Notes), Tags: refs(r.Tags),
	}
}

func styleValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == 0 {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "True"
		}
	}
	return ""
}

func note(n *rawNote) model.Note {
	out := model.Note{
		Object: n.object(), Format: n.Format, Type: noteTypes.name(n.Type), Text: clean(n.Text.String), Tags: refs(n.Tags),
	}
	for _, s := range n.Text.Tags {
		style := model.NoteStyle{Name: styleTypes.name(s.Name), Value: styleValue(s.Value)}
		for _, r := range s.Ranges {
			if len(r) == 2 {
				style.Ranges = append(style.Ranges, model.StyleRange{Start: r[0], End: r[1]})
			}
		}
		out.Styles = append(out.Styles, style)
	}
	return out
}

func tag(t *rawTag) model.Tag {
	return model.Tag{Handle: link(t.Handle), Name: t.Name, Color: t.Color, Priority: t.Priority, Change: t.Change}
}
