package docview

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const ns = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

func pack(t *testing.T, parts map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func body(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><w:document ` + ns + `><w:body>` + inner + `<w:sectPr/></w:body></w:document>`
}

func read(t *testing.T, parts map[string]string) *Document {
	t.Helper()
	source := pack(t, parts)
	doc, err := FromDocx(source, source.Size(), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func plain(block Block) string {
	var out strings.Builder
	for _, run := range block.Runs {
		out.WriteString(run.Text)
	}
	return out.String()
}

func TestParagraphsAndFormats(t *testing.T) {
	doc := read(t, map[string]string{"word/document.xml": body(
		`<w:p><w:pPr><w:jc w:val="center"/><w:rPr><w:b/></w:rPr></w:pPr>` +
			`<w:r><w:t xml:space="preserve">Anna </w:t></w:r><w:r><w:t>Orlova</w:t></w:r>` +
			`<w:r><w:rPr><w:b/><w:i w:val="0"/></w:rPr><w:t>, born</w:t></w:r>` +
			`<w:r><w:rPr><w:i/><w:u w:val="single"/><w:strike/><w:vertAlign w:val="superscript"/></w:rPr><w:t>1</w:t></w:r>` +
			`<w:r><w:tab/><w:t>in</w:t><w:br/><w:t>Tver</w:t><w:br w:type="page"/></w:r>` +
			`<w:del><w:r><w:delText>removed</w:delText></w:r></w:del>` +
			`<w:ins><w:r><w:t>!</w:t></w:r></w:ins>` +
			`<w:r><w:rPr><w:vanish/></w:rPr><w:t>hidden</w:t></w:r></w:p>` +
			`<w:p/><w:p/><w:p><w:r><w:t>Second</w:t></w:r></w:p><w:p/>`)})
	if len(doc.Blocks) != 3 {
		t.Fatalf("blocks: %+v", doc.Blocks)
	}
	first := doc.Blocks[0]
	if first.Kind != KindParagraph || first.Align != "center" || plain(first) != "Anna Orlova, born1\tin\nTver!" {
		t.Fatalf("first block: %+v", first)
	}
	want := []Run{
		{Text: "Anna Orlova"}, {Text: ", born", Bold: true},
		{Text: "1", Italic: true, Underline: true, Strike: true, Script: "sup"}, {Text: "\tin\nTver!"},
	}
	if len(first.Runs) != len(want) {
		t.Fatalf("runs: %+v", first.Runs)
	}
	for i := range want {
		if first.Runs[i] != want[i] {
			t.Errorf("run %d: %+v, want %+v", i, first.Runs[i], want[i])
		}
	}
	if len(doc.Blocks[1].Runs) != 0 || plain(doc.Blocks[2]) != "Second" {
		t.Fatalf("the empty paragraphs: %+v", doc.Blocks)
	}
}

func TestHeadingsComeFromStyles(t *testing.T) {
	styles := `<w:styles ` + ns + `>` +
		`<w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Mine"><w:name w:val="My heading"/><w:basedOn w:val="H3"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="H3"><w:name w:val="Heading 3"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="T"><w:name w:val="Title"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Out"><w:name w:val="Other"/><w:pPr><w:outlineLvl w:val="8"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Loop"><w:name w:val="Loop"/><w:basedOn w:val="Loop"/></w:style></w:styles>`
	p := func(style string) string {
		return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`
	}
	doc := read(t, map[string]string{"word/styles.xml": styles, "word/document.xml": body(
		p("1") + p("Mine") + p("T") + p("Out") + p("Loop") + p("Unknown") +
			`<w:p><w:pPr><w:outlineLvl w:val="1"/></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`)})
	var got []int
	for _, block := range doc.Blocks {
		level := 0
		if block.Kind == KindHeading {
			level = block.Level
		}
		got = append(got, level)
	}
	want := []int{1, 3, 1, 6, 0, 0, 2}
	if len(got) != len(want) {
		t.Fatalf("levels: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("levels: %v, want %v", got, want)
		}
	}
}

func TestListsGetMarkers(t *testing.T) {
	numbering := `<w:numbering ` + ns + `>` +
		`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>` +
		`<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%1.%2)"/></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#xF0B7;"/></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:start w:val="4"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="%1"/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>` +
		`<w:num w:numId="3"><w:abstractNumId w:val="2"/></w:num></w:numbering>`
	styles := `<w:styles ` + ns + `><w:style w:type="paragraph" w:styleId="Bullet"><w:name w:val="List Bullet"/>` +
		`<w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr></w:style></w:styles>`
	item := func(num, level, text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + level + `"/><w:numId w:val="` + num + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	doc := read(t, map[string]string{"word/numbering.xml": numbering, "word/styles.xml": styles, "word/document.xml": body(
		item("1", "0", "a") + item("1", "1", "b") + item("1", "1", "c") + item("1", "0", "d") + item("1", "1", "e") +
			item("2", "0", "f") + item("3", "0", "g") + item("3", "0", "h") + item("0", "0", "no list") + item("9", "0", "unknown") +
			`<w:p><w:pPr><w:pStyle w:val="Bullet"/></w:pPr><w:r><w:t>from style</w:t></w:r></w:p>` +
			`<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:p>`)})
	var got []string
	for _, block := range doc.Blocks {
		got = append(got, block.Kind+":"+block.Marker)
	}
	want := "item:1.|item:1.a)|item:1.b)|item:2.|item:2.a)|item:•|item:IV|item:V|p:|item:•|item:•"
	if strings.Join(got, "|") != want {
		t.Fatalf("markers:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if doc.Blocks[1].Level != 1 {
		t.Fatalf("level: %+v", doc.Blocks[1])
	}
}

func TestNumberFormats(t *testing.T) {
	cases := map[string]string{
		"decimal 12": "12", "lowerLetter 1": "a", "lowerLetter 27": "aa", "upperLetter 3": "C", "lowerRoman 9": "ix",
		"upperRoman 1994": "MCMXCIV", "russianLower 2": "б", "russianUpper 1": "А", "decimalZero 7": "07", "other 5": "5",
		"upperRoman 0": "0", "lowerLetter 0": "0",
	}
	for input, want := range cases {
		format, number, _ := strings.Cut(input, " ")
		n := 0
		for _, c := range number {
			n = n*10 + int(c-'0')
		}
		if got := formatNumber(n, format); got != want {
			t.Errorf("%s: %q, want %q", input, got, want)
		}
	}
}

func TestTables(t *testing.T) {
	cell := func(props, text string) string {
		return `<w:tc><w:tcPr>` + props + `</w:tcPr><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:tc>`
	}
	nested := `<w:tbl><w:tr>` + cell("", "deep") + `</w:tr></w:tbl>`
	doc := read(t, map[string]string{"word/document.xml": body(
		`<w:tbl><w:tblPr/><w:tblGrid><w:gridCol/></w:tblGrid>` +
			`<w:tr><w:trPr/>` + cell(`<w:gridSpan w:val="2"/>`, "Name") + `</w:tr>` +
			`<w:tr>` + cell("", "Anna") + `<w:tc><w:p/>` + nested + `<w:p/></w:tc></w:tr></w:tbl>` +
			`<w:tbl></w:tbl>`)})
	if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != KindTable || len(doc.Blocks[0].Rows) != 2 {
		t.Fatalf("blocks: %+v", doc.Blocks)
	}
	rows := doc.Blocks[0].Rows
	if len(rows[0].Cells) != 1 || rows[0].Cells[0].Span != 2 || plain(rows[0].Cells[0].Blocks[0]) != "Name" {
		t.Fatalf("first row: %+v", rows[0])
	}
	second := rows[1].Cells
	if len(second) != 2 || len(second[1].Blocks) != 1 || second[1].Blocks[0].Kind != KindTable {
		t.Fatalf("second row: %+v", second)
	}
	if plain(second[1].Blocks[0].Rows[0].Cells[0].Blocks[0]) != "deep" {
		t.Fatalf("nested table: %+v", second[1].Blocks[0])
	}
	// The document must become JSON with a list for every cell.
	data, err := json.Marshal(doc)
	if err != nil || strings.Contains(string(data), `"blocks":null`) {
		t.Fatalf("json: %s %v", data, err)
	}
}

func TestTablesDoNotNestWithoutEnd(t *testing.T) {
	inner := `<w:p><w:r><w:t>x</w:t></w:r></w:p>`
	for range 8 {
		inner = `<w:tbl><w:tr><w:tc>` + inner + `</w:tc></w:tr></w:tbl>`
	}
	doc := read(t, map[string]string{"word/document.xml": body(inner)})
	depth := 0
	blocks := doc.Blocks
	for len(blocks) > 0 && blocks[0].Kind == KindTable {
		depth++
		blocks = blocks[0].Rows[0].Cells[0].Blocks
	}
	if depth != maxDepth {
		t.Fatalf("depth %d", depth)
	}
}

func TestLinks(t *testing.T) {
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="x" Target="https://example.org/a?b=1" TargetMode="External"/>` +
		`<Relationship Id="rId2" Type="x" Target="javascript:alert(1)" TargetMode="External"/>` +
		`<Relationship Id="rId3" Type="x" Target="https://example.org/internal"/>` +
		`<Relationship Id="rId4" Type="x" Target="mailto:anna@example.org" TargetMode="External"/></Relationships>`
	link := func(id, text string) string {
		return `<w:hyperlink r:id="` + id + `"><w:r><w:t>` + text + `</w:t></w:r></w:hyperlink>`
	}
	field := func(code, text string) string {
		return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">` + code + `</w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + text + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	doc := read(t, map[string]string{"word/_rels/document.xml.rels": rels, "word/document.xml": body(
		`<w:p>` + link("rId1", "safe") + link("rId2", "script") + link("rId3", "inner") + link("rId4", "mail") +
			`<w:hyperlink w:anchor="top"><w:r><w:t>anchor</w:t></w:r></w:hyperlink>` +
			field(` HYPERLINK "http://example.org/f" `, "field") + field(` HYPERLINK "file:///etc/passwd" `, "file") +
			field(` PAGE `, "7") + `<w:r><w:t>after</w:t></w:r></w:p>`)})
	got := map[string]string{}
	for _, run := range doc.Blocks[0].Runs {
		got[run.Text] = run.Link
	}
	want := map[string]string{
		"safe": "https://example.org/a?b=1", "mail": "mailto:anna@example.org", "field": "http://example.org/f",
		"scriptinner": "", "anchor": "", "file7after": "",
	}
	for text, link := range want {
		if value, ok := got[text]; !ok || value != link {
			t.Errorf("%q: link %q (found %v); all runs: %+v", text, value, ok, doc.Blocks[0].Runs)
		}
	}
}

