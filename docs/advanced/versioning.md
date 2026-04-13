# Versioning

RMS emulates a specific Jellyfin API version configured via `jellyfin_version`. This version is reported to clients in `/System/Info/Public` and controls which endpoints and response fields are active.

## Setting the Version

```yaml
app:
  jellyfin_version: "10.10.0"
```

## Version-Conditional Features

RMS uses `jfVersionAtLeast(major, minor)` to gate features:

### 10.10 (Base)

All core functionality:

- Authentication (MediaBrowser scheme)
- Library browsing, items, images
- Video streaming (direct play + remux)
- Subtitle serving (SRT + WebVTT)
- Sessions (stubs)
- User data (progress, played, favorites)
- Search, filters, genres

### 10.11 (Additions)

- `POST /Sessions/Playing/ReportStart`
- `POST /Sessions/Playing/ReportProgress`
- `POST /Sessions/Playing/ReportStopped`
- `DELETE /Sessions/Logout`
- `GET /System/Storage`

## Client Compatibility Matrix

| Client | Minimum Version | Recommended |
|--------|----------------|-------------|
| Streamyfin | 10.10 | 10.10 |
| Moonfin | 10.10 | 10.10 |
| Kodi (Jellyfin plugin) | 10.10 | 10.10 |
| Findroid | 10.11 | 10.11 |

## Adding a New Version

1. Add conditional routes in `server.go`:

    ```go
    if s.jfVersionAtLeast(10, 12) {
        jfAuth.HandleFunc("/NewEndpoint", s.handler).Methods("GET")
    }
    ```

2. Add conditional response fields in handlers where needed.

3. Update this documentation.

## OpenAPI Specs

Reference specs are stored in `specs/` for comparison:

- `specs/jellyfin-openapi-10.11.8.json` — Jellyfin 10.11 full API spec
