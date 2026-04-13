# Web UI

RMS includes a built-in web interface for browsing and playing media directly from a browser.

## Enabling

```yaml
app:
  ui_enabled: true
```

The UI is served at the root path (`/`). Static assets are served from `/css/`, `/js/`, and `/web/`.

## Features

- Library browsing with poster artwork
- Metadata display (plot, rating, runtime, genres, studio)
- Video.js-based player with playback controls
- Multi-language subtitle selection
- Search functionality
- Breadcrumb navigation
- Dark theme
- Mobile-responsive

## Authentication

The Web UI authenticates via the native RMS API (`POST /api/v1/login`). It uses JWT Bearer tokens for all subsequent requests.

## Architecture

The UI is a single-page application (`web/index.html` + `web/js/app.js`) that communicates with the `/api/v1/*` endpoints. It does not use the Jellyfin API — it's a native RMS client.

!!! note
    The Web UI is independent from Jellyfin client compatibility. Disabling it (`ui_enabled: false`) has no effect on Jellyfin clients.
