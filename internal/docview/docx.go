package docview

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// maxDepth bounds tables inside tables.
const maxDepth = 3

type docStyle struct {
	name    string
	basedOn string
	outline int // -1 if not set
	numID   string
	level   int
	// run holds the text format that the style sets.
	run runProps
	// fill and align are the background and the alignment of a paragraph.
	fill     string
	fillSet  bool
	align    string
	alignSet bool
}

// runProps is a list of changes to a text format. A style and a run set only
// some properties, and the rest comes from the level below.
type runProps []func(*runFormat)

func (props runProps) apply(format *runFormat) {
	for _, set := range props {
		set(format)
	}
}

// hexColor returns #RRGGBB for a color value of Word, or nothing for "auto".
func hexColor(value string) string {
	if len(value) != 6 {
		return ""
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return "#" + strings.ToUpper(value)
}

// highlights are the named marker colors of Word.
var highlights = map[string]string{
	"yellow": "#FFFF00", "green": "#00FF00", "cyan": "#00FFFF", "magenta": "#FF00FF", "blue": "#0000FF", "red": "#FF0000",
	"darkBlue": "#000080", "darkCyan": "#008080", "darkGreen": "#008000", "darkMagenta": "#800080", "darkRed": "#800000",
	"darkYellow": "#808000", "darkGray": "#808080", "lightGray": "#C0C0C0", "black": "#000000", "white": "#FFFFFF",
}

// alignOf returns the alignment for a value of w:jc.
func alignOf(value string) string {
	switch value {
	case "center":
		return "center"
	case "right", "end":
		return "end"
	case "both", "distribute":
		return "justify"
	}
	return ""
}

type numLevel struct {
	format string
	text   string
	start  int
}

type docx struct {
	limits Limits
	styles map[string]docStyle
	// defaults is the text format of the whole file. normal is the style of a
	// paragraph that names no style. bodySize is the text size of that style.
	defaults runProps
	normal   string
	bodySize int
	formats  map[string]runFormat
	abstract map[string]map[int]numLevel
	nums     map[string]string
	counters map[string][]int
	links    map[string]string
	doc      *Document
	text     int
	blocks   int
	// noteOrder lists the notes in the order of their first reference.
	noteOrder []string
	noteIndex map[string]int
	// inNotes is true while the reader is inside the notes themselves.
	inNotes bool
}

// FromDocx reads a Word document (.docx).
func FromDocx(r io.ReaderAt, size int64, limits Limits) (*Document, error) {
	archive, err := zip.NewReader(r, size)
	if err != nil {
		return nil, ErrFormat
	}
	parts := map[string]*zip.File{}
	for _, file := range archive.File {
		parts[file.Name] = file
	}
	main := parts["word/document.xml"]
	if main == nil {
		return nil, ErrFormat
	}
	d := &docx{
		limits: limits, styles: map[string]docStyle{}, abstract: map[string]map[int]numLevel{}, nums: map[string]string{},
		counters: map[string][]int{}, links: map[string]string{}, doc: &Document{Blocks: []Block{}}, noteIndex: map[string]int{},
		formats: map[string]runFormat{},
	}
	// The side parts make the result better. A broken one does not stop the reader.
	_ = d.part(parts["word/styles.xml"], d.readStyles)
	_ = d.part(parts["word/numbering.xml"], d.readNumbering)
	_ = d.part(parts["word/_rels/document.xml.rels"], d.readLinks)
	d.bodySize = d.styleFormat(d.normal).size
	if d.bodySize <= 0 {
		// Word uses 10 points when a file sets no size.
		d.bodySize = 20
	}

	err = d.part(main, func(x *xml.Decoder) error {
		if err := seek(x, "body"); err != nil {
			return err
		}
		return d.readBlocks(x, &d.doc.Blocks, 0)
	})
	switch {
	case errors.Is(err, errLimit):
		d.doc.Truncated = true
	case err != nil:
		return nil, ErrFormat
	}
	if len(d.noteOrder) > 0 {
		found := map[string][]Block{}
		d.inNotes = true
		_ = d.part(parts["word/footnotes.xml"], func(x *xml.Decoder) error { return d.readNotes(x, "footnote", "f", found) })
		_ = d.part(parts["word/endnotes.xml"], func(x *xml.Decoder) error { return d.readNotes(x, "endnote", "e", found) })
		for i, key := range d.noteOrder {
			d.doc.Notes = append(d.doc.Notes, Note{Number: i + 1, Blocks: tidy(found[key])})
		}
	}
	d.doc.Blocks = tidy(d.doc.Blocks)
	return d.doc, nil
}

// part runs fn on one XML part of the archive.
func (d *docx) part(file *zip.File, fn func(*xml.Decoder) error) error {
	if file == nil {
		return nil
	}
	if file.UncompressedSize64 > uint64(d.limits.PartBytes) { // #nosec G115 -- the limit is positive.
		return ErrFormat
	}
	source, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	// The size in the header can be false, so the reader has its own limit.
	return fn(xml.NewDecoder(io.LimitReader(source, d.limits.PartBytes)))
}

// seek moves to the start of the first element with this name.
func seek(x *xml.Decoder, name string) error {
	for {
		token, err := x.Token()
		if err != nil {
			return err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == name {
			return nil
		}
	}
}

// attr returns an attribute by its local name.
func attr(start xml.StartElement, name string) string {
	for _, a := range start.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// on reads a toggle such as <w:b/> or <w:b w:val="0"/>.
func on(start xml.StartElement) bool {
	switch attr(start, "val") {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

// children calls fn for each child element of the open element. fn must
// consume the child: it reads to the end tag or calls Skip.
func children(x *xml.Decoder, fn func(start xml.StartElement) error) error {
	for {
		token, err := x.Token()
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if err := fn(t); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}

func (d *docx) readStyles(x *xml.Decoder) error {
	if err := seek(x, "styles"); err != nil {
		return err
	}
	return children(x, func(start xml.StartElement) error {
		if start.Name.Local == "docDefaults" {
			return children(x, func(start xml.StartElement) error {
				if start.Name.Local != "rPrDefault" {
					return x.Skip()
				}
				return children(x, func(start xml.StartElement) error {
					if start.Name.Local != "rPr" {
						return x.Skip()
					}
					props, _, err := readRunProps(x)
					d.defaults = props
					return err
				})
			})
		}
		if start.Name.Local != "style" {
			return x.Skip()
		}
		id := attr(start, "styleId")
		switch attr(start, "default") {
		case "1", "true", "on":
			if attr(start, "type") == "paragraph" {
				d.normal = id
			}
		}
		style := docStyle{outline: -1}
		err := children(x, func(start xml.StartElement) error {
			switch start.Name.Local {
			case "name":
				style.name = strings.ToLower(attr(start, "val"))
			case "basedOn":
				style.basedOn = attr(start, "val")
			case "rPr":
				props, _, err := readRunProps(x)
				style.run = props
				return err
			case "pPr":
				return children(x, func(start xml.StartElement) error {
					switch start.Name.Local {
					case "jc":
						style.align, style.alignSet = alignOf(attr(start, "val")), true
					case "shd":
						style.fill, style.fillSet = hexColor(attr(start, "fill")), true
					case "outlineLvl":
						if n, err := strconv.Atoi(attr(start, "val")); err == nil {
							style.outline = n
						}
					case "numPr":
						return children(x, func(start xml.StartElement) error {
							switch start.Name.Local {
							case "numId":
								style.numID = attr(start, "val")
							case "ilvl":
								style.level, _ = strconv.Atoi(attr(start, "val"))
							}
							return x.Skip()
						})
					}
					return x.Skip()
				})
			}
			return x.Skip()
		})
		if id != "" && len(d.styles) < 5000 {
			d.styles[id] = style
		}
		return err
	})
}

func (d *docx) readNumbering(x *xml.Decoder) error {
	if err := seek(x, "numbering"); err != nil {
		return err
	}
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "abstractNum":
			id := attr(start, "abstractNumId")
			levels := map[int]numLevel{}
			err := children(x, func(start xml.StartElement) error {
				if start.Name.Local != "lvl" {
					return x.Skip()
				}
				index, _ := strconv.Atoi(attr(start, "ilvl"))
				level := numLevel{format: "decimal", start: 1}
				err := children(x, func(start xml.StartElement) error {
					switch start.Name.Local {
					case "numFmt":
						level.format = attr(start, "val")
					case "lvlText":
						level.text = attr(start, "val")
					case "start":
						if n, err := strconv.Atoi(attr(start, "val")); err == nil {
							level.start = n
						}
					}
					return x.Skip()
				})
				if index >= 0 && index < 9 {
					levels[index] = level
				}
				return err
			})
			if len(d.abstract) < 5000 {
				d.abstract[id] = levels
			}
			return err
		case "num":
			id := attr(start, "numId")
			return children(x, func(start xml.StartElement) error {
				if start.Name.Local == "abstractNumId" && len(d.nums) < 5000 {
					d.nums[id] = attr(start, "val")
				}
				return x.Skip()
			})
		}
		return x.Skip()
	})
}

// safeLink returns the address if it is safe to open, or nothing.
func safeLink(target string) string {
	target = strings.TrimSpace(target)
	lower := strings.ToLower(target)
	if len(target) > 2000 || strings.ContainsAny(target, "\n\r\t ") {
		return ""
	}
	for _, scheme := range []string{"https://", "http://", "mailto:"} {
		if strings.HasPrefix(lower, scheme) && len(target) > len(scheme) {
			return target
		}
	}
	return ""
}

func (d *docx) readLinks(x *xml.Decoder) error {
	if err := seek(x, "Relationships"); err != nil {
		return err
	}
	return children(x, func(start xml.StartElement) error {
		if start.Name.Local == "Relationship" && attr(start, "TargetMode") == "External" && len(d.links) < 5000 {
			if link := safeLink(attr(start, "Target")); link != "" {
				d.links[attr(start, "Id")] = link
			}
		}
		return x.Skip()
	})
}

func (d *docx) readNotes(x *xml.Decoder, element, prefix string, out map[string][]Block) error {
	if err := seek(x, element+"s"); err != nil {
		return err
	}
	return children(x, func(start xml.StartElement) error {
		key := prefix + attr(start, "id")
		if _, wanted := d.noteIndex[key]; start.Name.Local != element || !wanted || attr(start, "type") != "" {
			return x.Skip()
		}
		var blocks []Block
		err := d.readBlocks(x, &blocks, maxDepth)
		out[key] = blocks
		return err
	})
}

// count takes one block from the budget.
func (d *docx) count() error {
	d.blocks++
	if d.blocks > d.limits.Blocks {
		return errLimit
	}
	return nil
}

// readBlocks reads paragraphs and tables up to the end of the open element.
func (d *docx) readBlocks(x *xml.Decoder, out *[]Block, depth int) error {
	return d.readBlocksIn(x, out, depth, nil)
}

// readBlocksIn does the work of readBlocks. For the content of a table cell
// it also reads the properties of the cell.
func (d *docx) readBlocksIn(x *xml.Decoder, out *[]Block, depth int, cell *Cell) error {
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "tcPr":
			if cell == nil {
				return x.Skip()
			}
			return children(x, func(start xml.StartElement) error {
				switch start.Name.Local {
				case "gridSpan":
					if n, err := strconv.Atoi(attr(start, "val")); err == nil && n > 1 && n <= 63 {
						cell.Span = n
					}
				case "shd":
					cell.Fill = hexColor(attr(start, "fill"))
				}
				return x.Skip()
			})
		case "p":
			if err := d.count(); err != nil {
				return err
			}
			block, err := d.readParagraph(x)
			if err != nil {
				return err
			}
			*out = append(*out, block)
			return nil
		case "tbl":
			if depth >= maxDepth {
				return x.Skip()
			}
			if err := d.count(); err != nil {
				return err
			}
			table, err := d.readTable(x, depth)
			if err != nil {
				return err
			}
			if len(table.Rows) > 0 {
				*out = append(*out, table)
			}
			return nil
		case "sdt", "sdtContent", "customXml", "smartTag", "ins", "moveTo":
			// These only wrap other blocks.
			return d.readBlocksIn(x, out, depth, cell)
		}
		return x.Skip()
	})
}

