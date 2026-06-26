package tv

import (
	"strings"
	"testing"
)

const sampleM3U = `#EXTM3U @LaQuay
#EXTM3U url-tvg="https://example.com/epg.xml.gz"
#EXTINF:-1 tvg-id="La1.TV" tvg-logo="https://logos/la1.jpg" group-title="Generalistas" tvg-name="La 1",La 1
https://cdn.example.com/la1/main.m3u8
#EXTINF:-1 tvg-id="TDP.TV" tvg-logo="https://logos/tdp.jpg" group-title="Deportivos" tvg-name="Teledeporte",Teledeporte GEO
https://cdn.example.com/tdp/main.m3u8
#EXTINF:-1 tvg-logo="https://logos/cex.jpg" group-title="Infantiles" tvg-name="Infantil (Canal Extremadura)",Infantil (Canal Extremadura)
#EXTVLCOPT:http-user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64)
#EXTVLCOPT:http-referrer=https://player.example.net/
https://cdn.example.com/cex/master.m3u8
#EXTINF:-1 group-title="" tvg-name="No Group",No Group
https://cdn.example.com/nogroup/master.m3u8
`

func TestParseM3U_Fields(t *testing.T) {
	chs, err := ParseM3U(strings.NewReader(sampleM3U))
	if err != nil {
		t.Fatalf("ParseM3U: %v", err)
	}
	if len(chs) != 4 {
		t.Fatalf("expected 4 channels, got %d", len(chs))
	}

	la1 := chs[0]
	if la1.Name != "La 1" || la1.Group != "Generalistas" || la1.TvgID != "La1.TV" {
		t.Errorf("la1 fields wrong: %+v", la1)
	}
	if la1.Logo != "https://logos/la1.jpg" {
		t.Errorf("la1 logo wrong: %q", la1.Logo)
	}
	if la1.URL != "https://cdn.example.com/la1/main.m3u8" {
		t.Errorf("la1 url wrong: %q", la1.URL)
	}

	// tvg-name is preferred over the display title after the comma.
	if got := chs[1].Name; got != "Teledeporte" {
		t.Errorf("expected tvg-name 'Teledeporte', got %q", got)
	}
}

func TestParseM3U_VLCOptHeaders(t *testing.T) {
	chs, _ := ParseM3U(strings.NewReader(sampleM3U))
	cex := chs[2]
	if cex.Headers["User-Agent"] == "" {
		t.Errorf("expected User-Agent header, got %+v", cex.Headers)
	}
	if cex.Headers["Referer"] != "https://player.example.net/" {
		t.Errorf("expected Referer header, got %q", cex.Headers["Referer"])
	}
	// Headers must not leak into the next channel.
	if chs[3].Headers != nil {
		t.Errorf("headers leaked into next channel: %+v", chs[3].Headers)
	}
}

func TestParseM3U_EmptyGroupNormalized(t *testing.T) {
	chs, _ := ParseM3U(strings.NewReader(sampleM3U))
	if chs[3].Group != defaultGroup {
		t.Errorf("expected empty group normalized to %q, got %q", defaultGroup, chs[3].Group)
	}
}

func TestSplitExtinf(t *testing.T) {
	cases := []struct{ in, wantName string }{
		{`-1 tvg-name="A",Simple`, "Simple"},
		{`-1 tvg-name="X",Title, with comma`, "Title, with comma"},
		{`-1 group-title="News, Live",Channel`, "Channel"},
		{`-1 tvg-name="NoComma"`, ""},
	}
	for _, c := range cases {
		_, name := splitExtinf(c.in)
		if name != c.wantName {
			t.Errorf("splitExtinf(%q) name = %q, want %q", c.in, name, c.wantName)
		}
	}
}

func TestParseM3U_SkipsURLWithoutExtinf(t *testing.T) {
	in := "#EXTM3U\nhttps://orphan.example.com/x.m3u8\n#EXTINF:-1,Good\nhttps://good.example.com/x.m3u8\n"
	chs, _ := ParseM3U(strings.NewReader(in))
	if len(chs) != 1 || chs[0].Name != "Good" {
		t.Fatalf("expected 1 channel 'Good', got %+v", chs)
	}
}
