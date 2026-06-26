package tv

import (
	"encoding/json"
	"io"
	"strings"
)

// jsonChannel is one entry in a JSON playlist. All fields are optional except
// url; name falls back to the URL's last path segment if absent.
type jsonChannel struct {
	Name    string            `json:"name"`
	Group   string            `json:"group"`
	Logo    string            `json:"logo"`
	URL     string            `json:"url"`
	TvgID   string            `json:"tvg_id"`
	Headers map[string]string `json:"headers"`
}

// jsonPlaylist accepts either a bare array of channels or an object wrapping
// them under "channels".
type jsonPlaylist struct {
	Channels []jsonChannel `json:"channels"`
}

// ParseJSON reads a JSON playlist and returns its raw entries in file order.
// Merging into Channels happens later, in the store.
func ParseJSON(r io.Reader) ([]Entry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var raw []jsonChannel
	if err := json.Unmarshal(data, &raw); err != nil {
		// Not a bare array — try the object form.
		var wrapped jsonPlaylist
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return nil, err // report the array error; it's the documented form
		}
		raw = wrapped.Channels
	}

	entries := make([]Entry, 0, len(raw))
	for _, jc := range raw {
		if strings.TrimSpace(jc.URL) == "" {
			continue
		}
		e := Entry{
			Name:    strings.TrimSpace(jc.Name),
			Group:   strings.TrimSpace(jc.Group),
			Logo:    strings.TrimSpace(jc.Logo),
			URL:     strings.TrimSpace(jc.URL),
			TvgID:   strings.TrimSpace(jc.TvgID),
			Headers: jc.Headers,
		}
		if e.Group == "" {
			e.Group = defaultGroup
		}
		entries = append(entries, e)
	}
	return entries, nil
}