func (d *docx) readTable(x *xml.Decoder, depth int) (Block, error) {
	table := Block{Kind: KindTable}
	err := d.readRows(x, &table, depth)
	return table, err
}

// readRows reads the rows up to the end of the open element.
func (d *docx) readRows(x *xml.Decoder, table *Block, depth int) error {
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "tr":
			row := Row{Cells: []Cell{}}
			err := d.readCells(x, &row, depth)
			if len(row.Cells) > 0 {
				table.Rows = append(table.Rows, row)
			}
			return err
		case "sdt", "sdtContent", "ins":
			return d.readRows(x, table, depth)
		}
		return x.Skip()
	})
}

// readCells reads the cells up to the end of the open element.
func (d *docx) readCells(x *xml.Decoder, row *Row, depth int) error {
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "tc":
			if err := d.count(); err != nil {
				return err
			}
			cell := Cell{}
			err := d.readBlocksIn(x, &cell.Blocks, depth+1, &cell)
			cell.Blocks = tidy(cell.Blocks)
			row.Cells = append(row.Cells, cell)
			return err
		case "sdt", "sdtContent", "ins":
			return d.readCells(x, row, depth)
		}
		return x.Skip()
	})
}

type runFormat struct {
	bold, italic, underline, strike, hidden bool
	script                                  string
	color, fill                             string
	// size is the size of the text in half points, or 0 if nothing sets it.
	size int
}

