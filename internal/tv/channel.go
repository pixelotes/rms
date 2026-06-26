// Package tv parses M3U/JSON IPTV playlists into an in-memory channel store.
//
// It is the TV/Live TV counterpart to the file-based internal/media package:
// where media maps sha256(path) -> filesystem path, tv maps an identity-derived
// UUID -> *Channel. The two ID spaces never collide because tv keys are
// prefixed, so request handlers can dispatch unambiguously (try the channel
// store, else the file store).
//
// Playlists list the SAME logical channel multiple times — one entry per
// alternate stream/CDN (e.g. "La 1" twice, "24h" three times). The store
// therefore parses raw Entries and MERGES entries that share an identity
// (tvg-id, else normalized name) into a single Channel carrying several
// Sources, so the UI shows one item with built-in failover.
package tv

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Entry is one raw playlist row (a single #EXTINF + URL in M3U terms), before
// merging. Parsers emit Entries; the store turns them into Channels.
type Entry struct {
	Name    string            // tvg-name (preferred) or the #EXTINF display title
	Group   string            // group-title; "" normalized to defaultGroup at ingest
	Logo    string            // tvg-logo URL ("" if none)
	URL     string            // stream URL (HLS .m3u8, MPEG-TS, …)
	TvgID   string            // tvg-id ("" if none)
	Headers map[string]string // HTTP headers from #EXTVLCOPT (User-Agent, Referer)
}

// ChannelSource is one playable stream of a Channel. A Channel has one or more,
// in playlist order; the first is the primary, the rest are failover.
type ChannelSource struct {
	URL     string
	Headers map[string]string
}

// Channel is a logical channel: the merge of every Entry sharing its identity.
type Channel struct {
	ID      string          // deterministic UUID, identity-derived
	Name    string          // display name (from the first merged entry)
	Group   string          // category (first group the channel appeared in)
	Logo    string          // first non-empty tvg-logo among merged entries
	TvgID   string          // tvg-id if any merged entry had one
	Sources []ChannelSource // 1+ alternate streams
	LibKey  string          // owning library key (its config Path) — see store
	Number  int             // 1-based position within its library (first-seen order)
}

// Group is a category (distinct group-title) within one TV library.
type Group struct {
	ID         string // deterministic UUID = GroupID(LibKey, Name)
	Name       string
	LibKey     string
	ChannelIDs []string // channel IDs in first-seen order
}

// defaultGroup is used when an entry has no group-title.
const defaultGroup = "Uncategorized"

// identity returns the merge key for an entry: its tvg-id when present,
// otherwise its lowercased name. Empty when neither is usable.
func (e Entry) identity() string {
	if id := strings.TrimSpace(e.TvgID); id != "" {
		return "id:" + strings.ToLower(id)
	}
	if n := strings.TrimSpace(e.Name); n != "" {
		return "name:" + strings.ToLower(n)
	}
	return ""
}

// ChannelID returns the deterministic UUID for a channel identity within a
// library. libKey is the library's config Path (stable and user-independent,
// unlike its slice index). The "tvchan|" prefix guarantees the value never
// equals a media.ItemID (which hashes a bare path), keeping the ID spaces
// disjoint.
func ChannelID(libKey, identity string) string {
	return uuidFromKey("tvchan|" + libKey + "|" + identity)
}

// GroupID returns the deterministic UUID for a (library, group-title) pair.
func GroupID(libKey, group string) string {
	return uuidFromKey("tvgroup|" + libKey + "|" + group)
}

func uuidFromKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
