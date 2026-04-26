# Jellyfin API Versioning

RMS emulates a Jellyfin server. Different Jellyfin client versions expect different API behaviors.
The version is controlled by `app.jellyfin_version` in `config.yml` (default: `"10.11.0"`).

## Architecture

```
server.go                  Route registration with version-conditional blocks
jf_system.go               /System/* endpoints
jf_auth.go                 Auth endpoints + user DTOs
jf_items.go                /Items/*, /UserViews, search, filters
jf_tvshows.go              /Shows/* (seasons, episodes)
jf_playback.go             PlaybackInfo, streaming, subtitles, media sources
jf_stubs.go                Session stubs, user data stubs, display prefs
jf_helpers.go              Shared helpers: ID generation, item building, query params
```

## Version dispatch

A single helper controls all branching:

```go
s.jfVersionAtLeast(10, 11)  // true if configured version >= 10.11
```

Routes and response fields use this inline:

```go
// Route-level (in server.go registerRoutes)
if s.jfVersionAtLeast(10, 11) {
    jfAuth.HandleFunc("/Sessions/Playing/ReportStart", s.jfSessionStub).Methods("POST")
}

// Response-level (in handler)
if s.jfVersionAtLeast(10, 11) {
    info["CastReceiverApplications"] = []interface{}{}
}
```

## Differences between 10.10 and 10.11

### Routes
| Endpoint | 10.10 | 10.11 |
|----------|-------|-------|
| `POST /Sessions/Playing/ReportStart` | N/A | Stub (replaces /Sessions/Playing) |
| `POST /Sessions/Playing/ReportProgress` | N/A | Stub (replaces /Sessions/Playing/Progress) |
| `POST /Sessions/Playing/ReportStopped` | N/A | Stub (replaces /Sessions/Playing/Stopped) |
| `DELETE /Sessions/Logout` | N/A | Stub |
| `GET /System/Storage` | N/A | Returns empty storage info |

### Response fields
- `UserItemDataDto` requires `ItemId` field in 10.11+ SDK clients
- `SystemInfo` may include additional fields per version

## API reference specs

Official Jellyfin OpenAPI specs are stored in `specs/` for reference:

- `specs/jellyfin-openapi-10.10.7.json` — Jellyfin 10.10.x
- `specs/jellyfin-openapi-10.11.8.json` — Jellyfin 10.11.x

Use these to verify endpoint signatures, required DTO fields, and response shapes when adding or debugging compatibility.

## Adding a new version (e.g. 10.12)

1. Add routes: `if s.jfVersionAtLeast(10, 12) { ... }` in `registerRoutes()`
2. Add handlers to the appropriate `jf_*.go` file
3. Add response field variations with `s.jfVersionAtLeast()` in the handler
4. No new files or abstractions needed
