# ADR-008: Minimal Session Support

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin has a full session management system, but RMS doesn't implement it. Session features include:

- Session creation
- Session deletion
- Session commands
- Session viewing
- Session capabilities
- Session messages
- Session user operations

Implementing session management would require:

- Session storage (database or filesystem)
- Session creation logic
- Session deletion logic
- Session command handling
- Session timeout handling
- Significant memory and complexity

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**

Session endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/System/Configuration` | POST | ❌ `jfSessionStub` |
| `/System/Configuration/Branding` | POST | ❌ `jfSessionStub` |
| `/System/Configuration/{key}` | POST | ❌ `jfSessionStub` |
| `/System/Restart` | POST | ❌ `jfSessionStub` |
| `/System/Shutdown` | POST | ❌ `jfSessionStub` |
| `/Users/{userId}` | DELETE | ❌ `jfSessionStub` |
| `/Users/{userId}/Policy` | POST | ❌ `jfSessionStub` |
| `/Items/{itemId}/Images/{imageType}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}/Index` | POST | ❌ `jfSessionStub` |
| `/Items/{itemId}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Items` | DELETE | ❌ `jfSessionStub` |
| `/Videos/{itemId}/Subtitles` | POST | ❌ `jfSessionStub` |
| `/Videos/{itemId}/Subtitles/{index}` | DELETE | ❌ `jfSessionStub` |
| `/Videos/{itemId}/AlternateSources` | DELETE | ❌ `jfSessionStub` |
| `/Videos/MergeVersions` | POST | ❌ `jfSessionStub` |
| `/Audio/{itemId}/Lyrics` | POST, DELETE | ❌ `jfSessionStub` |
| `/Audio/{itemId}/RemoteSearch/Lyrics/{lyricId}` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Command` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Command/{command}` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Message` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Playing` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Playing/{command}` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/System/{command}` | POST | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/User/{userId}` | POST, DELETE | ❌ `jfSessionStub` |
| `/Sessions/{sessionId}/Viewing` | POST | ❌ `jfSessionStub` |
| `/Sessions/Viewing` | POST | ❌ `jfSessionStub` |
| `/Sessions/Capabilities` | POST | ❌ `jfSessionStub` |
| `/Sessions/Capabilities/Full` | POST | ❌ `jfSessionStub` |
| `/Sessions/Playing/Ping` | POST | ❌ `jfSessionStub` |
| `/Sessions/Logout` | DELETE, POST | ❌ `jfSessionStub` |
| `/ClientLog/Document` | POST | ❌ `jfSessionStub` |
| `/UserItems/{itemId}/Rating` | POST, DELETE | ❌ `jfSessionStub` |

## Decision

**Return session stubs for all auth-required endpoints.** Don't implement actual session management.

### Stub Response Format

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

### Implementation

```go
// Session stub ID generator
func stableID(name string) string {
    return fmt.Sprintf("rms-%s", name)
}

// Session stub handler
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

## Consequences

### Positive

- **Simple**: Just return stub response
- **Low memory**: No session data to store
- **No session management**: No session state
- **Compatible**: Clients get consistent responses
- **Ephemeral**: Sessions don't persist

### Negative

- **No session persistence**: Sessions reset on restart
- **No session management**: Can't manage sessions
- **Limited functionality**: Features requiring sessions don't work
- **Client confusion**: Clients might expect real sessions

### Client Behavior

Clients should:
1. Make unauthenticated request
2. Get session token from response
3. Use session token for authenticated requests
4. Handle stub session responses gracefully

### Future Considerations

- If users request session management, implement it
- Add session persistence (filesystem or database)
- Add session timeout handling
- Add session command handling

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [ADR-003: Authentication-Only Endpoints as Session Stubs](./003-auth-endpoints-as-session-stubs.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
