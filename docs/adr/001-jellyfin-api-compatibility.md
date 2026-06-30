# ADR-001: Jellyfin API Compatibility Strategy

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

RMS (Raspberry Media Server) needs to be compatible with Jellyfin clients (Kodi, web UI, mobile apps, etc.) to provide a familiar API and user experience. However, implementing the full Jellyfin API would require:

- 100+ endpoints
- Complex authentication and session management
- Database for user management
- Plugin and package management
- Live TV support
- Significant memory usage (Jellyfin uses ~1GB, RMS aims for ~23x less)

Many of these features are not needed for RMS's core functionality as a lightweight, filesystem-based media server.

## Decision

Implement Jellyfin API compatibility using **route-based dispatching** with **stubs for unimplemented endpoints**:

1. **Route-based dispatching**: Use gorilla/mux router to match URLs to handler functions (NOT if/then/switch)
2. **Selective implementation**: Only implement essential endpoints
3. **Stubs for unimplemented features**: Return appropriate empty responses for features not supported by RMS

### Implementation Details

```go
// All handlers in single file
internal/server/jf_compat.go

// Route registration
jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
jf.HandleFunc("/Users/Public", s.jfUsersPublic).Methods("GET")
// ... ~100+ routes

// Version checking
func (s *Server) jfVersionAtLeast(major, minor int) bool {
    m, n := s.config.App.JellyfinMajorMinor()
    return m > major || (m == major && n >= minor)
}

// Stub handlers
func (s *Server) jfNotFound(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
}

func (s *Server) jfEmptyItems(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}
```

### Stubs Used

| Stub Type | Response | Use Case |
|-----------|----------|----------|
| `jfNotFound` | 404 Not Found | Features that don't make sense for RMS |
| `jfEmptyItems` | `{"Items": []}` | Features that return no data |
| `jfEmptyArray` | `[]` | Features that return empty arrays |
| `jfEmptyObject` | `{}` | Features that return empty objects |
| `jfSessionStub` | Session-specific stub | Features requiring authentication |

### Endpoint Statistics

| Stub Type | Count |
|-----------|-------|
| Total stubs | 90 |
| jfSessionStub | 56 |
| jfEmptyItems | 14 |
| jfEmptyArray | 10 |
| jfEmptyObject | 2 |
| jfNotFound | 8 |

## Consequences

### Positive

- **Small codebase**: ~626 lines in single file
- **Low memory usage**: ~23x less than Jellyfin
- **Easy to maintain**: Route-based dispatching, no complex conditionals
- **Backward compatible**: Version checking allows supporting multiple Jellyfin versions
- **Graceful degradation**: Clients handle stubs gracefully

### Negative

- **Limited functionality**: Many features not available
- **No authentication**: Single-user design
- **No database**: Filesystem-based storage only
- **No transcoding**: Direct play and remux only

### Future Considerations

- If users request specific features, implement them as new handlers
- Consider adding database support if user management becomes necessary
- Live TV support would require significant additional work

## References

- [Jellyfin API Documentation](https://github.com/jellyfin/jellyfin-api)
- [RMS Architecture](./architecture.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
