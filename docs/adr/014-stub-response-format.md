# ADR-014: Stub Response Format

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Different stub types return different response formats. Consistency is important for:

- Client expectations
- Error handling
- API documentation
- Testing

Having inconsistent stub responses could confuse clients and make debugging harder.

## Decision

**Use consistent response formats for each stub type.**

### Response Formats

#### 1. `jfNotFound` (404 Not Found)

```http
HTTP/1.1 404 Not Found
```

**Use when:** The feature doesn't exist or doesn't make sense for RMS.

**Implementation:**
```go
func (s *Server) jfNotFound(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
}
```

#### 2. `jfEmptyItems` (Empty Items Object)

```json
{
  "Items": [],
  "StartIndex": 0,
  "TotalRecordCount": 0
}
```

**Use when:** The feature returns a list that should be empty.

**Implementation:**
```go
func (s *Server) jfEmptyItems(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}
```

#### 3. `jfEmptyArray` (Empty Array)

```json
[]
```

**Use when:** The feature returns an array that should be empty.

**Implementation:**
```go
func (s *Server) jfEmptyArray(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}
```

#### 4. `jfEmptyObject` (Empty Object)

```json
{}
```

**Use when:** The feature returns an object that should be empty.

**Implementation:**
```go
func (s *Server) jfEmptyObject(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{})
}
```

#### 5. `jfSessionStub` (Session-Specific Stub)

```json
{
  "UserId": "stub-session-id",
  "Name": "rms",
  "IsAuthenticated": false,
  "SessionId": "stub-session-id",
  "Policies": {
    "EnableRemoteContentDownload": false,
    "EnableContentScanning": false,
    "EnableLiveTvAccess": false,
    "EnableMediaPlayback": true,
    "EnablePlaybackRecording": false,
    "EnableContentSharing": false
  },
  "IsAdministrator": false,
  "Capabilities": {
    "Audio": false,
    "Video": false,
    "AudioPlayback": false,
    "VideoPlayback": false,
    "LiveTv": false,
    "PlaybackRecording": false,
    "ContentSharing": false
  }
}
```

**Use when:** The feature requires authentication and session management.

**Implementation:**
```go
func (s *Server) jfSessionStub(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "UserId":      stableID("session"),
        "Name":        "rms",
        "IsAuthenticated": false,
        "SessionId":   stableID("session"),
        "Policies":    map[string]bool{},
        "IsAdministrator": false,
        "Capabilities": map[string]bool{},
    })
}
```

### Implementation Helpers

```go
// Generate stable session ID
func stableID(name string) string {
    return fmt.Sprintf("rms-%s", name)
}

// Common JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}
```

## Consequences

### Positive

- **Consistent**: Same format for same stub type
- **Predictable**: Clients know what to expect
- **Easy to parse**: Standard JSON structure
- **Easy to test**: Known response format
- **Easy to document**: Clear response format

### Negative

- **More code**: Multiple stub implementations
- **Potential confusion**: Clients might expect different formats

### Client Behavior

Clients should:
1. Check response status code
2. Parse JSON response
3. Handle stub responses gracefully
4. Fall back to default behavior

### Future Considerations

- Add `X-RMS-Stub` header to indicate stub responses
- Document stub response formats
- Consider adding stub type to response

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