func TestNotes(t *testing.T) {
	ref := func(kind, id string) string { return `<w:r><w:` + kind + `Reference w:id="` + id + `"/></w:r>` }
	note := func(kind, id, extra, text string) string {
		return `<w:` + kind + ` w:id="` + id + `"` + extra + `><w:p><w:r><w:` + kind + `Ref/></w:r><w:r><w:t>` + text + `</w:t></w:r>` +
			ref("footnote", "99") + `</w:p></w:` + kind + `>`
	}
	doc := read(t, map[string]string{
		"word/document.xml": body(`<w:p><w:r><w:t>One</w:t></w:r>` + ref("footnote", "3") + `<w:r><w:t>two</w:t></w:r>` +
			ref("endnote", "2") + ref("footnote", "3") + ref("footnote", "8") + `</w:p>`),
		"word/footnotes.xml": `<w:footnotes ` + ns + `>` + note("footnote", "0", ` w:type="separator"`, "sep") +
			note("footnote", "2", "", "not used") + note("footnote", "3", "", "first note") + `</w:footnotes>`,
		"word/endnotes.xml": `<w:endnotes ` + ns + `>` + note("endnote", "2", "", "end note") + `</w:endnotes>`,
	})
	var refs []int
	for _, run := range doc.Blocks[0].Runs {
		if run.Note != 0 {
			refs = append(refs, run.Note)
		}
	}
	if len(refs) != 4 || refs[0] != 1 || refs[1] != 2 || refs[2] != 1 || refs[3] != 3 {
		t.Fatalf("references: %v", refs)
	}
	if len(doc.Notes) != 3 || doc.Notes[0].Number != 1 || plain(doc.Notes[0].Blocks[0]) != "first note" ||
		plain(doc.Notes[1].Blocks[0]) != "end note" || len(doc.Notes[2].Blocks) != 0 {
		t.Fatalf("notes: %+v", doc.Notes)
	}
}

