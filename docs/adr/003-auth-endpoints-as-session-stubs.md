# ADR-003: Authentication-Only Endpoints as Session Stubs

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

RMS doesn't implement user authentication or session management. However, the Jellyfin API has many endpoints that require authentication. These include:

- User management (`/Users`, `/Users/{userId}`)
- Session management (`/Sessions`, `/Sessions/{sessionId}`)
- Protected item operations (`/Users/{userId}/Items/{itemId}`)
- Password reset (`/Users/Password`)
- Configuration changes (`/System/Configuration`)

Implementing proper authentication would require:
- User database
- Password hashing
- Session management
- Token generation and validation
- Access control

This would add significant complexity and memory usage, which contradicts RMS's design goals.

## Decision

All authentication-required endpoints return **session stubs** (`jfSessionStub`).

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
  "IsAdministrator": false
}
```

### Endpoints Using Session Stubs

| Route | Method | Purpose |
|-------|--------|---------|
| `/System/Configuration` | POST | User configuration |
| `/System/Configuration/Branding` | POST | Branding configuration |
| `/System/Configuration/{key}` | POST | Key configuration |
| `/System/Restart` | POST | System restart |
| `/System/Shutdown` | POST | System shutdown |
| `/Users/{userId}` | DELETE | Delete user |
| `/Users/{userId}/Policy` | POST | Update user policy |
| `/Users/Configuration` | POST | User configuration |
| `/Users/Password` | POST | Change password |
| `/Items/{itemId}/Images/{imageType}` | POST, DELETE | Image operations |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}` | POST, DELETE | Image operations |
| `/Items/{itemId}/Images/{imageType}/{imageIndex}/Index` | POST | Set image index |
| `/Items/{itemId}` | POST, DELETE | Item operations |
| `/Items` | DELETE | Delete items |
| `/Videos/{itemId}/Subtitles` | POST | Add subtitles |
| `/Videos/{itemId}/Subtitles/{index}` | DELETE | Delete subtitle |
| `/Videos/{itemId}/AlternateSources` | DELETE | Delete source |
| `/Videos/MergeVersions` | POST | Merge versions |
| `/Audio/{itemId}/Lyrics` | POST, DELETE | Lyrics operations |
| `/Audio/{itemId}/RemoteSearch/Lyrics/{lyricId}` | POST | Add lyric |
| `/Sessions/{sessionId}/Command` | POST | Session command |
| `/Sessions/{sessionId}/Command/{command}` | POST | Session command |
| `/Sessions/{sessionId}/Message` | POST | Send message |
| `/Sessions/{sessionId}/Playing` | POST | Session playing |
| `/Sessions/{sessionId}/Playing/{command}` | POST | Playing command |
| `/Sessions/{sessionId}/System/{command}` | POST | System command |
| `/Sessions/{sessionId}/User/{userId}` | POST, DELETE | User operations |
| `/Sessions/{sessionId}/Viewing` | POST | Session viewing |
| `/Sessions/Viewing` | POST | Session viewing |
| `/Sessions/Capabilities` | POST | Session capabilities |
| `/Sessions/Capabilities/Full` | POST | Full capabilities |
| `/Sessions/Playing/Ping` | POST | Ping session |
| `/Sessions/Logout` | DELETE, POST | Logout |
| `/ClientLog/Document` | POST | Log document |
| `/UserItems/{itemId}/Rating` | POST, DELETE | Rating operations |
| `/Devices` | DELETE | Delete devices |
| `/Devices/Options` | POST | Device options |
| `/Auth/Keys/{key}` | DELETE | Delete auth key |
| `/Startup/Complete` | POST | Startup complete |
| `/Startup/Configuration` | POST | Startup configuration |
| `/Startup/RemoteAccess` | POST | Remote access |
| `/Startup/User` | POST | Startup user |
| `/Library/Media/Updated` | POST | Media updated |
| `/Library/Movies/Added` | POST | Movies added |
| `/Library/Movies/Updated` | POST | Movies updated |
| `/Library/Series/Added` | POST | Series added |
| `/Library/Series/Updated` | POST | Series updated |
| `/Library/VirtualFolders` | POST, DELETE | Virtual folders |
| `/Library/VirtualFolders/LibraryOptions` | POST | Options |
| `/Library/VirtualFolders/Name` | POST | Set name |
| `/Library/VirtualFolders/Paths` | POST, DELETE | Paths |
| `/Library/VirtualFolders/Paths/Update` | POST | Update paths |
| `/ScheduledTasks/{taskId}/Triggers` | POST | Task triggers |
| `/ScheduledTasks/Running/{taskId}` | DELETE | Delete running task |
| `/Packages/Installed/{name}` | POST | Install package |
| `/Packages/Installing/{packageId}` | DELETE | Uninstall package |
| `/Plugins/{pluginId}` | DELETE | Delete plugin |
| `/Plugins/{pluginId}/{version}` | DELETE | Delete plugin |
| `/Plugins/{pluginId}/{version}/Disable` | POST | Disable plugin |
| `/Plugins/{pluginId}/{version}/Enable` | POST | Enable plugin |
| `/Plugins/{pluginId}/{version}/Image` | GET | Plugin image |
| `/Plugins/{pluginId}/Configuration` | POST | Save config |
| `/Plugins/{pluginId}/Manifest` | POST | Save manifest |

## Consequences

### Positive

- **Simple implementation**: Just return stub response
- **No authentication needed**: Clients can make authenticated requests
- **No session management**: No state to maintain
- **Low memory usage**: No session data to store

### Negative

- **Limited functionality**: Many features not available
- **No user management**: Single-user design
- **No password reset**: Users can't change passwords
- **No session management**: Sessions are ephemeral

### Client Behavior

Clients should:
1. Make unauthenticated request to `/Users/Public`
2. Get user list
3. Make authenticated request with user token
4. Handle stub responses gracefully

### Future Considerations

- If users request authentication, implement proper auth
- Consider adding basic auth for simple cases
- Document stub behavior in API documentation

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
