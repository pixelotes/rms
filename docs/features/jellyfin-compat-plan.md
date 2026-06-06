# Jellyfin Client Compatibility Plan

This is a living implementation note for improving Jellyfin client compatibility
against the local OpenAPI specs in `specs/`.

## Goal

Make RMS work with as many Jellyfin clients as possible while staying lightweight:
direct play/direct stream only, no real transcoding pipeline, no admin UI, and no
full Jellyfin database.

## Current Baseline

- Known working: Streamyfin, Moonfin, Kodi Jellyfin add-on mode.
- Jellyfin macOS official app should be treated as Jellyfin Web compatibility,
  because it is effectively a wrapper around the web UI.
- API specs available locally:
  - `specs/jellyfin-openapi-10.10.7.json`
  - `specs/jellyfin-openapi-10.11.8.json`

## Quick Wins

These are useful because many clients probe them during startup or detail-page
loading, and they can safely return empty/stub responses:

- System/admin probes: `/System/Ping`, `/System/Endpoint`,
  `/System/ActivityLog/Entries`, `/System/Logs`, configuration GET/POST stubs.
- User/account probes: `/Users`, password/reset provider routes, auth key stubs.
- Device/session probes: `/Devices/*`, session command/viewing/message stubs.
- Display preferences: support both GET and POST.
- Library/admin triggers: `/Library/Refresh`, virtual folder and media update
  POST stubs.
- Metadata extras: remote images, metadata editor, external IDs, critic reviews,
  instant mix/similar/recommendation empty results.
- Startup wizard probes: `/Startup/*` should report already completed.
- Plugin/package/task probes: return empty lists or no-op success.
- Image route variants with tag/size path segments should map to the existing
  item image handler.
- Official aliases for user data/favorites/played state should be registered
  alongside the `/Users/{userId}/...` variants.

## Not Quick Wins

These need real behavior or can mislead clients if stubbed too aggressively:

- HLS manifests and segment endpoints (`master.m3u8`, `main.m3u8`, `/hls/...`).
- Real transcoding/remux decision logic.
- Trickplay tile generation.
- Audio-library specific behavior beyond direct streaming.
- LiveTV/recording/tuner support beyond disabled/empty probes.
- Jellyfin Web as a primary supported target.

## Implementation Log

- Added startup/admin/client-probe stubs for system, plugins, packages,
  scheduled tasks, devices, localization, LiveTV info, and library metadata.
- Added user-data aliases and route-based playback progress support.
- Added basic web-wrapper note to the Jellyfin compatibility docs.
- Added broader no-op coverage for sessions, startup wizard, auth keys, system
  logs/activity, library update triggers, metadata editor/remote search, image
  route variants, plugin configuration, package details, scheduled task details,
  QuickConnect unavailable responses, user-admin no-ops, grouping options,
  device deletion, video additional parts, audio lyrics stubs,
  subtitle upload/delete no-ops, attachment/alternate-source/version stubs, and
  `/Audio/{itemId}/universal`.
- Replaced several stubs with real lightweight behavior:
  - `/Items/Counts` now counts movies, series, episodes, and total items.
  - `/Items/{itemId}/Images` lists real local primary/backdrop images.
  - `/Items/{itemId}/MetadataEditor` returns the real item DTO when possible.
  - `/Items/{itemId}/ExternalIdInfos` returns common provider definitions.
  - Similar/recommendation endpoints return local library items instead of empty
    results.
  - `/Shows/NextUp` returns the first unwatched episode per series.
  - `/Library/Refresh`, `/Items/{itemId}/Refresh`, and the `ScanLibrary`
    scheduled task refresh the in-memory item index.
  - `/Items/Latest` aggregates all accessible libraries when no parent id is
    provided, and `/Items/Suggestions` now returns latest local items.
- Findroid investigation:
  - Findroid lists series/seasons but did not request
    `/Shows/{seriesId}/Episodes` when entering a season; it only requested the
    season via `/Items/{seasonId}`.
  - Season DTOs now include `SeriesId`, `SeriesName`, `ParentId`,
    `IndexNumber`, and `ChildCount`.
  - TV/anime video files now become `Type: Episode` when browsed via generic
    `/Items?ParentId={seasonId}` endpoints.
  - Recursive item collection now includes video files, so
    `Recursive=true&IncludeItemTypes=Episode` can return episodes.
  - `/Shows/NextUp?seriesId=...` now honors the `seriesId` filter.
- Findroid playback follow-up:
  - Playback is now advertised as direct play/direct stream only
    (`SupportsTranscoding=false`) to avoid clients choosing missing HLS
    transcoding routes.
  - `PlaybackInfo` now includes `ErrorCode`, a stable `PlaySessionId`,
    default audio/subtitle stream indexes, bitrate, empty required headers,
    `PlaySessionId` on the media source, and an extension-bearing
    `DirectStreamUrl`.

## Remaining Focus

After the quick-win pass, remaining focused gaps are mostly in these categories:

- HLS/transcoding-style video and audio routes:
  `/Videos/*/master.m3u8`, `/Videos/*/main.m3u8`, `/Videos/*/hls/...`,
  `/Audio/*/master.m3u8`, `/Audio/*/main.m3u8`, `/Audio/*/hls/...`.
- Subtitle HLS manifests:
  `/Videos/{itemId}/{mediaSourceId}/Subtitles/{index}/subtitles.m3u8`.
- Trickplay tiles:
  `/Videos/{itemId}/Trickplay/{width}/tiles.m3u8` and tile jpg routes.
- Real user administration.
