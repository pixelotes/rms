# Jellyfin Compatibility

RMS emulates a subset of the Jellyfin API — enough to work with popular clients while staying lightweight.

## Supported Clients

| Client | Platform | Status |
|--------|----------|--------|
| **Streamyfin** | iOS/Android | Fully working |
| **Moonfin** | Android TV | Fully working |
| **Kodi** (Jellyfin plugin) | All platforms | Fully working (Add-on mode) |
| **Findroid** | Android | Requires API 10.11 |
| **Jellyfin Web / Desktop wrapper** | Browser/macOS | Not supported as a primary target |

!!! note
    The official Jellyfin app on macOS is effectively a wrapper around Jellyfin Web. If it works, treat that as Web UI compatibility rather than native client API compatibility.

## How It Works

RMS registers routes that match the Jellyfin API surface. Clients connect to RMS as if it were a Jellyfin server. The key differences:

- **No transcoding by default** — `SupportsDirectPlay` and `SupportsDirectStream` are always `true`; a progressive transcode is only advertised when `player.stream_strategy` lists `transcode`.
- **Lightweight sessions** — every device/user pair gets an in-memory session fed by its Capabilities and Playing reports. `GET /Sessions` lists them (with `NowPlayingItem` and `PlayState`) and `POST /Sessions/{id}/Playing|Playing/{command}|Command|Message` are forwarded as `Play`/`Playstate`/`GeneralCommand` messages over the target device's WebSocket, so "play on" and remote control between clients work. Sessions without a socket expire after 15 minutes of inactivity; nothing is persisted.
- **Minimal WebSocket** — `/socket` speaks just enough of the Jellyfin session protocol for clients to stay connected and refresh: `ForceKeepAlive`/`KeepAlive`, `UserDataChanged` when played/favorite/position/rating change, and `LibraryChanged` after a rescan. Subscriptions (`SessionsStart`, `ActivityLogEntryStart`...) are accepted and ignored.
- **YAML-based user management** — users (and per-library access) are defined in `config.yml`. The built-in `rms` admin user with `app.ui_password` is always available and sees all libraries; additional users can be added under `users:`. There is no in-app registration, password reset, or admin panel.
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
