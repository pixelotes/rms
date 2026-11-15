# Crawlers

RMS ships two crawler binaries for fetching metadata and subtitles. They are separate processes that run on demand or on a schedule — they don't consume server memory when idle.

Both use [Scout](https://github.com/pixelotes/scout), the metadata and subtitle library shared with Reel. `crawlers.backends` chooses where lookups run: `local` (default, providers inside the binaries), `service` (a Scout server, which then holds the API keys, the cache and the subtitle quota), or both in order, the second used only when the first fails.

```yaml
crawlers:
  backends: ["service", "local"]
  service:
    url: "http://scout:8090"
    token: ""
    timeout: 10
```

## Metacrawler

Downloads metadata (NFO files), artwork (posters, backdrops, logos), and optionally generates video thumbnails.

### Providers

| Content Type | Providers | API Key Required |
|-------------|-----------|-----------------|
| Movies | TMDB, IMDb, Trakt (AniList, Kitsu for anime films) | TMDB, Trakt: yes; IMDb: no |
| TV Series | TVmaze, TMDB, Trakt | TVmaze: no |
| Anime | TVmaze, AniList, Kitsu, TMDB, Trakt | TVmaze, AniList, Kitsu: no |

For anime, keep `tvmaze` first: AniList and Kitsu list each season as a separate title, so a show identified through them only gets its first season, numbered episodes and no IMDb ID.

NFOs carry every external ID the provider knows (`<uniqueid>` for imdb, tmdb, tvdb, tvmaze, anilist, kitsu, mal, trakt).

### Usage

```bash
# Process all libraries from config
./metacrawler -config config/config.yml

# Process a single directory
./metacrawler -path /media/Movies/Blade\ Runner\ \(1982\) -type movies

# Generate thumbnails (extracts frame at 1m30s via FFmpeg)
./metacrawler -config config/config.yml --thumbnails

# Force overwrite existing metadata
./metacrawler -config config/config.yml -force
```

### What It Creates

For a movie directory:

```
Movie Name (Year)/
  movie.nfo        # Kodi-format XML metadata
  poster.jpg       # Primary artwork
  fanart.jpg       # Backdrop
  logo.png         # Logo (if available)
```

For a TV show:

```
Show Name (Year)/
  tvshow.nfo       # Show-level metadata
  poster.jpg
  folder.jpg
  fanart.jpg
```

Episode NFOs are not written.

### Configuration

```yaml
crawlers:
  metadata:
    tmdb_api_key: ${TMDB_API_KEY}
    trakt_client_id: ""
    anime_providers: [tvmaze, anilist, kitsu]   # default
    movie_providers: [tmdb, imdb]               # default
    tvseries_providers: [tvmaze]                # default (no API key needed)
```

Per-library provider override:

```yaml
libraries:
  - friendly_name: Anime
    path: /app/media/Anime
    content_type: anime
    providers: [kitsu, anilist]   # replaces anime_providers for this library
    download_metadata: false      # skip in the scheduled crawl (default true)
```

## Subcrawler

Downloads missing subtitles. Three providers are searched together and their results merged:

| Provider | Config | Notes |
|----------|--------|-------|
| OpenSubtitles | `api_key` | The only one matching by file hash. Daily download quota. |
| SubDL | `subdl_api_key` (free) | By IMDb/TMDB ID or title. |
| Subdivx | `flaresolverr_url` | Spanish only, through a FlareSolverr-compatible solver such as [Parasol](https://github.com/pixelotes/parasol). Scraping: it can break when the site changes. |

### Usage

```bash
# Process all libraries from config
./subcrawler -config config/config.yml

# Process a single file
./subcrawler -path /media/Movies/movie.mp4

# Process a directory recursively
./subcrawler -path /media/Movies/ -recursive
```

### How It Works

1. Skips videos that already have every configured language
2. Computes the OpenSubtitles file hash
3. Reads the IMDb/TMDB IDs and title from the nearest `movie.nfo` / `tvshow.nfo`, and the episode from the file name (`S01E02`, `1x02`)
4. Searches every configured provider; OpenSubtitles tries hash, then ID, then title
5. Per language, downloads the best candidate (hash match, trusted, most downloaded); season packs are opened to extract the episode
6. Saves as UTF-8 `.lang.srt` (e.g., `movie.en.srt`, `movie.es.srt`)
7. Stops when OpenSubtitles reports its daily quota used up; skips libraries with `download_subtitles: false`

### Configuration

```yaml
crawlers:
  subtitles:
    api_key: ${OPENSUBTITLES_KEY}
    subdl_api_key: ""
    flaresolverr_url: ""      # e.g. http://parasol:8191
    # providers: [opensubtitles, subdl, subdivx]   # default
    languages:
      - en
      - es
```

## Auto-Scan

RMS can run crawlers automatically on a schedule:

```yaml
crawlers:
  auto_scan:
    enabled: true
    schedule: "03:00"       # Daily at 3 AM
    metadata: true          # Run metacrawler
    subtitles: true         # Run subcrawler
    thumbnails: true        # Run metacrawler --thumbnails
```

After each auto-scan completes, the library ID store is refreshed so new content appears in all clients.

Alternative to `schedule`, use `interval_hours` for periodic runs:

```yaml
crawlers:
  auto_scan:
    enabled: true
    interval_hours: 12      # Every 12 hours
```

## New Content Detection

Three strategies, in order of preference for a low-power device:

### 1. Webhook (recommended — zero idle overhead)

Configure a static token and call the hook from Sonarr/Radarr/qBittorrent or any post-download script:

```yaml
app:
  webhook_token: ${RMS_WEBHOOK_TOKEN}
```

```bash
curl -fsS -X POST http://rms:8096/api/v1/library/rescan-hook \
  -H "X-Webhook-Token: ${RMS_WEBHOOK_TOKEN}"
```

Rapid calls within 5 seconds are debounced into one walk. No crawlers run — just a fast filesystem index refresh.

### 2. Periodic index refresh (fallback for manual copies)

Adds a lightweight ticker that refreshes the item index without running any crawlers. Independent of `auto_scan.enabled`.

```yaml
crawlers:
  auto_scan:
    rescan_interval_minutes: 10   # 0 = disabled
```

A filesystem walk of a typical library on a Raspberry Pi 3B takes under a second with warm page cache.

### 3. Manual rescan

Trigger on demand from the web UI or via API (requires a valid session token):

```bash
curl -X POST http://rms:8096/api/v1/library/rescan \
  -H "Authorization: Bearer <token>"
```

All three strategies only refresh the in-memory index. They do not download metadata or subtitles.
