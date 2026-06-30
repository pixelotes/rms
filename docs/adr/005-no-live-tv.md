# ADR-005: No Live TV Support

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin has extensive Live TV support, but RMS doesn't implement it. Live TV features include:

- Live TV channels
- Live TV programs
- Live TV recordings
- Live TV guides
- DVR functionality
- EPG (Electronic Program Guide)

Implementing Live TV would require:

- Live stream handling (HLS, MPEG-TS, etc.)
- Recording management
- Schedule parsing
- EPG data
- Tuner management
- Significant memory and CPU usage

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**

Live TV endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/LiveTv/Programs/Recommended` | GET | ❌ `jfEmptyItems` |
| `/LiveTv/Channels` | GET | ❌ `jfEmptyItems` |
| `/LiveTv/Programs` | GET, POST | ❌ `jfEmptyItems` |
| `/Sessions/{sessionId}/Viewing` | POST | ❌ `jfSessionStub` |
| `/Sessions/Viewing` | POST | ❌ `jfSessionStub` |

## Decision

**Don't implement Live TV support.** Return **empty responses** for all Live TV endpoints.

### Implementation

```go
// /LiveTv/Programs/Recommended GET
func (s *Server) jfLiveTvProgramsRecommended(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

// /LiveTv/Channels GET
func (s *Server) jfLiveTvChannels(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

// /LiveTv/Programs GET, POST
func (s *Server) jfLiveTvPrograms(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}
```

### Stub Response

```json
{
  "Items": [],
  "StartIndex": 0,
  "TotalRecordCount": 0
}
```

## Consequences

### Positive

- **Simple**: No Live TV code
- **Low memory**: No Live TV data
- **Low CPU**: No live stream processing
- **Compatible**: Clients get consistent responses

### Negative

- **No Live TV**: Can't watch live TV
- **No recordings**: Can't record live TV
- **No EPG**: No program guide
- **Limited functionality**: Live TV clients can't use RMS

### Client Behavior

Clients should:
1. Check if Live TV is supported
2. Handle empty Live TV responses gracefully
3. Fall back to other media sources

### Future Considerations

- If users request Live TV support, implement it
- Consider adding HLS/MPEG-TS support
- Add EPG data integration

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