func TestPicturesAreCountedAndTheirTextStaysOut(t *testing.T) {
	doc := read(t, map[string]string{"word/document.xml": body(
		`<w:p><w:r><w:t>Before</w:t></w:r><w:r><w:drawing><a:graphic><a:p><a:r><a:t>inside a picture</a:t></a:r></a:p>` +
			`<w:p><w:r><w:t>text box</w:t></w:r></w:p></a:graphic></w:drawing></w:r>` +
			`<w:r><mc:AlternateContent><mc:Choice><w:drawing/></mc:Choice><mc:Fallback><w:pict/></mc:Fallback></mc:AlternateContent></w:r>` +
			`<w:r><w:pict/></w:r><w:r><w:t>after</w:t></w:r></w:p>` +
			`<w:sdt><w:sdtPr><w:alias w:val="x"/></w:sdtPr><w:sdtContent><w:p><w:r><w:t>In a control</w:t></w:r></w:p></w:sdtContent></w:sdt>`)})
	if doc.Pictures != 3 || len(doc.Blocks) != 2 || plain(doc.Blocks[0]) != "Beforeafter" || plain(doc.Blocks[1]) != "In a control" {
		t.Fatalf("document: %+v", doc)
	}
}

func TestLimits(t *testing.T) {
	var many strings.Builder
	for range 50 {
		many.WriteString(`<w:p><w:r><w:t>0123456789</w:t></w:r></w:p>`)
	}
	source := pack(t, map[string]string{"word/document.xml": body(many.String())})
	doc, err := FromDocx(source, source.Size(), Limits{PartBytes: 1 << 20, TextBytes: 1 << 20, Blocks: 20})
	if err != nil || !doc.Truncated || len(doc.Blocks) != 20 {
		t.Fatalf("block limit: %v %+v", err, doc)
	}
	doc, err = FromDocx(source, source.Size(), Limits{PartBytes: 1 << 20, TextBytes: 95, Blocks: 1000})
	if err != nil || !doc.Truncated || len(doc.Blocks) != 9 {
		t.Fatalf("text limit: %v, %d blocks", err, len(doc.Blocks))
	}
	if _, err = FromDocx(source, source.Size(), Limits{PartBytes: 100, TextBytes: 1 << 20, Blocks: 1000}); !errors.Is(err, ErrFormat) {
		t.Fatalf("part limit: %v", err)
	}
}

