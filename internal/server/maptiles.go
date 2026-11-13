package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The map of streets. Heirfold draws its own map from a file inside the
// program, and that map has no streets. An operator who wants streets names
// a tile service (--map-tiles). Then the server gets the pictures of the map
// from that service, keeps them on its disk, and gives them to the page.
//
// This is a choice with a price, so it is off until the operator turns it
// on: the tile service sees the address of the server and which parts of the
// world the family looks at. It never sees a relative: the browser of a
// relative asks only the family server.

const (
	// maxTileZoom is the closest zoom of the usual tile services.
	maxTileZoom = 19
	// maxTileBytes limits one picture of the map.
	maxTileBytes = 2 << 20
	// tileFresh is how long a picture on the disk counts as new. The tile
	// services ask for at least a week.
	tileFresh = 30 * 24 * time.Hour
	// tileKeep is how long an unused picture stays on the disk.
	tileKeep = 180 * 24 * time.Hour
	// maxTileCache limits all pictures of the map on the disk.
	maxTileCache = 1 << 30
	// tileFetches limits the questions to the tile service at one time. The
	// tile services ask for at most two.
	tileFetches = 2
)

// ErrMapTiles means that the address of the tile service is not usable.
var ErrMapTiles = errors.New("server: the address of the map tiles needs https and the parts {z}, {x} and {y}")

// CheckMapTiles checks the address of a tile service, such as
// "https://tile.openstreetmap.org/{z}/{x}/{y}.png".
func CheckMapTiles(template string) error {
	filled := strings.NewReplacer("{z}", "1", "{x}", "1", "{y}", "1").Replace(template)
	address, err := url.Parse(filled)
	local := err == nil && (address.Hostname() == "127.0.0.1" || address.Hostname() == "localhost")
	if err != nil || address.Host == "" || (address.Scheme != "https" && (address.Scheme != "http" || !local)) ||
		!strings.Contains(template, "{z}") || !strings.Contains(template, "{x}") || !strings.Contains(template, "{y}") ||
		strings.ContainsAny(filled, "{}") || address.User != nil {
		return ErrMapTiles
	}
	return nil
}

// tileStore gets the pictures of the map and keeps them on the disk.
type tileStore struct {
	template string
	dir      string
	agent    string
	client   *http.Client
	slots    chan struct{}

	mu      sync.Mutex
	waiting map[string]chan struct{}
}

func newTileStore(template, dataDir, version string) *tileStore {
	if template == "" {
		return nil
	}
	return &tileStore{
		template: template,
		dir:      filepath.Join(dataDir, "cache", "tiles"),
		// A tile service asks each program to say its name.
		agent:   "Heirfold/" + version + " (self-hosted family tree; tiles are cached)",
		client:  &http.Client{Timeout: 20 * time.Second},
		slots:   make(chan struct{}, tileFetches),
		waiting: map[string]chan struct{}{},
	}
}

// tileTypes lists the kinds of pictures that a tile service sends.
var tileTypes = []string{"image/png", "image/jpeg", "image/webp"}

func tileType(data []byte) string {
	kind := http.DetectContentType(data)
	if slices.Contains(tileTypes, kind) {
		return kind
	}
	return ""
}

func (t *tileStore) path(z, x, y int) string {
	return filepath.Join(t.dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y))
}

// read returns a picture from the disk and says whether it is still new.
func (t *tileStore) read(z, x, y int) (data []byte, fresh bool) {
	path := t.path(z, x, y)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err = os.ReadFile(path) // #nosec G304 -- the path is the cache folder and three numbers.
	if err != nil || tileType(data) == "" {
		return nil, false
	}
	return data, time.Since(info.ModTime()) < tileFresh
}

// fetch asks the tile service for one picture and writes it to the disk.
func (t *tileStore) fetch(ctx context.Context, z, x, y int) ([]byte, error) {
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	address := strings.NewReplacer("{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(t.template)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", t.agent)
	res, err := t.client.Do(req) // #nosec G107 -- the operator set the address; the request adds three checked numbers.
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile service: status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxTileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTileBytes || tileType(data) == "" {
		return nil, errors.New("tile service: the answer is no picture")
	}
	path := t.path(z, x, y)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		// A file with another name first, so nobody reads half a picture.
		if temp, err := os.CreateTemp(filepath.Dir(path), ".new-*"); err == nil {
			_, writeErr := temp.Write(data)
			closeErr := temp.Close()
			if writeErr != nil || closeErr != nil || os.Rename(temp.Name(), path) != nil {
				_ = os.Remove(temp.Name())
			}
		}
	}
	return data, nil
}

// get returns one picture of the map: from the disk when it is new, else
// from the tile service. An old picture is better than none when the service
// does not answer. Two questions for the same picture ask the service once.
func (t *tileStore) get(ctx context.Context, z, x, y int) ([]byte, error) {
	key := strconv.Itoa(z) + "/" + strconv.Itoa(x) + "/" + strconv.Itoa(y)
	for {
		old, fresh := t.read(z, x, y)
		if fresh {
			return old, nil
		}
		t.mu.Lock()
		busy, taken := t.waiting[key]
		if !taken {
			done := make(chan struct{})
			t.waiting[key] = done
			t.mu.Unlock()
			data, err := t.fetch(ctx, z, x, y)
			t.mu.Lock()
			delete(t.waiting, key)
			t.mu.Unlock()
			close(done)
			if err != nil && old != nil {
				return old, nil
			}
			return data, err
		}
		t.mu.Unlock()
		select {
		case <-busy:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// sweep removes the pictures that nobody asked for in a long time, and then
// the oldest pictures until the folder is inside its limit.
func (t *tileStore) sweep(now time.Time) {
	type entry struct {
		path string
		at   time.Time
		size int64
	}
	var files []entry
	var stale []string
	var total int64
	_ = filepath.WalkDir(t.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // a folder that the walk cannot read does not stop the clean-up.
		}
		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // a file that left in the meantime does not stop the clean-up.
		}
		if now.Sub(info.ModTime()) > tileKeep || strings.HasPrefix(d.Name(), ".new-") && now.Sub(info.ModTime()) > time.Hour {
			stale = append(stale, path)
			return nil
		}
		files = append(files, entry{path: path, at: info.ModTime(), size: info.Size()})
		total += info.Size()
		return nil
	})
	// The walk only lists. The files leave after it.
	for _, path := range stale {
		_ = os.Remove(path)
	}
	if total <= maxTileCache {
		return
	}
	slices.SortFunc(files, func(a, b entry) int { return a.at.Compare(b.at) })
	for _, file := range files {
		if total <= maxTileCache*9/10 {
			return
		}
		if os.Remove(file.path) == nil {
			total -= file.size
		}
	}
}

// handleMapTile gives one picture of the map of streets to the page.
func (s *Server) handleMapTile(w http.ResponseWriter, r *http.Request) {
	if s.tiles == nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(r.PathValue("y"))
	if errZ != nil || errX != nil || errY != nil || z < 0 || z > maxTileZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	if !s.limits.allow("tiles:"+strconv.FormatInt(userFrom(r).ID, 10), ruleMapTile) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	data, err := s.tiles.get(r.Context(), z, x, y)
	if err != nil {
		if r.Context().Err() == nil {
			s.log.Warn("map tile failed", "error", err)
		}
		writeError(w, http.StatusBadGateway, "map_unavailable")
		return
	}
	h := w.Header()
	h.Set("Content-Type", tileType(data))
	h.Set("Cache-Control", "private, max-age=604800")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
