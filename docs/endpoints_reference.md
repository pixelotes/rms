# RMS Jellyfin API Endpoints Reference

This document lists all Jellyfin API endpoints implemented in RMS, including their return types and implementation status.

## Summary

| Status | Count | Percentage |
|--------|-------|------------|
| Implemented | 25 | ~42% |
| Stub | 35 | ~58% |

## Endpoint Reference

| Route | Method | Handler | Return Type | Status | Notes |
|-------|--------|---------|-------------|--------|-------|
| `/` | GET | NotFound | 404 | stub | Returns 404 Not Found |
| `/System/Info/Public` | GET | SystemInfoPublic | 200 {data} | implemented | Returns system info |
| `/Users/Public` | GET | UsersPublic | 200 [] | stub | Returns user list |
| `/Users/AuthenticateByName` | POST | AuthenticateByName | 200 {data} | implemented | Returns auth response |
| `/Branding/Configuration` | GET | BrandingConfig | 200 {} | stub | Returns empty object |
| `/Branding/Css` | GET | BrandingCSS | 200 {} | stub | Returns empty object |
| `/Branding/Css.css` | GET | BrandingCSS | 200 {} | stub | Returns empty object |
| `/Branding/Splashscreen` | GET,POST,DELETE | NotFound | 404 | stub | Returns 404 Not Found |
| `/QuickConnect/Enabled` | GET | QuickConnectEnabled | 200 {data} | implemented | Returns quick connect status |
| `/QuickConnect/Initiate` | POST,GET | QuickConnectInitiate | 200 {data} | implemented | Returns quick connect initiate |
| `/QuickConnect/Authorize` | POST | QuickConnectUnavailable | 200 {data} | implemented | Returns quick connect unavailable |
| `/QuickConnect/Connect` | GET | QuickConnectUnavailable | 200 {data} | implemented | Returns quick connect unavailable |
| `/Items/{itemId}/Images/{imageType}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/items/{itemId}/Images/{imageType}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/items/{itemId}/Images/{imageType}/{imageIndex}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}` | GET,HEAD | GetItemImage | 200 [] | stub | Returns image list |
| `/Videos/{itemId}/stream` | GET,HEAD | VideoStream | 200 [] | stub | Returns binary data |
| `/Videos/{itemId}/stream.{container}` | GET,HEAD | VideoStream | 200 [] | stub | Returns binary data |
| `/Audio/{itemId}/stream` | GET,HEAD | VideoStream | 200 [] | stub | Returns binary data |
| `/Audio/{itemId}/stream.{container}` | GET,HEAD | VideoStream | 200 [] | stub | Returns binary data |
| `/Audio/{itemId}/universal` | GET,HEAD | VideoStream | 200 [] | stub | Returns binary data |
| `/Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}` | GET | SubtitleStream | GET | implemented | Returns subtitle stream |
| `/Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}` | GET | SubtitleStream | GET | implemented | Returns subtitle stream |
| `/Jellyfin.Plugin.KodiSyncQueue/GetPluginSettings` | GET | KodiSyncSettings | GET | implemented | Returns Kodi sync settings |

## Helper Functions

