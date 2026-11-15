# Streaming strategies and ffmpeg

`player.stream_strategy` lists the strategies the server may use, in the order clients try them. Default: `["direct", "remux-lite", "remux"]`.

| Strategy | What it does | Needs ffmpeg | Seek |
|---|---|---|---|
| `direct` | Serves the file as is. | No | Yes (HTTP Range) |
| `remux-lite` | Repackages MKV with H.264/HEVC video and AAC audio into fragmented MP4, in Go. | No | From a keyframe via `?start=` |
| `remux` | Copies the video and re-encodes audio to AAC stereo with ffmpeg. Optional on-disk cache. | Yes | `?start=`, full seek once cached |
| `transcode` | Re-encodes video to H.264 and audio to AAC. Off by default. | Yes | `?start=` |

## remux-lite

`remux-lite` covers the most common case, an MKV whose codecs the browser already decodes but whose container it does not (Safari, iOS, Firefox on some systems). It reads the Matroska file and writes MP4 fragments, one per GOP, without decoding anything. It uses no ffmpeg slot and almost no CPU.

It handles:

- Video: H.264 (`V_MPEG4/ISO/AVC`) and HEVC (`V_MPEGH/ISO/HEVC`; the browser must support HEVC).
- Audio: AAC. With several audio tracks, the default AAC track is used.
- Laced blocks, unknown-size clusters (files still being written) and truncated files.

Anything else (AC-3, E-AC-3, DTS or TrueHD audio, MP4/AVI/TS containers, header-compressed tracks) is passed to `remux` when it is enabled, otherwise played directly. The log says why.

Seeking with `?start=N` reads the file up to the first keyframe at or after N; it does not use the Matroska index yet.

## The ffmpeg build in the Docker image

The image compiles its own ffmpeg and ffprobe with only what rms uses, instead of Alpine's `ffmpeg` package: the image drops from about 211 MB to 82 MB.

Included:

- Input containers: Matroska/WebM, MP4/MOV, AVI, MPEG-TS/PS, FLV, ASF/WMV, Ogg, raw audio and images.
- Video decoders (transcode, thumbnails): H.264, HEVC, MPEG-4 Part 2, MPEG-1/2, MS-MPEG4v3, VC-1/WMV3, VP8, VP9.
- Audio decoders: AAC, AC-3, E-AC-3, DTS, TrueHD/MLP, MP3, MP2, FLAC, Opus, Vorbis, WMA v2, PCM.
- Encoders: libx264, AAC, MJPEG, PNG, libwebp. Output: MP4 and images.

Not included: AV1 decoding (AV1 is still copied by `remux`), network protocols, devices and all other filters. A file that needs a missing decoder fails in `remux`/`transcode` with an "unknown decoder" error in the log; add it to the `--enable-decoder` list in the `Dockerfile`.

The ffmpeg version and its SHA-256 are pinned with the `FFMPEG_VERSION` and `FFMPEG_SHA256` build arguments. Security fixes need a version bump there; they no longer arrive with `apk upgrade`.
