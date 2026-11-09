package server

import (
	"bytes"
	"crypto/md5" // #nosec G501 -- Gramps stores an MD5 checksum of each file; it protects nothing.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Renset/heirfold/internal/imaging"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// maxPhotoBytes limits one uploaded picture.
const maxPhotoBytes = 25 << 20

// photoTypes lists the picture formats that an editor can upload.
var photoTypes = map[string]struct{ mime, ext string }{
	"jpeg": {"image/jpeg", ".jpg"},
	"png":  {"image/png", ".png"},
}

// maxUploadDirBytes limits all pictures that editors add, so a script with
// an editor account cannot fill the disk.
const maxUploadDirBytes = 4 << 30

func dirSize(dir string) int64 {
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

// storedPhoto is an uploaded picture that is on the disk.
type storedPhoto struct {
	// src is the path inside the media directory.
	src, mime, checksum string
	path                string
	// created is false when the same picture was on the disk before.
	created bool
}

// drop removes the file of a refused upload, if this upload made it.
func (p *storedPhoto) drop() {
	if p.created {
		_ = os.Remove(p.path)
	}
}

// uploadAllowed checks that the server takes pictures and that the account
// did not send too many.
func (s *Server) uploadAllowed(w http.ResponseWriter, r *http.Request, rule bucketRule) bool {
	if s.mediaDir == "" {
		writeError(w, http.StatusConflict, "media_off")
		return false
	}
	if !s.limits.allow("upload:"+strconv.FormatInt(userFrom(r).ID, 10), rule) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

// storePhoto reads a picture from the request and stores it in the media
// directory. The server reads the format from the bytes, never from the name
// or the header of the upload. On a failure it writes the answer itself.
func (s *Server) storePhoto(w http.ResponseWriter, r *http.Request) (*storedPhoto, bool) {
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	case <-r.Context().Done():
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large")
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "upload_failed")
		return nil, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	kind, known := photoTypes[format]
	if err != nil || !known || !imaging.Fits(config, maxImagePixels, maxImageBytes) {
		writeError(w, http.StatusUnprocessableEntity, "bad_image")
		return nil, false
	}
	// A full decode proves that the whole file is a picture.
	if _, err := s.renderImage(r.Context(), "", imaging.Options{MaxSide: 64, MaxPixels: maxImagePixels, MaxBytes: maxImageBytes}, bytes.NewReader(data)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "bad_image")
		return nil, false
	}
	// The content names the file, so the same picture is stored once and a
	// name from the browser never reaches the disk.
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + kind.ext
	dir := filepath.Join(s.mediaDir, "heirfold")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.internal(w, err)
		return nil, false
	}
	path := filepath.Join(dir, name)
	created := false
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- the server built this path from a hash.
	switch {
	case errors.Is(err, os.ErrExist):
	case err != nil:
		s.internal(w, err)
		return nil, false
	default:
		created = true
		if dirSize(dir)+int64(len(data)) > maxUploadDirBytes {
			_ = file.Close()
			_ = os.Remove(path)
			writeError(w, http.StatusInsufficientStorage, "storage_full")
			return nil, false
		}
		_, writeErr := file.Write(data)
		if closeErr := file.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			_ = os.Remove(path)
			s.internal(w, writeErr)
			return nil, false
		}
	}
	checksum := md5.Sum(data) // #nosec G401 -- the checksum field of the Gramps format.
	return &storedPhoto{
		src: "heirfold/" + name, mime: kind.mime, checksum: hex.EncodeToString(checksum[:]), path: path, created: created,
	}, true
}

// handlePhotoUpload stores a picture in the media directory and makes it the
// portrait of a person.
func (s *Server) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	if !s.uploadAllowed(w, r, ruleUpload) {
		return
	}
	// The person must exist before the server reads a large body.
	personID := r.PathValue("id")
	if err := s.store.Inspect(r.Context(), func(e *store.Edit) error {
		_, err := e.Person(personID)
		return err
	}); err != nil {
		s.editError(w, err)
		return
	}
	photo, ok := s.storePhoto(w, r)
	if !ok {
		return
	}
	result, err := s.store.Edit(r.Context(), actor(r), "media.add", func(e *store.Edit) error {
		return e.AddPhoto(personID, photo.src, photo.mime, photo.checksum)
	})
	if err != nil {
		photo.drop()
		s.editError(w, err)
		return
	}
	s.fileInfoNow(r.Context())
	s.kickFaces()
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMediaRemove(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "media.remove", func(e *store.Edit) error {
		return e.RemoveMedia(r.PathValue("id"), r.PathValue("media"))
	})
}

// handlePortraitClear leaves a person without a portrait. The pictures stay.
func (s *Server) handlePortraitClear(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "portrait.clear", func(e *store.Edit) error {
		return e.ClearPortrait(r.PathValue("id"))
	})
}

type photoRequest struct {
	Region  *model.Region `json:"region"`
	Primary bool          `json:"primary"`
}

// handlePhotoUpdate changes the part of a picture that a person shows, and
// can make the picture the portrait.
func (s *Server) handlePhotoUpdate(w http.ResponseWriter, r *http.Request) {
	var req photoRequest
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "media.update", func(e *store.Edit) error {
		return e.SetPhoto(r.PathValue("id"), r.PathValue("media"), store.PhotoInput{Region: req.Region, Primary: req.Primary})
	})
}
