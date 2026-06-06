# Remux/Transcode Seeking — Implementation Plan

> **Status:** Remux cache implemented (Option 2, background-build variant).
> Transcode caching not yet wired. HLS (Option 3) still pending.
> See `internal/server/stream_cache.go` and `streamRemux` in `stream.go`.

## Current state

Direct mode (`http.ServeFile`) supports HTTP Range requests natively, so the
browser can seek freely (the file is on disk, the kernel handles byte offsets).

Remux and Transcode mode (`internal/server/stream.go`) pipe `ffmpeg` stdout
straight to the response writer. There is no Content-Length, no Range support,
and the stream cannot be rewound — once a byte has been written, the only way
back is to close and re-open the connection at a different start time.

Result: the player shows the correct total duration (we patched
`/api/v1/duration` to return seconds and override `player.duration()`), but
dragging the progress bar only works within the buffered range.

## Three viable approaches

### 1. Range + ffmpeg re-seek

When a `Range: bytes=N-` request arrives:

1. Compute target timestamp `T = N / estimated_bitrate`.
2. Start `ffmpeg -ss T -i <file> …` (input-side seek = fast, keyframe-aligned).
3. Stream the new pipe; report a fake `Content-Range`/`Content-Length` large
   enough that the browser doesn't stop reading.

Pros: no disk usage, instant first-play.
Cons:
- Mapping bytes → time is approximate. For remux (`-c:v copy`) it's accurate
  enough because the bitrate is whatever the source is. For transcode it's
  very fuzzy.
- Browsers issue many small range probes during seeks (e.g. `bytes=0-1`,
  `bytes=N-N+1024`, then the real seek). Each one might spawn a new ffmpeg
  if not handled carefully — needs a small "head bytes" cache.
- Real clients sometimes do parallel range requests for prefetch.

### 2. Cache full remux to disk, then ServeFile  ★ recommended first step

On first stream request for a file:

1. Hash the source path (mtime+size) → cache key.
2. If cache miss, spawn `ffmpeg` with output to `/tmp/rms-cache/<hash>.mp4`
   (or under `app.cache_path` if we add it). Use the same flags as today but
   without `empty_moov` so the final moov is properly indexed (-movflags
   `+faststart`).
3. While caching, either:
   - (simple) block the client until the cache is complete, then `ServeFile`.
     Adds a startup delay but seek works perfectly afterwards.
   - (better) stream live to the client AND write to disk in parallel.
     Subsequent requests get `ServeFile` against the completed cache file.
4. Background eviction: LRU on the cache directory above a configurable size.

Pros:
- Once cached, behaves identically to direct mode for seek/range. No fuzzy
  byte→time mapping. Subtitles, multiple clients, all work normally.
- ~50–100 lines of Go.

Cons:
- Disk usage (a remuxed mp4 is ~the same size as the source).
- First play has either a delay (block) or initial seeks fail (live+save).
- Needs eviction policy and a config option for cache root + max size.

Best fit for the Raspberry Pi setup if there is an external SSD or enough
SD card headroom.

### 3. HLS segmented output

Generate `index.m3u8` + a series of small `.ts` segments via
`ffmpeg -f hls -hls_time 6 -hls_playlist_type vod …`.

The client uses `hls.js` (or native HLS on Safari/iOS). Each segment is a
plain file served by `http.ServeFile`. Seek works by jumping to the segment
covering the target time.

Pros:
- Industry standard. Plex/Jellyfin use this for streaming with seek.
- Adaptive bitrate is a future option (multiple renditions).
- Per-segment caching is natural.

Cons:
- Bigger change: need an HLS endpoint, segment generation, client-side
  hls.js. Video.js plays HLS but needs the source declared with the right
  MIME (`application/vnd.apple.mpegurl`).
- More moving parts (playlists, segment cleanup, manifest refresh).

## Suggested order

1. **Option 2** (cache-to-disk) for remux first — gets seek working with
   minimal code, no new client deps, no protocol surface change. Live+save
   variant if first-play delay is unacceptable; otherwise block-then-serve
   is simpler.
2. **Option 3** (HLS) later if/when we want adaptive bitrate, or if the
   cache approach proves too disk-hungry.
3. Skip Option 1 unless we specifically need zero-disk operation —
   byte→time math is too brittle for a small payoff.

## Touch points (option 2)

- `internal/config/config.go`: add `app.cache_path` (default `./cache`) and
  `app.cache_max_gb` (default e.g. 10).
- New file `internal/server/stream_cache.go` (when we split — see the
  earlier discussion about splitting `stream.go`).
- `streamRemux` becomes: check cache → if hit, `http.ServeFile`; if miss,
  remux and write to cache + stream.
- Client-side: nothing to change. `player.duration` override stays — once
  the cached file is served via ServeFile, the browser reads the real
  duration from the moov and the override falls through.
- Optionally drop the override entirely for cached responses (an
  `X-Cached: 1` header, the client checks it, skips the probe).

## Edge cases to remember

- Two clients ask for the same source while it's being remuxed → singleflight.
- ffmpeg dies mid-write → leave the partial file with a `.partial` suffix and
  delete it on next request that finds it.
- Source file changes (mtime updated) → cache key changes, old entry is
  abandoned and eventually evicted.
- Subtitles are not in the remuxed output (we don't burn them in); the
  existing `/api/v1/subtitles*` flow keeps working unchanged.
