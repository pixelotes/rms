# Raspberry Media Server

A lightweight, database-free media server that speaks the Jellyfin API. Built in Go for the Raspberry Pi.

## Why RMS?

Jellyfin is great software, but it's heavy. On a Raspberry Pi with limited RAM, it competes with everything else for resources. RMS takes a different approach:

- **~9 MB RAM** at idle (vs ~200 MB for Jellyfin)
- **No database** — the filesystem is the source of truth
- **No transcoding engine** — direct play and remux only
- **Three separate binaries** — crawlers don't load into server memory
- **Jellyfin API compatible** — works with Streamyfin, Moonfin, Kodi, and other Jellyfin clients

## Design Philosophy

RMS is built around a few core principles:

1. **The filesystem is the database.** Media files, NFO metadata, artwork, and subtitles all live on disk in standard Kodi/Jellyfin directory structures. There is nothing to migrate, backup, or corrupt.

2. **Separate concerns into separate processes.** The server (`rms`) only serves. Metadata downloading (`metacrawler`) and subtitle fetching (`subcrawler`) are standalone binaries that run on demand or on a schedule.

3. **Fake what you don't need.** RMS emulates the Jellyfin API surface that clients actually use. Transcoding capabilities are advertised but not implemented — clients fall back to direct play, which is what you want on a Pi anyway.

4. **Optional persistence.** By default, everything is in memory. Playback progress, favorites, and watch status can optionally be persisted to a JSON file. No database required.

## Stack

RMS is part of a self-hosted media stack:

| Component | Role |
|-----------|------|
| **RMS** | Media server (Jellyfin-compatible) |
| **Reel** | Download manager and media organizer |
| **Scarf** | Torrent indexer/aggregator |

All three run on a single Raspberry Pi.

## Tech Stack

| | |
|---|---|
| **Language** | Go 1.24 |
| **Router** | gorilla/mux |
| **Auth** | JWT (golang-jwt/v5) |
| **Config** | YAML (gopkg.in/yaml.v3) |
| **Runtime deps** | FFmpeg (remux + probe), Alpine Linux |
| **Container** | Docker (multi-stage, ~30 MB image) |

## Quick Start

```yaml
# config/config.yml
app:
  port: 8082
  jwt_secret: "your-secret-here"
  jellyfin_version: "10.10.0"

users:
  - username: admin
    password: ${MEDIA_PASSWORD}

libraries:
  - friendly_name: Movies
    path: /app/media/Movies
    content_type: movies
  - friendly_name: TV Shows
    path: /app/media/TV
    content_type: tvseries
```

```bash
docker run -d \
  -p 8082:8082 \
  -v ./config:/app/config \
  -v /media:/app/media:ro \
  pixelotes/rms:arm64
```

Point any Jellyfin client at `http://<pi-ip>:8082` and log in.
