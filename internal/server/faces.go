package server

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Renset/heirfold/internal/faces"
	"github.com/Renset/heirfold/internal/imaging"
	"github.com/Renset/heirfold/internal/store"
)

// The search for faces. The server looks at each picture of the tree once,
// in the background, and stores the faces that it finds. An editor then sees
// a frame on each face and, if the recognition model is installed, the
// people whose marked faces are like it. Nothing becomes a part of the tree
// until an editor marks a person.

const (
	// faceSide is the longest side of the picture that the finder gets.
	faceSide = 1600
	// facePause keeps the server free for people between two pictures.
	facePause = 200 * time.Millisecond
	// faceInterval is the time between two looks for new pictures.
	faceInterval = 15 * time.Minute
)

// faceFinder holds the models and wakes the search.
type faceFinder struct {
	wake   chan struct{}
	mu     sync.Mutex
	engine *faces.Engine
	failed bool
	// orientations remembers the camera orientation of the files.
	orientations sync.Map
}

// facesOn reports whether this server searches pictures for faces.
func (s *Server) facesOn() bool {
	return !s.cfg.NoFaces && s.cfg.Sync == nil && s.mediaDir != ""
}

// faceWorkers is the number of processors for the search. People come first:
// the search uses half of the processors.
func faceWorkers() int { return max(1, runtime.NumCPU()/2) }

// faceEngine loads the models at the first use. The recognition model is a
// file in the folder "models" of the data directory.
func (s *Server) faceEngine() *faces.Engine {
	f := s.faces
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.engine != nil || f.failed {
		return f.engine
	}
	engine, err := faces.New(filepath.Join(s.cfg.DataDir, "models", faces.RecognizerFile))
	if errors.Is(err, faces.ErrModel) {
		s.log.Error("the face recognition model is not the expected file; faces are found but not compared",
			"file", faces.RecognizerFile, "sha256", faces.RecognizerSHA256)
		engine, err = faces.New("")
	}
	if err != nil {
		s.log.Error("the face models did not load", "error", err)
		f.failed = true
		return nil
	}
	f.engine = engine
	s.log.Info("face search is ready", "recognition", engine.Recognizes())
	return engine
}

// kickFaces asks for a search soon, for example after an upload.
func (s *Server) kickFaces() {
	select {
	case s.faces.wake <- struct{}{}:
	default:
	}
}

// runFaces searches new pictures until the server stops.
func (s *Server) runFaces(ctx context.Context) {
	if !s.facesOn() {
		return
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.faces.wake:
		}
		if _, err := s.searchFaces(ctx, facePause); err != nil && ctx.Err() == nil {
			s.log.Error("face search failed", "error", err)
		}
		timer.Reset(faceInterval)
	}
}

// searchFaces searches every picture that waits. It returns the number of pictures.
func (s *Server) searchFaces(ctx context.Context, pause time.Duration) (int, error) {
	engine := s.faceEngine()
	if engine == nil {
		return 0, nil
	}
	done := 0
	for {
		jobs, err := s.store.FaceJobs(ctx, engine.Recognizes(), 20)
		if err != nil || len(jobs) == 0 {
			return done, err
		}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return done, err
			}
			if err := s.searchPicture(ctx, engine, job); err != nil {
				return done, err
			}
			done++
			if pause > 0 {
				select {
				case <-ctx.Done():
					return done, ctx.Err()
				case <-time.After(pause):
				}
			}
		}
	}
}

// searchPicture finds the faces of one picture and stores them. A picture
// that the server cannot read gets a record of the failure, not an error.
func (s *Server) searchPicture(ctx context.Context, engine *faces.Engine, job store.FaceJob) error {
	fail := func(reason string) error {
		return s.store.SaveFaces(ctx, job, 0, 0, engine.Recognizes(), nil, reason)
	}
	path, _, err := s.mediaPath(&store.MediaFile{Path: job.Path})
	if err != nil {
		return fail("the file is missing")
	}
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	source, err := os.Open(path) // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
	if err != nil {
		return fail("the file is missing")
	}
	defer func() { _ = source.Close() }()
	img, orientation, err := imaging.Pixels(source, imaging.Options{MaxSide: faceSide, MaxPixels: maxImagePixels, MaxBytes: maxImageBytes})
	if err != nil {
		return fail("the picture cannot be read")
	}
	found, err := engine.Find(img)
	if err != nil {
		return fail("the search failed")
	}
	// The stored picture lies on its side for the orientations 5 to 8.
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if orientation >= 5 {
		width, height = height, width
	}
	out := make([]store.FoundFace, 0, len(found))
	for _, face := range found {
		ax, ay := imaging.StoredPoint(face.X, face.Y, orientation)
		bx, by := imaging.StoredPoint(face.X+face.W, face.Y+face.H, orientation)
		item := store.FoundFace{
			X1: 100 * math.Max(0, math.Min(ax, bx)), Y1: 100 * math.Max(0, math.Min(ay, by)),
			X2: 100 * math.Min(1, math.Max(ax, bx)), Y2: 100 * math.Min(1, math.Max(ay, by)),
			Score: face.Score,
		}
		if item.X2 <= item.X1 || item.Y2 <= item.Y1 {
			continue
		}
		if engine.Recognizes() {
			if item.Vector, err = engine.Vector(img, face); err != nil {
				item.Vector = nil
			}
		}
		out = append(out, item)
	}
	return s.store.SaveFaces(ctx, job, width, height, engine.Recognizes(), out, "")
}

type foundDTO struct {
	*store.PhotoFaces
	// On is false when this server does not search for faces.
	On bool `json:"on"`
	// Recognition is true when the server can say who a face is like.
	Recognition bool `json:"recognition"`
}

// handleFoundFaces tells an editor which faces of a photo have no mark yet.
func (s *Server) handleFoundFaces(w http.ResponseWriter, r *http.Request) {
	view := s.store.View(userFrom(r).Role.Scope())
	found, err := view.FoundFaces(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	out := foundDTO{PhotoFaces: found, On: s.facesOn()}
	if out.On {
		if engine := s.faceEngine(); engine != nil {
			out.Recognition = engine.Recognizes()
		}
		if !found.Scanned {
			s.kickFaces()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleFaceDismiss records that a found face needs no mark.
func (s *Server) handleFaceDismiss(w http.ResponseWriter, r *http.Request) {
	face, err := strconv.ParseInt(r.PathValue("face"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	ok, err := s.store.DismissFace(r.Context(), r.PathValue("id"), face)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMediaInfo tells the page how the camera held a picture. A part of a
// picture is stored on the pixels of the file, and the page shows the picture
// upright, so the page needs this to draw a frame at the right place.
func (s *Server) handleMediaInfo(w http.ResponseWriter, r *http.Request) {
	_, path, info, ok := s.mediaFile(w, r)
	if !ok {
		return
	}
	key := path + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	orientation := 1
	if known, hit := s.faces.orientations.Load(key); hit {
		orientation = known.(int)
	} else {
		if source, err := os.Open(path); err == nil { // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
			orientation = imaging.Orientation(source)
			_ = source.Close()
		}
		s.faces.orientations.Store(key, orientation)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, map[string]int{"orientation": orientation})
}
