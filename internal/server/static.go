package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/Renset/heirfold/internal/web"
)

var mimeTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".woff2":       "font/woff2",
	".webmanifest": "application/manifest+json",
	".json":        "application/json; charset=utf-8",
	".txt":         "text/plain; charset=utf-8",
}

type staticFile struct {
	body    []byte
	gzipped []byte
	mime    string
	etag    string
	hashed  bool
}

// staticFiles serves the embedded frontend from memory.
type staticFiles struct {
	files map[string]*staticFile
}

const missingFrontend = `<!doctype html><title>Heirfold</title><p>The frontend is not built. Run: make build</p>`

func loadStatic() (*staticFiles, error) {
	root := web.FS()
	out := &staticFiles{files: make(map[string]*staticFile)}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path.Base(p), ".") {
			return err
		}
		body, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		out.files["/"+p] = newStaticFile(p, body)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := out.files["/index.html"]; !ok {
		out.files["/index.html"] = newStaticFile("index.html", []byte(missingFrontend))
	}
	return out, nil
}

func newStaticFile(name string, body []byte) *staticFile {
	sum := sha256.Sum256(body)
	mime, ok := mimeTypes[path.Ext(name)]
	if !ok {
		mime = "application/octet-stream"
	}
	file := &staticFile{
		body: body, mime: mime,
		etag:   `"` + hex.EncodeToString(sum[:16]) + `"`,
		hashed: strings.HasPrefix(name, "assets/"),
	}
	if path.Ext(name) != ".woff2" && path.Ext(name) != ".png" {
		var buf bytes.Buffer
		gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err == nil {
			_, writeErr := gz.Write(body)
			if writeErr == nil && gz.Close() == nil && buf.Len() < len(body) {
				file.gzipped = buf.Bytes()
			}
		}
	}
	return file
}

func (s *staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Path
	if name == "/" {
		name = "/index.html"
	}
	file, ok := s.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", file.mime)
	h.Set("ETag", file.etag)
	h.Set("Vary", "Accept-Encoding")
	if file.hashed {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if r.Header.Get("If-None-Match") == file.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := file.body
	if file.gzipped != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = file.gzipped
	}
	_, _ = w.Write(body)
}
