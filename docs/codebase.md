# Raspberry Media Server (RMS) Codebase Overview

## 1. Project Scope
RMS is a lightweight, database‑free media server that emulates the Jellyfin API. It is designed for low‑power devices such as the Raspberry Pi and provides a web UI, direct‑play and remuxing/transcoding paths, subtitle handling, and optional metadata crawling.

## 2. Architecture
```
┌─────────────────────┐
│   Docker image      │
│   (rms, metacrawler, subcrawler) │
└──────┬───────────────┘
       │
┌──────▼───────────────┐
│  Go binaries (cmd)    │
│   ├─ rms              │  HTTP server + Jellyfin‑compatible API
│   ├─ metacrawler      │  NFO / artwork fetching (TMDB, TVMaze, AniList)
│   └─ subcrawler       │  OpenSubtitles subtitle fetching
└──────┬───────────────┘
       │
┌──────▼─────────────────────────────────────┐
│  Internal packages (internal/)              │
│  ├─ config   – YAML parsing, env var expansion, defaults
│  ├─ server   – HTTP routing, Jellyfin endpoints, auth, streaming, user data
│  │   ├─ jf_*   – handlers for different Jellyfin sections
│  │   ├─ stream.go / stream_cache.go – direct, remux, transcode
│  │   ├─ jf_helpers.go – path / ID helpers, item building
│  │   └─ jf_userdata.go – per‑user playback state
│  ├─ crawlers │
│  │   ├─ metadata – TMDB, TVMaze, AniList, NFO parsing, thumbnail extraction
│  │   └─ subtitles – OpenSubtitles client, SRT→VTT conversion
│  └─ media    – filesystem utilities, path resolution, sub‑title discovery
└───────────────────────────────────────────────
```

## 3. Configuration (config/config.go)
* **app** – port, UI & JWT secrets, Jellyfin version.
* **player** – `stream_strategy` order (`direct`, `remux`, `transcode`).
* **libraries** – list of media roots with friendly name, path, language, content type.
* **users** – optional per‑user library access (default user `rms`).
* **crawlers** – OpenSubtitles API key & languages, TMDB key, auto‑scan settings.

Env variable interpolation (`${VAR}` or `$VAR`) is supported in all string fields.

## 4. Server (`cmd/rms` / `internal/server`)
* **HTTP router**: `gorilla/mux`.
* **Auth** – JWT bearer + cookie (`rms_token`).  Handlers: `/login`, `/logout`, `/me`, `/config`.  Jellyfin auth middleware injects `username` into request context.
* **Jellyfin API** – implemented via a set of `jf_*` handlers.  Most endpoints return stubbed data (e.g., empty arrays) where the Jellyfin protocol requires a response but the server does not provide that data (e.g., library monitoring).
* **Version dispatch** – `jfVersionAtLeast(major,minor)` is used throughout to enable/disable endpoints or fields based on the configured Jellyfin version.
* **Item discovery** – `buildFolderItem`, `buildVideoItem`, `collectItems*` walk the configured library paths and generate Jellyfin item DTOs.  UUIDs are deterministic (`stableID` from index/name).
* **Streaming** – `stream.go` handles `direct`, `remux`, `transcode`.  Remuxing uses `ffmpeg` to create a fast‑start MP4, optionally cached in `streamCache`.  Transcode is a placeholder that streams raw video (no hardware acceleration).
* **User data** – `jf_userdata.go` implements in‑memory store with optional disk persistence.  Tracks playback position, play count, favorite flag, and resumes.
* **Subtitles** – `subtitles.go` serves SRT→VTT on‑the‑fly and integrates with the OpenSubtitles crawler.

## 5. Crawlers
* **metacrawler** (`cmd/metacrawler`) – reads NFO files, calls external APIs (TMDB, TVMaze, AniList) to fetch poster/backdrop/episode metadata, and writes to the media folder.  Runs once per library or via auto‑scan.
* **subcrawler** (`cmd/subcrawler`) – looks for missing subtitles, queries OpenSubtitles, downloads, and saves `.srt` files.  Uses the OpenSubtitles Go client.

Both crawlers are separate binaries; the server never loads them at runtime.

## 6. Media package (`internal/media`)
Provides helpers for:
* Resolving item IDs to absolute paths (`ItemPath`).
* Detecting allowed paths for a user.
* Extracting episode numbers from filenames.
* Finding subtitles (`FindSubtitles`) and converting SRT to VTT.

## 7. Key Patterns & Conventions
* **Deterministic IDs** – `stableID` generates a UUID‑like string from a stable key (e.g., `rms-library-0`).
* **Version‑aware dispatch** – `jfVersionAtLeast` guards fields/endpoints that changed between Jellyfin 10.10 and 10.11.
* **Streaming cache** – `streamCache` keeps a LRU of remuxed MP4s on disk, automatically evicting when exceeding `maxGB`.
* **Config defaults** – `setDefaults` populates missing values; `resolvePaths` normalizes relative paths to absolute.
* **Error handling** – `respondError` standardizes JSON error responses.
* **Middleware** – `jwtMiddleware` and `jellyfinAuthMiddleware` wrap routes to enforce auth.

## 8. Testing & Build
* Dockerfile builds a multi‑stage Alpine image that includes `ffmpeg`.
* `go.mod` lists only the standard libraries and `gorilla/mux`.
* The project has no unit tests; all functionality is exercised by the integration tests in the Docker container.

## 9. Extensibility
* Adding a new Jellyfin endpoint is a matter of creating a `jf_*` handler, registering it in `registerRoutes()`, and optionally adding version guards.
* New media sources can be added by extending `buildFolderItem`/`buildVideoItem` to recognise additional file types.
* The crawler API can be expanded by adding new `metadata` providers.

---

This summary captures the essential structure and data flow of RMS, enabling future contributors to navigate the codebase and extend functionality.