// readRunProps reads w:rPr. It returns the changes and the character style.
func readRunProps(x *xml.Decoder) (runProps, string, error) {
	var props runProps
	style := ""
	toggle := func(start xml.StartElement, set func(*runFormat, bool)) {
		value := on(start)
		props = append(props, func(f *runFormat) { set(f, value) })
	}
	err := children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "rStyle":
			style = attr(start, "val")
		case "b":
			toggle(start, func(f *runFormat, v bool) { f.bold = v })
		case "i":
			toggle(start, func(f *runFormat, v bool) { f.italic = v })
		case "u":
			toggle(start, func(f *runFormat, v bool) { f.underline = v })
		case "strike", "dstrike":
			toggle(start, func(f *runFormat, v bool) { f.strike = v })
		case "vanish":
			toggle(start, func(f *runFormat, v bool) { f.hidden = v })
		case "vertAlign":
			script := ""
			switch attr(start, "val") {
			case "superscript":
				script = "sup"
			case "subscript":
				script = "sub"
			}
			props = append(props, func(f *runFormat) { f.script = script })
		case "color":
			color := hexColor(attr(start, "val"))
			props = append(props, func(f *runFormat) { f.color = color })
		case "highlight":
			fill := highlights[attr(start, "val")]
			props = append(props, func(f *runFormat) { f.fill = fill })
		case "shd":
			fill := hexColor(attr(start, "fill"))
			props = append(props, func(f *runFormat) { f.fill = fill })
		case "sz":
			if n, err := strconv.Atoi(attr(start, "val")); err == nil && n >= 2 && n <= 800 {
				props = append(props, func(f *runFormat) { f.size = n })
			}
		}
		return x.Skip()
	})
	return props, style, err
}

