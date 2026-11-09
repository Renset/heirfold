// Package gramps reads and writes the Gramps XML format.
//
// The code implements the public format definition (grampsxml.dtd, version
// 1.7). It contains no code from Gramps or Gramps Web. See docs/adr/0005.
package gramps

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Renset/heirfold/internal/model"
)

// Limits protects the parser from very large input.
type Limits struct {
	// MaxBytes is the maximum size of the XML after decompression.
	MaxBytes int64
	// MaxObjects is the maximum number of primary objects.
	MaxObjects int
}

// DefaultLimits are the limits for files that users upload.
var DefaultLimits = Limits{MaxBytes: 256 << 20, MaxObjects: 2_000_000}

// Errors that the parser returns.
var (
	ErrTooLarge      = errors.New("gramps: file exceeds the size limit")
	ErrTooManyObject = errors.New("gramps: file exceeds the object limit")
	ErrNotGramps     = errors.New("gramps: file is not Gramps XML")
	ErrTooDeep       = errors.New("gramps: elements are nested too deeply")
)

// maxDepth is far above the depth that the Gramps format uses. The XML decoder
// keeps memory for each open element, so deep nesting would exhaust memory.
const maxDepth = 32

// depthLimiter stops the decoder when elements nest too deeply.
type depthLimiter struct {
	inner *xml.Decoder
	depth int
}

// Token returns the next raw token. The outer decoder checks the structure
// and resolves the name spaces.
func (d *depthLimiter) Token() (xml.Token, error) {
	token, err := d.inner.RawToken()
	if err != nil {
		return nil, err
	}
	switch token.(type) {
	case xml.StartElement:
		d.depth++
		if d.depth > maxDepth {
			return nil, ErrTooDeep
		}
	case xml.EndElement:
		d.depth--
	}
	return token, nil
}

const supportedPrefix = "http://gramps-project.org/xml/1.7."

// Warning describes one problem that did not stop the import.
// A warning contains object IDs but no names or other personal data.
type Warning struct {
	Code   string `json:"code"`
	Kind   string `json:"kind,omitempty"`
	Object string `json:"object,omitempty"`
	Target string `json:"target,omitempty"`
}

// Result is the outcome of a parse.
type Result struct {
	Tree     *model.Tree
	Warnings []Warning
	// WarningsTotal can be larger than len(Warnings). The parser keeps only
	// the first warnings.
	WarningsTotal int
}

const maxWarnings = 200

type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	return n, err
}

// Parse reads a .gramps file. The file can be plain XML or gzip-compressed.
//
// encoding/xml does not load external entities and does not expand custom
// entities in strict mode, so XXE and entity expansion do not apply.
func Parse(r io.Reader, limits Limits) (*Result, error) {
	buffered := bufio.NewReader(r)
	magic, err := buffered.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var src io.Reader = buffered
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, ErrNotGramps
		}
		defer func() { _ = gz.Close() }()
		src = gz
	}
	inner := xml.NewDecoder(&limitedReader{r: src, remaining: limits.MaxBytes})
	inner.Strict = true
	decoder := xml.NewTokenDecoder(&depthLimiter{inner: inner})

	tree := &model.Tree{}
	p := &parser{decoder: decoder, tree: tree, limits: limits}
	if err := p.run(); err != nil {
		return nil, err
	}
	if tree.Namespace == "" {
		return nil, ErrNotGramps
	}
	return Check(tree), nil
}

type parser struct {
	decoder *xml.Decoder
	tree    *model.Tree
	limits  Limits
	objects int
}

