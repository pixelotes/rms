# ADR-009: Image Fetching Only

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin has image management features, but RMS only implements image fetching. Image features include:

- Image fetching (GET)
- Image upload (POST)
- Image deletion (DELETE)
- Image index setting (POST)
- Image operations

Implementing image management would require:

- Image storage (filesystem or database)
- Image upload handling
- Image deletion handling
- Image thumbnail generation
- Significant disk usage and complexity

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**
- **No database**

Image management endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/Items/{itemId}/Images/{imageType}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}/Index` | POST | ❌ `jfSessionStub` |
| `/Items/{itemId}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items` | DELETE | ❌ `jfSessionStub` |

## Decision

**Implement only image fetching (GET).** Don't implement image management (POST/DELETE).

### Implementation

```go
// Image fetching endpoint - IMPLEMENTED
func (s *Server) jfGetItemImage(w http.ResponseWriter, r *http.Request) {
    // Fetch image from filesystem
    // Return image data
}

// Image upload endpoint - STUB
func (s *Server) jfUploadItemImage(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Success": false,
        "Error":   "Image upload not supported",
    })
}

// Image deletion endpoint - STUB
func (s *Server) jfDeleteItemImage(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

// Image index setting - STUB
func (s *Server) jfSetItemImageIndex(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Success": false,
        "Error":   "Image index setting not supported",
    })
}
```

### Image Storage

Images are stored in the filesystem alongside media files:

```
/path/to/media/
├── movie.mp4
└── .thumb/
    ├── Primary 85x28
    ├── Primary 32x16
    └── Poster 260x167
```

### Image Fetching

```go
func (s *Server) jfGetItemImage(w http.ResponseWriter, r *http.Request) {
    itemId := mux.Vars(r)["itemId"]
    imageType := mux.Vars(r)["imageType"]
    imageIndex := mux.Vars(r)["imageIndex"]
    
    // Find image path
    imagePath := s.findImagePath(itemId, imageType, imageIndex)
    
    // Return image
    w.Header().Set("Content-Type", "image/jpeg")
    w.Header().Set("Content-Disposition", `inline; filename="poster.jpg"`)
    w.WriteHeader(http.StatusOK)
    
    f, err := os.Open(imagePath)
    if err != nil {
        w.WriteHeader(http.StatusNotFound)
        return
    }
    io.Copy(w, f)
    f.Close()
}
```

## Consequences

### Positive

- **Simple**: Only image fetching
- **Low disk usage**: No image storage
- **Low memory**: No image data in memory
- **Compatible**: Clients can fetch images
- **Fast**: Direct filesystem access

### Negative

- **No image upload**: Can't add custom images
- **No image deletion**: Can't remove images
- **No thumbnails**: No automatic thumbnail generation
- **Limited functionality**: Image management features don't work

### Client Behavior

Clients should:
1. Use image fetching for standard images
2. Handle "upload not supported" errors
3. Generate thumbnails manually if needed
4. Use cached images when available

### Future Considerations

- If users request image upload, implement it
- Add image thumbnail generation
- Add image caching
- Consider image compression

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
