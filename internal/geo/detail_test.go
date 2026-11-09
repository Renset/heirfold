package geo

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"
)

type square struct {
	Rivers  [][]int `json:"v"`
	Roads   [][]int `json:"d"`
	Borders [][]int `json:"b"`
	Lakes   [][]int `json:"l"`
}

func readSquare(t *testing.T, row, column int) (square, bool) {
	t.Helper()
	data, ok, err := Detail(row, column)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return square{}, false
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var out square
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, true
}

func TestDetail(t *testing.T) {
	// The square of a large city on two rivers: 54 to 56 degrees north, 54 to 56 degrees east.
	row, column := (54+90)/DetailSquare, (54+180)/DetailSquare
	got, ok := readSquare(t, row, column)
	if !ok || len(got.Rivers) == 0 || len(got.Roads) == 0 || len(got.Borders) == 0 {
		t.Fatalf("the square of a city: %v, rivers %d, roads %d, borders %d", ok, len(got.Rivers), len(got.Roads), len(got.Borders))
	}
	// Every line starts in its square, and no step is longer than the square.
	const q = 2000
	check := func(kind string, line []int, ranked bool) {
		if ranked {
			if line[0] < 0 || line[0] > 12 {
				t.Errorf("%s: rank %d", kind, line[0])
			}
			line = line[1:]
		}
		if len(line) < 4 || len(line)%2 != 0 {
			t.Fatalf("%s: a line of %d numbers", kind, len(line))
		}
		lon, lat := float64(line[0])/q, float64(line[1])/q
		if lon < 54 || lon > 56 || lat < 54 || lat > 56 {
			t.Errorf("%s: a line starts at %.3f, %.3f", kind, lon, lat)
		}
		for i := 2; i < len(line); i++ {
			if line[i] > 3*q || line[i] < -3*q {
				t.Errorf("%s: a step of %d", kind, line[i])
			}
		}
	}
	for _, line := range got.Rivers {
		check("river", line, true)
	}
	for _, line := range got.Roads {
		check("road", line, true)
	}
	for _, line := range got.Borders {
		check("border", line, false)
	}

	// The open sea has no square, and a number outside the globe has none.
	for _, at := range [][2]int{{(-30 + 90) / DetailSquare, (-20 + 180) / DetailSquare}, {-1, 0}, {DetailRows, 0}, {0, DetailColumns}} {
		if data, ok, err := Detail(at[0], at[1]); err != nil || ok || data != nil {
			t.Errorf("square %v: %v %v", at, ok, err)
		}
	}
}
