package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Renset/heirfold/internal/geo"
	"github.com/Renset/heirfold/internal/imaging"
	"github.com/Renset/heirfold/internal/store"
)

// infoInterval is the time between two looks for pictures whose files the
// server has not read. An upload reads its file at once; the look finds the
// pictures that came with a tree.
const infoInterval = 10 * time.Minute

// readFileInfo reads the time and the place from the files of
// the pictures that have no record yet. It returns the number of pictures.
// The data stays on the server: no service gets a file or a place.
func (s *Server) readFileInfo(ctx context.Context) (int, error) {
	if s.mediaDir == "" {
		return 0, nil
	}
	done := 0
	for {
		jobs, err := s.store.InfoJobs(ctx, 50)
		if err != nil || len(jobs) == 0 {
			return done, err
		}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return done, err
			}
			// A file that is missing or is no JPEG gets an empty record.
			var info store.MediaInfo
			if path, _, err := s.mediaPath(&store.MediaFile{Path: job.Path}); err == nil {
				if source, err := os.Open(path); err == nil { // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
					meta := imaging.ReadMeta(source)
					_ = source.Close()
					info = store.MediaInfo{Taken: meta.Taken, HasPlace: meta.HasPlace, Lat: meta.Lat, Lon: meta.Lon}
				}
			}
			if info.HasPlace {
				towns, err := s.towns.open()
				if err != nil {
					return done, err
				}
				if town, ok := towns.Nearest(info.Lat, info.Lon); ok {
					info.Town, info.TownRu, info.Country = town.Name, town.NameRu, town.Country
				}
			}
			if err := s.store.SaveInfo(ctx, job, info); err != nil {
				return done, err
			}
			done++
		}
	}
}

// runFileInfo reads the files of new pictures until the server stops.
func (s *Server) runFileInfo(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if done, err := s.readFileInfo(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("reading picture files failed", "error", err)
		} else if done > 0 {
			s.log.Info("picture files read", "pictures", done)
		}
		timer.Reset(infoInterval)
	}
}

// fileInfoNow reads the file of a picture that just came, so the page of the
// photo shows the time and the place at once. A failure only delays that.
func (s *Server) fileInfoNow(ctx context.Context) {
	if _, err := s.readFileInfo(ctx); err != nil {
		s.log.Warn("reading picture files failed", "error", err)
	}
}

// townList keeps the list of settlements while somebody uses it. The list
// takes about 25 MB and the server needs it rarely: for a new photo with a
// place, and while an editor looks for a place. So the server opens the list
// on the first use and drops it after some quiet minutes.
type townList struct {
	mu    sync.Mutex
	index *geo.Index
	timer *time.Timer
}

// townIdle is how long the list of settlements stays in memory without use.
const townIdle = 5 * time.Minute

