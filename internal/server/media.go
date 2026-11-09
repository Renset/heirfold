package server

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Renset/heirfold/internal/imaging"
	"github.com/Renset/heirfold/internal/store"
	"github.com/Renset/heirfold/internal/treesync"
)

// Synchronizer keeps the tree equal to an outside source.
type Synchronizer interface {
	Status() treesync.Status
	Sync(ctx context.Context, force bool) (bool, error)
}

// Limits of the picture service.
const (
	maxImagePixels  = 60_000_000
	maxImageBytes   = 256 << 20
	imageCacheBytes = 96 << 20
)

// imageSizes maps the size names of the API to the longest side in pixels.
var imageSizes = map[string]struct{ side, quality int }{
	"s": {192, 80},
	"m": {720, 82},
	"l": {2400, 86},
}

// imageCache keeps rendered pictures in memory, up to a byte limit.
type imageCache struct {
	mu    sync.Mutex
	limit int
	used  int
	order *list.List
	items map[string]*list.Element
}

type imageEntry struct {
	key  string
	data []byte
}

func newImageCache(limit int) *imageCache {
	return &imageCache{limit: limit, order: list.New(), items: map[string]*list.Element{}}
}

func (c *imageCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*imageEntry).data, true
}

func (c *imageCache) put(key string, data []byte) {
	if len(data) > c.limit/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	c.items[key] = c.order.PushFront(&imageEntry{key: key, data: data})
	c.used += len(data)
	for c.used > c.limit {
		last := c.order.Back()
		if last == nil {
			break
		}
		entry := last.Value.(*imageEntry)
		c.order.Remove(last)
		delete(c.items, entry.key)
		c.used -= len(entry.data)
	}
}

// mediaRoot resolves the media directory once. An empty result means that
// the server shows no files.
func mediaRoot(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("media directory: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("media directory %s is not a directory", dir)
	}
	return filepath.Abs(root)
}

// errNoFile means that the file of a media object cannot be shown.
var errNoFile = errors.New("server: no media file")

// mediaPath returns the real path of a media file. The file must lie inside
// the media directory: a tree can name any path, and a path outside that
// directory would let the tree read other files of the server.
func (s *Server) mediaPath(file *store.MediaFile) (string, os.FileInfo, error) {
	if s.mediaDir == "" || file.Path == "" {
		return "", nil, errNoFile
	}
	path := filepath.FromSlash(file.Path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.mediaDir, path)
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", nil, errNoFile
	}
	rel, err := filepath.Rel(s.mediaDir, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", nil, errNoFile
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errNoFile
	}
	return real, info, nil
}

func (s *Server) mediaFile(w http.ResponseWriter, r *http.Request) (*store.MediaFile, string, os.FileInfo, bool) {
	view := s.store.View(userFrom(r).Role.Scope())
	file, err := view.MediaFile(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, "", nil, false
	}
	if err != nil {
		s.internal(w, err)
		return nil, "", nil, false
	}
	path, info, err := s.mediaPath(file)
	if err != nil {
		writeError(w, http.StatusNotFound, "file_unavailable")
		return nil, "", nil, false
	}
	return file, path, info, true
}

func parseRegion(text string) (*imaging.Region, bool) {
	if text == "" {
		return nil, true
	}
	parts := strings.Split(text, ",")
	if len(parts) != 4 {
		return nil, false
	}
	var values [4]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 100 {
			return nil, false
		}
		values[i] = value
	}
	region := &imaging.Region{X1: values[0], Y1: values[1], X2: values[2], Y2: values[3]}
	return region, region.Valid()
}