// chain returns a style and the styles that it builds on, the base first.
func (d *docx) chain(id string) []docStyle {
	var out []docStyle
	seen := map[string]bool{}
	for range 12 {
		style, ok := d.styles[id]
		if !ok || seen[id] {
			break
		}
		seen[id] = true
		out = append(out, style)
		id = style.basedOn
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// styleFormat returns the text format of a paragraph style.
func (d *docx) styleFormat(id string) runFormat {
	if format, ok := d.formats[id]; ok {
		return format
	}
	var format runFormat
	d.defaults.apply(&format)
	for _, style := range d.chain(id) {
		style.run.apply(&format)
	}
	d.formats[id] = format
	return format
}

var fieldLink = regexp.MustCompile(`^\s*HYPERLINK\s+"([^"]+)"`)

// paragraph holds the state of one paragraph while the reader walks it.
type paragraph struct {
	block    Block
	style    string
	fillSet  bool
	alignSet bool
	// base is the text format of the style of the paragraph.
	base     runFormat
	hasBase  bool
	heading  bool
	numID    string
	level    int
	hasNum   bool
	outline  int
	instr    strings.Builder
	inInstr  bool
	fieldURL string
}

func (d *docx) readParagraph(x *xml.Decoder) (Block, error) {
	p := &paragraph{block: Block{Kind: KindParagraph}, outline: -1}
	if err := d.readInline(x, p, ""); err != nil {
		return p.block, err
	}
	d.classify(p)
	return p.block, nil
}

// readInline reads the runs up to the end of the open element.
func (d *docx) readInline(x *xml.Decoder, p *paragraph, link string) error {
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "pPr":
			return d.readParagraphProps(x, p)
		case "r":
			return d.readRun(x, p, link)
		case "hyperlink":
			target := d.links[attr(start, "id")]
			if target == "" {
				target = link
			}
			return d.readInline(x, p, target)
		case "ins", "moveTo", "smartTag", "sdt", "sdtContent", "fldSimple", "customXml", "dir", "bdo":
			return d.readInline(x, p, link)
		case "AlternateContent", "oMath", "oMathPara":
			d.doc.Pictures++
			return x.Skip()
		}
		return x.Skip()
	})
}