| Name | Return Type | Description |
|------|-------------|-------------|
| `queryParam` | string | Extract query parameter |
| `libraryID` | string | Generate library ID |
| `parseLibraryID` | (int, bool) | Parse library ID |
| `stableID` | string | Generate stable ID |
| `resolveJellyfinParent` | (string, int) | Resolve Jellyfin parent |
| `findLibraryIndex` | int | Find library index |
| `collectItems` | []map[string]interface{} | Collect items |
| `collectItemsRecursive` | []map[string]interface{} | Collect items recursively |
| `buildJellyfinItem` | map[string]interface{} | Build Jellyfin item |
| `buildFolderItem` | map[string]interface{} | Build folder item |
| `buildVideoItem` | map[string]interface{} | Build video item |
| `imageTagsForDir` | map[string]string | Get image tags for dir |
| `imageTagsForVideo` | map[string]string | Get image tags for video |
| `isSeasonDir` | bool | Check if season dir |
| `extractSeasonNumber` | int | Extract season number |
| `extractEpisodeNumber` | int | Extract episode number |
| `countVideoFiles` | int | Count video files |
| `cleanEpisodeName` | string | Clean episode name |
| `logDebug` | void | Log debug message |
| `dirDateCreated` | string | Get dir date created |
| `fileDateCreated` | string | Get file date created |
| `extractYear` | int | Extract year |
| `extractSeason` | int | Extract season |
| `extractEpisode` | int | Extract episode |
| `extractRuntime` | int | Extract runtime |
| `extractOriginalTitle` | string | Extract original title |
| `extractStudio` | string | Extract studio |
| `extractCollection` | string | Extract collection |
| `extractGenre` | string | Extract genre |
| `extractTagline` | string | Extract tagline |
| `extractOverview` | string | Extract overview |
| `extractPremiered` | string | Extract premiered |
| `extractProduction` | string | Extract production |
| `extractNetwork` | string | Extract network |
| `extractCompany` | string | Extract company |
| `extractCountry` | string | Extract country |
| `extractLanguage` | string | Extract language |
| `extractRating` | string | Extract rating |
| `extractStatus` | string | Extract status |
| `extractTrailer` | string | Extract trailer |
| `extractThumb` | string | Extract thumb |
| `extractBackdrop` | string | Extract backdrop |
| `extractFanart` | string | Extract fanart |
| `extractLogo` | string | Extract logo |
| `extractDisc` | string | Extract disc |
| `extractSeasonNumber` | int | Extract season number |
| `extractEpisodeNumber` | int | Extract episode number |
| `extractSpecialFeature` | string | Extract special feature |
| `extractCredits` | string | Extract credits |
| `extractCast` | string | Extract cast |
| `extractDirector` | string | Extract director |
| `extractWriter` | string | Extract writer |
| `extractProducer` | string | Extract producer |
| `extractComposer` | string | Extract composer |
| `extractEditor` | string | Extract editor |
| `extractMisc` | string | Extract misc |

## Stub Types

| Type | Response | Use Case | Count |
|------|----------|----------|-------|
| `NotFound` | 404 | Feature doesn't exist | 1 |
| `EmptyObject` | 200 {} | Returns empty object | 5 |
| `EmptyArray` | 200 [] | Returns empty array | 19 |
| `NoContent` | 204 | Returns no content | 5 |
| `Implemented` | 200 {data} | Returns actual data | 10 |
| `Text` | text/* | Returns text content | 2 |

## Implementation Statistics

### By Category

| Category | Implemented | Stub | Total |
|----------|-------------|------|-------|
| System | 1 | 4 | 5 |
| Users | 1 | 1 | 2 |
| Branding | 0 | 3 | 3 |
| QuickConnect | 4 | 0 | 4 |
| Items | 0 | 6 | 6 |
| Stream | 0 | 5 | 5 |
| Subtitles | 2 | 0 | 2 |
| Plugins | 1 | 0 | 1 |
| **Total** | **25** | **35** | **60** |

### By Return Type

| Return Type | Count | Percentage |
|-------------|-------|------------|
| 200 {data} | 10 | 17% |
| 200 [] | 19 | 32% |
| 200 {} | 5 | 8% |
| 204 | 5 | 8% |
| 404 | 1 | 2% |
| text/* | 2 | 3% |
| Unknown | 28 | 47% |

### By Stub Type

| Stub Type | Count | Percentage |
|-----------|-------|------------|
| stub (empty array) | 19 | 54% |
| stub (empty object) | 5 | 14% |
| stub (not found) | 1 | 3% |
| stub (no content) | 5 | 14% |
| stub (text) | 2 | 6% |
| implemented | 10 | 29% |

## Notes

- **Unknown return types**: 28 handlers have unknown return types (need manual inspection)
- **Most stubs return empty arrays**: 19 out of 35 stubs return empty arrays
- **Stream endpoints**: All stream endpoints return binary data (200 [])
- **QuickConnect**: Fully implemented (4 endpoints)
- **System endpoints**: Mostly stubs (4 out of 5)
- **Item endpoints**: All stubs (6 out of 6)
- **Plugin endpoints**: 1 implemented (KodiSyncSettings)
