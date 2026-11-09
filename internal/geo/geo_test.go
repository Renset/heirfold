package geo

import "testing"

func TestNearest(t *testing.T) {
	index, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if index.Len() < 200_000 {
		t.Fatalf("the list has %d settlements", index.Len())
	}
	for _, c := range []struct {
		about    string
		lat, lon float64
		name, ru string
		country  string
	}{
		{"the middle of a capital", 55.7539, 37.6208, "Moscow", "Москва", "RU"},
		// A point inside a large city, where a small town of the list is nearer than the middle of the city.
		{"the edge of a capital", 55.6100, 37.7200, "Moscow", "Москва", "RU"},
		{"a city on a river", 54.7350, 55.9580, "Ufa", "Уфа", "RU"},
		{"a capital in the west", 48.8584, 2.2945, "Paris", "Париж", "FR"},
		{"a city in the south half", -33.8568, 151.2153, "Sydney", "Сидней", "AU"},
		// A town of its own next to a capital keeps its name.
		{"a town next to a capital", 55.8890, 37.4450, "Khimki", "Химки", "RU"},
	} {
		got, ok := index.Nearest(c.lat, c.lon)
		if !ok || got.Country != c.country || (c.name != "" && (got.Name != c.name || got.NameRu != c.ru)) {
			t.Errorf("%s: got %+v, %v", c.about, got, ok)
		}
	}
	for about, point := range map[string][2]float64{
		"the open sea":         {-12.51, -45.25},
		"the middle of a pole": {89.9, 10},
		"no point":             {120, 10},
	} {
		if got, ok := index.Nearest(point[0], point[1]); ok {
			t.Errorf("%s: got %+v", about, got)
		}
	}
}

func BenchmarkNearest(b *testing.B) {
	index, err := Open()
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		index.Nearest(54.7350, 55.9580)
	}
}

func TestNearestGoesAroundTheGlobe(t *testing.T) {
	index := &Index{cells: map[[2]int32][]int32{}}
	index.add(Place{Name: "East End", Country: "XX", Lat: 10, Lon: 179.95, People: 900})
	index.add(Place{Name: "Far Town", Country: "XX", Lat: 10, Lon: 170, People: 900})
	// The point is on the other side of the line of the change of the date, 11 km away.
	if got, ok := index.Nearest(10, -179.95); !ok || got.Name != "East End" {
		t.Errorf("across the line: %+v %v", got, ok)
	}
	if got, ok := index.Nearest(10, 175); ok {
		t.Errorf("far from every place: %+v", got)
	}
}

func TestSearch(t *testing.T) {
	index, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	// The largest place with the name comes first, in both languages and with any letters.
	for text, want := range map[string]string{"Каза": "Kazan", "kaza": "Kazan", "  УФА ": "Ufa", "Pari": "Paris", "стерлит": "Sterlitamak"} {
		got := index.Search(text, 5)
		if len(got) == 0 || got[0].Name != want {
			t.Errorf("%q: got %+v, want %s first", text, got, want)
		}
		if len(got) > 5 {
			t.Errorf("%q: %d places", text, len(got))
		}
	}
	for _, text := range []string{"", " ", "a", "я", "qqqqzzzz"} {
		if got := index.Search(text, 5); len(got) != 0 {
			t.Errorf("%q: got %+v", text, got)
		}
	}
}

func TestWithin(t *testing.T) {
	index, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	// A box of one degree around a large city: the city is first, and every place is inside.
	got := index.Within(54.2, 55.4, 55.2, 56.6, 40)
	if len(got) < 5 || len(got) > 40 || got[0].Name != "Ufa" {
		t.Fatalf("places in the box: %d, first %+v", len(got), got[:min(len(got), 1)])
	}
	for i, place := range got {
		if place.Lat < 54.2 || place.Lat > 55.2 || place.Lon < 55.4 || place.Lon > 56.6 {
			t.Errorf("%+v is outside the box", place)
		}
		if i > 0 && place.People > got[i-1].People {
			t.Errorf("%+v stands after a smaller place", place)
		}
	}
	for name, box := range map[string][4]float64{
		"an empty box":        {10, 10, 10, 10},
		"a box upside down":   {55, 55, 54, 56},
		"a box off the globe": {80, 170, 95, 190},
		"the open sea":        {-30, -20, -29, -19},
	} {
		if got := index.Within(box[0], box[1], box[2], box[3], 10); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}