func (d *docx) readParagraphProps(x *xml.Decoder, p *paragraph) error {
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "pStyle":
			p.style = attr(start, "val")
		case "outlineLvl":
			if n, err := strconv.Atoi(attr(start, "val")); err == nil {
				p.outline = n
			}
		case "jc":
			p.block.Align, p.alignSet = alignOf(attr(start, "val")), true
		case "shd":
			p.block.Fill, p.fillSet = hexColor(attr(start, "fill")), true
		case "rPr", "pPrChange", "sectPr":
			// The format of the paragraph mark and the old properties are not text.
			return x.Skip()
		case "numPr":
			p.hasNum = true
			return children(x, func(start xml.StartElement) error {
				switch start.Name.Local {
				case "numId":
					p.numID = attr(start, "val")
				case "ilvl":
					p.level, _ = strconv.Atoi(attr(start, "val"))
				}
				return x.Skip()
			})
		}
		return x.Skip()
	})
}

func (d *docx) addText(p *paragraph, format runFormat, link, text string) error {
	if text == "" || format.hidden {
		return nil
	}
	d.text += len(text)
	if d.text > d.limits.TextBytes {
		return errLimit
	}
	if link == "" {
		link = p.fieldURL
	}
	run := Run{
		Text: text, Bold: format.bold, Italic: format.italic, Underline: format.underline, Strike: format.strike,
		Script: format.script, Link: link, Color: format.color, Fill: format.fill,
	}
	// A heading has the size of its level. Other text keeps its size relative to the body text.
	if !p.heading && format.size > 0 {
		scale := math.Round(float64(format.size)/float64(d.bodySize)*20) / 20
		if scale < 0.95 || scale > 1.05 {
			run.Scale = min(max(scale, 0.7), 2.2)
		}
	}
	p.block.Runs = appendRun(p.block.Runs, run)
	return nil
}

