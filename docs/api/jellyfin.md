# Jellyfin API

RMS emulates the Jellyfin API surface that clients actually use. This page documents all implemented endpoints.

## Authentication

### Public Endpoints (no auth)

```
GET  /System/Info/Public
GET  /Users/Public
POST /Users/AuthenticateByName
GET  /Branding/Configuration
GET  /QuickConnect/Enabled
```

### Auth Scheme

After authenticating via `/Users/AuthenticateByName`, use the token in any of these formats:

```
Authorization: MediaBrowser Token="<jwt>", Client="...", Device="...", DeviceId="...", Version="..."
X-Emby-Token: <jwt>
X-MediaBrowser-Token: <jwt>
?api_key=<jwt>
```

### Authenticate

```
POST /Users/AuthenticateByName
```

**Request:**

```json
{
  "Username": "admin",
  "Pw": "password"
}
```

**Response:**

```json
{
  "User": {
    "Name": "admin",
    "Id": "9d2486d1-...",
    "HasPassword": true,
    "Policy": { "IsAdministrator": true, ... },
    "Configuration": { ... }
  },
  "AccessToken": "eyJhbGciOiJIUzI1NiIs...",
  "ServerId": "d3adb33f-cafe-babe-f00d-deadbeef1234"
}
```

## System

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/System/Info/Public` | Server name, version, ID (public) |
| GET | `/System/Info` | Full system info (authenticated) |
| GET | `/System/Configuration/encoding` | FFmpeg/encoding config |
| GET | `/System/Storage` | Storage info (10.11+) |
| GET | `/Branding/Configuration` | Branding stub |

## Users

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Users/Public` | List users for login screen |
| GET | `/Users/Me` | Current authenticated user |
| GET | `/Users/{userId}` | User by ID |

## Libraries & Items

### Views

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/UserViews` | List library views |
| GET | `/Users/{userId}/Views` | Same, with user prefix |

### Items

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Items` | Query items (supports pagination, sort, filter, search, recursive) |
| GET | `/Items/{itemId}` | Single item details |
| GET | `/Items/Latest` | Recently added items |
| GET | `/Items/Suggestions` | Stub (empty) |
| GET | `/Items/Filters` | Genre and year filters |
| GET | `/Items/{itemId}/Similar` | Stub (empty) |
| GET | `/Users/{userId}/Items` | Items with user prefix |
| GET | `/Users/{userId}/Items/{itemId}` | Item detail with user prefix |
| GET | `/Users/{userId}/Items/Latest` | Latest with user prefix |
| GET | `/Users/{userId}/Items/Resume` | Resume watching |

**Query parameters for `/Items`:**

| Parameter | Description |
|-----------|-------------|
| `parentId` | Filter by parent (library or folder ID) |
| `includeItemTypes` | Filter by type (Movie, Series, Episode) |
| `sortBy` | Sort field (SortName, ProductionYear) |
| `startIndex`, `limit` | Pagination |
| `searchTerm` | Text search |
| `recursive` | Include subdirectories |
| `ids` | Comma-separated item IDs |
| `filters` | `IsResumable`, `IsFavorite` |

### TV Shows

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Shows/{showId}/Seasons` | Seasons for a show |
| GET | `/Shows/{showId}/Episodes` | Episodes (filterable by season) |
| GET | `/Shows/NextUp` | Stub (empty) |

### Images

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Items/{itemId}/Images/{imageType}` | Item artwork |
| GET | `/Items/{itemId}/Images/{imageType}/{imageIndex}` | Indexed artwork |

Image types: `Primary` (poster), `Backdrop` (fanart), `Logo`, `Thumb`.

Both `/Items/` and `/items/` (lowercase) are supported.

## Playback

### Playback Info

```
POST /Items/{itemId}/PlaybackInfo
GET  /Items/{itemId}/PlaybackInfo
```

Returns media sources with codec info, subtitle tracks, and streaming URLs.

**Response:**

