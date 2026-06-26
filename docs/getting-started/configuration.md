# Configuration

RMS uses a single YAML configuration file. By default, it looks for `config/config.yml`.

## Environment Variables

All values support environment variable substitution:

```yaml
app:
  jwt_secret: ${JWT_SECRET}
  ui_password: $MEDIA_PASSWORD
```

Both `${VAR}` and `$VAR` syntax are supported. If the variable is not set, the literal string is kept.

## Full Reference

```yaml
app:
  port: 8082                          # Server port (default: 8082)
  ui_enabled: true                    # Enable web UI at /
  ui_password: "secret"               # Web UI password (legacy single-user mode)
  jwt_secret: "change-me"             # JWT signing secret (required)
  jellyfin_version: "10.10.0"         # Jellyfin API version to emulate
  kodi_sync_queue: false              # Emulate Kodi SyncQueue plugin
  userdata_path: ""                   # Path to persist user data (empty = memory only)
  debug: false                        # Log all requests and debug info
  webhook_token: ""                   # If set, enables POST /api/v1/library/rescan-hook

player:
  stream_strategy:                    # Playback fallback order
    - direct                          # Serve file as-is
    - remux                           # Remux to MP4 via FFmpeg
    - transcode                       # Transcode via FFmpeg

users:
  - username: admin
    password: ${MEDIA_PASSWORD}
    libraries: []                     # Empty = access to all libraries
  - username: kids
    password: "kidspass"
    libraries:                        # Restrict to specific libraries
      - "Family Movies"
      - "Cartoons"

libraries:
  - friendly_name: Movies
    path: /app/media/Movies
    content_type: movies              # movies | tvseries | anime
    metadata_lang: en                 # Language for metadata lookups
    download_metadata: true           # Auto-fetch metadata on scan
    download_subtitles: true          # Auto-fetch subtitles on scan
    providers:                        # Override default providers
      - tmdb

  - friendly_name: TV Shows
    path: /app/media/TV
    content_type: tvseries
    metadata_lang: en
    download_metadata: true
    download_subtitles: true

  - friendly_name: Anime
    path: /app/media/Anime
    content_type: anime
    providers:
      - anilist
      - kitsu

crawlers:
  metadata:
    tmdb_api_key: ${TMDB_API_KEY}     # Required for TMDB provider
    trakt_client_id: ""               # Optional Trakt integration
    anime_providers:                  # Default providers per content type
      - anilist
    movie_providers:
      - tmdb
    tvseries_providers:
      - tvmaze

  subtitles:
    api_key: ${OPENSUBTITLES_KEY}     # OpenSubtitles API key
    languages:
      - en
      - es

  auto_scan:
    enabled: true
    schedule: "03:00"                 # Daily at 3 AM (HH:MM format)
    # interval_hours: 24              # Alternative: run every N hours
    rescan_interval_minutes: 10       # Lightweight index refresh (0 = off)
    metadata: true                    # Download missing metadata
    subtitles: true                   # Download missing subtitles
    thumbnails: true                  # Generate missing thumbnails
```

## Minimal Config

The bare minimum to get RMS running:

```yaml
app:
  jwt_secret: "any-secret-string"

users:
  - username: admin
    password: admin

libraries:
  - friendly_name: Movies
    path: /app/media/Movies
    content_type: movies
```

## App Options

### `jellyfin_version`

Controls which Jellyfin API features are enabled. RMS supports version-conditional routes:

- `10.10.x` — Base compatibility (Streamyfin, Moonfin, Kodi)
- `10.11.x` — Adds `ReportStart/Progress/Stopped`, `Sessions/Logout`, `System/Storage`

Set this to match what your clients expect. Default is `10.11.0`.

### `kodi_sync_queue`

When `true`, RMS emulates the Jellyfin KodiSyncQueue server plugin. This prevents Kodi from showing a warning about the missing plugin. The emulated queue always returns empty deltas, so Kodi performs full library scans.

### `webhook_token`

When set to a non-empty string, enables the `POST /api/v1/library/rescan-hook` endpoint. This lets external tools (Sonarr, Radarr, qBittorrent, shell scripts) trigger a library index refresh the moment new content arrives — zero polling overhead.

```bash
# Add to Sonarr/Radarr as a Custom Script connection, or call directly:
curl -fsS -X POST http://rms:8096/api/v1/library/rescan-hook \
  -H "X-Webhook-Token: ${RMS_WEBHOOK_TOKEN}"
```

The token can also be passed as `?token=<value>`. Calls within 5 seconds of each other are debounced into a single rescan so a batch of files triggers one walk, not one per file.

Leave empty (default) to keep the endpoint disabled.

### `userdata_path`

When set, playback progress, favorites, and watch status are persisted to this JSON file. Data is flushed every 5 minutes and on graceful shutdown.

```yaml
app:
  userdata_path: /app/config/userdata.json
```

When empty (default), all user data lives in memory and is lost on restart. This is ideal for read-only storage (SD cards).

## Users

If no `users` are configured, RMS falls back to a single default user (`rms`) authenticated with `ui_password`.

Each user can be restricted to specific libraries by listing their `friendly_name`:

```yaml
users:
  - username: kids
    password: "safe"
    libraries:
      - "Family Movies"
```

## Libraries

Each library must have:

- `friendly_name` — display name (used in client UIs and user restrictions)
- `path` — absolute path to the media directory
- `content_type` — one of `movies`, `tvseries`, or `anime`

The `content_type` determines how RMS interprets the directory structure and which metadata providers are used.