func TestFilesThatAreNotWord(t *testing.T) {
	notZip := bytes.NewReader([]byte("plain bytes, no archive"))
	if _, err := FromDocx(notZip, notZip.Size(), DefaultLimits); !errors.Is(err, ErrFormat) {
		t.Fatalf("not a zip: %v", err)
	}
	other := pack(t, map[string]string{"content.xml": "<a/>"})
	if _, err := FromDocx(other, other.Size(), DefaultLimits); !errors.Is(err, ErrFormat) {
		t.Fatalf("another archive: %v", err)
	}
	for _, broken := range []string{"", "<w:document", body(`<w:p><w:r><w:t>open`), `<x><!DOCTYPE a [<!ENTITY e "x">]><w:body>&e;</w:body></x>`} {
		source := pack(t, map[string]string{"word/document.xml": broken})
		if _, err := FromDocx(source, source.Size(), DefaultLimits); !errors.Is(err, ErrFormat) {
			t.Errorf("broken XML %q: %v", broken, err)
		}
	}
	// Broken side parts do not stop the reader.
	doc := read(t, map[string]string{
		"word/styles.xml": "<broken", "word/numbering.xml": "", "word/_rels/document.xml.rels": "<x/>",
		"word/document.xml": body(`<w:p><w:r><w:t>ok</w:t></w:r></w:p>`),
	})
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks: %+v", doc.Blocks)
	}
}

