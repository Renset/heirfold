package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Renset/heirfold/internal/auth"
	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/grampsdb"
	"github.com/Renset/heirfold/internal/grampsdb/grampstest"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
	"github.com/Renset/heirfold/internal/treesync"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// mediaHarness loads the invented Gramps tree and a media directory.
// The secret file lies outside that directory.
func mediaHarness(t *testing.T, withDir bool, edit func(*model.Tree)) (*harness, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "media")
	for _, dir := range []string{"photos", "book", "letters"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte) {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "photos", "orlin.jpg"), testJPEG(t, 800, 400))
	write(filepath.Join(root, "book", "page-47.jpg"), testJPEG(t, 300, 600))
	write(filepath.Join(root, "book", "notes.pdf"), []byte("%PDF-1.4 invented"))
	write(filepath.Join(root, "letters", "letter-1.png"), []byte("not really a png"))
	write(filepath.Join(base, "secret.jpg"), testJPEG(t, 50, 50))

	result, err := grampsdb.Read(context.Background(), grampstest.BuildTree(t, ""), gramps.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(result.Tree)
	}
	h := newHarness(t, func(cfg *Config) {
		if withDir {
			cfg.MediaDir = root
		}
	})
	if _, err := h.store.ReplaceTree(context.Background(), result.Tree); err != nil {
		t.Fatal(err)
	}
	return h, base
}

func mediaByID(tree *model.Tree, id string) *model.Media {
	for i := range tree.Media {
		if tree.Media[i].ID == id {
			return &tree.Media[i]
		}
	}
	return nil
}

func TestMediaImageIsScaledAndCached(t *testing.T) {
	h, _ := mediaHarness(t, true, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)

	res, body := admin.do("GET", "/api/v1/media/O0001/image?size=s", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("status %d, type %s: %s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if got := res.Header.Get("Cache-Control"); got != "private, no-cache" {
		t.Fatalf("cache control %q", got)
	}
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 192 || img.Bounds().Dy() != 96 {
		t.Fatalf("size %v", img.Bounds())
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	res, _ = admin.do("GET", "/api/v1/media/O0001/image?size=s", nil, map[string]string{"If-None-Match": etag})
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: status %d", res.StatusCode)
	}

	// The region of the link cuts the picture.
	_, body = admin.do("GET", "/api/v1/media/O0001/image?size=m&region=10,20,60,80", nil, nil)
	if img, err = jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 400 || img.Bounds().Dy() != 240 {
		t.Fatalf("region size %v", img.Bounds())
	}

	for _, query := range []string{"", "?size=xl", "?size=s&region=1,2,3", "?size=s&region=60,0,40,100", "?size=s&region=0,0,101,100", "?size=s&region=a,b,c,d"} {
		if res, _ := admin.do("GET", "/api/v1/media/O0001/image"+query, nil, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("query %q: status %d", query, res.StatusCode)
		}
	}
	// A file that is not a picture has no picture form.
	if res, _ := admin.do("GET", "/api/v1/media/O0004/image?size=s", nil, nil); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("pdf as image: status %d", res.StatusCode)
	}
}

func TestMediaImageRefusesAFileThatIsNoPicture(t *testing.T) {
	h, _ := mediaHarness(t, true, func(tree *model.Tree) {
		// The record says JPEG, but the file holds other bytes.
		mediaByID(tree, "O0004").File.Mime = "image/jpeg"
	})
	admin := h.user("admin@example.org", auth.RoleAdmin)
	if res, body := admin.do("GET", "/api/v1/media/O0004/image?size=s", nil, nil); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status %d: %s", res.StatusCode, body)
	}
}

