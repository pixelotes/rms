# Architecture

## Project Structure

```
rms/
├── cmd/
│   ├── rms/            # Main server binary
│   ├── metacrawler/    # Metadata/artwork downloader
│   └── subcrawler/     # Subtitle downloader
├── internal/
│   ├── config/         # YAML config parsing
│   ├── media/          # Filesystem operations (scanner, NFO, subtitles, probe)
│   ├── server/         # HTTP server, routes, handlers
│   └── crawlers/
│       ├── metadata/   # TMDB, TVmaze, AniList, Kitsu clients
│       └── subtitles/  # OpenSubtitles client
├── web/                # Web UI (HTML/JS/CSS)
├── specs/              # Jellyfin OpenAPI specs for reference
├── config/             # Example config
└── docs/               # This documentation
```

## Design Decisions

### Three Binaries, One Config

The server, metadata crawler, and subtitle crawler are separate binaries that share the same `config.yml`. This means:

- Crawlers don't consume server memory when idle
- Crawlers can run on a different schedule or even a different machine
- The server stays lean (~9 MB idle)

### Filesystem as Database

RMS has no database. All data comes from:

| Data | Source |
|------|--------|
| Media catalog | Directory structure + `PopulateIDStore` walk |
| Metadata | NFO files (Kodi XML format) |
| Artwork | Image files (poster.jpg, fanart.jpg, logo.png) |
| Subtitles | SRT files alongside video files |
| User state | In-memory map (optionally persisted to JSON) |

The ID store (`sync.Map`) maps deterministic UUIDs (SHA-256 of path) to filesystem paths. It's populated at startup and refreshed after auto-scans or manual rescan.

### Jellyfin API as an Adapter Layer

The Jellyfin API handlers are an adapter layer over the internal `media` package:

```
Client Request
    → gorilla/mux route
    → jf_*.go handler (adapter)
    → internal/media (filesystem)
    → JSON response
```

The adapter translates between Jellyfin DTOs and filesystem operations. This keeps the internal media package clean and reusable.

### Stub Philosophy

Many Jellyfin endpoints are stubbed with empty responses. The approach:

1. **If a client crashes without it** — implement a minimal stub
2. **If a client degrades gracefully** — leave it as a 404 (caught by the debug logger)
3. **If a client needs real data** — implement it properly

The `[UNHANDLED]` debug log makes it easy to discover which endpoints clients need.

## Key Components

### Item IDs

Every filesystem path gets a deterministic UUID:

```go
func ItemID(path string) string {
    h := sha256.Sum256([]byte(path))
    return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", ...)
}
```

IDs are stable across restarts (same path = same ID). The reverse mapping is stored in a `sync.Map` populated at startup.

### User Data Store

```go
type UserDataStore struct {
    mu       sync.RWMutex
    data     map[string]map[string]*UserDataEntry  // userID → itemID → entry
    filePath string                                 // empty = memory only
    dirty    bool
}
```

- Thread-safe via `RWMutex`
- Flushed to disk every 5 minutes if dirty (when `filePath` is set)
- Flushed on graceful shutdown
- Atomic writes (temp file + rename)

### Streaming

```
GET /Videos/{itemId}/stream?static=true   → http.ServeFile (supports Range)
GET /Videos/{itemId}/stream?static=false  → http.ServeFile (direct play)
```

RMS doesn't do real transcoding. Both `static=true` and `static=false` serve the file directly. The native API also supports remux (FFmpeg copy to MP4) and transcode (FFmpeg H.264/AAC) via the `strategy` parameter.

### Image Fallback Chain

```
Episode thumbnail → Season poster → Show poster
Season poster → Show poster
Movie poster → (none)
```

The `imageTagsForDir` and `imageTagsForVideo` functions check each level and populate `ImageTags` in the DTO accordingly. The `jfGetItemImage` handler follows the same chain when serving the actual image.

## Memory Profile

| Component | Memory |
|-----------|--------|
| Go runtime | ~5 MB |
| ID store (10K items) | ~2 MB |
| User data (100 entries) | <1 MB |
| HTTP connections | ~1 MB per active stream |
| **Total idle** | **~9 MB** |

## Concurrency

- HTTP server: gorilla/mux with Go's default `net/http` server (goroutine per connection)
- ID store: `sync.Map` (lock-free reads)
- User data: `sync.RWMutex` (concurrent reads, exclusive writes)
- Auto-scan: single goroutine with `time.Ticker`
- Userdata flush: single goroutine with `time.Ticker`
