# ADR-012: Version Compatibility Strategy

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin API changes across versions. Some endpoints are only available in newer versions, while older versions have different endpoints. Supporting multiple Jellyfin versions would require:

- Version detection
- Conditional endpoint registration
- Multiple handler implementations
- Complex routing logic

This contradicts RMS's design goals:

- **Simple architecture**
- **~23x less memory than Jellyfin**
- **Easy to maintain**

Version checking is already implemented:

```go
func (s *Server) jfVersionAtLeast(major, minor int) bool {
    m, n := s.config.App.JellyfinMajorMinor()
    return m > major || (m == major && n >= minor)
}
```

## Decision

**Use version checking to conditionally enable endpoints.** Don't implement multiple versions.

### Implementation

```go
// Version detection
func (s *Server) jfVersionAtLeast(major, minor int) bool {
    m, n := s.config.App.JellyfinMajorMinor()
    return m > major || (m == major && n >= minor)
}

// Conditional route registration
func (s *Server) SetupRoutes() {
    // Register all routes
    jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
    
    // Conditionally register version-specific routes
    if s.jfVersionAtLeast(10, 10) {
        jf.HandleFunc("/System/Info", s.jfSystemInfo).Methods("GET")
    }
    
    if s.jfVersionAtLeast(10, 9) {
        jf.HandleFunc("/System/ActivityLog/Entries", s.jfEmptyItems).Methods("GET")
    }
}
```

### Version Detection

```go
// Detect Jellyfin version from API
func (s *Server) jfSystemInfoPublic(w http.ResponseWriter, r *http.Request) {
    // ...
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Version": "10.8.10",
        // ...
    })
}

// Parse version string
func parseVersion(version string) (major, minor int) {
    parts := strings.Split(version, ".")
    if len(parts) >= 1 {
        if m, err := strconv.Atoi(parts[0]); err == nil {
            major = m
        }
    }
    if len(parts) >= 2 {
        if n, err := strconv.Atoi(parts[1]); err == nil {
            minor = n
        }
    }
    return major, minor
}
```

### Version-Specific Handlers

Some endpoints have different implementations for different versions:

```go
// Jellyfin 10.8+
func (s *Server) jfSystemInfoNew(w http.ResponseWriter, r *http.Request) {
    // New format
}

// Jellyfin < 10.8
func (s *Server) jfSystemInfoOld(w http.ResponseWriter, r *http.Request) {
    // Old format
}
```

## Consequences

### Positive

- **Compatible**: Works with multiple Jellyfin versions
- **Simple**: Only one handler per endpoint
- **Low memory**: No multiple handler implementations
- **Easy to maintain**: Fewer handlers to maintain

### Negative

- **Version detection required**: Must detect version first
- **Conditional logic**: Must check version before registering routes
- **Limited compatibility**: Only supports configured versions
- **Complex routing**: Routes depend on version

### Client Behavior

Clients should:
1. Detect server version
2. Use appropriate endpoints
3. Handle version-specific responses
4. Fall back to older versions if needed

### Future Considerations

- Add more version checks as needed
- Add version-specific handlers if necessary
- Consider version negotiation
- Document supported versions

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