func TestMediaFileIsADownload(t *testing.T) {
	h, _ := mediaHarness(t, true, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	res, body := admin.do("GET", "/api/v1/media/O0004/file", nil, nil)
	if res.StatusCode != http.StatusOK || string(body) != "%PDF-1.4 invented" {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") || !strings.Contains(got, "notes.pdf") {
		t.Fatalf("disposition %q", got)
	}
	if got := res.Header.Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("type %q", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff header %q", got)
	}
}

func TestMediaFileGoesOutAsPlainBytesForOtherTypes(t *testing.T) {
	h, _ := mediaHarness(t, true, func(tree *model.Tree) {
		item := mediaByID(tree, "O0004")
		item.File.Mime = "image/svg+xml"
	})
	admin := h.user("admin@example.org", auth.RoleAdmin)
	res, _ := admin.do("GET", "/api/v1/media/O0004/file", nil, nil)
	if got := res.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("an SVG must not keep its type: %q", got)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("disposition %q", got)
	}
}

func testDocx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	w, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `</w:body></w:document>`))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMediaDocumentGivesTheTextAsData(t *testing.T) {
	h, base := mediaHarness(t, true, func(tree *model.Tree) {
		word := mediaByID(tree, "O0004")
		word.File.Src, word.File.Mime = "book/report.docx", docxMime
		text := mediaByID(tree, "O0003")
		text.File.Src, text.File.Mime = "book/list.txt", "text/plain"
	})
	book := filepath.Join(base, "media", "book")
	script := `<w:p><w:r><w:t>&lt;script&gt;alert(1)&lt;/script&gt; Anna Orlova</w:t></w:r></w:p>`
	if err := os.WriteFile(filepath.Join(book, "report.docx"), testDocx(t, script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(book, "list.txt"), []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	admin := h.user("admin@example.org", auth.RoleAdmin)

	res, body := admin.do("GET", "/api/v1/media/O0004/document", nil, nil)
	var word struct {
		Kind   string
		Blocks []struct {
			Kind string
			Runs []struct{ Text string }
		}
	}
	if err := json.Unmarshal(body, &word); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if word.Kind != "docx" || len(word.Blocks) != 1 || word.Blocks[0].Runs[0].Text != "<script>alert(1)</script> Anna Orlova" {
		t.Fatalf("document: %s", body)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("type %q", got)
	}

	res, body = admin.do("GET", "/api/v1/media/O0003/document", nil, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"kind":"text"`) || !strings.Contains(string(body), `first line\nsecond line`) {
		t.Fatalf("text file: status %d: %s", res.StatusCode, body)
	}

	// A picture has no text view. A file that only has the name of a Word file has none either.
	if res, _ := admin.do("GET", "/api/v1/media/O0001/document", nil, nil); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("picture: status %d", res.StatusCode)
	}
	if err := os.WriteFile(filepath.Join(book, "report.docx"), []byte("no archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, body := admin.do("GET", "/api/v1/media/O0004/document", nil, nil); res.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(string(body), "no_preview") {
		t.Fatalf("broken file: status %d: %s", res.StatusCode, body)
	}
	if res, _ := admin.do("GET", "/api/v1/media/O9999/document", nil, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown file: status %d", res.StatusCode)
	}
	// Without a session there is no document.
	if res, _ := h.client().do("GET", "/api/v1/media/O0004/document", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: status %d", res.StatusCode)
	}
}

func TestMediaStaysInsideItsDirectory(t *testing.T) {
	var base string
	h, base := mediaHarness(t, true, func(tree *model.Tree) {
		mediaByID(tree, "O0001").File.Src = "../secret.jpg"
		mediaByID(tree, "O0004").File.Src = "book/link.jpg"
		mediaByID(tree, "O0004").File.Mime = "image/jpeg"
	})
	// A link inside the directory that points outside of it.
	if err := os.Symlink(filepath.Join(base, "secret.jpg"), filepath.Join(base, "media", "book", "link.jpg")); err != nil {
		t.Skip("this system has no symbolic links")
	}
	admin := h.user("admin@example.org", auth.RoleAdmin)
	for _, path := range []string{
		"/api/v1/media/O0001/image?size=s", "/api/v1/media/O0001/file", // a path with ..
		"/api/v1/media/O0002/file",                                     // an absolute path outside the directory
		"/api/v1/media/O0004/image?size=s", "/api/v1/media/O0004/file", // a symbolic link
	} {
		res, body := admin.do("GET", path, nil, nil)
		if res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "file_unavailable") {
			t.Errorf("%s: status %d: %s", path, res.StatusCode, body)
		}
	}
	// The same absolute path works when it lies inside the directory.
	h2, base2 := mediaHarness(t, true, func(tree *model.Tree) {})
	_ = base2
	inside := filepath.Join(h2.app.mediaDir, "photos", "orlin.jpg")
	result, err := h2.store.ExportTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mediaByID(result, "O0001").File.Src = inside
	if _, err := h2.store.ReplaceTree(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if res, _ := h2.user("admin@example.org", auth.RoleAdmin).do("GET", "/api/v1/media/O0001/image?size=s", nil, nil); res.StatusCode != http.StatusOK {
		t.Errorf("absolute path inside the directory: status %d", res.StatusCode)
	}
}

func TestMediaIsOffWithoutADirectory(t *testing.T) {
	h, _ := mediaHarness(t, false, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	if res, _ := admin.do("GET", "/api/v1/media/O0001/image?size=s", nil, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", res.StatusCode)
	}
	list := decode[struct {
		Media []store.MediaView `json:"media"`
		Files bool              `json:"files"`
	}](t, admin.expect(http.StatusOK, "GET", "/api/v1/media", nil))
	if list.Files || len(list.Media) != 4 {
		t.Fatalf("list: files=%t count=%d", list.Files, len(list.Media))
	}
}

func TestGuestGetsNoHiddenMedia(t *testing.T) {
	h, _ := mediaHarness(t, true, func(tree *model.Tree) {
		// The private letter gets a readable file, so only the privacy rule can stop it.
		mediaByID(tree, "O0002").File.Src = "book/page-47.jpg"
		mediaByID(tree, "O0002").File.Mime = "image/jpeg"
		// The notes belong to a living person through that person's own link.
		for i := range tree.People {
			if tree.People[i].ID == "I0004" {
				tree.People[i].MediaRefs = append(tree.People[i].MediaRefs, model.MediaRef{HLink: mediaByID(tree, "O0004").Handle})
			}
		}
	})
	anonymous := h.client()
	for _, path := range []string{"/api/v1/media", "/api/v1/media/O0001/image?size=s", "/api/v1/media/O0001/file"} {
		if res, _ := anonymous.do("GET", path, nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a session: status %d", path, res.StatusCode)
		}
	}
	guest := h.user("guest@example.org", auth.RoleGuest)
	member := h.user("member@example.org", auth.RoleMember)
	// The photo of a dead person is open to a guest.
	guest.expect(http.StatusOK, "GET", "/api/v1/media/O0001/image?size=s", nil)
	for _, id := range []string{"O0002", "O0004"} {
		for _, suffix := range []string{"/image?size=s", "/file"} {
			res, body := guest.do("GET", "/api/v1/media/"+id+suffix, nil, nil)
			if res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "not_found") {
				t.Errorf("guest %s%s: status %d: %s", id, suffix, res.StatusCode, body)
			}
		}
		member.expect(http.StatusOK, "GET", "/api/v1/media/"+id+"/file", nil)
	}
	list := decode[struct {
		Media []store.MediaView `json:"media"`
	}](t, guest.expect(http.StatusOK, "GET", "/api/v1/media", nil))
	for _, item := range list.Media {
		if item.ID == "O0002" || item.ID == "O0004" {
			t.Errorf("the guest list holds %s", item.ID)
		}
	}
	if len(list.Media) == 0 {
		t.Error("the guest list must hold the open media")
	}
}

func TestPersonCarriesPhotosAndPageScans(t *testing.T) {
	h, _ := mediaHarness(t, true, nil)
	member := h.user("member@example.org", auth.RoleMember)
	detail := decode[store.PersonDetail](t, member.expect(http.StatusOK, "GET", "/api/v1/people/I0001", nil))
	if detail.Photo == nil || detail.Photo.ID != "O0001" || detail.Photo.Region == nil || detail.Photo.Region.X1 != 10 {
		t.Fatalf("photo: %+v", detail.Photo)
	}
	if len(detail.Media) != 2 || !detail.Media[0].Image || detail.Media[0].Region == nil {
		t.Fatalf("media: %+v", detail.Media)
	}
	scans := 0
	for _, citation := range detail.Citations {
		for _, item := range citation.Media {
			if item.ID == "O0003" {
				scans++
			}
		}
	}
	if scans == 0 {
		t.Fatalf("the citation must carry the scan of its page: %+v", detail.Citations)
	}

	// A guest gets the photo but not the private link to the letter.
	guest := h.user("guest@example.org", auth.RoleGuest)
	public := decode[store.PersonDetail](t, guest.expect(http.StatusOK, "GET", "/api/v1/people/I0001", nil))
	if public.Photo == nil || len(public.Media) != 1 || public.Media[0].ID != "O0001" {
		t.Fatalf("guest media: photo=%+v media=%+v", public.Photo, public.Media)
	}
}

type fakeSync struct {
	mu     sync.Mutex
	forced int
}

func (f *fakeSync) Status() treesync.Status {
	return treesync.Status{Source: "gramps", Name: "Sink", SyncedAt: 1700000000, Counts: &model.Counts{People: 5}}
}

func (f *fakeSync) Sync(_ context.Context, force bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if force {
		f.forced++
	}
	return false, nil
}

func TestSyncStateAndControl(t *testing.T) {
	fake := &fakeSync{}
	h := newHarness(t, func(cfg *Config) { cfg.Sync = fake })
	h.loadFixture()
	admin := h.user("admin@example.org", auth.RoleAdmin)
	guest := h.user("guest@example.org", auth.RoleGuest)

	type state struct {
		Revision int64            `json:"revision"`
		Source   string           `json:"source"`
		Status   *treesync.Status `json:"status"`
	}
	seen := decode[state](t, guest.expect(http.StatusOK, "GET", "/api/v1/sync", nil))
	if seen.Revision != 1 || seen.Source != "gramps" || seen.Status != nil {
		t.Fatalf("guest state: %+v", seen)
	}
	full := decode[state](t, admin.expect(http.StatusOK, "GET", "/api/v1/sync", nil))
	if full.Status == nil || full.Status.Name != "Sink" {
		t.Fatalf("administrator state: %+v", full)
	}
	h.loadFixture()
	if next := decode[state](t, guest.expect(http.StatusOK, "GET", "/api/v1/sync", nil)); next.Revision != 2 {
		t.Fatalf("revision after a change: %d", next.Revision)
	}

	guest.expect(http.StatusForbidden, "POST", "/api/v1/sync", map[string]string{})
	admin.expect(http.StatusOK, "POST", "/api/v1/sync", map[string]string{})
	if fake.forced != 1 {
		t.Fatalf("forced syncs: %d", fake.forced)
	}

	// A tree that follows Gramps takes no upload.
	res, body := admin.do("POST", "/api/v1/imports", []byte("<database/>"), nil)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "managed_by_gramps") {
		t.Fatalf("upload: status %d: %s", res.StatusCode, body)
	}
	res, body = admin.do("POST", "/api/v1/imports/abc/commit", map[string]bool{"replace": true}, nil)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "managed_by_gramps") {
		t.Fatalf("commit: status %d: %s", res.StatusCode, body)
	}
	me := decode[struct {
		ImportTree bool `json:"importTree"`
		ManageTree bool `json:"manageTree"`
	}](t, admin.expect(http.StatusOK, "GET", "/api/v1/me", nil))
	if me.ImportTree || !me.ManageTree {
		t.Fatalf("me: %+v", me)
	}
}

func TestSyncIsOffWithoutAGrampsTree(t *testing.T) {
	h := newHarness(t, nil)
	admin := h.user("admin@example.org", auth.RoleAdmin)
	res, body := admin.do("POST", "/api/v1/sync", map[string]string{}, nil)
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "no_sync") {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	state := decode[struct {
		Source string `json:"source"`
	}](t, admin.expect(http.StatusOK, "GET", "/api/v1/sync", nil))
	if state.Source != "upload" {
		t.Fatalf("source %q", state.Source)
	}
}