```json
{
  "MediaSources": [
    {
      "Id": "abc123",
      "Container": "mkv",
      "Size": 1605009025,
      "SupportsDirectPlay": true,
      "SupportsDirectStream": true,
      "SupportsTranscoding": true,
      "MediaStreams": [
        { "Type": "Video", "Codec": "h264", "Index": 0 },
        { "Type": "Audio", "Codec": "aac", "Index": 1 },
        { "Type": "Subtitle", "Codec": "srt", "Language": "en", "IsExternal": true, "Index": 2 }
      ],
      "DirectStreamUrl": "/Videos/abc123/stream?static=true&mediaSourceId=abc123"
    }
  ],
  "PlaySessionId": "ps-abc12345"
}
```

### Video Stream

```
GET /Videos/{itemId}/stream
GET /Videos/{itemId}/stream.{container}
GET /Audio/{itemId}/stream
```

Serves the video file directly. Supports HTTP Range requests for seeking.

When `static=true`, the file is served as-is. When `static=false`, RMS also serves direct play (no real transcoding).

### Subtitles

```
GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}
GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}
```

Serves subtitle files. SRT is converted to WebVTT on the fly when `format=vtt`.

## Sessions

All session endpoints accept playback state and track progress in the user data store.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Sessions` | List sessions (empty array) |
| POST | `/Sessions/Capabilities` | Report capabilities (stub) |
| POST | `/Sessions/Capabilities/Full` | Report capabilities (stub) |
| POST | `/Sessions/Playing` | Report playback start (tracks position) |
| POST | `/Sessions/Playing/Progress` | Report progress (tracks position) |
| POST | `/Sessions/Playing/Stopped` | Report stopped (tracks position) |
| POST | `/Sessions/Playing/Ping` | Heartbeat (stub) |

10.11+ additions:

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/Sessions/Playing/ReportStart` | Tracks position |
| POST | `/Sessions/Playing/ReportProgress` | Tracks position |
| POST | `/Sessions/Playing/ReportStopped` | Tracks position |
| DELETE | `/Sessions/Logout` | Stub |

## User Data

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/Users/{userId}/Items/{itemId}/UserData` | Update playback position, played, favorite |
| POST | `/Users/{userId}/PlayedItems/{itemId}` | Mark as played |
| DELETE | `/Users/{userId}/PlayedItems/{itemId}` | Mark as unplayed |
| POST | `/Users/{userId}/FavoriteItems/{itemId}` | Add to favorites |
| DELETE | `/Users/{userId}/FavoriteItems/{itemId}` | Remove from favorites |
| GET | `/UserItems/Resume` | Items with playback position |

## Search

```
GET /Search/Hints?searchTerm=blade
```

Returns matching items across all libraries.

## Catalog

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Genres` | List all genres (from NFO metadata) |
| GET | `/Persons` | Stub (empty) |
| GET | `/Studios` | Stub (empty) |
| GET | `/Artists` | Stub (empty) |

## Stubs

These endpoints return empty/minimal responses to prevent client errors:

| Endpoint | Response |
|----------|----------|
| `/Items/{itemId}/Intros` | Empty items |
| `/Items/{itemId}/ThemeMedia` | Empty theme songs/videos |
| `/Items/{itemId}/ThemeSongs` | Empty items |
| `/Items/{itemId}/ThemeVideos` | Empty items |
| `/Items/{itemId}/SpecialFeatures` | Empty array |
| `/Items/{itemId}/LocalTrailers` | Empty array |
| `/MediaSegments/{itemId}` | Empty items |
| `/DisplayPreferences/{id}` | Default preferences |
| `/LiveTv/Programs/Recommended` | Empty items |
| `/Videos/ActiveEncodings` | No content (DELETE) |
| `/ClientLog/Document` | No content |

## Kodi SyncQueue

Optional (enable with `kodi_sync_queue: true`):

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/Jellyfin.Plugin.KodiSyncQueue/GetPluginSettings` | Returns enabled status |
| GET | `/Jellyfin.Plugin.KodiSyncQueue/{userId}/GetItems` | Returns empty deltas |
