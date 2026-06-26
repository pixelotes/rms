# ADR-011: Streaming Strategy — Direct, Remux, and Best-Effort Transcode

**Status:** ✅ Revised  
**Date:** 2026-06-26  
**Author:** pixelotes

## Context

RMS exposes three streaming strategies selectable via `?strategy=` on the native API
and via `stream_strategy` in config:

| Strategy | What it does |
|----------|-------------|
| `direct` | `http.ServeFile` — no processing, full Range support |
| `remux` | FFmpeg copies video stream, re-encodes audio to stereo AAC, outputs fragmented MP4 |
| `transcode` | FFmpeg re-encodes video as H.264 (libx264, ultrafast, crf 23) + stereo AAC |

All three are **implemented and shipped**. The distinction is that transcode is a
second-class citizen: it works, but without any of the orchestration Jellyfin
provides (transcoding sessions, progress tracking, adaptive bitrate, profile
management, seek support on live pipes).

## Decision

### What is implemented

**Direct play** — always the preferred path. `http.ServeFile` handles Range requests
natively so clients can seek freely. Zero CPU overhead.

**Remux** — for clients that can't decode the source container (e.g. MKV in a
browser). Copies video bit-for-bit, normalises audio to stereo AAC/MP4. An optional
on-disk cache (`app.cache_path`) stores the remuxed file so the second play gets full
seek support. Without cache, the pipe has no `Content-Length` and clients can only
seek forward.

**Transcode** — full re-encode via `libx264 + AAC`. The same live-pipe limitation as
remux: no seek on first play, no `Content-Length`. No queue, no session tracking, no
adaptive bitrate. On a Raspberry Pi 3B this saturates the CPU and is not recommended
as a primary strategy. On a Pi 5 or any x86 host it is usable.

### What is NOT implemented

The Jellyfin transcoding protocol layer — session lifecycle, `/Videos/ActiveEncodings`,
transcode progress polling, subtitle burning, ABR profile negotiation — is stubbed.
Clients that depend on that layer (e.g. requesting a specific bitrate ladder) will not
get it.

### Why this is enough in practice

Modern Jellyfin clients (Streamyfin, Infuse, Moonfin, Kodi) negotiate direct play
first and only fall back to transcoding when they cannot decode the source. The vast
majority of content people run on home servers (H.264/H.265 in MKV/MP4) is direct-
playable on any recent device. Transcode exists as a last resort.

### Configuration

```yaml
player:
  stream_strategy:
    - direct      # Always try first
    - remux       # Fallback: fixes container issues, preserves video quality
    - transcode   # Last resort: remove on Pi 3B to avoid CPU saturation
```

On a Raspberry Pi 3B, the recommended config is `[direct, remux]`.  
On a Pi 5 or x86, `[direct, remux, transcode]` is fine.

## Consequences

### Positive

- All three strategies work out of the box with no extra dependencies beyond FFmpeg
- Direct play covers the overwhelming majority of real-world playback
- Remux solves container compatibility without quality loss
- Transcode is available for edge cases or more capable hardware

### Negative

- Transcode has no seek support on first play (live pipe, no `Content-Length`)
- No adaptive bitrate: the client gets one fixed resolution/bitrate
- No subtitle burning
- No transcode queue: concurrent transcode requests compete for the same CPU

## References

- [stream.go](../../internal/server/stream.go) — actual implementation of all three strategies
- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
