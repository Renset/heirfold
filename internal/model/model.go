// Package model holds the genealogical objects of a family tree.
//
// The structs follow the Gramps XML 1.7 element order, so one definition
// serves three purposes: it reads Gramps XML, it writes Gramps XML, and it is
// the JSON document that the store keeps. The JSON form keeps every field,
// which makes an export lossless.
package model

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// Flag is a boolean that Gramps XML writes as 0 or 1.
type Flag bool

// MarshalXMLAttr writes the flag as 0 or 1.
func (f Flag) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	if f {
		return xml.Attr{Name: name, Value: "1"}, nil
	}
	return xml.Attr{Name: name, Value: "0"}, nil
}

// UnmarshalXMLAttr reads 0, 1, false or true.
func (f *Flag) UnmarshalXMLAttr(attr xml.Attr) error {
	switch attr.Value {
	case "1", "true":
		*f = true
	case "0", "false", "":
		*f = false
	default:
		return fmt.Errorf("model: bad flag value for %s", attr.Name.Local)
	}
	return nil
}

// Kind names one of the object types.
type Kind string

// The object types of a tree.
const (
	KindPerson     Kind = "person"
	KindFamily     Kind = "family"
	KindEvent      Kind = "event"
	KindPlace      Kind = "place"
	KindSource     Kind = "source"
	KindCitation   Kind = "citation"
	KindRepository Kind = "repository"
	KindNote       Kind = "note"
	KindMedia      Kind = "media"
	KindTag        Kind = "tag"
)

// Kinds lists all object types in a stable order.
var Kinds = []Kind{
	KindTag, KindEvent, KindPerson, KindFamily, KindCitation, KindSource,
	KindPlace, KindMedia, KindRepository, KindNote,
}

// Object holds the fields that every primary object has.
type Object struct {
	Handle  string `xml:"handle,attr" json:"handle"`
	ID      string `xml:"id,attr,omitempty" json:"id,omitempty"`
	Private Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Change  int64  `xml:"change,attr" json:"change"`
}

// Ref links to another object by its handle.
type Ref struct {
	HLink string `xml:"hlink,attr" json:"h"`
}

// DateVal is a single date, optionally before, after or about it.
type DateVal struct {
	Val       string `xml:"val,attr" json:"val"`
	Type      string `xml:"type,attr,omitempty" json:"type,omitempty"`
	Quality   string `xml:"quality,attr,omitempty" json:"quality,omitempty"`
	CFormat   string `xml:"cformat,attr,omitempty" json:"cformat,omitempty"`
	DualDated Flag   `xml:"dualdated,attr,omitempty" json:"dualdated,omitempty"`
	NewYear   string `xml:"newyear,attr,omitempty" json:"newyear,omitempty"`
}

// DatePair is a date range or a date span.
type DatePair struct {
	Start     string `xml:"start,attr" json:"start"`
	Stop      string `xml:"stop,attr" json:"stop"`
	Quality   string `xml:"quality,attr,omitempty" json:"quality,omitempty"`
	CFormat   string `xml:"cformat,attr,omitempty" json:"cformat,omitempty"`
	DualDated Flag   `xml:"dualdated,attr,omitempty" json:"dualdated,omitempty"`
	NewYear   string `xml:"newyear,attr,omitempty" json:"newyear,omitempty"`
}

// DateStr is free text that Gramps could not read as a date.
type DateStr struct {
	Val string `xml:"val,attr" json:"val"`
}

// Dated holds the optional date of an object. At most one field is set.
type Dated struct {
	DateRange *DatePair `xml:"daterange,omitempty" json:"daterange,omitempty"`
	DateSpan  *DatePair `xml:"datespan,omitempty" json:"datespan,omitempty"`
	DateVal   *DateVal  `xml:"dateval,omitempty" json:"dateval,omitempty"`
	DateStr   *DateStr  `xml:"datestr,omitempty" json:"datestr,omitempty"`
}

// HasDate reports whether the object has any date.
func (d Dated) HasDate() bool {
	return d.DateRange != nil || d.DateSpan != nil || d.DateVal != nil || d.DateStr != nil
}

// Attribute is a typed key and value with optional evidence.
type Attribute struct {
	Private   Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Type      string `xml:"type,attr" json:"type"`
	Value     string `xml:"value,attr" json:"value"`
	Citations []Ref  `xml:"citationref,omitempty" json:"citations,omitempty"`
	Notes     []Ref  `xml:"noteref,omitempty" json:"notes,omitempty"`
}

