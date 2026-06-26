# ADR-015: Live TV from M3U / IPTV Playlists

**Status:** ✅ Accepted (revises [ADR-005](./005-no-live-tv.md))
**Date:** 2026-06-26
**Author:** pixelotes

## Context

[ADR-005](./005-no-live-tv.md) decided **not** to implement Live TV, stubbing every
`/LiveTv/*` endpoint to empty responses. Its rationale was that Jellyfin's Live TV stack
(tuners, DVR/recordings, EPG, schedules) is large, stateful, and CPU/RAM heavy — at odds
with RMS's goals.

That rationale still holds for **DVR-style** Live TV. But users asked for the *common*
case that does **not** need any of that: pointing RMS at an **M3U/IPTV playlist** of HLS
channels and watching them. ADR-005 explicitly left this door open:

> ### Future Considerations
> - If users request Live TV support, implement it
> - Consider adding HLS/MPEG-TS support

A playlist of channels is a good fit for RMS because it stays true to the core model:

- **No database** — the playlist is the source of truth, parsed into memory on boot/rescan.
- **No disk writes** — streams are remote URLs; RMS redirects or proxies, never stores.
- **Filesystem-driven config** — a new `content_type: tv` library, like every other library.

## Decision

Implement a **read-only subset** of the Live TV API backed by parsed M3U/JSON playlists.
Channels are browsable as a normal library (root → categories → channels) **and** exposed
through the `/LiveTv/*` API so Jellyfin clients show their native Live TV section.

### In scope

| Capability | How |
|---|---|
| Parse M3U (`#EXTINF`, `tvg-id`, `tvg-name`, `tvg-logo`, `group-title`, `#EXTVLCOPT`) | `internal/tv` package |
| Parse JSON playlists | `internal/tv` package |
| Group channels by `group-title` (categories) | in-memory `ChannelStore` |
| Browse: library → categories → channels | reuses `parentId` browse + native `/api/v1/browse` |
| `GET /LiveTv/Info`, `GET /LiveTv/Channels`, `GET /LiveTv/Channels/{id}` | real handlers |
| Channel playback via `PlaybackInfo` + `POST /LiveStreams/Open` | HLS `MediaSource` |
| Stream delivery: **redirect** (default) or **proxy** | `?strategy` / `tv_proxy` |
| Channel logos | redirect to `tvg-logo` (proxy + optional cache later) |

### Explicitly NOT in scope (still stubbed, per ADR-005)

- EPG / program guide (`/LiveTv/Programs*`, `/GuideInfo`) → remain empty.
- Recordings / DVR (`/LiveTv/Recordings*`, `/LiveTv/Timers*`, `/LiveTv/SeriesTimers*`).
- Tuners / listing providers (`/LiveTv/TunerHosts*`, `/LiveTv/ListingProviders*`).
- Channel mapping, transcoding of live streams, ABR.

A channel is an **infinite, non-seekable** HLS source. RMS does not transcode it.

### Data model

A new library type. The playlist is the source; channels live in an in-memory store
parallel to the existing file `idStore`:

```yaml
libraries:
  - friendly_name: "TV"
    content_type: tv
    path: "./lists/tv.m3u"   # .m3u | .json | directory of them | http(s) URL
    tv_proxy: false          # false = HTTP 302 redirect (default); true = proxy stream
    tv_refresh_hours: 0      # re-fetch a remote playlist every N hours (0 = boot/rescan only)
```

### Hierarchy and conditional visibility

Three levels, reusing the existing folder/`parentId` browse:

```
TV (library — shown ONLY when enabled AND ≥1 channel parsed)
└── Generalistas      (category = group-title)
    └── La 1          (channel = tvg-name + logo + HLS URL)
```

The TV library is **hidden** unless `content_type: tv` is configured *and* its source
parsed at least one channel. If the playlist is missing, empty, or unparseable, the
library simply does not appear — other libraries are unaffected. Validity is re-evaluated
on every `Populate` (boot/rescan), consistent with RMS's stateless model.

### Streaming

- **Redirect (default):** `302` to the channel URL. Works for native Jellyfin clients and
  for browsers when the stream allows cross-origin and needs no custom headers.
- **Proxy (`tv_proxy: true`, or when `#EXTVLCOPT` headers are present):** RMS fetches the
  stream with the required `User-Agent`/`Referer` and rewrites the HLS manifest so variant
  and segment requests route back through RMS. This is byte-piping only — **no transcode** —
  but each viewer holds an upstream connection.

### Why represent channels through `/LiveTv` and not just as videos

The WebUI uses the native API (`/api/v1/browse` + `/api/v1/stream`), so it needs its own
channel browse and HLS playback **regardless**. For Jellyfin clients, returning a populated
`/LiveTv/Info` (`Services` non-empty, `IsEnabled: true`) is what makes the client reveal its
dedicated Live TV section and treat channels as infinite live streams (no seek bar, correct
"live" UI). Both surfaces are backed by the **same** `ChannelStore`, so there is one parser
and one source of truth.

## Consequences

### Positive

- Common IPTV use case works in both the WebUI and Jellyfin clients.
- Stays database-free and disk-write-free (redirect mode).
- Reuses existing browse, image, and routing plumbing.
- `video.js` already bundles VHS, so browser HLS needs no new dependency.

### Negative

- Proxy mode holds one upstream connection per viewer (watch on a Pi 3B).
- No EPG: clients show channels without program metadata.
- Channels requiring `#EXTVLCOPT` headers won't play in redirect mode (need proxy).
- Browser CORS may force proxy mode for some channels.

## References

- [ADR-005: No Live TV Support](./005-no-live-tv.md) (revised by this ADR)
- [ADR-007: No Database](./007-no-database.md)
- [ADR-011: Streaming Strategy](./011-direct-play-remux-only.md)
- [PLAN-tv.md](../../PLAN-tv.md) — implementation plan
- `specs/jellyfin-openapi-10.11.9.json` — `GetLiveTvInfo`, `GetLiveTvChannels`, `OpenLiveStream`
