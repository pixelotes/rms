package tv

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// attrRegex matches key="value" attribute pairs on an #EXTINF line.
var attrRegex = regexp.MustCompile(`([A-Za-z0-9_-]+)="([^"]*)"`)

// ParseM3U reads an extended M3U playlist and returns its raw entries in file
// order. Merging into Channels happens later, in the store.
//
// Recognized per entry:
//   - #EXTINF:<dur> tvg-id="…" tvg-name="…" tvg-logo="…" group-title="…",<display>
//   - #EXTVLCOPT:http-user-agent=… / http-referrer=…  (→ Headers)
//   - the next non-comment, non-blank line is the stream URL
//
// Malformed entries (no URL, or a URL with no preceding #EXTINF) are skipped.
func ParseM3U(r io.Reader) ([]Entry, error) {
	var entries []Entry
	var cur Entry
	var have bool // an #EXTINF has been seen and is awaiting its URL

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#EXTM3U"):
			continue

		case strings.HasPrefix(line, "#EXTINF:"):
			cur = parseExtinf(strings.TrimPrefix(line, "#EXTINF:"))
			have = true

		case strings.HasPrefix(line, "#EXTVLCOPT:"):
			if have {
				applyVLCOpt(&cur, strings.TrimPrefix(line, "#EXTVLCOPT:"))
			}

		case strings.HasPrefix(line, "#"):
			continue // other directives (e.g. #EXTGRP) ignored for now

		default: // URL line
			if !have {
				continue
			}
			cur.URL = line
			if cur.Group == "" {
				cur.Group = defaultGroup
			}
			entries = append(entries, cur)
			cur = Entry{}
			have = false
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// parseExtinf parses the part of an #EXTINF line after the "#EXTINF:" prefix.
func parseExtinf(s string) Entry {
	attrs, display := splitExtinf(s)

	var e Entry
	for _, m := range attrRegex.FindAllStringSubmatch(attrs, -1) {
		switch strings.ToLower(m[1]) {
		case "tvg-id":
			e.TvgID = m[2]
		case "tvg-name":
			e.Name = m[2]
		case "tvg-logo":
			e.Logo = m[2]
		case "group-title":
			e.Group = m[2]
		}
	}
	// User asked to show tvg-name; fall back to the display title after the comma.
	if e.Name == "" {
		e.Name = strings.TrimSpace(display)
	}
	return e
}

// splitExtinf separates the attribute section from the trailing display title.
// The split point is the first comma that is NOT inside a quoted attribute
// value, so titles containing commas survive and quoted values are respected.
func splitExtinf(s string) (attrs, display string) {
	inQuote := false
	for i, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				return s[:i], s[i+1:]
			}
		}
	}
	return s, ""
}

// applyVLCOpt maps #EXTVLCOPT directives to HTTP headers used when proxying.
func applyVLCOpt(e *Entry, opt string) {
	k, v, ok := strings.Cut(opt, "=")
	if !ok {
		return
	}
	k = strings.ToLower(strings.TrimSpace(k))
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	var header string
	switch k {
	case "http-user-agent":
		header = "User-Agent"
	case "http-referrer", "http-referer":
		header = "Referer"
	case "http-origin":
		header = "Origin"
	default:
		return
	}
	if e.Headers == nil {
		e.Headers = map[string]string{}
	}
	e.Headers[header] = v
}
