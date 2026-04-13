# Jellyfin Compatibility

RMS emulates a subset of the Jellyfin API — enough to work with popular clients while staying lightweight.

## Supported Clients

| Client | Platform | Status |
|--------|----------|--------|
| **Streamyfin** | iOS/Android | Fully working |
| **Moonfin** | Android TV | Fully working |
| **Kodi** (Jellyfin plugin) | All platforms | Fully working (Add-on mode) |
| **Findroid** | Android | Requires API 10.11 |
| **Jellyfin Web** | Browser | Not supported |

## How It Works

RMS registers routes that match the Jellyfin API surface. Clients connect to RMS as if it were a Jellyfin server. The key differences:

- **No transcoding** — `SupportsDirectPlay` and `SupportsDirectStream` are always `true`. Clients fall back to direct play.
- **No real sessions** — playback state is tracked in memory (or optionally persisted to file), but there's no session management.
- **No user management** — users are defined in YAML config. No registration, password reset, or admin panel.
- **Stub endpoints** — many endpoints return empty responses. Clients handle this gracefully.

## Authentication

RMS supports the Jellyfin `MediaBrowser` authentication scheme:

```
Authorization: MediaBrowser Token="<jwt>", Client="...", Device="...", DeviceId="...", Version="..."
```

Also accepted:

- `X-Emby-Token: <jwt>` header
- `X-MediaBrowser-Token: <jwt>` header
- `?api_key=<jwt>` query parameter

Tokens are standard JWTs signed with the configured `jwt_secret`.

## API Version

Set `jellyfin_version` in config to control which API features are active:

```yaml
app:
  jellyfin_version: "10.10.0"  # or "10.11.0"
```

Version 10.11 adds:

- `POST /Sessions/Playing/ReportStart`
- `POST /Sessions/Playing/ReportProgress`
- `POST /Sessions/Playing/ReportStopped`
- `DELETE /Sessions/Logout`
- `GET /System/Storage`

## Kodi Integration

The Kodi Jellyfin plugin works in **Add-on mode** (not Direct Path mode). Enable the Kodi SyncQueue emulation to suppress the plugin warning:

```yaml
app:
  kodi_sync_queue: true
```

!!! note
    Direct Path mode doesn't work because Kodi tries to open the filesystem path from inside the container (`/app/media/...`). Use Add-on mode, which streams via HTTP.

## User Data

Playback progress, watch status, and favorites are tracked per user. By default this is in-memory only. To persist across restarts:

```yaml
app:
  userdata_path: /app/config/userdata.json
```

Supported operations:

- Resume playback from last position
- Mark as played/unplayed
- Toggle favorites
- Filter by `IsResumable` or `IsFavorite`

## Image Handling

RMS serves artwork from the filesystem with intelligent fallback:

1. **Movies** — poster.jpg, fanart.jpg from the movie directory
2. **Seasons** — poster.jpg from the season directory, falls back to show directory
3. **Episodes** — episode-specific thumbnail, falls back to season, then show
4. **Libraries** — poster.jpg from the library root

All images are served with `Cache-Control: public, max-age=86400`.
