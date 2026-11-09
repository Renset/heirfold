// Package docview turns a text document into a small model that a browser can
// show without any markup from the file. The reader of a Word file keeps the
// text, the headings, the lists, the tables, the links, the notes, the colors
// and the sizes of the text. It drops the pictures, the fonts and the page layout.
package docview

import (
	"errors"
)

// Document is the readable content of a file.
type Document struct {
	Blocks []Block `json:"blocks"`
	Notes  []Note  `json:"notes,omitempty"`
	// Pictures counts the pictures and drawings that the view does not show.
	Pictures int `json:"pictures,omitempty"`
	// Truncated is true when the file is longer than the view shows.
	Truncated bool `json:"truncated,omitempty"`
}

// The kinds of a block.
const (
	KindParagraph = "p"
	KindHeading   = "h"
	KindItem      = "item"
	KindTable     = "table"
	KindText      = "text"
)

// Block is a paragraph, a heading, a list item, a table or a plain text.
type Block struct {
	Kind string `json:"kind"`
	// Level is 1 to 6 for a heading and 0 to 8 for a list item.
	Level int `json:"level,omitempty"`
	// Marker is the bullet or the number of a list item.
	Marker string `json:"marker,omitempty"`
	// Align is "center", "end" or "justify". The default is the start of the line.
	Align string `json:"align,omitempty"`
	// Fill is the background color of the block, as #RRGGBB.
	Fill string `json:"fill,omitempty"`
	Runs []Run  `json:"runs,omitempty"`
	Rows []Row  `json:"rows,omitempty"`
}

// Row is a row of a table.
type Row struct {
	Cells []Cell `json:"cells"`
}

// Cell is a cell of a table.
type Cell struct {
	// Span is the number of columns that the cell covers, if more than one.
	Span int `json:"span,omitempty"`
	// Fill is the background color of the cell, as #RRGGBB.
	Fill   string  `json:"fill,omitempty"`
	Blocks []Block `json:"blocks"`
}

// Run is a piece of text with one format.
type Run struct {
	Text      string `json:"text,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Strike    bool   `json:"strike,omitempty"`
	// Script is "sup" or "sub".
	Script string `json:"script,omitempty"`
	// Color is the color of the text and Fill is the color behind it, as #RRGGBB.
	Color string `json:"color,omitempty"`
	Fill  string `json:"fill,omitempty"`
	// Scale is the size of the text relative to the body text, if it differs.
	Scale float64 `json:"scale,omitempty"`
	// Link is an address that starts with http:, https: or mailto:.
	Link string `json:"link,omitempty"`
	// Note is the number of a note. Such a run has no text.
	Note int `json:"note,omitempty"`
}

// Note is a footnote or an endnote.
type Note struct {
	Number int     `json:"number"`
	Blocks []Block `json:"blocks"`
}

// Limits bound the work on one file.
type Limits struct {
	// PartBytes is the largest size of one XML part after decompression.
	PartBytes int64
	// TextBytes is the largest amount of text in the result.
	TextBytes int
	// Blocks is the largest number of blocks and cells in the result.
	Blocks int
}

// DefaultLimits fit a long research report.
var DefaultLimits = Limits{PartBytes: 32 << 20, TextBytes: 2 << 20, Blocks: 20000}

// ErrFormat means that the file is not a document of the expected kind.
var ErrFormat = errors.New("docview: the file is not a readable document")

// errLimit stops the reader when the result is full.
var errLimit = errors.New("docview: limit reached")

// same reports whether two runs have the same format.
func (r Run) same(o Run) bool {
	return r.Bold == o.Bold && r.Italic == o.Italic && r.Underline == o.Underline && r.Strike == o.Strike &&
		r.Script == o.Script && r.Link == o.Link && r.Color == o.Color && r.Fill == o.Fill && r.Scale == o.Scale &&
		r.Note == 0 && o.Note == 0
}

// appendRun adds a run and joins it with the last one when the format is the same.
func appendRun(runs []Run, run Run) []Run {
	if run.Text == "" && run.Note == 0 {
		return runs
	}
	if n := len(runs); n > 0 && runs[n-1].same(run) {
		runs[n-1].Text += run.Text
		return runs
	}
	return append(runs, run)
}
