package tv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"raspberry-media-server/internal/config"
)

func TestParseJSON_BothForms(t *testing.T) {
	array := `[{"name":"A","group":"G","url":"https://a/x.m3u8"},{"url":"https://b/x.m3u8"}]`
	chs, err := ParseJSON(strings.NewReader(array))
	if err != nil || len(chs) != 2 {
		t.Fatalf("array form: got %d channels, err=%v", len(chs), err)
	}
	if chs[1].Group != defaultGroup {
		t.Errorf("missing group not normalized: %q", chs[1].Group)
	}

	wrapped := `{"channels":[{"name":"A","url":"https://a/x.m3u8"}]}`
	chs, err = ParseJSON(strings.NewReader(wrapped))
	if err != nil || len(chs) != 1 {
		t.Fatalf("wrapped form: got %d channels, err=%v", len(chs), err)
	}
}

func TestParseJSON_SkipsEmptyURL(t *testing.T) {
	chs, _ := ParseJSON(strings.NewReader(`[{"name":"A","url":""},{"name":"B","url":"https://b/x"}]`))
	if len(chs) != 1 || chs[0].Name != "B" {
		t.Fatalf("expected only B, got %+v", chs)
	}
}

func TestChannelID_StableAndDistinct(t *testing.T) {
	a := ChannelID("/lib/a.m3u", "id:la1.tv")
	if a != ChannelID("/lib/a.m3u", "id:la1.tv") {
		t.Error("ChannelID not deterministic")
	}
	if a == ChannelID("/lib/a.m3u", "id:la2.tv") {
		t.Error("different identities produced same ID")
	}
	if a == ChannelID("/lib/b.m3u", "id:la1.tv") {
		t.Error("same identity in different libraries produced same ID")
	}
	if !strings.Contains(a, "-") {
		t.Errorf("ID not UUID-shaped: %q", a)
	}
}

func TestPopulate_MergesAlternateSources(t *testing.T) {
	// Three entries: La 1 twice (same tvg-id, different CDNs) + one other.
	const m3u = `#EXTM3U
#EXTINF:-1 tvg-id="La1.TV" group-title="Generalistas" tvg-name="La 1",La 1
https://cdn-a/la1.m3u8
#EXTINF:-1 tvg-id="La1.TV" group-title="Generalistas" tvg-name="La 1",La 1
https://cdn-b/la1.m3u8
#EXTINF:-1 group-title="Deportivos" tvg-name="TDP",TDP
https://cdn-a/tdp.m3u8
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "list.m3u"), []byte(m3u), 0o644); err != nil {
		t.Fatal(err)
	}
	libs := []config.Library{{FriendlyName: "TV", ContentType: "tv", Path: dir}}

	total, errs := Populate(libs)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if total != 2 {
		t.Fatalf("expected 2 merged channels, got %d", total)
	}

	gen := ChannelsForGroup(GroupID(dir, "Generalistas"))
	if len(gen) != 1 {
		t.Fatalf("expected 1 channel in Generalistas, got %d", len(gen))
	}
	if got := len(gen[0].Sources); got != 2 {
		t.Errorf("expected La 1 to have 2 sources, got %d", got)
	}
	if gen[0].Sources[0].URL != "https://cdn-a/la1.m3u8" {
		t.Errorf("primary source order wrong: %q", gen[0].Sources[0].URL)
	}
}

func TestPopulate_RealPlaylist(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "lists", "tv.m3u"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	// lists/ is gitignored (user-provided, not shipped with the repo). Skip when
	// the local playlist is absent rather than failing on a fresh clone / CI.
	if _, err := os.Stat(path); err != nil {
		t.Skipf("local playlist %s not present; skipping real-playlist test", path)
	}
	libs := []config.Library{
		{FriendlyName: "Movies", ContentType: "movies", Path: "/tmp/movies"},
		{FriendlyName: "TV", ContentType: "tv", Path: path},
	}

	total, errs := Populate(libs)
	if len(errs) != 0 {
		t.Fatalf("Populate errors: %v", errs)
	}
	if total == 0 {
		t.Fatal("expected channels from lists/tv.m3u, got 0")
	}

	// The store is keyed by the library's config Path.
	if ChannelCount(path) != total {
		t.Errorf("ChannelCount(tv)=%d != total %d", ChannelCount(path), total)
	}
	if ChannelCount("/tmp/movies") != 0 {
		t.Errorf("non-tv library reported %d channels", ChannelCount("/tmp/movies"))
	}

	groups := GroupsForLibrary(path)
	if len(groups) == 0 {
		t.Fatal("expected at least one group")
	}
	// Groups must be sorted case-insensitively.
	for i := 1; i < len(groups); i++ {
		if strings.ToLower(groups[i-1].Name) > strings.ToLower(groups[i].Name) {
			t.Errorf("groups not sorted: %q before %q", groups[i-1].Name, groups[i].Name)
		}
	}

	// Every channel in every group must resolve and carry sane fields.
	seen := 0
	for _, g := range groups {
		for _, ch := range ChannelsForGroup(g.ID) {
			if len(ch.Sources) == 0 || ch.Sources[0].URL == "" || ch.Name == "" {
				t.Errorf("channel with no source/Name in group %q: %+v", g.Name, ch)
			}
			if got, ok := LookupChannel(ch.ID); !ok || got.ID != ch.ID {
				t.Errorf("LookupChannel(%s) failed", ch.ID)
			}
			seen++
		}
	}
	if seen != total {
		t.Errorf("walked %d channels via groups, total is %d", seen, total)
	}
}

func TestPopulate_RemoteFetchFailsFallsBackToLastGood(t *testing.T) {
	const m3u = `#EXTM3U
#EXTINF:-1 tvg-id="La1.TV" group-title="Generalistas",La 1
https://cdn-a/la1.m3u8
`
	// Server serves the playlist once, then fails every subsequent request.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			http.Error(w, "upstream down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-mpegurl")
		_, _ = w.Write([]byte(m3u))
	}))
	defer srv.Close()

	url := srv.URL + "/list.m3u"
	libs := []config.Library{{FriendlyName: "TV", ContentType: "tv", Path: url}}

	// First Populate succeeds and primes the last-good cache.
	if total, errs := Populate(libs); total != 1 || len(errs) != 0 {
		t.Fatalf("first populate: total=%d errs=%v", total, errs)
	}

	// Second Populate hits the 500. Channels must survive via the cache, and the
	// error must be surfaced (so the operator sees the stale data).
	total, errs := Populate(libs)
	if total != 1 {
		t.Fatalf("expected 1 channel served from last-good cache, got %d", total)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "last-good") {
		t.Fatalf("expected a surfaced last-good error, got %v", errs)
	}
	if ChannelCount(url) != 1 {
		t.Errorf("ChannelCount after fallback = %d, want 1", ChannelCount(url))
	}
}

func TestPopulate_NoTVLibraryIsEmpty(t *testing.T) {
	Populate([]config.Library{{FriendlyName: "Movies", ContentType: "movies", Path: "/tmp/m"}})
	if ChannelCount("/tmp/m") != 0 {
		t.Errorf("expected empty store, got %d", ChannelCount("/tmp/m"))
	}
	if _, ok := LookupChannel("nope"); ok {
		t.Error("unexpected channel lookup hit")
	}
}
