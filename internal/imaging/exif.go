package imaging

import (
	"io"
	"math"
	"strings"
)

// Meta holds what a camera wrote into a picture.
type Meta struct {
	// Taken is the time of the picture on the clock of the camera, as
	// "2006-01-02T15:04:05". A camera writes no time zone here. It is empty
	// if the file has no such time.
	Taken string
	// HasPlace is true when the file names the place of the picture.
	HasPlace bool
	// Lat and Lon are the place in degrees. North and east are positive.
	Lat, Lon float64
	// Camera is the maker and the model of the camera, in one line.
	Camera string
}

// ReadMeta reads the time, the place and the camera from a JPEG file. Every
// other file, and a JPEG without these marks, gives an empty result. The
// reader takes numbers and short texts only and trusts no size in the file.
func ReadMeta(src io.Reader) Meta {
	data, err := io.ReadAll(io.LimitReader(src, exifLimit))
	if err != nil {
		return Meta{}
	}
	t := newTiff(exifBlock(data))
	if t == nil {
		return Meta{}
	}
	var meta Meta
	main := t.dir(t.u32(4))
	maker := cleanText(t.text(main[0x010F]))
	model := cleanText(t.text(main[0x0110]))
	switch {
	case model == "":
		meta.Camera = maker
	case maker == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(maker)):
		meta.Camera = model
	default:
		meta.Camera = maker + " " + model
	}
	var details map[int]tiffEntry
	if entry, ok := main[0x8769]; ok {
		details = t.dir(entry.value)
	}
	// The time of the shot comes first. A scanner and an editor write only the common time.
	for _, entry := range []tiffEntry{details[0x9003], details[0x9004], main[0x0132]} {
		if taken := exifTime(t.text(entry)); taken != "" {
			meta.Taken = taken
			break
		}
	}
	if entry, ok := main[0x8825]; ok {
		place := t.dir(entry.value)
		lat, okLat := t.degrees(place[2], t.text(place[1]), "N", "S", 90)
		lon, okLon := t.degrees(place[4], t.text(place[3]), "E", "W", 180)
		// A camera without a signal writes zeros.
		if okLat && okLon && (lat != 0 || lon != 0) {
			meta.HasPlace, meta.Lat, meta.Lon = true, lat, lon
		}
	}
	return meta
}

// exifBlock returns the TIFF block of the EXIF segment of a JPEG, or nil.
func exifBlock(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	at := 2
	for at+4 <= len(data) {
		if data[at] != 0xFF {
			return nil
		}
		marker := data[at+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			at += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return nil
		}
		size := int(data[at+2])<<8 | int(data[at+3])
		if size < 2 || at+2+size > len(data) {
			return nil
		}
		if segment := data[at+4 : at+2+size]; marker == 0xE1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			return segment[6:]
		}
		at += 2 + size
	}
	return nil
}

// tiff reads the lists of marks of an EXIF block.
type tiff struct {
	data   []byte
	little bool
}

// tiffEntry is one mark: its type, the number of values, and the four bytes
// that hold the values or their place in the block.
type tiffEntry struct {
	kind, count, value int
	at                 int
}

func newTiff(data []byte) *tiff {
	if len(data) < 8 {
		return nil
	}
	t := &tiff{data: data}
	switch string(data[:2]) {
	case "II":
		t.little = true
	case "MM":
	default:
		return nil
	}
	if t.u16(2) != 42 {
		return nil
	}
	return t
}

func (t *tiff) u16(at int) int {
	if at < 0 || at+2 > len(t.data) {
		return 0
	}
	b := t.data[at:]
	if t.little {
		return int(b[0]) | int(b[1])<<8
	}
	return int(b[1]) | int(b[0])<<8
}

func (t *tiff) u32(at int) int {
	if at < 0 || at+4 > len(t.data) {
		return 0
	}
	b := t.data[at:]
	if t.little {
		return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24
	}
	return int(b[3]) | int(b[2])<<8 | int(b[1])<<16 | int(b[0])<<24
}

