# RMS - Raspberry Media Server

A lightweight, database-free media server that implements the Jellyfin API. Built for low-power devices like the Raspberry Pi.

## Why

Jellyfin uses ~200 MB of RAM idle. RMS uses ~9 MB. Same libraries, same apps, 23x less memory.

| | RMS | Jellyfin |
|---|---|---|
| Idle memory | ~9 MB | ~200 MB |
| Streaming memory | ~16 MB | ~250 MB |
| Disk writes | 0 B | ~2 GB |
| Database | None | SQLite |
| Startup time | Instant | ~30s |

## Features

- **Jellyfin-compatible API** — works with Streamyfin, Kodi (Jellyfin plugin), and other Jellyfin clients
- **Direct play** with remux and transcode fallback (FFmpeg)
- **Web UI** — browse libraries, view metadata/artwork, play videos, manage subtitles (optional, toggleable)
- **NFO + artwork** — reads Kodi-standard NFO files, posters, backdrops, logos, episode thumbnails
- **Subtitles** — SRT to WebVTT conversion on the fly, multi-language support
- **Per-user library access** — each user sees only their assigned libraries
- **No database** — reads directly from the filesystem. Add or remove content and it appears instantly
- **Environment variable support** — use `${VAR}` in config for Docker secrets
- **Auto-scan** — optional periodic job to fill in missing metadata, subtitles, and thumbnails

## Architecture

Three binaries, one Docker image:

| Binary | Purpose | Memory |
|---|---|---|
| `rms` | Media server (Jellyfin API + web UI) | ~9 MB idle |
| `metacrawler` | Downloads NFO + artwork (TMDB, TVmaze, AniList) | Runs and exits |
| `subcrawler` | Downloads subtitles (OpenSubtitles) | Runs and exits |

Crawlers run as separate processes — their code never loads into the server's memory.

## Quick Start

### Docker Compose

```yaml
services:
  rms:
    image: pixelotes/rms:arm64
    container_name: raspberry-media-server
    restart: unless-stopped
    ports:
      - "8082:8082"
    environment:
      - "TZ=Europe/Madrid"
    volumes:
      - "/path/to/media:/app/media"
      - "/path/to/config:/app/config"
```

### Configuration

Copy `config/config.example.yml` to your config directory and edit:

```yaml
app:
  port: 8082
  ui_enabled: true
  ui_password: "${RMS_PASSWORD}"
  jwt_secret: "${RMS_JWT_SECRET}"
  jellyfin_version: "10.10.7"

player:
  stream_strategy: ["direct", "remux", "transcode"]

libraries:
  - friendly_name: "Movies"
    path: "./media/Movies"
    metadata_lang: en
    content_type: movies
  - friendly_name: "TV Shows"
    path: "./media/TV"
    content_type: tvseries
  - friendly_name: "Anime"
    path: "./media/Anime"
    content_type: anime

# Optional: per-user library access
users:
  - username: "raul"
    password: "${RAUL_PASSWORD}"
    libraries: ["Movies", "Anime"]
  - username: "maria"
    password: "${MARIA_PASSWORD}"
    libraries: ["Movies", "TV Shows"]

# Optional: crawler settings
crawlers:
  subtitles:
    api_key: "${OPENSUBTITLES_API_KEY}"
    languages: ["en", "es"]
  metadata:
    tmdb_api_key: "${TMDB_API_KEY}"
  auto_scan:
    enabled: false
    interval_hours: 24
    metadata: true
    subtitles: true
    thumbnails: false
```

If no `users` section is defined, a single user `rms` with `ui_password` has access to all libraries.

## Connecting Clients

### Streamyfin / Jellyfin Apps

- Server URL: `http://your-device:8082`
- Username: any configured user (or anything if using default)
- Password: the user's password (or `ui_password`)

### Web UI

Navigate to `http://your-device:8082` in a browser. Features include:
- Library browsing with poster art
- Metadata display (plot, rating, runtime, genres)
- Video playback with multi-language subtitles
- Buttons to download metadata, subtitles, and generate thumbnails

## Crawlers

### metacrawler

Downloads NFO files and artwork. Provider is auto-selected by `content_type`:
- `movies` → TMDB (requires API key)
- `tvseries` → TVmaze (no key needed)
- `anime` → AniList (no key needed)

```bash
# Process all libraries from config
docker exec raspberry-media-server ./metacrawler -config /app/config/config.yml

# Single directory
docker exec raspberry-media-server ./metacrawler --type movies --tmdb-key xxx --path "/app/media/Movies/Big Buck Bunny (2008)"

# Generate thumbnails (extracts a frame from each video)
docker exec raspberry-media-server ./metacrawler --thumbnails -config /app/config/config.yml
```

### subcrawler

Downloads subtitles from OpenSubtitles. Searches by file hash first, then by filename.

```bash
# Process all libraries from config
docker exec raspberry-media-server ./subcrawler -config /app/config/config.yml

# Single directory
docker exec raspberry-media-server ./subcrawler --api-key xxx --langs en,es --recursive --path "/app/media/Movies/"
```

Both crawlers skip content that already has metadata/subtitles.

## Building from Source

```bash
# Server
go build -o rms ./cmd/rms

# Crawlers
go build -o metacrawler ./cmd/metacrawler
go build -o subcrawler ./cmd/subcrawler

# Cross-compile for Raspberry Pi
GOOS=linux GOARCH=arm64 go build -o rms ./cmd/rms
```

## Expected Directory Structure

RMS reads standard Kodi/Jellyfin media layouts:

```
Movies/
  Big Buck Bunny (2008)/
    Big Buck Bunny (2008) [1080p].mp4
    Big Buck Bunny (2008) [1080p].en.srt
    Big Buck Bunny (2008) [1080p].es.srt
    movie.nfo
    poster.jpg
    fanart.jpg
    logo.png

TV Shows/
  Sintel (2010)/
    tvshow.nfo
    poster.jpg / folder.jpg
    fanart.jpg / backdrop.jpg
    Season 1/
      Sintel.S01E01.720p.mkv
      Sintel.S01E01.720p.nfo
      Sintel.S01E01.720p.en.srt
      Sintel.S01E01.720p-thumb.jpg
```

## Part of the Stack

RMS is designed to work alongside:
- **[Reel](https://github.com/pixelotes/reel)** — media downloader, organizer, metadata fetcher
- **[Scarf](https://github.com/pixelotes/scarf)** — torrent indexer/searcher

## License

MIT
