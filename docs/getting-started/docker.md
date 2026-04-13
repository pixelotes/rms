# Docker

## Image

RMS is published as `pixelotes/rms:arm64` on Docker Hub.

- **Base:** Alpine 3.21
- **Size:** ~30 MB
- **Includes:** FFmpeg, tzdata
- **Binaries:** `rms`, `metacrawler`, `subcrawler`

## Volumes

| Mount | Purpose | Mode |
|-------|---------|------|
| `/app/config` | Configuration and optional userdata | `rw` |
| `/app/media` | Media libraries | `ro` recommended |

Mount media as read-only (`:ro`) unless you're running crawlers that write metadata/artwork to the media directories.

## Resource Limits

RMS runs comfortably within tight resource constraints:

```yaml
deploy:
  resources:
    limits:
      memory: 128M
```

Typical idle usage is ~9 MB. During streaming, it stays under 20 MB.

## Docker Compose Example

```yaml
services:
  rms:
    image: pixelotes/rms:arm64
    container_name: raspberry-media-server
    ports:
      - "8082:8082"
    volumes:
      - ./config:/app/config
      - /media/movies:/app/media/Movies:ro
      - /media/tv:/app/media/TV:ro
      - /media/anime:/app/media/Anime:ro
    environment:
      - MEDIA_PASSWORD=changeme
      - TMDB_API_KEY=your-key
      - OPENSUBTITLES_KEY=your-key
    deploy:
      resources:
        limits:
          memory: 128M
    restart: unless-stopped
```

## Building the Image

```bash
# For Raspberry Pi (arm64)
docker buildx build --platform linux/arm64 -t pixelotes/rms:arm64 --push .

# For local development (native)
docker build -t rms:dev .
```

## Read-Only Filesystem

If your media is on an SD card and you want zero writes at runtime, don't set `userdata_path` in the config. All state stays in memory. The only writes would come from:

- Crawlers writing NFO/artwork (only if `auto_scan` is enabled)
- Userdata persistence (only if `userdata_path` is set)

Without both, RMS is fully read-only at runtime.

## Running Crawlers Manually

The crawler binaries are included in the image. Run them via `docker exec`:

```bash
# Download metadata for all libraries
docker exec rms ./metacrawler -config /app/config/config.yml

# Download subtitles
docker exec rms ./subcrawler -config /app/config/config.yml

# Generate thumbnails
docker exec rms ./metacrawler -config /app/config/config.yml --thumbnails

# Rescan library (make new items visible)
docker exec rms curl -s -X POST http://localhost:8082/api/v1/library/rescan \
  -H "Authorization: Bearer $(cat /app/config/token)"
```
