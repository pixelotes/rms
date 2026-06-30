# ADR-002: Stub Strategy for Unimplemented Features

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

RMS implements 90 stub endpoints out of ~100+ Jellyfin API endpoints. These stubs return empty responses because the features are not supported by RMS. However, using a single stub type for all unimplemented features would be confusing and could lead to client errors.

Different stub types serve different purposes:

- **404 Not Found**: The feature doesn't exist or doesn't make sense for RMS
- **Empty Items**: The feature exists but returns no data
- **Empty Array**: The feature exists but returns an empty array
- **Empty Object**: The feature exists but returns an empty object
- **Session Stub**: The feature requires authentication which RMS doesn't support

## Decision

Use **specific stub types** based on the feature category:

### 1. `jfNotFound` (404 Not Found)

**Use when:** The feature doesn't make sense for RMS or would return no meaningful data.

**Examples:**
- `/Branding/Splashscreen` - RMS doesn't have branding assets
- `/System/Logs/Log` - No logging system
- `/Videos/{videoId}/{mediaSourceId}/Attachments/{index}` - No attachments
- Image endpoints for persons/studios/artists - No person/studio/artist entities

**Response:**
```http
HTTP/1.1 404 Not Found
```

### 2. `jfEmptyItems` (Empty Array in Object)

**Use when:** The feature returns a list that should be empty.

**Examples:**
- `/System/ActivityLog/Entries` - No activity log
- `/Items/{itemId}/InstantMix` - No recommendations
- `/Items/{itemId}/CriticReviews` - No reviews
- `/Shows/Upcoming` - No upcoming shows
- `/LiveTv/Programs` - No live TV

**Response:**
```json
{
  "Items": [],
  "StartIndex": 0,
  "TotalRecordCount": 0
}
```

### 3. `jfEmptyArray` (Empty Array)

**Use when:** The feature returns a list that should be empty.

**Examples:**
- `/System/Logs` - No logs
- `/Users/{userId}/Items/{itemId}/LocalTrailers` - No trailers
- `/Items/RemoteSearch/Book` - No search results
- `/Plugins` - No plugins
- `/Packages` - No packages

**Response:**
```json
[]
```

### 4. `jfEmptyObject` (Empty Object)

**Use when:** The feature returns an object that should be empty.

**Examples:**
- `/Audio/{itemId}/Lyrics` - No lyrics
- `/Plugins/{pluginId}/Configuration` - No configuration

**Response:**
```json
{}
```

### 5. `jfSessionStub` (Session-Specific Stub)

**Use when:** The feature requires authentication and session management.

**Examples:**
- `/System/Configuration` - No user configuration
- `/Users/{userId}` - No user management
- `/Sessions/{sessionId}/Command` - No session management
- `/Devices` - No device management

**Response:**
```json
{
  "UserId": "stub-session-id",
  "Name": "rms",
  "IsAuthenticated": false,
  "SessionId": "stub-session-id"
}
```

## Implementation

```go
// Stub handler implementations
func (s *Server) jfNotFound(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
}

func (s *Server) jfEmptyItems(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfEmptyArray(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfEmptyObject(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) jfSessionStub(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "UserId": "stub-session-id",
        "Name":   "rms",
        "IsAuthenticated": false,
        "SessionId": "stub-session-id",
    })
}
```

## Consequences

### Positive

- **Clear semantics**: Clients can understand why they're getting empty responses
- **Better error handling**: 404 vs 200 with empty data
- **Easier debugging**: Can tell which features are not implemented

### Negative

- **More code**: 5 different stub implementations
- **Potential confusion**: Clients might not know which stub to expect

### Future Considerations

- Consider adding a `X-RMS-Stub` header to indicate stub responses
- Document stub behavior in API documentation
- Consider implementing features if users request them

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