func TestPlainText(t *testing.T) {
	cases := map[string][]byte{
		"UTF-8":        []byte("Анна\r\nOrlova\r\n\r\n"),
		"UTF-8 BOM":    append([]byte{0xEF, 0xBB, 0xBF}, []byte("Анна\nOrlova")...),
		"Windows-1251": {0xC0, 0xED, 0xED, 0xE0, '\n', 'O', 'r', 'l', 'o', 'v', 'a'},
		"UTF-16 LE":    {0xFF, 0xFE, 0x10, 0x04, 0x3D, 0x04, 0x3D, 0x04, 0x30, 0x04, '\n', 0, 'O', 0, 'r', 0, 'l', 0, 'o', 0, 'v', 0, 'a', 0},
		"UTF-16 BE":    {0xFE, 0xFF, 0x04, 0x10, 0x04, 0x3D, 0x04, 0x3D, 0x04, 0x30, 0, '\n', 0, 'O', 0, 'r', 0, 'l', 0, 'o', 0, 'v', 0, 'a'},
	}
	for name, data := range cases {
		doc, err := FromText(bytes.NewReader(data), DefaultLimits)
		if err != nil || len(doc.Blocks) != 1 || doc.Blocks[0].Kind != KindText || plain(doc.Blocks[0]) != "Анна\nOrlova" {
			t.Errorf("%s: %v %+v", name, err, doc)
		}
	}
	if doc, err := FromText(strings.NewReader(" \n"), DefaultLimits); err != nil || doc.Blocks == nil || len(doc.Blocks) != 0 {
		t.Errorf("empty text: %v %+v", err, doc)
	}
	if _, err := FromText(bytes.NewReader([]byte{'a', 0, 1, 2, 'b'}), DefaultLimits); !errors.Is(err, ErrFormat) {
		t.Errorf("binary file: %v", err)
	}
	doc, err := FromText(strings.NewReader(strings.Repeat("я", 100)), Limits{TextBytes: 51})
	if err != nil || !doc.Truncated || plain(doc.Blocks[0]) != strings.Repeat("я", 25) {
		t.Errorf("long text: %v %+v", err, doc)
	}
	if got := decode1251([]byte{0xA8, 0xB8, 0xB9, 0x97, 0xFF}); got != "Ёё№—я" {
		t.Errorf("Windows-1251: %q", got)
	}
}

