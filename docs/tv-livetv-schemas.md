# TV / LiveTV — JSON schemas (Jellyfin-compatible)

Exact response/request shapes for the Live TV handlers added per [ADR-015](adr/015-live-tv-from-m3u.md).
Field names match `specs/jellyfin-openapi-10.11.9.json`. RMS emits **PascalCase** keys
(Jellyfin convention), unlike the native `/api/v1` API which is snake_case.

Conventions used below:
- `<chan>` — channel id = `tv.ChannelID(url)` (sha256-derived UUID, like `media.ItemID`).
- `<group>` — category id = `tv.GroupID(libIndex, groupTitle)`.
- `<tvlib>` — `libraryID(i)` of the `content_type: tv` library.
- `<server>` — server id reported elsewhere by RMS (`System/Info`).

---

## 1. `GET /LiveTv/Info`

Gates the Live TV section in clients. `Services` MUST be non-empty and `IsEnabled: true`,
otherwise clients hide Live TV.

```json
{
  "Services": [
    {
      "Name": "RMS",
      "HomePageUrl": "",
      "Status": "Ok",
      "StatusMessage": "",
      "Version": "1.0",
      "HasUpdateAvailable": false,
      "IsVisible": true,
      "Tuners": []
    }
  ],
  "IsEnabled": true,
  "EnabledUsers": []
}
```

Returned only when at least one `content_type: tv` library has ≥1 parsed channel for the
requesting user; otherwise `IsEnabled: false` and `Services: []`.

---

## 2. `GET /LiveTv/Channels`

Query params honored: `UserId`, `StartIndex`, `Limit`, `SortBy` (ignored — sorted by
`Number`), `AddCurrentProgram` (ignored — no EPG). Respects per-user library access.

Response is a `BaseItemDtoQueryResult`:

```json
{
  "Items": [
    {
      "Id": "<chan>",
      "ServerId": "<server>",
      "Name": "La 1",
      "Type": "TvChannel",
      "MediaType": "Video",
      "ChannelType": "TV",
      "IsFolder": false,
      "Number": "1",
      "ChannelNumber": "1",
      "LocationType": "Remote",
      "ImageTags": { "Primary": "<chan>" },
      "MediaSources": [],
      "UserData": {
        "PlaybackPositionTicks": 0,
        "PlayCount": 0,
        "IsFavorite": false,
        "Played": false,
        "Key": "<chan>"
      }
    }
  ],
  "StartIndex": 0,
  "TotalRecordCount": 1
}
```

Notes:
- `Number` is a stable 1-based index assigned at parse time (per library, in playlist order).
- `MediaSources` is empty in the list; the real source is resolved at `PlaybackInfo` /
  `OpenLiveStream` time (matches Jellyfin behavior for channels).
- `ImageTags.Primary` present only when the channel has a `tvg-logo`.

---

## 3. `GET /LiveTv/Channels/{channelId}`

A single channel item, same shape as one `Items[]` entry above, plus a populated
`MediaSources` (see §5 for the source object).

---

## 4. `POST /Items/{itemId}/PlaybackInfo` (channel branch)

When `itemId` resolves to a channel, return a live HLS source. Request body
(`PlaybackInfoDto`) is accepted but only `UserId`/`MediaSourceId` are read.

```json
{
  "MediaSources": [ <MediaSource — see §5> ],
  "PlaySessionId": "ps-<chan[:8]>",
  "ErrorCode": null
}
```

---

## 5. `POST /LiveStreams/Open`

Request (`OpenLiveStreamDto`) — RMS reads `OpenToken` or `ItemId` to identify the channel:

```json
{ "OpenToken": "<chan>", "ItemId": "<chan>", "UserId": "<user>", "PlaySessionId": "ps-<chan>" }
```

Response (`LiveStreamResponse`):

```json
{ "MediaSource": <MediaSource — see below> }
```

### The MediaSource object (HLS / infinite)

