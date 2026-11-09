// Package geo names the settlement near a point of the globe. It asks no
// service: the list of settlements is a file inside the program, so the
// place of a family photo never leaves the server.
//
// The list comes from GeoNames (https://www.geonames.org), under the Creative
// Commons Attribution 4.0 License. scripts/geo/build.py makes the file.
package geo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed data/places.tsv.gz
var placesFile []byte

// Place is a town or a village.
type Place struct {
	// Name is the usual international name. NameRu is the Russian name, or
	// an empty text when the list has none.
	Name, NameRu string
	// Country is the code of the country in two letters, such as "FR".
	Country  string
	Lat, Lon float64
	// People is the number of people who live there, or 0 if nobody knows.
	People int
}

// cell is the side of one square of the index, in degrees.
const cell = 0.5

// reach is how far from a settlement a point still gets its name, in
// kilometres. A point at sea or in a desert gets no name.
const reach = 25.0

// Index finds settlements by place. It holds the whole list in memory, about
// 25 MB, so a caller that needs it rarely opens it, uses it and drops it.
type Index struct {
	places []Place
	cells  map[[2]int32][]int32
}

func cellOf(lat, lon float64) [2]int32 {
	return [2]int32{int32(math.Floor(lat / cell)), int32(math.Floor(lon / cell))}
}

// Open reads the list of settlements.
func Open() (*Index, error) {
	reader, err := gzip.NewReader(bytes.NewReader(placesFile))
	if err != nil {
		return nil, fmt.Errorf("geo: %w", err)
	}
	defer func() { _ = reader.Close() }()
	index := &Index{cells: map[[2]int32][]int32{}}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("geo: line %d has %d fields", len(index.places)+1, len(fields))
		}
		lat, errLat := strconv.ParseFloat(fields[3], 64)
		lon, errLon := strconv.ParseFloat(fields[4], 64)
		people, errPeople := strconv.Atoi(fields[5])
		if errLat != nil || errLon != nil || errPeople != nil {
			return nil, fmt.Errorf("geo: line %d has a bad number", len(index.places)+1)
		}
		index.add(Place{Name: fields[0], NameRu: fields[1], Country: fields[2], Lat: lat, Lon: lon, People: people})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("geo: %w", err)
	}
	return index, nil
}

func (x *Index) add(place Place) {
	key := cellOf(place.Lat, place.Lon)
	x.cells[key] = append(x.cells[key], int32(len(x.places))) // #nosec G115 -- the list has about 230 000 lines.
	x.places = append(x.places, place)
}

// Len returns the number of settlements.
func (x *Index) Len() int { return len(x.places) }

// distance returns the kilometres between two points that are near to each other.
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	const perDegree = 111.2
	dLon := math.Abs(lon1 - lon2)
	if dLon > 180 {
		dLon = 360 - dLon
	}
	x := dLon * math.Cos((lat1+lat2)/2*math.Pi/180)
	y := lat1 - lat2
	return perDegree * math.Sqrt(x*x+y*y)
}

// size is a guess of how far a settlement reaches from its middle, in
// kilometres. A city of millions covers the villages that it grew over.
func size(people int) float64 {
	return 1.5 + 0.6*math.Sqrt(float64(people)/3000/math.Pi)
}

// Nearest returns the settlement of a point. A large city wins over a small
// place near it when the point lies inside the city, and a part of a city
// that the list names as a place of its own gives the name of the city. The
// second result is false when no settlement is within reach.
func (x *Index) Nearest(lat, lon float64) (Place, bool) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return Place{}, false
	}
	// The widest city of the list reaches about 40 km.
	const look = 45.0
	rows := int32(math.Ceil(look / 111.2 / cell))
	shrink := math.Max(math.Cos(lat*math.Pi/180), 0.05)
	columns := int32(math.Min(math.Ceil(look/111.2/shrink/cell), 360/cell))
	home := cellOf(lat, lon)
	width := int32(360 / cell)
	best, bestScore, found := Place{}, math.Inf(1), false
	var near []Place
	for row := home[0] - rows; row <= home[0]+rows; row++ {
		for step := -columns; step <= columns; step++ {
			// The index goes around the globe at the line of the change of the date.
			column := ((home[1]+step+width/2)%width+width)%width - width/2
			for _, at := range x.cells[[2]int32{row, column}] {
				place := x.places[at]
				far := distance(lat, lon, place.Lat, place.Lon)
				within := size(place.People)
				if far > math.Max(reach, within) {
					continue
				}
				near = append(near, place)
				if score := far / within; score < bestScore {
					best, bestScore, found = place, score, true
				}
			}
		}
	}
	// The list names some parts of a city as places, such as the districts of
	// Paris. A place deep inside a much larger city is a part of that city.
	for _, city := range near {
		if city.People >= 5*max(best.People, 1) && city.People > 100_000 &&
			distance(best.Lat, best.Lon, city.Lat, city.Lon) <= 0.6*size(city.People) {
			best = city
		}
	}
	return best, found
}

// startsWith reports whether text starts with prefix. It takes capital and
// small letters as the same. prefix must be in small letters.
func startsWith(text, prefix string) bool {
	for len(prefix) > 0 {
		if len(text) == 0 {
			return false
		}
		a, sizeA := utf8.DecodeRuneInString(text)
		b, sizeB := utf8.DecodeRuneInString(prefix)
		if unicode.ToLower(a) != b {
			return false
		}
		text, prefix = text[sizeA:], prefix[sizeB:]
	}
	return true
}

// Search returns the settlements whose international or Russian name starts
// with the text: first the places with exactly that name, then the others,
// each group with the most people first.
func (x *Index) Search(text string, limit int) []Place {
	prefix := strings.ToLower(strings.TrimSpace(text))
	if utf8.RuneCountInString(prefix) < 2 || limit <= 0 {
		return nil
	}
	var found []Place
	for i := range x.places {
		if place := &x.places[i]; startsWith(place.Name, prefix) || (place.NameRu != "" && startsWith(place.NameRu, prefix)) {
			found = append(found, *place)
		}
	}
	// A place with exactly this name comes before a longer name, then the place with the most people.
	whole := func(place Place) int {
		if strings.ToLower(place.Name) == prefix || strings.ToLower(place.NameRu) == prefix {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(found, func(a, b Place) int {
		if byName := whole(a) - whole(b); byName != 0 {
			return byName
		}
		return b.People - a.People
	})
	if len(found) > limit {
		found = found[:limit]
	}
	return found
}

// Within returns the settlements inside a box of the globe, the place with
// the most people first. The box must not cross the line of the change of
// the date. A map uses it to name the towns and the villages that it shows.
func (x *Index) Within(south, west, north, east float64, limit int) []Place {
	if !(south < north) || !(west < east) || south < -90 || north > 90 || west < -180 || east > 180 || limit <= 0 {
		return nil
	}
	var found []Place
	from, to := cellOf(south, west), cellOf(north, east)
	for row := from[0]; row <= to[0]; row++ {
		for column := from[1]; column <= to[1]; column++ {
			for _, at := range x.cells[[2]int32{row, column}] {
				if place := &x.places[at]; place.Lat >= south && place.Lat <= north && place.Lon >= west && place.Lon <= east {
					found = append(found, *place)
				}
			}
		}
	}
	slices.SortStableFunc(found, func(a, b Place) int { return b.People - a.People })
	if len(found) > limit {
		found = found[:limit]
	}
	return found
}
