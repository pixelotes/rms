# RMS Jellyfin API Endpoints Reference

This document lists all Jellyfin API endpoints implemented in RMS, including their return types and implementation status.

## Summary

| Status | Count | Percentage |
|--------|-------|------------|
| Implemented | 25 | ~42% |
| Stub | 35 | ~58% |

## Endpoint Reference

| Line | Handler | Return Type | Status | Notes |
|------|---------|-------------|--------|-------|
| 15 | `NotFound` | 404 | stub | Returns 404 Not Found |
| 19 | `BrandingCSS` | text/css | stub | Returns empty object |
| 24 | `SystemPing` | text/plain | stub | Returns "Jellyfin Server" |
| 30 | `SystemEndpoint` | 200 {data} | stub | Returns minimal endpoint info |
| 37 | `SystemConfiguration` | 200 {data} | stub | Returns empty object |
| 48 | `SystemConfigurationValue` | 200 [] | stub | Returns empty array |
| 58 | `MetadataOptionsDefault` | 200 [] | stub | Returns empty arrays |
| 69 | `EmptyObject` | 200 [] | stub | Returns empty object |
| 73 | `GetUsers` | 200 [] | stub | Returns user list |
| 85 | `GetRoot` | 200 [] | stub | Returns root folder |
| 94 | `GroupingOptions` | 200 [] | stub | Returns grouping options |
| 103 | `ItemCounts` | 200 {data} | implemented | Returns item counts per library |
| 176 | `ItemImages` | 200 [] | stub | Returns image list |
| 217 | `MetadataEditor` | 200 {data} | implemented | Returns metadata editor response |
| 242 | `ExternalIdInfos` | 200 {data} | implemented | Returns external ID info list |
| 255 | `NamedStubItem` | 200 {data} | implemented | Returns named item stub |
| 267 | `SimilarItems` | 200 {data} | implemented | Returns similar items |
| 313 | `MovieRecommendations` | 200 {data} | implemented | Returns movie recommendations |
| 338 | `NextUp` | 200 {data} | implemented | Returns next up episodes |
| 390 | `LiveTVInfo` | 200 {} | stub | Returns empty object |
| 398 | `Devices` | 200 [] | stub | Returns device list |
| 406 | `DeviceInfo` | 200 [] | stub | Returns device info |
| 410 | `DeviceOptions` | 200 [] | stub | Returns device options |
| 416 | `BitrateTest` | 200 [] | stub | Returns binary data |
| 421 | `VirtualFolders` | 200 [] | stub | Returns virtual folder list |
| 436 | `PhysicalPaths` | 200 [] | stub | Returns physical paths |
| 445 | `LocalizationCountries` | 200 [] | stub | Returns countries list |
| 449 | `LocalizationCultures` | 200 [] | stub | Returns cultures list |
| 453 | `LocalizationOptions` | 200 [] | stub | Returns localization options |
| 460 | `AuthProviders` | 200 [] | stub | Returns auth providers list |
| 467 | `PasswordResetProviders` | 200 [] | stub | Returns password reset providers |
| 474 | `QuickConnectEnabled` | 200 {data} | implemented | Returns quick connect status |
| 478 | `QuickConnectInitiate` | 200 {data} | implemented | Returns quick connect initiate |
| 484 | `QuickConnectUnavailable` | 200 {data} | implemented | Returns quick connect unavailable |
| 491 | `ForgotPassword` | 200 [] | stub | Returns forgot password response |
| 497 | `ForgotPasswordPin` | 200 [] | stub | Returns forgot password pin |
| 503 | `AuthKey` | 200 [] | stub | Returns auth key |
| 510 | `StartupConfiguration` | 200 [] | stub | Returns startup configuration |
| 521 | `StartupUser` | 204 | stub | Returns no content |
| 528 | `ScheduledTask` | 204 | stub | Returns no content |
| 539 | `ScheduledTasks` | 204 | stub | Returns scheduled tasks |
| 559 | `RunScheduledTask` | 204 | stub | Returns no content |
| 567 | `RefreshLibrary` | 204 | stub | Returns no content |
| 572 | `PackageInfo` | 200 {data} | implemented | Returns package info |

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
| `extractRuntime` | int | Extract runtime |
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
