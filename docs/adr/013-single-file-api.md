# ADR-013: Single-File API Implementation

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin API handlers could be organized in multiple files:

```
internal/server/
├── jf_compat.go
├── jf_system.go
├── jf_users.go
├── jf_items.go
├── jf_tvshows.go
├── jf_movies.go
└── jf_auth.go
```

Or in a single file:

```
internal/server/
└── jf_compat.go (all handlers)
```

Multiple files would require:

- More files to maintain
- More imports
- More complex organization
- Slower compilation

Single file has 626 lines, which is manageable.

## Decision

**Keep all Jellyfin API handlers in a single file** (`jf_compat.go`).

### Implementation

```go
// All handlers in single file
internal/server/jf_compat.go

// Handler functions
func (s *Server) jfSystemInfoPublic(...) { ... }
func (s *Server) jfUsersPublic(...) { ... }
func (s *Server) jfGetItems(...) { ... }
func (s *Server) jfGetItem(...) { ... }
// ... ~100 handlers

// Helper functions
func respondJSON(...) { ... }
func respondError(...) { ... }
func jfVersionAtLeast(...) bool { ... }
func stableID(...) string { ... }
```

### Route Registration

```go
// Register all routes in one place
jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
jf.HandleFunc("/Users/Public", s.jfUsersPublic).Methods("GET")
jf.HandleFunc("/Items/{itemId}", s.jfGetItem).Methods("GET")
// ... ~100 routes
```

### Helper Functions

```go
// Common response helpers
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
    respondJSON(w, status, map[string]string{"Error": msg})
}

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

## Consequences

### Positive

- **Simple**: One file to maintain
- **Fast compilation**: Less file I/O
- **Easy to find**: All handlers in one place
- **Easy to test**: One file to test
- **Less overhead**: No import resolution

### Negative

- **Large file**: 626 lines
- **Hard to navigate**: Hard to find specific handler
- **Hard to edit**: Changes affect entire file
- **Hard to review**: Large diff on changes

### Workarounds

- Use clear function names
- Add comments between handler groups
- Use code formatting tools
- Split into logical sections

### Future Considerations

- If file grows too large, consider splitting
- Add file organization guidelines
- Consider handler grouping by feature

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
