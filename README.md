# RMS

A lightweight, database-free media server for the Raspberry Pi that implements the Jellyfin API.

Part of [MangoStack](https://github.com/pixelotes/MangoStack), a self-hosted media stack for the Raspberry Pi.

## Footprint

| | RMS | Jellyfin |
|---|---|---|
| Idle memory | ~9 MB (last measured on a Raspberry Pi, web UI off, no background jobs; needs re-measuring) | ~200 MB |
| Streaming memory | ~16 MB direct play (same conditions; needs re-measuring) | ~250 MB |
| Memory inside MangoStack | ~37 MB (`docker stats`, 128 MB limit, web UI on, default stack config) | - |
| Database | None | SQLite |
| Disk writes | See below | Database, metadata cache, transcodes |

What changes the numbers:

- **Web UI** (`app.ui_enabled`): adds the UI routes and static files; more memory than API-only.
- **Number of items**: the item index is held in memory, so it grows with library size.
- **Background jobs**: `auto_scan`, `rescan_interval_minutes` and `tv_refresh_hours` are off by default; when on they add periodic work.
- **Remux / transcode**: run as separate `ffmpeg` child processes (capped by `player.max_ffmpeg_processes`, default 1). Their memory and CPU are not part of the `rms` process, but they count against the container limit. Transcode is the heaviest and is off unless listed in `stream_strategy`.
- **Crawlers**: `metacrawler` and `subcrawler` are separate binaries that run and exit; their code never loads into the server.
- **GC tuning**: `app.gogc` / `app.memory_limit_mb` (or `GOGC` / `GOMEMLIMIT`) trade RAM for CPU.

Disk writes: with the default config RMS writes nothing to disk. It writes only when you enable one of these:

- `app.cache_path`: remux cache and resized image variants (`<cache_path>/images`), capped by `cache_max_gb` (default 10).
- `app.userdata_path`: per-user watched/progress state as a JSON file, flushed every `userdata_flush_minutes` (default 5) and on shutdown. Without it, user data lives in memory only.
- Crawlers (run by `auto_scan`, from the web UI buttons, or by hand): write NFO files, artwork, subtitles and thumbnails into the media folders. This only happens on request.

## Features

- **Jellyfin-compatible API**: works with Streamyfin, Kodi (Jellyfin add-on, optional KodiSyncQueue endpoints) and other Jellyfin clients.
- **Direct play** with stream-copy remux fallback via ffmpeg; transcode is opt-in.
- **Web UI** (optional): browse libraries, view metadata and artwork, play videos, search/download subtitles, trigger crawlers.
- **NFO + artwork**: reads Kodi-standard NFO files, posters, backdrops, logos, episode thumbnails.
- **Subtitles**: SRT to WebVTT conversion on the fly, multiple languages.
- **Live TV** from M3U/JSON playlists (`content_type: tv`), local file, directory or URL.
- **Per-user library access**.
- **No database**: reads the filesystem directly. Index refresh via webhook, polling or auto-scan.
- **`${VAR}` / `$VAR` substitution** in the config file.

Three binaries in one image:

| Binary | Purpose |
|---|---|
| `rms` | Media server (Jellyfin API + web UI) |
| `metacrawler` | Downloads NFO + artwork; runs and exits |
| `subcrawler` | Downloads subtitles (OpenSubtitles, SubDL, Subdivx); runs and exits |

## Quick start

```yaml
services:
  rms:
    image: pixelotes/rms:arm64
    container_name: rms
    restart: unless-stopped
    ports:
      - "8082:8082"
    environment:
      - "TZ=Europe/Madrid"
    volumes:
      - "/path/to/media:/app/media"
      - "/path/to/config:/app/config"
```

The image runs `./rms -config /app/config/config.yml` and includes ffmpeg. Inside MangoStack it is already wired in `docker-compose.yaml`.

## Configuration

Copy `config/config.example.yml` to `/app/config/config.yml`. `${VAR}` and `$VAR` are expanded from the environment (left untouched if unset).

```yaml
app:
  port: 8082                    # default 8082
  ui_enabled: true
  ui_password: "${RMS_PASSWORD}"  # password of the built-in "rms" user
  jwt_secret: "${RMS_JWT_SECRET}" # default "change-me-in-production"
  jellyfin_version: "10.11.0"   # version reported to clients; default 10.11.0
  # cache_path: "./cache"       # enables remux + image cache (disk writes)
  # cache_max_gb: 10
  # userdata_path: "./config/userdata.json"  # persist watched state (disk writes)
  # userdata_flush_minutes: 5
  # kodi_sync_queue: false      # KodiSyncQueue plugin endpoints
  # kodi_sync_retention_days: 30
  # webhook_token: "${RMS_WEBHOOK_TOKEN}"    # enables POST /api/v1/library/rescan-hook
  # gogc: 50
  # memory_limit_mb: 256
  debug: false

player:
  stream_strategy: ["direct", "remux"]   # default; add "transcode" only on Pi 5 / x86
  max_ffmpeg_processes: 1                # extra requests get 503

libraries:
  - friendly_name: "Movies"
    path: "./media/Movies"
    metadata_lang: en
    content_type: movies       # movies | tvseries | anime | tv
  - friendly_name: "TV Shows"
    path: "./media/TV"
    content_type: tvseries
  - friendly_name: "Anime"
    path: "./media/Anime"
    content_type: anime
  # - friendly_name: "TV"
  #   content_type: tv
  #   path: "./lists/tv.m3u"   # file, directory or http(s) URL
  #   tv_proxy: false          # false = 302 redirect, true = proxy the stream
  #   tv_refresh_hours: 0

# Optional: extra users with per-library access (empty libraries = all)
users:
  - username: "alice"
    password: "${ALICE_PASSWORD}"
    libraries: ["Movies", "Anime"]
  - username: "bob"
    password: "${BOB_PASSWORD}"
    libraries: ["Movies", "TV Shows"]

crawlers:
  backends: ["local"]           # local | service (a Scout server) | both, in order
  # service: {url: "http://scout:8090", token: "", timeout: 10}
  subtitles:
    api_key: "${OPENSUBTITLES_API_KEY}"
    subdl_api_key: ""           # SubDL, free key
    flaresolverr_url: ""        # enables Subdivx (Spanish), e.g. http://parasol:8191
    languages: ["en", "es"]
  metadata:
    tmdb_api_key: "${TMDB_API_KEY}"
    movie_providers: ["tmdb", "imdb"]              # defaults
    tvseries_providers: ["tvmaze"]
    anime_providers: ["tvmaze", "anilist", "kitsu"]
  auto_scan:
    enabled: false              # runs metacrawler/subcrawler as child processes
    schedule: ""                # "HH:MM" daily; overrides interval_hours
    interval_hours: 24
    rescan_interval_minutes: 0  # index-only refresh, no crawlers; 0 = off
    metadata: true
    subtitles: true
    thumbnails: false           # CPU intensive
```

The built-in user `rms` (password `ui_password`) always exists and sees every library. At least one of `ui_password` or `users` is required.

## Usage

### Jellyfin clients

- Server URL: `http://your-device:8082`
- Username/password: `rms` + `ui_password`, or any configured user.

### Web UI

Open `http://your-device:8082` (when `ui_enabled: true`): library browsing, metadata, playback with subtitles, and buttons to fetch metadata, subtitles and thumbnails.

### Library rescans

- `POST /api/v1/library/rescan-hook` with `webhook_token` (for Reel, Sonarr, Radarr, scripts); calls within 5 s are debounced.
- Or `auto_scan.rescan_interval_minutes` for polling.

### metacrawler

Writes `movie.nfo` / `tvshow.nfo` (with every external ID found: IMDb, TMDB, TVDB, TVmaze, AniList…) and artwork into the media folders. Lookups go through [Scout](https://github.com/pixelotes/scout), shared with Reel, inside RMS or on a Scout service (`crawlers.backends`). Providers are tried in order: a library's own `providers`, else `crawlers.metadata.<type>_providers`:

- `movies`: TMDB (API key), IMDb (keyless), Trakt; AniList/Kitsu for anime films
- `tvseries`: TVmaze (keyless), TMDB, Trakt
- `anime`: TVmaze first (every season, episode titles, IMDb ID), then AniList and Kitsu (one entry per season)

Libraries with `download_metadata: false` are skipped by the scheduled crawl.

```bash
# All libraries
docker exec rms ./metacrawler -config /app/config/config.yml

# One directory, overriding the content type
docker exec rms ./metacrawler -config /app/config/config.yml -type movies -path "/app/media/Movies/Big Buck Bunny (2008)"

# Re-fetch even if an NFO exists
docker exec rms ./metacrawler -config /app/config/config.yml -force

# Thumbnails only (extracts a frame from each video)
docker exec rms ./metacrawler -config /app/config/config.yml -thumbnails
```

### subcrawler

Downloads missing subtitles through Scout from OpenSubtitles (`api_key`), SubDL (`subdl_api_key`) and Subdivx for Spanish (`flaresolverr_url`, through Parasol or FlareSolverr), searched together. OpenSubtitles matches by file hash first; all use the IMDb/TMDB IDs from the nearest NFO and the title. Season packs are opened to extract the episode. The scan stops when OpenSubtitles' daily quota is used up, and skips libraries with `download_subtitles: false`.

```bash
docker exec rms ./subcrawler -config /app/config/config.yml
docker exec rms ./subcrawler -config /app/config/config.yml -path "/app/media/Movies/" -recursive
```

Both crawlers skip content that already has metadata/subtitles (unless `-force` for metacrawler).

### Directory layout

RMS reads standard Kodi/Jellyfin layouts:

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

## Building from source

```bash
go build -o rms ./cmd/rms
go build -o metacrawler ./cmd/metacrawler
go build -o subcrawler ./cmd/subcrawler

# Cross-compile for Raspberry Pi
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o rms ./cmd/rms
```

ffmpeg/ffprobe must be on `PATH` for remux, transcode, thumbnails and image resizing.

## Limits

- Transcoding saturates a Raspberry Pi 3B; keep it off there. Only one ffmpeg process runs at a time by default.
- The first play of a remuxed file cannot seek; with `cache_path` set, later plays can.
- Without `userdata_path`, watched state is lost on restart.
- Without a TMDB key, movie metadata comes from IMDb suggestions + Cinemeta (unofficial). Subtitles need at least one of: an OpenSubtitles key, a SubDL key, or a FlareSolverr-compatible solver for Subdivx.
- Episode NFOs are not written; only `movie.nfo` and `tvshow.nfo`.
- Every scheduled subtitle scan searches again for videos that had no match last time.
- Only part of the Jellyfin API is implemented; some endpoints are stubs.
- Do not expose it to the internet. It assumes a trusted LAN. Change `jwt_secret` from its default.

## Part of MangoStack

| App | Role |
|---|---|
| [Scarf](https://github.com/pixelotes/scarf) | Indexer proxy (Torznab) |
| [Parasol](https://github.com/pixelotes/parasol) | Optional Cloudflare solver for Scarf |
| [Reel](https://github.com/pixelotes/reel) | Movie & TV automation |
| [Tango](https://github.com/pixelotes/tango) | Web UI and REST API for aria2c |
| [RMS](https://github.com/pixelotes/rms) | Media server (Jellyfin API) |
| [Suika](https://github.com/pixelotes/suika) | Manga reader |
| [Kaze](https://github.com/pixelotes/kaze) | Music server (Subsonic API) |
| [Sumi](https://github.com/pixelotes/sumi) | EPUB reader |
| [Dashboarr](https://github.com/pixelotes/dashboarr) | Homepage |

## License

MIT
