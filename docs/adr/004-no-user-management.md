# ADR-004: No User Management

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

RMS is designed as a lightweight, filesystem-based media server. Implementing user management would require:

- User database (SQLite or similar)
- Password hashing and storage
- User creation and deletion
- Password reset
- User permissions and policies
- Session management

This would add significant complexity and memory usage, which contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **No database** (filesystem-based storage)
- **Simple architecture**

Additionally, 10+ user management endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/Users/Public` | GET | ✅ Implemented |
| `/Users/AuthenticateByName` | POST | ✅ Implemented |
| `/Users/{userId}` | DELETE | ❌ `jfSessionStub` |
| `/Users/{userId}/Policy` | POST | ❌ `jfSessionStub` |
| `/Users/Configuration` | POST | ❌ `jfSessionStub` |
| `/Users/Password` | POST | ❌ `jfSessionStub` |
| `/Startup/User` | POST | ❌ `jfSessionStub` |

## Decision

**Don't implement user management.** Use a **single hardcoded user**:

```go
// Hardcoded user
const defaultUsername = "rms"
const defaultPassword = "" // No password required

// Default policies
type DefaultPolicies struct {
    EnableRemoteContentDownload: false
    EnableContentScanning:       false
    EnableLiveTvAccess:          false
    EnableMediaPlayback:         true
    EnablePlaybackRecording:     false
    EnableContentSharing:        false
}
```

### Implementation

```go
// In server.go
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Name":     "rms",
        "Username": "rms",
        "Policies": map[string]bool{
            "EnableRemoteContentDownload": false,
            "EnableContentScanning":       false,
            "EnableLiveTvAccess":          false,
            "EnableMediaPlayback":         true,
            "EnablePlaybackRecording":     false,
            "EnableContentSharing":        false,
        },
    })
}
```

### Stub Responses

For endpoints that require user management, return stubs:

```go
// /Users/{userId} DELETE
func (s *Server) jfDeleteUser(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNoContent)
}

// /Users/{userId}/Policy POST
func (s *Server) jfUpdateUserPolicy(w http.ResponseWriter, r *http.Request) {
    // Ignore policy changes
    w.WriteHeader(http.StatusNoContent)
}

// /Users/Password POST
func (s *Server) jfChangePassword(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "Success": false,
        "Error":   "Password change not supported",
    })
}
```

## Consequences

### Positive

- **Simple**: No user management code
- **Low memory**: No user data to store
- **Fast**: No user lookup or validation
- **Compatible**: Works with Jellyfin clients

### Negative

- **Single user only**: Can't have multiple users
- **No password protection**: Anyone can access
- **No user-specific data**: All users see same data
- **Limited features**: Features requiring user management don't work

### Workarounds

Clients can work around limitations:

1. **Single user**: Configure client to use one user
2. **No password**: Use unauthenticated requests where possible
3. **Shared data**: All users see same library data

### Future Considerations

- If users request multi-user support, implement user management
- Consider adding basic auth for security
- Add user data persistence if needed

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [ADR-003: Authentication-Only Endpoints as Session Stubs](./003-auth-endpoints-as-session-stubs.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