func (d *docx) readRun(x *xml.Decoder, p *paragraph, link string) error {
	if !p.hasBase {
		style := p.style
		if style == "" {
			style = d.normal
		}
		p.base, p.hasBase = d.styleFormat(style), true
		p.heading = (p.outline >= 0 && p.outline < 9) || d.headingLevel(p.style) > 0
	}
	format := p.base
	return children(x, func(start xml.StartElement) error {
		switch start.Name.Local {
		case "rPr":
			props, style, err := readRunProps(x)
			for _, linked := range d.chain(style) {
				linked.run.apply(&format)
			}
			props.apply(&format)
			return err
		case "t":
			var text strings.Builder
			for {
				token, err := x.Token()
				if err != nil {
					return err
				}
				switch t := token.(type) {
				case xml.CharData:
					text.Write(t)
				case xml.StartElement:
					if err := x.Skip(); err != nil {
						return err
					}
				case xml.EndElement:
					if p.inInstr {
						return nil
					}
					return d.addText(p, format, link, text.String())
				}
			}
		case "instrText":
			var text strings.Builder
			for {
				token, err := x.Token()
				if err != nil {
					return err
				}
				switch t := token.(type) {
				case xml.CharData:
					if p.instr.Len()+text.Len() < 4000 {
						text.Write(t)
					}
				case xml.StartElement:
					if err := x.Skip(); err != nil {
						return err
					}
				case xml.EndElement:
					p.instr.WriteString(text.String())
					return nil
				}
			}
		case "fldChar":
			switch attr(start, "fldCharType") {
			case "begin":
				p.inInstr, p.fieldURL = true, ""
				p.instr.Reset()
			case "separate":
				p.inInstr = false
				if m := fieldLink.FindStringSubmatch(p.instr.String()); m != nil {
					p.fieldURL = safeLink(m[1])
				}
			case "end":
				p.inInstr, p.fieldURL = false, ""
			}
		case "tab":
			if err := x.Skip(); err != nil {
				return err
			}
			return d.addText(p, format, link, "\t")
		case "br", "cr":
			kind := attr(start, "type")
			if err := x.Skip(); err != nil {
				return err
			}
			if kind == "page" || kind == "column" {
				return nil
			}
			return d.addText(p, format, link, "\n")
		case "noBreakHyphen":
			if err := x.Skip(); err != nil {
				return err
			}
			return d.addText(p, format, link, "‑")
		case "footnoteReference", "endnoteReference":
			key := start.Name.Local[:1] + attr(start, "id")
			if err := x.Skip(); err != nil {
				return err
			}
			number, seen := d.noteIndex[key]
			if !seen {
				if d.inNotes || len(d.noteOrder) >= 5000 {
					return nil
				}
				d.noteOrder = append(d.noteOrder, key)
				number = len(d.noteOrder)
				d.noteIndex[key] = number
			}
			p.block.Runs = append(p.block.Runs, Run{Note: number})
			return nil
		case "drawing", "pict", "object", "AlternateContent":
			d.doc.Pictures++
		}
		return x.Skip()
	})
}

var headingName = regexp.MustCompile(`^heading ([1-9])$`)

// headingLevel returns 1 to 6 for a heading style, or 0.
func (d *docx) headingLevel(id string) int {
	for range 12 {
		style, ok := d.styles[id]
		if !ok {
			return 0
		}
		if m := headingName.FindStringSubmatch(style.name); m != nil {
			return min(int(m[1][0]-'0'), 6)
		}
		switch style.name {
		case "title":
			return 1
		case "subtitle":
			return 2
		}
		if style.outline >= 0 && style.outline < 9 {
			return min(style.outline+1, 6)
		}
		id = style.basedOn
	}
	return 0
}

