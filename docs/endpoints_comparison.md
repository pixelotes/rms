# RMS vs Jellyfin API Endpoints Comparison

This document compares the RMS Jellyfin API endpoints with the expected Jellyfin OpenAPI schemas.

## Summary

| Metric | Count |
|--------|-------|
| Total RMS Routes | 13 |
| Total Jellyfin Expected Schemas | 309 |
| Matching Routes | 10 |
| Mismatching Routes | 3 |
| Match Rate | 77% |

## Mismatched Routes

The following routes have mismatched return types compared to the expected Jellyfin schemas:

| Route | Method | Handler | RMS Return | Jellyfin Expected | Issue |
|-------|--------|---------|------------|------------------|-------|
| `/Branding/Css` | GET | BrandingCSS | 200 {data} | unknown | Return type mismatch |
| `/Branding/Css.css` | GET | BrandingCSS | 200 {data} | unknown | Return type mismatch |
| `/Branding/Splashscreen` | GET,POST,DELETE | NotFound | 404 | no 200 response | Status code mismatch |

## Matched Routes

The following routes match the expected Jellyfin schemas:

| Route | Method | Handler | RMS Return | Jellyfin Expected | Status |
|-------|--------|---------|------------|------------------|--------|
| `/Users/AuthenticateByName` | POST | AuthenticateByName | unknown | unknown | ✓ |
| `/Branding/Configuration` | GET | BrandingConfig | unknown | unknown | ✓ |
| `/Items/{itemId}/Images/{imageType}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/items/{itemId}/Images/{imageType}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/items/{itemId}/Images/{imageType}/{imageIndex}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}` | GET | GetItemImage | unknown | unknown | ✓ |
| `/Jellyfin.Plugin.KodiSyncQueue/GetPluginSettings` | GET | KodiSyncSettings | unknown | unknown | ✓ |
| `/Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}` | GET | SubtitleStream | unknown | unknown | ✓ |
| `/Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}` | GET | SubtitleStream | unknown | unknown | ✓ |
| `/System/Info/Public` | GET | SystemInfoPublic | unknown | unknown | ✓ |
| `/system/info/public` | GET | SystemInfoPublic | unknown | unknown | ✓ |
| `/Users/Public` | GET | UsersPublic | unknown | unknown | ✓ |
| `/Videos/{itemId}/stream` | GET | VideoStream | unknown | unknown | ✓ |
| `/Videos/{itemId}/stream.{container}` | GET | VideoStream | unknown | unknown | ✓ |
| `/Audio/{itemId}/stream` | GET | VideoStream | unknown | unknown | ✓ |
| `/Audio/{itemId}/stream.{container}` | GET | VideoStream | unknown | unknown | ✓ |
| `/Audio/{itemId}/universal` | GET | VideoStream | unknown | unknown | ✓ |

## Notes

1. **Unknown schemas**: Most Jellyfin endpoints have "unknown" schemas because the OpenAPI spec doesn't resolve schema references properly.

2. **Status code mismatch**: The `/Branding/Splashscreen` route returns 404, but Jellyfin expects a 200 response. This is a stub that returns 404.

3. **Return type mismatch**: The `/Branding/Css` and `/Branding/Css.css` routes return 200 {data}, but the expected schema is unknown. This needs manual verification.

4. **Unknown return types**: Most RMS handlers have "unknown" return types because the return type detection is based on simple pattern matching. Manual inspection is needed for accurate return types.

## Recommendations

1. **Manual verification**: Manually inspect the return types of handlers with "unknown" status.

2. **Fix status codes**: Change `/Branding/Splashscreen` to return 200 instead of 404.

3. **Fix return types**: Update handlers with mismatched return types to match the expected Jellyfin schemas.

4. **Add more routes**: Only 13 routes are registered, but Jellyfin has 309 expected schemas. Many routes are missing.

## Next Steps

1. Read the Jellyfin OpenAPI spec to get accurate schema definitions.
2. Compare each RMS handler's return type with the expected schema.
3. Update handlers to return the correct data structures.
4. Add missing routes for endpoints that are not implemented.
