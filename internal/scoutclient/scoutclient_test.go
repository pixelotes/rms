package scoutclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pixelotes/scout"
	"gopkg.in/yaml.v3"

	"raspberry-media-server/internal/config"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const showNFO = `<?xml version="1.0" encoding="UTF-8"?>
<tvshow><title>Severance</title><plot>x</plot>
<uniqueid type="imdb" default="true">tt11280740</uniqueid>
<uniqueid type="tvmaze">44933</uniqueid></tvshow>`

func TestParseEpisode(t *testing.T) {
	for name, want := range map[string][2]int{
		"Severance - S01E02 - Half Loop": {1, 2},
		"show.s2e10.1080p":               {2, 10},
		"Show 3x07":                      {3, 7},
		"The Matrix (1999)":              {0, 0},
	} {
		if s, e := ParseEpisode(name); s != want[0] || e != want[1] {
			t.Errorf("%q = %d,%d want %v", name, s, e, want)
		}
	}
}

func TestStripEpisode(t *testing.T) {
	if got := StripEpisode("Severance S01E03 web"); got != "Severance web" {
		t.Fatal(got)
	}
}

func TestSubtitleQueryReadsShowNFO(t *testing.T) {
	lib := t.TempDir()
	write(t, filepath.Join(lib, "Severance", "tvshow.nfo"), showNFO)
	video := filepath.Join(lib, "Severance", "Season 1", "Severance - S01E02.mkv")
	write(t, video, string(make([]byte, 100_000)))

	q := SubtitleQuery(video, lib, []string{"es"})
	if q.IMDbID != "tt11280740" || q.Query != "Severance" || q.Season != 1 || q.Episode != 2 || q.Hash == "" || q.Size != 100_000 {
		t.Fatalf("query %+v", q)
	}

	// Without an NFO the title comes from the show folder, not "Season 1".
	lib2 := t.TempDir()
	v2 := filepath.Join(lib2, "The Bear", "Season 2", "The Bear S02E07.mkv")
	write(t, v2, "x")
	if q := SubtitleQuery(v2, lib2, nil); q.Query != "The Bear" || q.IMDbID != "" {
		t.Fatalf("no-NFO query %+v", q)
	}
	// Movies: the folder name.
	v3 := filepath.Join(lib2, "Dune (2021)", "Dune.mkv")
	write(t, v3, "x")
	if q := SubtitleQuery(v3, lib2, nil); q.Query != "Dune (2021)" {
		t.Fatalf("movie query %+v", q)
	}
}

func TestMissingLanguages(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Movie.mkv")
	write(t, video, "x")
	write(t, filepath.Join(dir, "Movie.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhi\n")
	if got := MissingLanguages(video, []string{"en", "es"}); !reflect.DeepEqual(got, []string{"es"}) {
		t.Fatalf("got %v", got)
	}
}

func TestLibrarySettings(t *testing.T) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(`
libraries:
  - {friendly_name: A, path: /a, content_type: anime, providers: [kitsu]}
  - {friendly_name: B, path: /b, content_type: anime, download_metadata: false, download_subtitles: "false"}
crawlers:
  metadata:
    anime_providers: [tvmaze, anilist]
`), &cfg); err != nil {
		t.Fatal(err)
	}
	a, b := cfg.Libraries[0], cfg.Libraries[1]
	if !a.WantsMetadata() || !a.WantsSubtitles() || b.WantsMetadata() || b.WantsSubtitles() {
		t.Fatal("download_metadata/download_subtitles: unset must mean true, false must mean false")
	}
	if got := Providers(&cfg, a, "anime"); !reflect.DeepEqual(got, []string{"kitsu"}) {
		t.Fatalf("library providers should win: %v", got)
	}
	if got := Providers(&cfg, b, "anime"); !reflect.DeepEqual(got, []string{"tvmaze", "anilist"}) {
		t.Fatalf("type default: %v", got)
	}
	if lib, ok := LibraryFor(&cfg, "/b/Show/ep.mkv"); !ok || lib.FriendlyName != "B" {
		t.Fatal("LibraryFor")
	}
	if _, ok := LibraryFor(&cfg, "/bb/x"); ok {
		t.Fatal("/bb is not inside /b")
	}
}

type fakeBackend struct {
	scout.Backend
	q     scout.SubtitleQuery
	quota bool
}

func (f *fakeBackend) SearchSubtitles(_ context.Context, q scout.SubtitleQuery) ([]scout.Subtitle, error) {
	f.q = q
	return []scout.Subtitle{
		{Provider: "subdivx", FileID: "1", Language: "es", Downloads: 10},
		{Provider: "opensubtitles", FileID: "2", Language: "es", HashMatch: true},
	}, nil
}

func (f *fakeBackend) DownloadSubtitle(_ context.Context, p, id string) (*scout.SubtitleFile, error) {
	if f.quota {
		return nil, scout.ErrQuotaExceeded
	}
	return &scout.SubtitleFile{Data: []byte(p + ":" + id)}, nil
}

func TestFetchSubtitles(t *testing.T) {
	lib := t.TempDir()
	video := filepath.Join(lib, "Dune (2021)", "Dune.mkv")
	write(t, video, "x")
	f := &fakeBackend{}
	n, err := FetchSubtitles(context.Background(), f, video, lib, []string{"es"})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := os.ReadFile(filepath.Join(lib, "Dune (2021)", "Dune.es.srt"))
	if string(got) != "opensubtitles:2" {
		t.Fatalf("want the hash match, got %q", got)
	}
	// Present now: nothing to do, no search.
	f.q = scout.SubtitleQuery{}
	if n, _ := FetchSubtitles(context.Background(), f, video, lib, []string{"es"}); n != 0 || f.q.Query != "" {
		t.Fatal("searched although the language was present")
	}
	// Quota used up: the error comes back so the crawler can stop.
	os.Remove(filepath.Join(lib, "Dune (2021)", "Dune.es.srt"))
	f.quota = true
	if _, err := FetchSubtitles(context.Background(), f, video, lib, []string{"es"}); !errors.Is(err, scout.ErrQuotaExceeded) {
		t.Fatalf("want ErrQuotaExceeded, got %v", err)
	}
}
