package imaging

import (
	"bytes"
	"os"
	"testing"
)

func readMeta(t *testing.T, name string) Meta {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return ReadMeta(bytes.NewReader(data))
}

// testdata/exif.py draws the pictures and writes these values.
func TestReadMeta(t *testing.T) {
	got := readMeta(t, "exif-camera.jpg")
	want := Meta{Taken: "2019-07-14T15:42:10", HasPlace: true, Lat: -12.51, Lon: -45.25, Camera: "Example Cam 3"}
	if got != want {
		t.Errorf("a picture from a camera:\n got %+v\nwant %+v", got, want)
	}
	// A scanner writes only the common time and no place.
	got = readMeta(t, "exif-scanner.jpg")
	want = Meta{Taken: "2024-11-05T09:00:00", Camera: "Example Scan 9"}
	if got != want {
		t.Errorf("a picture from a scanner:\n got %+v\nwant %+v", got, want)
	}
	// Zeros are no time and no place.
	if got := readMeta(t, "exif-no-signal.jpg"); got != (Meta{}) {
		t.Errorf("a picture with zeros: %+v", got)
	}
	if got := readMeta(t, "exif-none.jpg"); got != (Meta{}) {
		t.Errorf("a picture without marks: %+v", got)
	}
}

func TestReadMetaTrustsNoFile(t *testing.T) {
	data, err := os.ReadFile("testdata/exif-camera.jpg")
	if err != nil {
		t.Fatal(err)
	}
	// Every cut and every broken byte must end without a panic.
	for cut := 0; cut <= len(data); cut++ {
		ReadMeta(bytes.NewReader(data[:cut]))
	}
	for at := 0; at < min(len(data), 400); at++ {
		for _, value := range []byte{0x00, 0xFF, 0x7F} {
			broken := bytes.Clone(data)
			broken[at] = value
			ReadMeta(bytes.NewReader(broken))
		}
	}
	for _, text := range []string{"", "not a picture", "\xFF\xD8\xFF\xE1\x00\x08Exif\x00\x00"} {
		if got := ReadMeta(bytes.NewReader([]byte(text))); got != (Meta{}) {
			t.Errorf("%q: %+v", text, got)
		}
	}
}

func TestExifTime(t *testing.T) {
	for text, want := range map[string]string{
		"2019:07:14 15:42:10": "2019-07-14T15:42:10",
		"2019-07-14 15:42:10": "2019-07-14T15:42:10",
		"0000:00:00 00:00:00": "",
		"2019:13:14 15:42:10": "",
		"2019:07:14 25:42:10": "",
		"2019:07:14":          "",
		"    :  :     :  :  ": "",
	} {
		if got := exifTime(text); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}
