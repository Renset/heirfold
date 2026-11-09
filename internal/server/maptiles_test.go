package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Renset/heirfold/internal/auth"
)

func TestCheckMapTiles(t *testing.T) {
	for _, good := range []string{
		"https://tile.openstreetmap.org/{z}/{x}/{y}.png",
		"https://maps.example.org/tiles/{z}/{x}/{y}?key=abc",
		"http://127.0.0.1:8080/{z}/{x}/{y}.png",
	} {
		if err := CheckMapTiles(good); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{
		"", "tiles", "http://tile.example.org/{z}/{x}/{y}.png", "https://tile.example.org/{z}/{x}.png",
		"https://tile.example.org/{z}/{x}/{y}/{s}.png", "https://user:secret@tile.example.org/{z}/{x}/{y}.png", "file:///{z}/{x}/{y}",
	} {
		if err := CheckMapTiles(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

// With a tile service the server gets each picture of the map once, keeps
// it on the disk, and gives it only to family members and above.
func TestMapTilesComeThroughTheServer(t *testing.T) {
	picture := testPNG(t, 8, 8)
	var asked atomic.Int32
	var agent atomic.Value
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		agent.Store(r.Header.Get("User-Agent") + "|" + r.Header.Get("Cookie") + "|" + r.Header.Get("Referer"))
		switch r.URL.Path {
		case "/3/4/2.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(picture)
		case "/3/4/3.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html>not a picture</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	h, _ := editHarness(t, func(cfg *Config) {
		cfg.MapTiles = service.URL + "/{z}/{x}/{y}.png"
		cfg.MapCredit = "© Example map"
		cfg.Version = "test"
	})
	member := h.user("member@example.org", auth.RoleMember)
	guest := h.user("guest@example.org", auth.RoleGuest)

	if me := decode[accountDTO](t, member.expect(http.StatusOK, "GET", "/api/v1/me", nil)); !me.MapTiles || me.MapCredit != "© Example map" {
		t.Fatalf("the account of a member: %+v", me)
	}
	if me := decode[accountDTO](t, guest.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.MapTiles {
		t.Fatalf("the account of a guest: %+v", me)
	}

	res, body := member.do("GET", "/api/v1/map/tiles/3/4/2", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || string(body) != string(picture) {
		t.Fatalf("a tile: %d %s, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(body))
	}
	// The service learns the name of the program and nothing about the person.
	if got := agent.Load().(string); got != "Heirfold/test (self-hosted family tree; tiles are cached)||" {
		t.Fatalf("what the service saw: %q", got)
	}
	// The second look comes from the disk.
	member.expect(http.StatusOK, "GET", "/api/v1/map/tiles/3/4/2", nil)
	if asked.Load() != 1 {
		t.Fatalf("the service was asked %d times", asked.Load())
	}
	cached := filepath.Join(h.app.cfg.DataDir, "cache", "tiles", "3", "4", "2")
	if data, err := os.ReadFile(cached); err != nil || string(data) != string(picture) {
		t.Fatalf("the tile on the disk: %v", err)
	}

	// An answer that is no picture, and a missing tile, do not pass.
	member.refuse(http.StatusBadGateway, "map_unavailable", "GET", "/api/v1/map/tiles/3/4/3", nil)
	member.refuse(http.StatusBadGateway, "map_unavailable", "GET", "/api/v1/map/tiles/3/4/4", nil)
	if _, err := os.Stat(filepath.Join(h.app.cfg.DataDir, "cache", "tiles", "3", "4", "3")); err == nil {
		t.Fatal("an answer that is no picture is on the disk")
	}
	before := asked.Load()
	for _, path := range []string{"3/8/0", "3/0/8", "-1/0/0", "20/0/0", "a/b/c", "3/4/2.png"} {
		member.refuse(http.StatusBadRequest, "invalid", "GET", "/api/v1/map/tiles/"+path, nil)
	}
	if asked.Load() != before {
		t.Fatal("a wrong address reached the service")
	}
	guest.refuse(http.StatusForbidden, "forbidden", "GET", "/api/v1/map/tiles/3/4/2", nil)
	h.client().refuse(http.StatusUnauthorized, "unauthorized", "GET", "/api/v1/map/tiles/3/4/2", nil)

	// An old picture is asked again. When the service is down, the old picture still shows.
	old := time.Now().Add(-tileFresh - time.Hour)
	if err := os.Chtimes(cached, old, old); err != nil {
		t.Fatal(err)
	}
	member.expect(http.StatusOK, "GET", "/api/v1/map/tiles/3/4/2", nil)
	if asked.Load() != before+1 {
		t.Fatalf("an old tile was not asked again: %d", asked.Load()-before)
	}
	if err := os.Chtimes(cached, old, old); err != nil {
		t.Fatal(err)
	}
	service.Close()
	member.expect(http.StatusOK, "GET", "/api/v1/map/tiles/3/4/2", nil)

	// The clean-up removes the pictures that nobody asked for in a long time.
	ancient := time.Now().Add(-tileKeep - time.Hour)
	if err := os.Chtimes(cached, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	h.app.tiles.sweep(time.Now())
	if _, err := os.Stat(cached); err == nil {
		t.Fatal("an unused tile stays on the disk")
	}
}

// Without a tile service the server has no route to one.
func TestNoMapTilesWithoutTheSetting(t *testing.T) {
	h, _ := editHarness(t, nil)
	member := h.user("member@example.org", auth.RoleMember)
	if me := decode[accountDTO](t, member.expect(http.StatusOK, "GET", "/api/v1/me", nil)); me.MapTiles || me.MapCredit != "" {
		t.Fatalf("the account: %+v", me)
	}
	member.refuse(http.StatusNotFound, "not_found", "GET", "/api/v1/map/tiles/3/4/2", nil)
}

// The close map of the program is a file for everybody, in squares.
func TestMapDetailSquares(t *testing.T) {
	h, _ := editHarness(t, nil)
	anyone := h.client()
	res, body := anyone.do("GET", "/geo/detail/72_117.json", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/json; charset=utf-8" || len(body) < 1000 || body[0] != '{' {
		t.Fatalf("a square of land: %d %s, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(body))
	}
	// The open sea is an empty answer, not an error.
	if res, body := anyone.do("GET", "/geo/detail/30_80.json", nil, nil); res.StatusCode != http.StatusOK || string(body) != "{}" {
		t.Fatalf("a square of sea: %d %q", res.StatusCode, body)
	}
	for _, name := range []string{"72_117", "a_b.json", "72.json", "72_117.txt", "../x.json"} {
		if res, _ := anyone.do("GET", "/geo/detail/"+name, nil, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d", name, res.StatusCode)
		}
	}
}
