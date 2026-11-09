package server

import (
	"net/http"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// The photo gallery: every picture of the tree, with the people who are
// marked on it. Family members and above see it. A guest gets no gallery,
// because the server cannot know who is in a photo that nobody marked yet.

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	view := s.store.View(userFrom(r).Role.Scope())
	photos, err := view.Gallery(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := writeCompressed(w, r, map[string]any{"photos": photos, "files": s.mediaDir != ""}); err != nil {
		s.log.Warn("response failed", "error", err)
	}
}

// handleGalleryUpload adds a picture to the gallery. The picture has no
// person yet and starts hidden from guests.
func (s *Server) handleGalleryUpload(w http.ResponseWriter, r *http.Request) {
	if !s.uploadAllowed(w, r, ruleGalleryUpload) {
		return
	}
	photo, ok := s.storePhoto(w, r)
	if !ok {
		return
	}
	result, err := s.store.Edit(r.Context(), actor(r), "gallery.add", func(e *store.Edit) error {
		return e.AddGalleryPhoto(photo.src, photo.mime, photo.checksum)
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

func (s *Server) handleGalleryUpdate(w http.ResponseWriter, r *http.Request) {
	var req store.GalleryInput
	if !readEdit(w, r, &req) {
		return
	}
	s.edit(w, r, "gallery.update", func(e *store.Edit) error {
		return e.UpdateGalleryPhoto(r.PathValue("id"), req)
	})
}

func (s *Server) handleGalleryRemove(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "gallery.remove", func(e *store.Edit) error {
		return e.RemoveGalleryPhoto(r.PathValue("id"))
	})
}

type faceRequest struct {
	Region *model.Region `json:"region"`
}

// handleFaceMark says where a person is on a photo.
func (s *Server) handleFaceMark(w http.ResponseWriter, r *http.Request) {
	var req faceRequest
	if !readEdit(w, r, &req) {
		return
	}
	if req.Region == nil {
		writeError(w, http.StatusBadRequest, "invalid")
		return
	}
	s.edit(w, r, "gallery.mark", func(e *store.Edit) error {
		return e.MarkFace(r.PathValue("id"), r.PathValue("person"), *req.Region)
	})
}

func (s *Server) handleFaceUnmark(w http.ResponseWriter, r *http.Request) {
	s.edit(w, r, "gallery.unmark", func(e *store.Edit) error {
		return e.UnmarkFace(r.PathValue("id"), r.PathValue("person"))
	})
}
