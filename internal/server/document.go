package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Renset/heirfold/internal/docview"
	"github.com/Renset/heirfold/internal/store"
)

const (
	docxMime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	// maxDocumentFile is the largest file that the server reads for a view.
	maxDocumentFile = 64 << 20
)

// documentKind names the reader for a file, or returns nothing.
func documentKind(file *store.MediaFile, path string) string {
	kind := strings.ToLower(strings.TrimSpace(file.Mime))
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case kind == docxMime || ext == ".docx":
		return "docx"
	case kind == "text/plain" || ext == ".txt":
		return "text"
	}
	return ""
}

type documentDTO struct {
	Kind string `json:"kind"`
	*docview.Document
}

// handleMediaDocument sends the text of a Word or text file as data. The
// browser builds the page from this data, so no markup of the file reaches it.
func (s *Server) handleMediaDocument(w http.ResponseWriter, r *http.Request) {
	file, path, info, ok := s.mediaFile(w, r)
	if !ok {
		return
	}
	kind := documentKind(file, path)
	if kind == "" || info.Size() > maxDocumentFile {
		writeError(w, http.StatusUnsupportedMediaType, "no_preview")
		return
	}
	source, err := os.Open(path) // #nosec G304 -- mediaPath checked that the file lies inside the media directory.
	if err != nil {
		writeError(w, http.StatusNotFound, "file_unavailable")
		return
	}
	defer func() { _ = source.Close() }()
	var doc *docview.Document
	if kind == "docx" {
		doc, err = docview.FromDocx(source, info.Size(), docview.DefaultLimits)
	} else {
		doc, err = docview.FromText(source, docview.DefaultLimits)
	}
	if errors.Is(err, docview.ErrFormat) {
		writeError(w, http.StatusUnsupportedMediaType, "no_preview")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, documentDTO{Kind: kind, Document: doc})
}