// SrcAttribute is an attribute of a source or a citation.
type SrcAttribute struct {
	Private Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Type    string `xml:"type,attr" json:"type"`
	Value   string `xml:"value,attr" json:"value"`
}

// Surname is one part of a family name.
type Surname struct {
	Prefix     string `xml:"prefix,attr,omitempty" json:"prefix,omitempty"`
	Primary    *Flag  `xml:"prim,attr,omitempty" json:"prim,omitempty"`
	Derivation string `xml:"derivation,attr,omitempty" json:"derivation,omitempty"`
	Connector  string `xml:"connector,attr,omitempty" json:"connector,omitempty"`
	Value      string `xml:",chardata" json:"value"`
}

// Name is a personal name.
type Name struct {
	Alt        Flag      `xml:"alt,attr,omitempty" json:"alt,omitempty"`
	Type       string    `xml:"type,attr,omitempty" json:"type,omitempty"`
	Private    Flag      `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Sort       string    `xml:"sort,attr,omitempty" json:"sort,omitempty"`
	Display    string    `xml:"display,attr,omitempty" json:"display,omitempty"`
	First      string    `xml:"first,omitempty" json:"first,omitempty"`
	Call       string    `xml:"call,omitempty" json:"call,omitempty"`
	Surnames   []Surname `xml:"surname,omitempty" json:"surnames,omitempty"`
	Suffix     string    `xml:"suffix,omitempty" json:"suffix,omitempty"`
	Title      string    `xml:"title,omitempty" json:"title,omitempty"`
	Nick       string    `xml:"nick,omitempty" json:"nick,omitempty"`
	FamilyNick string    `xml:"familynick,omitempty" json:"familynick,omitempty"`
	Group      string    `xml:"group,omitempty" json:"group,omitempty"`
	Dated
	Notes     []Ref `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations []Ref `xml:"citationref,omitempty" json:"citations,omitempty"`
}

// EventRef links a person or a family to an event.
type EventRef struct {
	HLink      string      `xml:"hlink,attr" json:"h"`
	Private    Flag        `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Role       string      `xml:"role,attr,omitempty" json:"role,omitempty"`
	Attributes []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	Notes      []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations  []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
}

// Val is an element that holds its value in a val attribute.
type Val struct {
	Val string `xml:"val,attr" json:"val"`
}

// LDSOrd is a Latter-day Saints ordinance. Heirfold stores it for lossless
// export and does not show it yet.
type LDSOrd struct {
	Private Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Type    string `xml:"type,attr" json:"type"`
	Dated
	Temple    *Val  `xml:"temple,omitempty" json:"temple,omitempty"`
	Place     *Ref  `xml:"place,omitempty" json:"place,omitempty"`
	Status    *Val  `xml:"status,omitempty" json:"status,omitempty"`
	SealedTo  *Ref  `xml:"sealed_to,omitempty" json:"sealedTo,omitempty"`
	Notes     []Ref `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations []Ref `xml:"citationref,omitempty" json:"citations,omitempty"`
}

// Region is a rectangle in an image, in percent of its size.
type Region struct {
	X1 int `xml:"corner1_x,attr" json:"x1"`
	Y1 int `xml:"corner1_y,attr" json:"y1"`
	X2 int `xml:"corner2_x,attr" json:"x2"`
	Y2 int `xml:"corner2_y,attr" json:"y2"`
}