Used by §3, §4 and §5. Key differences vs the file-based source in
[jf_playback.go](../internal/server/jf_playback.go): `Protocol: "Http"`, `IsRemote: true`,
`IsInfiniteStream: true`, no `RunTimeTicks`, no `Size`, container `hls`.

```json
{
  "Id": "<chan>",
  "Path": "/Videos/<chan>/stream",
  "Protocol": "Http",
  "Type": "Default",
  "Container": "hls",
  "Name": "La 1",
  "IsRemote": true,
  "IsInfiniteStream": true,
  "ETag": "<chan>",
  "SupportsDirectPlay": true,
  "SupportsDirectStream": true,
  "SupportsTranscoding": false,
  "SupportsProbing": false,
  "RequiresOpening": false,
  "RequiresClosing": false,
  "RequiresLooping": false,
  "ReadAtNativeFramerate": false,
  "MediaStreams": [
    {
      "Type": "Video", "Index": 0, "Codec": "h264",
      "IsDefault": true, "IsExternal": false, "VideoRange": "SDR"
    },
    {
      "Type": "Audio", "Index": 1, "Codec": "aac",
      "IsDefault": true, "IsExternal": false, "Language": "und"
    }
  ],
  "DirectStreamUrl": "/Videos/<chan>/stream?static=true&mediaSourceId=<chan>",
  "TranscodingUrl": null,
  "PlaySessionId": "ps-<chan>"
}
```

`Path`/`DirectStreamUrl` point at RMS's own stream endpoint, which then redirects (302) to
the upstream HLS URL or proxies it (`tv_proxy`). Pointing through RMS (rather than handing
the raw upstream URL) keeps per-user access checks and proxy mode possible.

---

## 6. `POST /LiveStreams/Close`

Request: `{ "LiveStreamId": "<chan>", "PlaySessionId": "ps-<chan>" }` → `204 No Content`.
RMS holds no per-stream state in redirect mode; in proxy mode it closes the upstream
connection if still open.

---

## 7. Channel images — `GET /Items/{itemId}/Images/Primary`

When `itemId` is a channel with a `tvg-logo`:
- Default: `302` redirect to the `tvg-logo` URL.
- Proxy/cache mode (later): stream the image through RMS, optionally cached under `covers/`.

`404` when the channel has no logo.

---

## 8. Library view — `GET /UserViews` (tv library entry)

A `content_type: tv` library, when visible, appears as:

```json
{
  "Name": "TV",
  "Id": "<tvlib>",
  "Type": "CollectionFolder",
  "CollectionType": "livetv",
  "IsFolder": true,
  "ImageTags": {}
}
```

Browsing it (`GET /Items?parentId=<tvlib>`) returns **categories** as folders:

```json
{
  "Items": [
    { "Id": "<group>", "Name": "Generalistas", "Type": "Folder",
      "IsFolder": true, "ChildCount": 5, "ParentId": "<tvlib>" }
  ],
  "StartIndex": 0, "TotalRecordCount": 1
}
```

Browsing a category (`GET /Items?parentId=<group>`) returns **channels** (same shape as
§2 `Items[]`, with `ParentId: "<group>"`).

---

## 9. Native API — `GET /api/v1/browse` (snake_case)

For the WebUI. Synthetic paths: `tv:<libIndex>` (→ categories), `tv:<libIndex>/<group>`
(→ channels). Channel items carry a `stream_type` so the player selects HLS.

```json
{
  "current_folder": { "path": "tv:0", "name": "TV" },
  "items": [
    { "name": "Generalistas", "path": "tv:0/Generalistas", "is_dir": true }
  ]
}
```

Channel level:

```json
{
  "current_folder": { "path": "tv:0/Generalistas", "name": "Generalistas" },
  "items": [
    {
      "name": "La 1",
      "path": "tv:chan:<chan>",
      "is_dir": false,
      "thumbnail": "/api/v1/tv/logo/<chan>",
      "stream_type": "hls"
    }
  ]
}
```

Playback: WebUI requests `/api/v1/stream/tv:chan:<chan>` and sets the `video.js` source
type to `application/x-mpegURL` when `stream_type === "hls"`.