func (l *townList) open() (*geo.Index, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.index == nil {
		index, err := geo.Open()
		if err != nil {
			return nil, err
		}
		l.index = index
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timer = time.AfterFunc(townIdle, func() {
		l.mu.Lock()
		l.index = nil
		l.mu.Unlock()
	})
	return l.index, nil
}

// foundPlace is one answer of the search for a place.
type foundPlace struct {
	Name    string  `json:"name"`
	NameRu  string  `json:"nameRu,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	// Tree is true for a place of the family tree.
	Tree bool `json:"tree,omitempty"`
	// People is the number of people who live there. A map uses it for the size of a name.
	People int `json:"people,omitempty"`
}

// maxFoundPlaces limits the answers of one search.
const maxFoundPlaces = 8

// handlePlaceSearch finds a place for a photo by the start of its name. The
// places of the tree come first: a family tree names villages that no list
// has. Then come the settlements of the list, the largest first.
func (s *Server) handlePlaceSearch(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow("places:"+strconv.FormatInt(userFrom(r).ID, 10), rulePlaceSearch) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := []foundPlace{}
	if utf8.RuneCountInString(query) < 2 || len(query) > 120 {
		writeJSON(w, http.StatusOK, map[string]any{"places": out})
		return
	}
	var own []store.TreePlace
	if err := s.store.Inspect(r.Context(), func(e *store.Edit) error {
		own = e.PlacesWithPoints()
		return nil
	}); err != nil {
		s.internal(w, err)
		return
	}
	for _, place := range own {
		if len(out) < maxFoundPlaces/2 && strings.Contains(strings.ToLower(place.Name), query) {
			out = append(out, foundPlace{Name: place.Name, Lat: place.Lat, Lon: place.Lon, Tree: true})
		}
	}
	towns, err := s.towns.open()
	if err != nil {
		s.internal(w, err)
		return
	}
	for _, town := range towns.Search(query, maxFoundPlaces-len(out)) {
		out = append(out, foundPlace{Name: town.Name, NameRu: town.NameRu, Country: town.Country, Lat: town.Lat, Lon: town.Lon})
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": out})
}

// maxMapBox limits the box of one question for the settlements of a map, in
// degrees. A map asks only when it is close; a larger box gets no answer.
const maxMapBox = 8.0

// maxMapPlaces limits the settlements of one answer.
const maxMapPlaces = 150

func queryNumber(r *http.Request, name string) (float64, bool) {
	value, err := strconv.ParseFloat(r.URL.Query().Get(name), 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// handlePlacesAround names the towns and the villages inside a box of the
// globe, the largest first. The map of the app draws them when it is close:
// its own file has only the large cities. The list of settlements is public
// data, and the box says only what part of the world a person looks at.
func (s *Server) handlePlacesAround(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow("places:"+strconv.FormatInt(userFrom(r).ID, 10), rulePlaceSearch) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	south, okS := queryNumber(r, "south")
	west, okW := queryNumber(r, "west")
	north, okN := queryNumber(r, "north")
	east, okE := queryNumber(r, "east")
	if !okS || !okW || !okN || !okE || south >= north || west >= east || north-south > maxMapBox || east-west > maxMapBox*2 {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	towns, err := s.towns.open()
	if err != nil {
		s.internal(w, err)
		return
	}
	out := []foundPlace{}
	for _, town := range towns.Within(math.Max(south, -90), math.Max(west, -180), math.Min(north, 90), math.Min(east, 180), maxMapPlaces) {
		out = append(out, foundPlace{Name: town.Name, NameRu: town.NameRu, Country: town.Country, Lat: town.Lat, Lon: town.Lon, People: town.People})
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	writeJSON(w, http.StatusOK, map[string]any{"places": out})
}

// handlePlaceNearest names the settlement of a point that an editor pressed
// on the map. The answer has no place when the point is far from every
// settlement.
func (s *Server) handlePlaceNearest(w http.ResponseWriter, r *http.Request) {
	if !s.limits.allow("places:"+strconv.FormatInt(userFrom(r).ID, 10), rulePlaceSearch) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	lat, okLat := queryNumber(r, "lat")
	lon, okLon := queryNumber(r, "lon")
	if !okLat || !okLon || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	towns, err := s.towns.open()
	if err != nil {
		s.internal(w, err)
		return
	}
	var out *foundPlace
	if town, ok := towns.Nearest(lat, lon); ok {
		out = &foundPlace{Name: town.Name, NameRu: town.NameRu, Country: town.Country, Lat: town.Lat, Lon: town.Lon}
	}
	writeJSON(w, http.StatusOK, map[string]any{"place": out})
}

// handleMapDetail gives one square of the close map of the program: rivers,
// lakes, borders of regions and main roads. It is the same public data for
// everybody, so it needs no sign-in, like the other files of the app.
func (s *Server) handleMapDetail(w http.ResponseWriter, r *http.Request) {
	name, found := strings.CutSuffix(r.PathValue("square"), ".json")
	row, column, ok := strings.Cut(name, "_")
	rowNumber, errRow := strconv.Atoi(row)
	columnNumber, errColumn := strconv.Atoi(column)
	if !found || !ok || errRow != nil || errColumn != nil {
		http.NotFound(w, r)
		return
	}
	data, has, err := geo.Detail(rowNumber, columnNumber)
	if err != nil {
		s.internal(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=604800")
	if !has {
		// The open sea. An empty answer that a browser can keep, not an error.
		_, _ = w.Write([]byte("{}"))
		return
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		h.Set("Vary", "Accept-Encoding")
		_, _ = w.Write(data)
		return
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		s.internal(w, err)
		return
	}
	_, _ = io.Copy(w, io.LimitReader(reader, 8<<20)) // #nosec G110 -- the data is a file of the program, and the copy has a limit.
}