// classify decides whether the paragraph is a heading or a list item. It also
// takes the background and the alignment from the style, if the paragraph sets none.
func (d *docx) classify(p *paragraph) {
	style := p.style
	if style == "" {
		style = d.normal
	}
	for _, linked := range d.chain(style) {
		if linked.fillSet && !p.fillSet {
			p.block.Fill = linked.fill
		}
		if linked.alignSet && !p.alignSet {
			p.block.Align = linked.align
		}
	}
	if p.outline >= 0 && p.outline < 9 {
		p.block.Kind, p.block.Level = KindHeading, min(p.outline+1, 6)
		return
	}
	if level := d.headingLevel(p.style); level > 0 {
		p.block.Kind, p.block.Level = KindHeading, level
		return
	}
	numID, level := p.numID, p.level
	if !p.hasNum {
		id := p.style
		for range 12 {
			style, ok := d.styles[id]
			if !ok {
				break
			}
			if style.numID != "" {
				numID, level = style.numID, style.level
				break
			}
			id = style.basedOn
		}
	}
	if numID == "" || numID == "0" || len(p.block.Runs) == 0 {
		return
	}
	level = min(max(level, 0), 8)
	p.block.Kind, p.block.Level = KindItem, level
	p.block.Marker = d.marker(numID, level)
}

// marker returns the bullet or the number of the next item of a list.
func (d *docx) marker(numID string, level int) string {
	levels := d.abstract[d.nums[numID]]
	counters := d.counters[numID]
	if counters == nil {
		counters = make([]int, 9)
		for i := range counters {
			counters[i] = 0
			if l, ok := levels[i]; ok {
				counters[i] = l.start - 1
			}
		}
		d.counters[numID] = counters
	}
	counters[level]++
	for i := level + 1; i < len(counters); i++ {
		counters[i] = 0
		if l, ok := levels[i]; ok {
			counters[i] = l.start - 1
		}
	}
	current, ok := levels[level]
	if !ok {
		return "•"
	}
	switch current.format {
	case "bullet":
		return "•"
	case "none":
		return ""
	}
	text := current.text
	if text == "" {
		text = fmt.Sprintf("%%%d.", level+1)
	}
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '%' && i+1 < len(text) && text[i+1] >= '1' && text[i+1] <= '9' {
			index := int(text[i+1] - '1')
			format := "decimal"
			if l, ok := levels[index]; ok {
				format = l.format
			}
			out.WriteString(formatNumber(counters[index], format))
			i++
			continue
		}
		out.WriteByte(text[i])
	}
	return out.String()
}

func letters(n int, alphabet []rune) string {
	if n < 1 {
		return strconv.Itoa(n)
	}
	// Word repeats the letter after the end of the alphabet: a, b, … z, aa, bb.
	repeat := (n-1)/len(alphabet) + 1
	return strings.Repeat(string(alphabet[(n-1)%len(alphabet)]), min(repeat, 8))
}

func roman(n int) string {
	if n < 1 || n > 3999 {
		return strconv.Itoa(n)
	}
	values := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	signs := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var out strings.Builder
	for i, value := range values {
		for n >= value {
			out.WriteString(signs[i])
			n -= value
		}
	}
	return out.String()
}

func formatNumber(n int, format string) string {
	switch format {
	case "lowerLetter":
		return letters(n, []rune("abcdefghijklmnopqrstuvwxyz"))
	case "upperLetter":
		return letters(n, []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	case "lowerRoman":
		return strings.ToLower(roman(n))
	case "upperRoman":
		return roman(n)
	case "russianLower":
		return letters(n, []rune("абвгдежзиклмнопрстуфхцчшщэюя"))
	case "russianUpper":
		return letters(n, []rune("АБВГДЕЖЗИКЛМНОПРСТУФХЦЧШЩЭЮЯ"))
	case "decimalZero":
		return fmt.Sprintf("%02d", n)
	}
	return strconv.Itoa(n)
}

func empty(block Block) bool {
	if block.Kind == KindTable {
		return false
	}
	for _, run := range block.Runs {
		if run.Note != 0 || strings.TrimSpace(run.Text) != "" {
			return false
		}
	}
	return true
}

// tidy removes the empty paragraphs at both ends and keeps one of each group
// of empty paragraphs in the middle.
func tidy(blocks []Block) []Block {
	out := make([]Block, 0, len(blocks))
	for _, block := range blocks {
		if empty(block) {
			if len(out) == 0 || empty(out[len(out)-1]) {
				continue
			}
			block = Block{Kind: KindParagraph}
		}
		out = append(out, block)
	}
	for len(out) > 0 && empty(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return out
}