func TestColorsAndSizes(t *testing.T) {
	styles := `<w:styles ` + ns + `>` +
		`<w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Base"><w:name w:val="Base"/><w:rPr><w:color w:val="c00000"/><w:b/></w:rPr>` +
		`<w:pPr><w:shd w:val="clear" w:fill="FFF2CC"/><w:jc w:val="both"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Small"><w:name w:val="Small"/><w:basedOn w:val="Base"/><w:rPr><w:sz w:val="18"/><w:b w:val="0"/></w:rPr></w:style>` +
		`<w:style w:type="character" w:styleId="Mark"><w:name w:val="Mark"/><w:rPr><w:highlight w:val="yellow"/><w:i/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="H1"><w:name w:val="heading 1"/><w:rPr><w:sz w:val="32"/><w:color w:val="2F5496"/></w:rPr></w:style>` +
		`</w:styles>`
	run := func(props, text string) string {
		return `<w:r><w:rPr>` + props + `</w:rPr><w:t>` + text + `</w:t></w:r>`
	}
	doc := read(t, map[string]string{"word/styles.xml": styles, "word/document.xml": body(
		`<w:p>` + run("", "plain") + run(`<w:color w:val="0563C1"/>`, "blue") + run(`<w:color w:val="auto"/><w:sz w:val="44"/>`, "big") +
			run(`<w:shd w:val="clear" w:color="auto" w:fill="E7E6E6"/>`, "grey") + run(`<w:highlight w:val="none"/><w:color w:val="red"/>`, "bad") +
			run(`<w:rStyle w:val="Mark"/>`, "marked") + run(`<w:sz w:val="23"/>`, "same") + `</w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="Small"/></w:pPr>` + run("", "small red") + run(`<w:color w:val="auto"/>`, "small") + `</w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="Base"/><w:shd w:val="clear" w:fill="auto"/><w:jc w:val="left"/></w:pPr>` + run("", "own") + `</w:p>` +
			`<w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr>` + run("", "Heading") + `</w:p>` +
			`<w:tbl><w:tr><w:tc><w:tcPr><w:shd w:val="clear" w:color="auto" w:fill="fff2cc"/></w:tcPr><w:p>` + run("", "cell") + `</w:p></w:tc>` +
			`<w:tc><w:tcPr><w:shd w:fill="zzzzzz"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`)})
	first := doc.Blocks[0].Runs
	want := []Run{
		{Text: "plain"}, {Text: "blue", Color: "#0563C1"}, {Text: "big", Scale: 2}, {Text: "grey", Fill: "#E7E6E6"}, {Text: "bad"},
		{Text: "marked", Italic: true, Fill: "#FFFF00"}, {Text: "same"},
	}
	if len(first) != len(want) {
		t.Fatalf("runs: %+v", first)
	}
	for i := range want {
		if first[i] != want[i] {
			t.Errorf("run %d: %+v, want %+v", i, first[i], want[i])
		}
	}
	small := doc.Blocks[1]
	if small.Fill != "#FFF2CC" || small.Align != "justify" || len(small.Runs) != 2 ||
		small.Runs[0] != (Run{Text: "small red", Color: "#C00000", Scale: 0.8}) || small.Runs[1] != (Run{Text: "small", Scale: 0.8}) {
		t.Fatalf("style chain: %+v", small)
	}
	if own := doc.Blocks[2]; own.Fill != "" || own.Align != "" || own.Runs[0] != (Run{Text: "own", Bold: true, Color: "#C00000"}) {
		t.Fatalf("own properties of a paragraph: %+v", own)
	}
	if heading := doc.Blocks[3]; heading.Kind != KindHeading || heading.Runs[0] != (Run{Text: "Heading", Color: "#2F5496"}) {
		t.Fatalf("heading: %+v", heading)
	}
	cells := doc.Blocks[4].Rows[0].Cells
	if cells[0].Fill != "#FFF2CC" || cells[1].Fill != "" {
		t.Fatalf("cells: %+v", cells)
	}
}

func TestSizeWithoutDefaults(t *testing.T) {
	// Without any style the body text has 10 points.
	doc := read(t, map[string]string{"word/document.xml": body(
		`<w:p><w:r><w:rPr><w:sz w:val="20"/></w:rPr><w:t>a</w:t></w:r><w:r><w:rPr><w:sz w:val="30"/></w:rPr><w:t>b</w:t></w:r>` +
			`<w:r><w:rPr><w:sz w:val="2"/></w:rPr><w:t>c</w:t></w:r><w:r><w:rPr><w:sz w:val="800"/></w:rPr><w:t>d</w:t></w:r></w:p>`)})
	runs := doc.Blocks[0].Runs
	if len(runs) != 4 || runs[0].Scale != 0 || runs[1].Scale != 1.5 || runs[2].Scale != 0.7 || runs[3].Scale != 2.2 {
		t.Fatalf("runs: %+v", runs)
	}
}
