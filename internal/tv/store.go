package tv

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"raspberry-media-server/internal/config"
)

// snapshot is an immutable view of all parsed channels. Populate swaps it in
// atomically, so readers never lock (mirrors media.idStore's lock-free reads).
type snapshot struct {
	channels map[string]*Channel // channel ID -> channel
	groups   map[string]*Group   // group ID -> group
	byLib    map[string][]*Group // library key (config Path) -> groups in display order
	libCount map[string]int      // library key -> channel count
}

func emptySnapshot() *snapshot {
	return &snapshot{
		channels: map[string]*Channel{},
		groups:   map[string]*Group{},
		byLib:    map[string][]*Group{},
		libCount: map[string]int{},
	}
}

// store holds the current snapshot. Default value (nil) is treated as empty.
var store atomic.Pointer[snapshot]

func current() *snapshot {
	if s := store.Load(); s != nil {
		return s
	}
	return emptySnapshot()
}

// httpClient is used to fetch remote playlists. A modest timeout keeps a slow
// or dead URL from stalling boot.
var httpClient = &http.Client{Timeout: 20 * time.Second}

// Populate (re)parses every content_type: "tv" library and atomically replaces
// the channel store. It returns the total number of (merged) channels loaded.
// Libraries that fail to load are reported via the returned error slice.
func Populate(libs []config.Library) (total int, errs []error) {
	snap := emptySnapshot()

	for _, lib := range libs {
		if lib.ContentType != "tv" {
			continue
		}
		entries, err := loadSource(lib.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("tv library %q: %w", lib.FriendlyName, err))
			continue
		}
		ingest(snap, lib.Path, entries)
	}

	store.Store(snap)
	for _, c := range snap.libCount {
		total += c
	}
	return total, errs
}

// ingest merges raw entries into channels and indexes them into the snapshot.
// Entries sharing an identity (tvg-id, else lowercased name) collapse into one
// Channel with multiple Sources, so the SAME channel listed N times in a
// playlist (alternate CDNs) becomes a single browsable item with failover.
func ingest(snap *snapshot, libKey string, entries []Entry) {
	byIdentity := make(map[string]*Channel) // identity -> channel, within this library
	number := 0

	for _, e := range entries {
		url := strings.TrimSpace(e.URL)
		if url == "" {
			continue
		}
		ident := e.identity()
		if ident == "" {
			continue // nothing to key on
		}

		ch, ok := byIdentity[ident]
		if !ok {
			number++
			groupName := e.Group
			if groupName == "" {
				groupName = defaultGroup
			}
			ch = &Channel{
				ID:     ChannelID(libKey, ident),
				Name:   e.Name,
				Group:  groupName,
				Logo:   e.Logo,
				TvgID:  e.TvgID,
				LibKey: libKey,
				Number: number,
			}
			byIdentity[ident] = ch
			snap.channels[ch.ID] = ch
			snap.libCount[libKey]++

			gid := GroupID(libKey, groupName)
			g, ok := snap.groups[gid]
			if !ok {
				g = &Group{ID: gid, Name: groupName, LibKey: libKey}
				snap.groups[gid] = g
				snap.byLib[libKey] = append(snap.byLib[libKey], g)
			}
			g.ChannelIDs = append(g.ChannelIDs, ch.ID)
		}

		// Append this stream as a source (skip exact-duplicate URLs).
		if !hasSourceURL(ch, url) {
			ch.Sources = append(ch.Sources, ChannelSource{URL: url, Headers: e.Headers})
		}
		if ch.Logo == "" && e.Logo != "" {
			ch.Logo = e.Logo
		}
		if ch.TvgID == "" && e.TvgID != "" {
			ch.TvgID = e.TvgID
		}
	}

	// Stable, case-insensitive group ordering for a predictable UI.
	groups := snap.byLib[libKey]
	sort.SliceStable(groups, func(a, b int) bool {
		return strings.ToLower(groups[a].Name) < strings.ToLower(groups[b].Name)
	})
}

func hasSourceURL(ch *Channel, url string) bool {
	for _, s := range ch.Sources {
		if s.URL == url {
			return true
		}
	}
	return false
}

// loadSource reads a playlist from a file, a directory of playlists, or an
// http(s) URL, and parses it into raw entries by extension/content.
func loadSource(path string) ([]Entry, error) {
	if isURL(path) {
		return loadURL(path)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return loadFile(path)
	}

	// Directory: concatenate every *.m3u / *.m3u8 / *.json inside it.
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var all []Entry
	for _, de := range dirEntries {
		if de.IsDir() || !isPlaylistFile(de.Name()) {
			continue
		}
		es, err := loadFile(filepath.Join(path, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", de.Name(), err)
		}
		all = append(all, es...)
	}
	return all, nil
}

func loadFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseByName(path, f)
}

func loadURL(url string) ([]Entry, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	return parseByName(url, resp.Body)
}

func parseByName(name string, r io.Reader) ([]Entry, error) {
	if strings.HasSuffix(strings.ToLower(stripQuery(name)), ".json") {
		return ParseJSON(r)
	}
	return ParseM3U(r)
}

// --- Read API (lock-free) ---

// LookupChannel returns the channel with the given ID, or (nil, false).
func LookupChannel(id string) (*Channel, bool) {
	c, ok := current().channels[id]
	return c, ok
}

// LookupGroup returns the group with the given ID, or (nil, false).
func LookupGroup(id string) (*Group, bool) {
	g, ok := current().groups[id]
	return g, ok
}

// GroupsForLibrary returns the categories of a TV library in display order.
// libKey is the library's config Path.
func GroupsForLibrary(libKey string) []*Group {
	return current().byLib[libKey]
}

// ChannelsForGroup returns the channels of a group in playlist order.
func ChannelsForGroup(groupID string) []*Channel {
	snap := current()
	g, ok := snap.groups[groupID]
	if !ok {
		return nil
	}
	out := make([]*Channel, 0, len(g.ChannelIDs))
	for _, id := range g.ChannelIDs {
		if c, ok := snap.channels[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

// ChannelCount returns how many channels were parsed for a library (keyed by
// its config Path). The TV library is shown in the UI only when this is > 0
// (ADR-015 visibility rule).
func ChannelCount(libKey string) int {
	return current().libCount[libKey]
}

// --- helpers ---

func isURL(p string) bool {
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

func isPlaylistFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u", ".m3u8", ".json":
		return true
	}
	return false
}

func stripQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
}
