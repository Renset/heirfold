package docview

import (
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// cp1251 holds the characters 0x80 to 0xBF of Windows-1251. The characters
// from 0xC0 are the Russian alphabet in order.
var cp1251 = [64]rune{
	0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021, 0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
	0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0xFFFD, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
	0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7, 0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
	0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7, 0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
}

func decode1251(data []byte) string {
	var out strings.Builder
	out.Grow(len(data) * 2)
	for _, b := range data {
		switch {
		case b < 0x80:
			out.WriteByte(b)
		case b < 0xC0:
			out.WriteRune(cp1251[b-0x80])
		default:
			out.WriteRune(rune(b-0xC0) + 0x0410)
		}
	}
	return out.String()
}

func decodeUTF16(data []byte, big bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if big {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return string(utf16.Decode(units))
}

// FromText reads a plain text file. It accepts UTF-8, UTF-16 with a byte
// order mark, and Windows-1251.
func FromText(r io.Reader, limits Limits) (*Document, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limits.TextBytes)+1))
	if err != nil {
		return nil, err
	}
	doc := &Document{}
	if len(data) > limits.TextBytes {
		data = data[:limits.TextBytes]
		doc.Truncated = true
	}
	var text string
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		text = decodeUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		text = decodeUTF16(data[2:], true)
	default:
		if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
			data = data[3:]
		}
		if doc.Truncated {
			// The cut can fall inside a character.
			for len(data) > 0 && !utf8.Valid(data) && len(data) > limits.TextBytes-4 {
				data = data[:len(data)-1]
			}
		}
		if utf8.Valid(data) {
			text = string(data)
		} else {
			text = decode1251(data)
		}
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	// A file with control characters is not a text for people.
	for _, c := range text {
		if c < 0x20 && c != '\n' && c != '\t' && c != '\f' {
			return nil, ErrFormat
		}
	}
	text = strings.TrimRight(text, "\n\t \f")
	if text != "" {
		doc.Blocks = []Block{{Kind: KindText, Runs: []Run{{Text: text}}}}
	} else {
		doc.Blocks = []Block{}
	}
	return doc, nil
}
