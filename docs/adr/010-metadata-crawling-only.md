# ADR-010: Metadata Crawling Only

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin has metadata storage features, but RMS only implements metadata crawling. Metadata features include:

- Metadata crawling (POST)
- Metadata storage (GET, POST, PUT, DELETE)
- Metadata updates
- Metadata caching

Implementing metadata storage would require:

- Database or filesystem for metadata
- Metadata cache management
- Metadata update handling
- Significant memory and disk usage
- Complex synchronization logic

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**
- **No database**

Metadata storage endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/Metadata/Options/Default` | GET | ❌ `jfEmptyItems` |
| `/Metadata/Editor` | POST | ❌ `jfSessionStub` |
| `/Metadata/ExternalIdInfos` | POST | ❌ `jfSessionStub` |
| `/Metadata/{itemId}` | GET, PUT, DELETE | ❌ `jfSessionStub` |
| `/Metadata/{itemId}/ExternalIdInfo` | POST | ❌ `jfSessionStub` |

## Decision

**Implement only metadata crawling (POST).** Don't implement metadata storage.

### Implementation

```go
// Metadata crawling endpoint - IMPLEMENTED
func (s *Server) jfCrawlMetadata(w http.ResponseWriter, r *http.Request) {
    // Crawl metadata from external sources
    // Store in memory
    // Return success
    w.WriteHeader(http.StatusNoContent)
}

// Metadata storage endpoints - STUB
func (s *Server) jfGetMetadata(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Success": false,
        "Error":   "Metadata storage not supported",
    })
}

func (s *Server) jfUpdateMetadata(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Success": false,
        "Error":   "Metadata update not supported",
    })
}

func (s *Server) jfDeleteMetadata(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}
```

### Metadata Crawling

```go
// Crawl metadata from TMDB, TV Maze, etc.
func (s *Server) jfCrawlMetadata(w http.ResponseWriter, r *http.Request) {
    paths := r.FormValue("paths")
    
    for _, path := range strings.Split(paths, "\n") {
        // Crawl metadata for each path
        s.crawlMetadata(path)
    }
    
    w.WriteHeader(http.StatusNoContent)
}
```

### Metadata Storage

Metadata is stored in memory, not persisted:

```go
type MetadataStore struct {
    items map[string]*MetadataItem
}

func (m *MetadataStore) Get(itemId string) *MetadataItem {
    if item, ok := m.items[itemId]; ok {
        return item
    }
    return nil
}

func (m *MetadataStore) Set(itemId string, item *MetadataItem) {
    m.items[itemId] = item
}
```

## Consequences

### Positive

- **Simple**: Only metadata crawling
- **Low memory**: Metadata in memory only
- **Low disk usage**: No metadata files
- **Compatible**: Clients can crawl metadata
- **Fast**: In-memory access

### Negative

- **No metadata persistence**: Metadata lost on restart
- **No metadata storage**: Can't store custom metadata
- **Memory usage**: Metadata uses RAM
- **Limited functionality**: Metadata features don't work fully

### Client Behavior

Clients should:
1. Use metadata crawling to get metadata
2. Handle "storage not supported" errors
3. Re-crawl metadata after restart
4. Cache metadata locally if possible

### Future Considerations

- If users request metadata persistence, implement it
- Add metadata caching
- Add metadata update handling
- Consider database for metadata storage

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [docs/advanced/architecture.md](../../docs/advanced/architecture.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