// maxTiffEntries limits one list of marks. A camera writes a few dozen.
const maxTiffEntries = 512

// dir reads the list of marks at a place of the block.
func (t *tiff) dir(offset int) map[int]tiffEntry {
	out := map[int]tiffEntry{}
	if offset < 8 || offset+2 > len(t.data) {
		return out
	}
	entries := min(t.u16(offset), maxTiffEntries)
	for i := 0; i < entries; i++ {
		start := offset + 2 + i*12
		if start+12 > len(t.data) {
			break
		}
		out[t.u16(start)] = tiffEntry{kind: t.u16(start + 2), count: t.u32(start + 4), value: t.u32(start + 8), at: start + 8}
	}
	return out
}

// bytes returns the values of a mark with values of the given size.
func (t *tiff) bytes(entry tiffEntry, size int) []byte {
	if entry.count <= 0 || entry.count > 1024 {
		return nil
	}
	length := entry.count * size
	start := entry.value
	if length <= 4 {
		start = entry.at
	}
	if start < 0 || start+length > len(t.data) {
		return nil
	}
	return t.data[start : start+length]
}

// text returns a mark that holds a text.
func (t *tiff) text(entry tiffEntry) string {
	if entry.kind != 2 {
		return ""
	}
	raw := t.bytes(entry, 1)
	if end := strings.IndexByte(string(raw), 0); end >= 0 {
		raw = raw[:end]
	}
	return string(raw)
}

// degrees reads a latitude or a longitude: degrees, minutes and seconds as
// three fractions, and the letter of the half of the globe.
func (t *tiff) degrees(entry tiffEntry, ref, plus, minus string, limit float64) (float64, bool) {
	if entry.kind != 5 || entry.count != 3 {
		return 0, false
	}
	raw := t.bytes(entry, 8)
	if raw == nil {
		return 0, false
	}
	base := entry.value
	var parts [3]float64
	for i := range parts {
		upper, lower := t.u32(base+i*8), t.u32(base+i*8+4)
		if lower == 0 {
			return 0, false
		}
		parts[i] = float64(uint32(upper)) / float64(uint32(lower)) // #nosec G115 -- u32 returns four bytes.
	}
	value := parts[0] + parts[1]/60 + parts[2]/3600
	switch strings.ToUpper(strings.TrimSpace(ref)) {
	case plus:
	case minus:
		value = -value
	default:
		return 0, false
	}
	if math.IsNaN(value) || math.Abs(value) > limit {
		return 0, false
	}
	// Seven places are about one centimetre: more than a camera knows.
	return math.Round(value*1e7) / 1e7, true
}

// exifTime turns "2006:01:02 15:04:05" into "2006-01-02T15:04:05". It
// returns an empty text for a time that is not a real one.
func exifTime(text string) string {
	text = strings.TrimSpace(text)
	if len(text) != 19 {
		return ""
	}
	number := func(from, to int) int {
		value := 0
		for _, c := range text[from:to] {
			if c < '0' || c > '9' {
				return -1
			}
			value = value*10 + int(c-'0')
		}
		return value
	}
	year, month, day := number(0, 4), number(5, 7), number(8, 10)
	hour, minute, second := number(11, 13), number(14, 16), number(17, 19)
	if year < 1826 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 ||
		hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 60 {
		return ""
	}
	return text[0:4] + "-" + text[5:7] + "-" + text[8:10] + "T" + text[11:13] + ":" + text[14:16] + ":" + text[17:19]
}

// cleanText keeps the printable part of a short text from a file.
func cleanText(text string) string {
	var out strings.Builder
	for _, c := range strings.TrimSpace(text) {
		if c < 0x20 || c == 0x7F || c > 0x7E {
			continue
		}
		out.WriteRune(c)
		if out.Len() >= 64 {
			break
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}