// handleMediaImage sends a picture in one of three sizes. The browser must
// ask again each time, so a person who lost access gets no picture from a cache.
func (s *Server) handleMediaImage(w http.ResponseWriter, r *http.Request) {
	size, ok := imageSizes[r.URL.Query().Get("size")]
	region, regionOK := parseRegion(r.URL.Query().Get("region"))
	if !ok || !regionOK {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	file, path, info, ok := s.mediaFile(w, r)
	if !ok {
		return
	}
	if !store.IsImage(file.Mime) {
		writeError(w, http.StatusUnsupportedMediaType, "not_an_image")
		return
	}
	key := fmt.Sprintf("%s|%d|%v|%d|%d", path, size.side, region, info.ModTime().UnixNano(), info.Size())
	sum := sha256.Sum256([]byte(key))
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Cookie")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, cached := s.images.get(key)
	if !cached {
		// Each decode costs memory and time. One account cannot ask for an
		// endless row of new regions or sizes.
		if !s.limits.allow("render:"+strconv.FormatInt(userFrom(r).ID, 10), ruleRender) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		var err error
		if data, err = s.renderImage(r.Context(), path, imaging.Options{
			MaxSide: size.side, Quality: size.quality, Region: region, MaxPixels: maxImagePixels, MaxBytes: maxImageBytes,
		}); err != nil {
			switch {
			case errors.Is(err, imaging.ErrTooLarge):
				writeError(w, http.StatusUnprocessableEntity, "image_too_large")
			case errors.Is(err, imaging.ErrUnsupported):
				writeError(w, http.StatusUnsupportedMediaType, "not_an_image")
			case r.Context().Err() != nil:
			default:
				s.internal(w, err)
			}
			return
		}
		s.images.put(key, data)
	}
	h.Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// renderImage decodes one picture. Only a few pictures are decoded at one
// time, because each of them needs a lot of memory.
// With a reader, it decodes that reader and ignores the path.
func (s *Server) renderImage(ctx context.Context, path string, opts imaging.Options, readers ...io.ReadSeeker) ([]byte, error) {
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if len(readers) > 0 {
		return imaging.Render(readers[0], opts)
	}
	source, err := os.Open(path) // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
	if err != nil {
		return nil, err
	}
	defer func() { _ = source.Close() }()
	return imaging.Render(source, opts)
}

// downloadTypes are the content types that a download may carry. Every other
// file goes out as plain bytes.
var downloadTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/tiff": true,
	"application/pdf": true, "text/plain": true,
}

// handleMediaFile sends the original file as a download. The browser never
// shows it inline, so a file with a script cannot run on this origin.
func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request) {
	file, path, info, ok := s.mediaFile(w, r)
	if !ok {
		return
	}
	source, err := os.Open(path) // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
	if err != nil {
		writeError(w, http.StatusNotFound, "file_unavailable")
		return
	}
	defer func() { _ = source.Close() }()
	contentType := strings.ToLower(strings.TrimSpace(file.Mime))
	if !downloadTypes[contentType] {
		contentType = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(path)}))
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Cookie")
	http.ServeContent(w, r, "", info.ModTime(), source)
}

func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request) {
	view := s.store.View(userFrom(r).Role.Scope())
	items, err := view.MediaList(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := writeCompressed(w, r, map[string]any{"media": items, "files": s.mediaDir != ""}); err != nil {
		s.log.Warn("response failed", "error", err)
	}
}

type syncDTO struct {
	Revision int64            `json:"revision"`
	Source   string           `json:"source"`
	Status   *treesync.Status `json:"status,omitempty"`
}

func (s *Server) syncState(r *http.Request) (*syncDTO, error) {
	revision, err := s.store.Revision(r.Context())
	if err != nil {
		return nil, err
	}
	out := &syncDTO{Revision: revision, Source: "upload"}
	if s.cfg.Sync != nil {
		out.Source = "gramps"
		// Only a person who can manage the tree sees the details of the sync.
		if userFrom(r).Role.CanManageTree() {
			status := s.cfg.Sync.Status()
			out.Status = &status
		}
	}
	return out, nil
}

// handleSyncState tells a client whether the tree changed since it loaded it.
func (s *Server) handleSyncState(w http.ResponseWriter, r *http.Request) {
	state, err := s.syncState(r)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleSyncNow reads the Gramps database at once.
func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Sync == nil {
		writeError(w, http.StatusConflict, "no_sync")
		return
	}
	changed, err := s.cfg.Sync.Sync(r.Context(), true)
	if err != nil {
		s.log.Warn("sync from Gramps failed", "error", err)
	}
	s.audit(r, userFrom(r), "sync.run", "", fmt.Sprintf("changed=%t ok=%t", changed, err == nil))
	state, stateErr := s.syncState(r)
	if stateErr != nil {
		s.internal(w, stateErr)
		return
	}
	writeJSON(w, http.StatusOK, state)
}
