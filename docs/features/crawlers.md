# Crawlers

RMS ships two crawler binaries for fetching metadata and subtitles. They are separate processes that run on demand or on a schedule — they don't consume server memory when idle.

## Metacrawler

Downloads metadata (NFO files), artwork (posters, backdrops, logos), and optionally generates video thumbnails.

### Providers

| Content Type | Providers | API Key Required |
|-------------|-----------|-----------------|
| Movies | TMDB | Yes |
| TV Series | TVmaze, TMDB | TVmaze: No, TMDB: Yes |
| Anime | AniList, Kitsu | No |

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
  fanart.jpg
  Season 1/
    Show.S01E01.nfo  # Per-episode metadata
    Show.S01E02.nfo
```

### Configuration

```yaml
crawlers:
  metadata:
    tmdb_api_key: ${TMDB_API_KEY}
    anime_providers:
      - anilist       # Default for anime
    movie_providers:
      - tmdb          # Default for movies
    tvseries_providers:
      - tvmaze        # Default for TV (no API key needed)
```

Per-library provider override:

```yaml
libraries:
  - friendly_name: Anime
    path: /app/media/Anime
    content_type: anime
    providers:
      - kitsu         # Use Kitsu instead of default AniList
```

## Subcrawler

Downloads subtitles from OpenSubtitles.

### Usage

```bash
# Process all libraries from config
./subcrawler -config config/config.yml

# Process a single file
./subcrawler -path /media/Movies/movie.mp4 -api-key YOUR_KEY -langs en,es

# Process a directory recursively
./subcrawler -path /media/Movies/ -recursive
```

### How It Works

1. Computes the OpenSubtitles file hash
2. Searches by hash first (most accurate)
3. Falls back to filename search
4. Downloads missing languages only (skips existing)
5. Saves as `.lang.srt` (e.g., `movie.en.srt`, `movie.es.srt`)

### Configuration

```yaml
crawlers:
  subtitles:
    api_key: ${OPENSUBTITLES_KEY}
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

After each auto-scan completes, the library ID store is refreshed so new content appears immediately in all clients.

Alternative to `schedule`, use `interval_hours` for periodic runs:

```yaml
crawlers:
  auto_scan:
    enabled: true
    interval_hours: 12      # Every 12 hours
```

## Library Rescan

If you add new media outside of auto-scan (e.g., via Reel or manual copy), trigger a rescan to make it visible:

```bash
curl -X POST http://localhost:8082/api/v1/library/rescan \
  -H "Authorization: Bearer <token>"
```

This re-indexes all library directories and registers new items. It's fast (filesystem walk only, no network calls).
