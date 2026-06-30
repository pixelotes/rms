# ADR-006: No Plugin/Package Management

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin has a plugin and package management system, but RMS doesn't implement it. Plugin features include:

- Plugin installation
- Plugin uninstallation
- Plugin configuration
- Plugin manifest
- Plugin images
- Plugin updates

Package management features:

- Package installation
- Package uninstallation
- Package list

Implementing plugin/package management would require:

- Package repository
- Download management
- Installation/upgrade logic
- Configuration handling
- Manifest parsing
- Significant complexity

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**
- **No database**

Plugin endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/Plugins` | GET | ❌ `jfEmptyArray` |
| `/Packages` | GET | ❌ `jfEmptyArray` |
| `/Plugins/{pluginId}` | DELETE | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}` | DELETE | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Disable` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Enable` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Image` | GET | ❌ `jfNotFound` |
| `/Plugins/{pluginId}/Configuration` | GET | ❌ `jfEmptyObject` |
| `/Plugins/{pluginId}/Configuration` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/Manifest` | POST | ❌ `jfSessionStub` |

## Decision

**Don't implement plugin/package management.** Return **empty responses** for all plugin/package endpoints.

### Implementation

```go
// /Plugins GET
func (s *Server) jfPlugins(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

// /Packages GET
func (s *Server) jfPackages(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, []interface{}{})
}

// /Plugins/{pluginId} DELETE
func (s *Server) jfDeletePlugin(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

// /Plugins/{pluginId}/Configuration GET
func (s *Server) jfPluginConfiguration(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{})
}
```

### Stub Responses

```json
// Empty array responses
[]

// Empty object responses
{}

// No content responses
HTTP/1.1 204 No Content
```

## Consequences

### Positive

- **Simple**: No plugin code
- **Low memory**: No plugin data
- **Low disk usage**: No plugin files
- **Compatible**: Clients get consistent responses

### Negative

- **No plugins**: Can't extend functionality
- **No packages**: Can't install packages
- **Limited functionality**: Many features require plugins
- **Not compatible**: Some Jellyfin clients expect plugins

### Client Behavior

Clients should:
1. Handle empty plugin/package responses
2. Use built-in features only
3. Fall back to manual configuration

### Future Considerations

- If users request plugin support, implement it
- Consider adding simple plugin system
- Add package repository integration

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