func (p *parser) run() error {
	for {
		tok, err := p.decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return p.wrap(err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "database" {
			return ErrNotGramps
		}
		if !strings.HasPrefix(start.Name.Space, "http://gramps-project.org/xml/") {
			return ErrNotGramps
		}
		p.tree.Namespace = start.Name.Space
		return p.database()
	}
}

func cleanXMLError(err error) string {
	var syntax *xml.SyntaxError
	if errors.As(err, &syntax) {
		return fmt.Sprintf("XML syntax error on line %d", syntax.Line)
	}
	return "XML error"
}

func (p *parser) database() error {
	for {
		tok, err := p.decoder.Token()
		if err != nil {
			return p.wrap(err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			if err := p.section(t); err != nil {
				return err
			}
		}
	}
}

func (p *parser) wrap(err error) error {
	if errors.Is(err, ErrTooLarge) {
		return ErrTooLarge
	}
	if errors.Is(err, ErrTooManyObject) || errors.Is(err, ErrTooDeep) {
		return err
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: unexpected end of file", ErrNotGramps)
	}
	return fmt.Errorf("%w: %s", ErrNotGramps, cleanXMLError(err))
}

func (p *parser) section(start xml.StartElement) error {
	switch start.Name.Local {
	case "header":
		return p.wrap2(p.decoder.DecodeElement(&p.tree.Header, &start))
	case "name-formats":
		return items(p, start, "format", &p.tree.NameFormats)
	case "tags":
		return items(p, start, "tag", &p.tree.Tags)
	case "events":
		return items(p, start, "event", &p.tree.Events)
	case "people":
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "home":
				p.tree.HomeHandle = attr.Value
			case "default":
				p.tree.DefaultID = attr.Value
			}
		}
		return items(p, start, "person", &p.tree.People)
	case "families":
		return items(p, start, "family", &p.tree.Families)
	case "citations":
		return items(p, start, "citation", &p.tree.Citations)
	case "sources":
		return items(p, start, "source", &p.tree.Sources)
	case "places":
		return items(p, start, "placeobj", &p.tree.Places)
	case "objects":
		return items(p, start, "object", &p.tree.Media)
	case "repositories":
		return items(p, start, "repository", &p.tree.Repositories)
	case "notes":
		return items(p, start, "note", &p.tree.Notes)
	case "bookmarks":
		return items(p, start, "bookmark", &p.tree.Bookmarks)
	case "namemaps":
		return items(p, start, "map", &p.tree.NameMaps)
	}
	return p.wrap2(p.decoder.Skip())
}

func (p *parser) wrap2(err error) error {
	if err == nil {
		return nil
	}
	return p.wrap(err)
}