// MediaRef links an object to a media object.
type MediaRef struct {
	HLink      string      `xml:"hlink,attr" json:"h"`
	Private    Flag        `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Region     *Region     `xml:"region,omitempty" json:"region,omitempty"`
	Attributes []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	Citations  []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
	Notes      []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
}

// Address is a postal address.
type Address struct {
	Private Flag `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Dated
	Street    string `xml:"street,omitempty" json:"street,omitempty"`
	Locality  string `xml:"locality,omitempty" json:"locality,omitempty"`
	City      string `xml:"city,omitempty" json:"city,omitempty"`
	County    string `xml:"county,omitempty" json:"county,omitempty"`
	State     string `xml:"state,omitempty" json:"state,omitempty"`
	Country   string `xml:"country,omitempty" json:"country,omitempty"`
	Postal    string `xml:"postal,omitempty" json:"postal,omitempty"`
	Phone     string `xml:"phone,omitempty" json:"phone,omitempty"`
	Notes     []Ref  `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations []Ref  `xml:"citationref,omitempty" json:"citations,omitempty"`
}

// URL is a web address.
type URL struct {
	Private     Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Type        string `xml:"type,attr,omitempty" json:"type,omitempty"`
	Href        string `xml:"href,attr" json:"href"`
	Description string `xml:"description,attr,omitempty" json:"description,omitempty"`
}

// PersonRef is an association between two people.
type PersonRef struct {
	HLink     string `xml:"hlink,attr" json:"h"`
	Private   Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	Rel       string `xml:"rel,attr" json:"rel"`
	Citations []Ref  `xml:"citationref,omitempty" json:"citations,omitempty"`
	Notes     []Ref  `xml:"noteref,omitempty" json:"notes,omitempty"`
}

// Person is one individual.
type Person struct {
	Object
	Gender     string      `xml:"gender" json:"gender"`
	Names      []Name      `xml:"name,omitempty" json:"names,omitempty"`
	EventRefs  []EventRef  `xml:"eventref,omitempty" json:"eventRefs,omitempty"`
	LDS        []LDSOrd    `xml:"lds_ord,omitempty" json:"lds,omitempty"`
	MediaRefs  []MediaRef  `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	Addresses  []Address   `xml:"address,omitempty" json:"addresses,omitempty"`
	Attributes []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	URLs       []URL       `xml:"url,omitempty" json:"urls,omitempty"`
	ChildOf    []Ref       `xml:"childof,omitempty" json:"childOf,omitempty"`
	ParentIn   []Ref       `xml:"parentin,omitempty" json:"parentIn,omitempty"`
	PersonRefs []PersonRef `xml:"personref,omitempty" json:"personRefs,omitempty"`
	Notes      []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations  []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
	Tags       []Ref       `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// PrimaryName returns the preferred name, or an empty name.
func (p *Person) PrimaryName() Name {
	for _, n := range p.Names {
		if !n.Alt {
			return n
		}
	}
	if len(p.Names) > 0 {
		return p.Names[0]
	}
	return Name{}
}

// SplitName returns the primary surname and the patronymic of a name.
// A patronymic is a surname part with the derivation Patronymic or Matronymic.
func SplitName(n Name) (surname, patronymic string) {
	first := ""
	for _, s := range n.Surnames {
		value := strings.TrimSpace(s.Value)
		if value == "" {
			continue
		}
		if s.Derivation == "Patronymic" || s.Derivation == "Matronymic" {
			if patronymic == "" {
				patronymic = value
			}
			continue
		}
		full := value
		if s.Prefix != "" {
			full = s.Prefix + " " + value
		}
		if first == "" {
			first = full
		}
		if (s.Primary == nil || bool(*s.Primary)) && surname == "" {
			surname = full
		}
	}
	if surname == "" {
		surname = first
	}
	return surname, patronymic
}

// ChildRef links a family to a child.
type ChildRef struct {
	HLink     string `xml:"hlink,attr" json:"h"`
	Private   Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	MRel      string `xml:"mrel,attr,omitempty" json:"mrel,omitempty"`
	FRel      string `xml:"frel,attr,omitempty" json:"frel,omitempty"`
	Citations []Ref  `xml:"citationref,omitempty" json:"citations,omitempty"`
	Notes     []Ref  `xml:"noteref,omitempty" json:"notes,omitempty"`
}

// FamilyRel is the relationship of a couple.
type FamilyRel struct {
	Type string `xml:"type,attr" json:"type"`
}

// Family is a couple and their children.
type Family struct {
	Object
	Rel        *FamilyRel  `xml:"rel,omitempty" json:"rel,omitempty"`
	Father     *Ref        `xml:"father,omitempty" json:"father,omitempty"`
	Mother     *Ref        `xml:"mother,omitempty" json:"mother,omitempty"`
	EventRefs  []EventRef  `xml:"eventref,omitempty" json:"eventRefs,omitempty"`
	LDS        []LDSOrd    `xml:"lds_ord,omitempty" json:"lds,omitempty"`
	MediaRefs  []MediaRef  `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	ChildRefs  []ChildRef  `xml:"childref,omitempty" json:"childRefs,omitempty"`
	Attributes []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	Notes      []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations  []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
	Tags       []Ref       `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// Event is something that happened at a time and a place.
type Event struct {
	Object
	Type string `xml:"type,omitempty" json:"type,omitempty"`
	Dated
	Place       *Ref        `xml:"place,omitempty" json:"place,omitempty"`
	Cause       string      `xml:"cause,omitempty" json:"cause,omitempty"`
	Description string      `xml:"description,omitempty" json:"description,omitempty"`
	Attributes  []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	Notes       []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations   []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
	MediaRefs   []MediaRef  `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	Tags        []Ref       `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// RepoRef links a source to a repository.
type RepoRef struct {
	HLink   string `xml:"hlink,attr" json:"h"`
	Private Flag   `xml:"priv,attr,omitempty" json:"priv,omitempty"`
	CallNo  string `xml:"callno,attr,omitempty" json:"callno,omitempty"`
	Medium  string `xml:"medium,attr,omitempty" json:"medium,omitempty"`
	Notes   []Ref  `xml:"noteref,omitempty" json:"notes,omitempty"`
}

// Source is a document, a book or another origin of information.
type Source struct {
	Object
	Title      string         `xml:"stitle,omitempty" json:"title,omitempty"`
	Author     string         `xml:"sauthor,omitempty" json:"author,omitempty"`
	PubInfo    string         `xml:"spubinfo,omitempty" json:"pubinfo,omitempty"`
	Abbrev     string         `xml:"sabbrev,omitempty" json:"abbrev,omitempty"`
	Notes      []Ref          `xml:"noteref,omitempty" json:"notes,omitempty"`
	MediaRefs  []MediaRef     `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	Attributes []SrcAttribute `xml:"srcattribute,omitempty" json:"attributes,omitempty"`
	RepoRefs   []RepoRef      `xml:"reporef,omitempty" json:"repoRefs,omitempty"`
	Tags       []Ref          `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// Citation points to a location within a source.
type Citation struct {
	Object
	Dated
	Page       string         `xml:"page,omitempty" json:"page,omitempty"`
	Confidence int            `xml:"confidence" json:"confidence"`
	Notes      []Ref          `xml:"noteref,omitempty" json:"notes,omitempty"`
	MediaRefs  []MediaRef     `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	Attributes []SrcAttribute `xml:"srcattribute,omitempty" json:"attributes,omitempty"`
	Source     Ref            `xml:"sourceref" json:"source"`
	Tags       []Ref          `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// PlaceName is one name of a place, optionally limited in time.
type PlaceName struct {
	Value string `xml:"value,attr" json:"value"`
	Lang  string `xml:"lang,attr,omitempty" json:"lang,omitempty"`
	Dated
}

// Coord holds the coordinates of a place as Gramps stores them.
type Coord struct {
	Long string `xml:"long,attr" json:"long"`
	Lat  string `xml:"lat,attr" json:"lat"`
}

// PlaceRef links a place to the place that encloses it.
type PlaceRef struct {
	HLink string `xml:"hlink,attr" json:"h"`
	Dated
}

// Location is an alternative location of a place.
type Location struct {
	Street   string `xml:"street,attr,omitempty" json:"street,omitempty"`
	Locality string `xml:"locality,attr,omitempty" json:"locality,omitempty"`
	City     string `xml:"city,attr,omitempty" json:"city,omitempty"`
	Parish   string `xml:"parish,attr,omitempty" json:"parish,omitempty"`
	County   string `xml:"county,attr,omitempty" json:"county,omitempty"`
	State    string `xml:"state,attr,omitempty" json:"state,omitempty"`
	Country  string `xml:"country,attr,omitempty" json:"country,omitempty"`
	Postal   string `xml:"postal,attr,omitempty" json:"postal,omitempty"`
	Phone    string `xml:"phone,attr,omitempty" json:"phone,omitempty"`
}

// Place is a geographic location.
type Place struct {
	Object
	Type      string      `xml:"type,attr" json:"type"`
	Title     string      `xml:"ptitle,omitempty" json:"title,omitempty"`
	Code      string      `xml:"code,omitempty" json:"code,omitempty"`
	Names     []PlaceName `xml:"pname,omitempty" json:"names,omitempty"`
	Coord     *Coord      `xml:"coord,omitempty" json:"coord,omitempty"`
	PlaceRefs []PlaceRef  `xml:"placeref,omitempty" json:"placeRefs,omitempty"`
	Locations []Location  `xml:"location,omitempty" json:"locations,omitempty"`
	MediaRefs []MediaRef  `xml:"objref,omitempty" json:"mediaRefs,omitempty"`
	URLs      []URL       `xml:"url,omitempty" json:"urls,omitempty"`
	Notes     []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Citations []Ref       `xml:"citationref,omitempty" json:"citations,omitempty"`
	Tags      []Ref       `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// MediaFile describes the file of a media object.
type MediaFile struct {
	Src         string `xml:"src,attr" json:"src"`
	Mime        string `xml:"mime,attr" json:"mime"`
	Checksum    string `xml:"checksum,attr,omitempty" json:"checksum,omitempty"`
	Description string `xml:"description,attr" json:"description"`
}

// Media is a photo, a scan or another file.
type Media struct {
	Object
	File       MediaFile   `xml:"file" json:"file"`
	Attributes []Attribute `xml:"attribute,omitempty" json:"attributes,omitempty"`
	Notes      []Ref       `xml:"noteref,omitempty" json:"notes,omitempty"`
	Dated
	Citations []Ref `xml:"citationref,omitempty" json:"citations,omitempty"`
	Tags      []Ref `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// Repository is an archive or a library.
type Repository struct {
	Object
	Name      string    `xml:"rname" json:"name"`
	Type      string    `xml:"type" json:"type"`
	Addresses []Address `xml:"address,omitempty" json:"addresses,omitempty"`
	URLs      []URL     `xml:"url,omitempty" json:"urls,omitempty"`
	Notes     []Ref     `xml:"noteref,omitempty" json:"notes,omitempty"`
	Tags      []Ref     `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// StyleRange is a span of characters.
type StyleRange struct {
	Start int `xml:"start,attr" json:"start"`
	End   int `xml:"end,attr" json:"end"`
}

// NoteStyle marks parts of a note as bold, italic or a link.
type NoteStyle struct {
	Name   string       `xml:"name,attr" json:"name"`
	Value  string       `xml:"value,attr,omitempty" json:"value,omitempty"`
	Ranges []StyleRange `xml:"range" json:"ranges"`
}

// Note is free text.
type Note struct {
	Object
	Format int         `xml:"format,attr,omitempty" json:"format,omitempty"`
	Type   string      `xml:"type,attr" json:"type"`
	Text   string      `xml:"text" json:"text"`
	Styles []NoteStyle `xml:"style,omitempty" json:"styles,omitempty"`
	Tags   []Ref       `xml:"tagref,omitempty" json:"tags,omitempty"`
}

// Tag is a named colored label.
type Tag struct {
	Handle   string `xml:"handle,attr" json:"handle"`
	Name     string `xml:"name,attr" json:"name"`
	Color    string `xml:"color,attr" json:"color"`
	Priority int    `xml:"priority,attr" json:"priority"`
	Change   int64  `xml:"change,attr" json:"change"`
}

// Researcher names the person who keeps the tree.
type Researcher struct {
	Name     string `xml:"resname,omitempty" json:"name,omitempty"`
	Addr     string `xml:"resaddr,omitempty" json:"addr,omitempty"`
	Locality string `xml:"reslocality,omitempty" json:"locality,omitempty"`
	City     string `xml:"rescity,omitempty" json:"city,omitempty"`
	State    string `xml:"resstate,omitempty" json:"state,omitempty"`
	Country  string `xml:"rescountry,omitempty" json:"country,omitempty"`
	Postal   string `xml:"respostal,omitempty" json:"postal,omitempty"`
	Phone    string `xml:"resphone,omitempty" json:"phone,omitempty"`
	Email    string `xml:"resemail,omitempty" json:"email,omitempty"`
}

// Created holds the export date and the Gramps version.
type Created struct {
	Date    string `xml:"date,attr" json:"date"`
	Version string `xml:"version,attr" json:"version"`
}

// Header holds the file metadata.
type Header struct {
	Created    Created     `xml:"created" json:"created"`
	Researcher *Researcher `xml:"researcher,omitempty" json:"researcher,omitempty"`
	MediaPath  string      `xml:"mediapath,omitempty" json:"mediapath,omitempty"`
}

// NameFormat is a custom name format.
type NameFormat struct {
	Number string `xml:"number,attr" json:"number"`
	Name   string `xml:"name,attr" json:"name"`
	FmtStr string `xml:"fmt_str,attr" json:"fmtStr"`
	Active *int   `xml:"active,attr,omitempty" json:"active,omitempty"`
}

// Bookmark marks one object as a favourite.
type Bookmark struct {
	Target string `xml:"target,attr" json:"target"`
	HLink  string `xml:"hlink,attr" json:"h"`
}

// NameMap maps a surname to a group name.
type NameMap struct {
	Type  string `xml:"type,attr" json:"type"`
	Key   string `xml:"key,attr" json:"key"`
	Value string `xml:"value,attr" json:"value"`
}

// Tree is one complete family tree.
type Tree struct {
	Namespace    string
	Header       Header
	NameFormats  []NameFormat
	Tags         []Tag
	Events       []Event
	HomeHandle   string
	DefaultID    string
	People       []Person
	Families     []Family
	Citations    []Citation
	Sources      []Source
	Places       []Place
	Media        []Media
	Repositories []Repository
	Notes        []Note
	Bookmarks    []Bookmark
	NameMaps     []NameMap
}

// Meta is the part of a tree that belongs to no object.
type Meta struct {
	Namespace   string       `json:"namespace"`
	Header      Header       `json:"header"`
	NameFormats []NameFormat `json:"nameFormats,omitempty"`
	HomeHandle  string       `json:"homeHandle,omitempty"`
	DefaultID   string       `json:"defaultId,omitempty"`
	Bookmarks   []Bookmark   `json:"bookmarks,omitempty"`
	NameMaps    []NameMap    `json:"nameMaps,omitempty"`
}

// Meta returns the metadata of the tree.
func (t *Tree) Meta() Meta {
	return Meta{
		Namespace: t.Namespace, Header: t.Header, NameFormats: t.NameFormats,
		HomeHandle: t.HomeHandle, DefaultID: t.DefaultID, Bookmarks: t.Bookmarks, NameMaps: t.NameMaps,
	}
}

// Counts holds the number of objects of each kind.
type Counts struct {
	People       int `json:"people"`
	Families     int `json:"families"`
	Events       int `json:"events"`
	Places       int `json:"places"`
	Sources      int `json:"sources"`
	Citations    int `json:"citations"`
	Repositories int `json:"repositories"`
	Notes        int `json:"notes"`
	Media        int `json:"media"`
	Tags         int `json:"tags"`
}

// Counts returns the number of objects of each kind.
func (t *Tree) Counts() Counts {
	return Counts{
		People: len(t.People), Families: len(t.Families), Events: len(t.Events),
		Places: len(t.Places), Sources: len(t.Sources), Citations: len(t.Citations),
		Repositories: len(t.Repositories), Notes: len(t.Notes), Media: len(t.Media), Tags: len(t.Tags),
	}
}

// Record is one object in storable form.
type Record struct {
	Kind    Kind
	Handle  string
	ID      string
	Private bool
	Change  int64
	Doc     []byte
}

// Records converts the tree into storable documents.
func (t *Tree) Records() ([]Record, error) {
	total := len(t.Tags) + len(t.Events) + len(t.People) + len(t.Families) + len(t.Citations) +
		len(t.Sources) + len(t.Places) + len(t.Media) + len(t.Repositories) + len(t.Notes)
	out := make([]Record, 0, total)
	add := func(kind Kind, o Object, v any) error {
		doc, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out = append(out, Record{Kind: kind, Handle: o.Handle, ID: o.ID, Private: bool(o.Private), Change: o.Change, Doc: doc})
		return nil
	}
	for i := range t.Tags {
		o := Object{Handle: t.Tags[i].Handle, ID: t.Tags[i].Name, Change: t.Tags[i].Change}
		if err := add(KindTag, o, &t.Tags[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Events {
		if err := add(KindEvent, t.Events[i].Object, &t.Events[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.People {
		if err := add(KindPerson, t.People[i].Object, &t.People[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Families {
		if err := add(KindFamily, t.Families[i].Object, &t.Families[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Citations {
		if err := add(KindCitation, t.Citations[i].Object, &t.Citations[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Sources {
		if err := add(KindSource, t.Sources[i].Object, &t.Sources[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Places {
		if err := add(KindPlace, t.Places[i].Object, &t.Places[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Media {
		if err := add(KindMedia, t.Media[i].Object, &t.Media[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Repositories {
		if err := add(KindRepository, t.Repositories[i].Object, &t.Repositories[i]); err != nil {
			return nil, err
		}
	}
	for i := range t.Notes {
		if err := add(KindNote, t.Notes[i].Object, &t.Notes[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PlaceCoord returns the coordinates of a place as decimal degrees.
// It reads the plain decimal form only.
func PlaceCoord(c *Coord) (lat, long float64, ok bool) {
	if c == nil {
		return 0, 0, false
	}
	var err1, err2 error
	lat, err1 = strconv.ParseFloat(strings.TrimSpace(c.Lat), 64)
	long, err2 = strconv.ParseFloat(strings.TrimSpace(c.Long), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || long < -180 || long > 180 {
		return 0, 0, false
	}
	return lat, long, true
}
