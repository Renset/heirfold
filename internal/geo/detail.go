package geo

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"sync"
)

// The close map. The file of the whole world has only the outlines of
// countries and the large rivers. For a map that comes close, the program
// holds rivers, lakes, the borders of regions and the main roads at a larger
// scale, from Natural Earth 1:10m (public domain), cut into squares of two
// degrees. scripts/geo/build.py makes the file.

//go:embed data/detail.pack
var detailFile []byte

const (
	// DetailSquare is the side of one square, in degrees.
	DetailSquare = 2
	// DetailRows and DetailColumns are the numbers of squares from south to
	// north and from west to east.
	DetailRows    = 180 / DetailSquare
	DetailColumns = 360 / DetailSquare

	detailMark = "HFMAP1\n"
)

var (
	detailOnce  sync.Once
	detailIndex map[[2]uint16][2]uint32
	errDetail   error
)

func readDetailIndex() {
	head := len(detailMark) + 4
	if len(detailFile) < head || string(detailFile[:len(detailMark)]) != detailMark {
		errDetail = errors.New("geo: the file of the close map is broken")
		return
	}
	count := int(binary.LittleEndian.Uint32(detailFile[len(detailMark):]))
	if count < 0 || head+12*count > len(detailFile) {
		errDetail = errors.New("geo: the list of the close map is broken")
		return
	}
	index := make(map[[2]uint16][2]uint32, count)
	for i := 0; i < count; i++ {
		entry := detailFile[head+12*i:]
		row, column := binary.LittleEndian.Uint16(entry), binary.LittleEndian.Uint16(entry[2:])
		offset, length := binary.LittleEndian.Uint32(entry[4:]), binary.LittleEndian.Uint32(entry[8:])
		if uint64(offset)+uint64(length) > uint64(len(detailFile)) {
			errDetail = errors.New("geo: a square of the close map lies outside the file")
			return
		}
		index[[2]uint16{row, column}] = [2]uint32{offset, length}
	}
	detailIndex = index
}

// Detail returns one square of the close map as gzip of JSON. The row counts
// from the south pole and the column from the line of the change of the
// date. The second result is false for a square without anything: open sea.
// The JSON has the lists "v" (rivers), "d" (roads), "b" (borders of regions)
// and "l" (lakes). Each line is a list of whole numbers of 1/2000 of a
// degree: the first point as longitude and latitude, then the steps. A river
// and a road start with their rank: a small number is a large river.
func Detail(row, column int) ([]byte, bool, error) {
	detailOnce.Do(readDetailIndex)
	if errDetail != nil {
		return nil, false, errDetail
	}
	if row < 0 || row >= DetailRows || column < 0 || column >= DetailColumns {
		return nil, false, nil
	}
	place, ok := detailIndex[[2]uint16{uint16(row), uint16(column)}] // #nosec G115 -- the numbers are inside the limits above.
	if !ok {
		return nil, false, nil
	}
	return detailFile[place[0] : place[0]+place[1]], true, nil
}
