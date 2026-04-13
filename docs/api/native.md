# Native API

The native RMS API is available under `/api/v1/`. All endpoints except `/login` require a JWT Bearer token.

## Authentication

### Login

```
POST /api/v1/login
```

**Request:**

```json
{
  "password": "your-password"
}
```

**Response:**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

Use the token in subsequent requests:

```
Authorization: Bearer <token>
```

## Library

### Browse

```
GET /api/v1/browse?path=/app/media/Movies
```

Returns directory contents with metadata, artwork URLs, and folder info.

**Response:**

```json
{
  "items": [
    {
      "name": "Blade Runner (1982)",
      "friendly_name": "Blade Runner",
      "path": "/app/media/Movies/Blade Runner (1982)",
      "is_dir": true,
      "icon": "/api/v1/images/abc123",
      "metadata": { "year": 1982 }
    }
  ],
  "current_folder": {
    "metadata": { "title": "Movies", "plot": "..." },
    "backdrop": "/api/v1/images/def456",
    "poster": "/api/v1/images/ghi789"
  }
}
```

### Rescan Libraries

```
POST /api/v1/library/rescan
```

Re-indexes all library directories. Call this after adding new media to make items visible in all clients.

**Response:**

```json
{
  "status": "ok"
}
```

## Streaming

### Stream Video

```
GET /api/v1/stream/{filePath}?strategy=direct
```

**Parameters:**

| Parameter | Values | Default |
|-----------|--------|---------|
| `strategy` | `direct`, `remux`, `transcode` | `direct` |

- `direct` — serves the file as-is (`http.ServeFile`, supports Range requests)
- `remux` — remuxes to MP4 via FFmpeg (copies video, transcodes audio to AAC)
- `transcode` — full transcode to H.264/AAC via FFmpeg

### Get Duration

```
GET /api/v1/duration/{filePath}
```

Returns video duration in seconds (via FFmpeg probe).

## Subtitles

### List Subtitles

```
GET /api/v1/subtitles-list/{filePath}
```

Returns available subtitle tracks for a video file.

### Get Subtitle

```
GET /api/v1/subtitles/{filePath}?lang=en
```

Returns the subtitle file. SRT files are converted to WebVTT on the fly.

## Images

### Get Image

```
GET /api/v1/images/{imageId}
```

Returns an artwork image by its stable ID.

## Crawlers

### Trigger Metadata Download

```
POST /api/v1/crawl/metadata
```

### Trigger Subtitle Download

```
POST /api/v1/crawl/subtitles
```

### Trigger Thumbnail Generation

```
POST /api/v1/crawl/thumbnails
```

All crawler endpoints run asynchronously and return immediately.