// items reads all child elements of one section into a slice.
func items[T any](p *parser, section xml.StartElement, element string, out *[]T) error {
	_ = section
	for {
		tok, err := p.decoder.Token()
		if err != nil {
			return p.wrap(err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			if t.Name.Local != element {
				if err := p.decoder.Skip(); err != nil {
					return p.wrap(err)
				}
				continue
			}
			p.objects++
			if p.limits.MaxObjects > 0 && p.objects > p.limits.MaxObjects {
				return ErrTooManyObject
			}
			var item T
			if err := p.decoder.DecodeElement(&item, &t); err != nil {
				return p.wrap(err)
			}
			*out = append(*out, item)
		}
	}
}

// Check finds links to objects that do not exist. Every reader of a tree
// calls it, so each source gives the same warnings.
func Check(t *model.Tree) *Result {
	res := &Result{Tree: t}
	warn := func(w Warning) {
		res.WarningsTotal++
		if len(res.Warnings) < maxWarnings {
			res.Warnings = append(res.Warnings, w)
		}
	}
	handles := make(map[string]struct{}, 1024)
	seenIDs := make(map[string]struct{}, 1024)
	register := func(kind model.Kind, o model.Object) {
		if _, dup := handles[o.Handle]; dup {
			warn(Warning{Code: "duplicate_handle", Kind: string(kind), Object: o.ID})
		}
		handles[o.Handle] = struct{}{}
		if o.ID == "" {
			warn(Warning{Code: "missing_id", Kind: string(kind)})
			return
		}
		key := string(kind) + "\x00" + o.ID
		if _, dup := seenIDs[key]; dup {
			warn(Warning{Code: "duplicate_id", Kind: string(kind), Object: o.ID})
		}
		seenIDs[key] = struct{}{}
	}
	for i := range t.People {
		register(model.KindPerson, t.People[i].Object)
	}
	for i := range t.Families {
		register(model.KindFamily, t.Families[i].Object)
	}
	for i := range t.Events {
		register(model.KindEvent, t.Events[i].Object)
	}
	for i := range t.Places {
		register(model.KindPlace, t.Places[i].Object)
	}
	for i := range t.Sources {
		register(model.KindSource, t.Sources[i].Object)
	}
	for i := range t.Citations {
		register(model.KindCitation, t.Citations[i].Object)
	}
	for i := range t.Repositories {
		register(model.KindRepository, t.Repositories[i].Object)
	}
	for i := range t.Notes {
		register(model.KindNote, t.Notes[i].Object)
	}
	for i := range t.Media {
		register(model.KindMedia, t.Media[i].Object)
	}

	link := func(kind model.Kind, object, target string) {
		if target == "" {
			return
		}
		if _, ok := handles[target]; !ok {
			warn(Warning{Code: "broken_link", Kind: string(kind), Object: object})
		}
	}
	for i := range t.People {
		person := &t.People[i]
		for _, r := range person.EventRefs {
			link(model.KindPerson, person.ID, r.HLink)
		}
		for _, r := range person.ChildOf {
			link(model.KindPerson, person.ID, r.HLink)
		}
		for _, r := range person.ParentIn {
			link(model.KindPerson, person.ID, r.HLink)
		}
		for _, r := range person.PersonRefs {
			link(model.KindPerson, person.ID, r.HLink)
		}
	}
	for i := range t.Families {
		family := &t.Families[i]
		if family.Father != nil {
			link(model.KindFamily, family.ID, family.Father.HLink)
		}
		if family.Mother != nil {
			link(model.KindFamily, family.ID, family.Mother.HLink)
		}
		for _, r := range family.ChildRefs {
			link(model.KindFamily, family.ID, r.HLink)
		}
		for _, r := range family.EventRefs {
			link(model.KindFamily, family.ID, r.HLink)
		}
	}
	for i := range t.Citations {
		link(model.KindCitation, t.Citations[i].ID, t.Citations[i].Source.HLink)
	}
	for i := range t.Events {
		if t.Events[i].Place != nil {
			link(model.KindEvent, t.Events[i].ID, t.Events[i].Place.HLink)
		}
	}
	if t.HomeHandle != "" {
		link(model.KindPerson, "home", t.HomeHandle)
	}
	if !strings.HasPrefix(t.Namespace, supportedPrefix) {
		warn(Warning{Code: "old_format"})
	}
	return res
}

// Write writes the tree as Gramps XML. The element order follows the DTD.
func Write(w io.Writer, t *model.Tree) error {
	namespace := t.Namespace
	if namespace == "" {
		namespace = "http://gramps-project.org/xml/1.7.2/"
	}
	version := strings.TrimSuffix(strings.TrimPrefix(namespace, "http://gramps-project.org/xml/"), "/")
	if _, err := fmt.Fprintf(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
		"<!DOCTYPE database PUBLIC \"-//Gramps//DTD Gramps XML %s//EN\"\n"+
		"\"http://gramps-project.org/xml/%s/grampsxml.dtd\">\n", version, version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "<database xmlns=%q>\n", namespace); err != nil {
		return err
	}
	encoder := xml.NewEncoder(w)
	encoder.Indent("  ", "  ")
	if err := encoder.EncodeElement(headerOrDefault(t.Header), start("header")); err != nil {
		return err
	}
	if err := section(encoder, "name-formats", "format", t.NameFormats, nil); err != nil {
		return err
	}
	if err := section(encoder, "tags", "tag", t.Tags, nil); err != nil {
		return err
	}
	if err := section(encoder, "events", "event", t.Events, nil); err != nil {
		return err
	}
	var peopleAttrs []xml.Attr
	if t.DefaultID != "" {
		peopleAttrs = append(peopleAttrs, xml.Attr{Name: xml.Name{Local: "default"}, Value: t.DefaultID})
	}
	if t.HomeHandle != "" {
		peopleAttrs = append(peopleAttrs, xml.Attr{Name: xml.Name{Local: "home"}, Value: t.HomeHandle})
	}
	if err := section(encoder, "people", "person", t.People, peopleAttrs); err != nil {
		return err
	}
	if err := section(encoder, "families", "family", t.Families, nil); err != nil {
		return err
	}
	if err := section(encoder, "citations", "citation", t.Citations, nil); err != nil {
		return err
	}
	if err := section(encoder, "sources", "source", t.Sources, nil); err != nil {
		return err
	}
	if err := section(encoder, "places", "placeobj", t.Places, nil); err != nil {
		return err
	}
	if err := section(encoder, "objects", "object", t.Media, nil); err != nil {
		return err
	}
	if err := section(encoder, "repositories", "repository", t.Repositories, nil); err != nil {
		return err
	}
	if err := section(encoder, "notes", "note", t.Notes, nil); err != nil {
		return err
	}
	if err := section(encoder, "bookmarks", "bookmark", t.Bookmarks, nil); err != nil {
		return err
	}
	if err := section(encoder, "namemaps", "map", t.NameMaps, nil); err != nil {
		return err
	}
	if err := encoder.Flush(); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n</database>\n")
	return err
}

func headerOrDefault(h model.Header) model.Header {
	if h.Created.Date == "" {
		h.Created.Date = time.Now().UTC().Format("2006-01-02")
	}
	if h.Created.Version == "" {
		h.Created.Version = "6.0.0"
	}
	return h
}

func start(name string, attrs ...xml.Attr) xml.StartElement {
	return xml.StartElement{Name: xml.Name{Local: name}, Attr: attrs}
}

func section[T any](e *xml.Encoder, name, element string, values []T, attrs []xml.Attr) error {
	if len(values) == 0 && len(attrs) == 0 {
		return nil
	}
	open := start(name, attrs...)
	if err := e.EncodeToken(open); err != nil {
		return err
	}
	for i := range values {
		if err := e.EncodeElement(&values[i], start(element)); err != nil {
			return err
		}
	}
	return e.EncodeToken(open.End())
}

// Marshal returns the tree as Gramps XML.
func Marshal(t *model.Tree) ([]byte, error) {
	var buf bytes.Buffer
	if err := Write(&buf, t); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
