# ADR-007: No Database - Filesystem-Based Storage

**Status:** ✅ Accepted  
**Date:** 2026-06-25  
**Author:** little-coder

## Context

Jellyfin uses SQLite for data storage, but RMS uses filesystem-based JSON storage. Database features include:

- User data
- Library data
- Item data
- Play history
- User preferences
- System configuration
- Scheduled tasks
- Live TV data
- Plugin data

Implementing a database would require:

- SQLite or similar database
- Connection management
- Query execution
- Index management
- Backup/restore
- Migration scripts
- Significant memory and complexity

This contradicts RMS's design goals:

- **~23x less memory than Jellyfin**
- **Filesystem-based storage**
- **Simple architecture**
- **Easy to deploy** (just copy files)

Database-dependent endpoints are stubs:

| Route | Method | Status |
|-------|--------|--------|
| `/System/ActivityLog/Entries` | GET | ❌ `jfEmptyItems` |
| `/System/Logs` | GET | ❌ `jfEmptyArray` |
| `/System/Logs/Log` | GET | ❌ `jfNotFound` |
| `/System/Configuration` | POST | ❌ `jfSessionStub` |
| `/System/Configuration/{key}` | POST | ❌ `jfSessionStub` |
| `/System/Restart` | POST | ❌ `jfSessionStub` |
| `/System/Shutdown` | POST | ❌ `jfSessionStub` |
| `/ScheduledTasks/{taskId}/Triggers` | POST | ❌ `jfSessionStub` |
| `/ScheduledTasks/Running/{taskId}` | DELETE | ❌ `jfSessionStub` |
| `/Packages/Installed/{name}` | POST | ❌ `jfSessionStub` |
| `/Packages/Installing/{packageId}` | DELETE | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}` | DELETE | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}` | DELETE | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Disable` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Enable` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/{version}/Image` | GET | ❌ `jfNotFound` |
| `/Plugins/{pluginId}/Configuration` | GET | ❌ `jfEmptyObject` |
| `/Plugins/{pluginId}/Configuration` | POST | ❌ `jfSessionStub` |
| `/Plugins/{pluginId}/Manifest` | POST | ❌ `jfSessionStub` |

## Decision

**Use filesystem-based JSON storage.** Don't implement a database.

### Implementation

```go
// Store data as JSON files
const dataDir = "/path/to/rms/data"

// User data
{
  "username": "rms",
  "policies": {
    "EnableRemoteContentDownload": false,
    "EnableContentScanning": false,
    "EnableMediaPlayback": true
  }
}

// Library data
{
  "id": "library-1",
  "name": "Movies",
  "path": "/path/to/movies",
  "contentType": "movie"
}

// Item data
{
  "id": "item-1",
  "path": "/path/to/movie.mp4",
  "name": "Movie Name",
  "type": "movie",
  "libId": "library-1"
}
```

### File Structure

```
rms/
├── data/
│   ├── users.json
│   ├── libraries.json
│   ├── items.json
│   └── config.json
├── metadata/
│   └── (crawled metadata files)
└── logs/
    └── (log files)
```

## Consequences

### Positive

- **Simple**: Just JSON files
- **Low memory**: No database in memory
- **~23x less memory than Jellyfin**
- **Easy to deploy**: Just copy files
- **Easy to backup**: Just copy directory
- **No dependencies**: No SQLite required

### Negative

- **No concurrent access**: Single-user design
- **No complex queries**: Limited data access
- **No indexes**: Slower searches
- **No transactions**: Data integrity issues
- **No migrations**: Schema changes are hard
- **No relationships**: Harder to join data

### Workarounds

- **Single user**: Avoid concurrent access
- **Simple queries**: Use file-based search
- **Manual backups**: Copy data directory
- **Manual schema changes**: Update JSON files

### Future Considerations

- If users request database support, implement SQLite
- Add file locking for concurrent access
- Add data validation
- Add migration scripts

## References

- [ADR-001: Jellyfin API Compatibility Strategy](./001-jellyfin-api-compatibility.md)
- [ADR-002: Stub Strategy for Unimplemented Features](./002-stub-strategy.md)
- [docs/advanced/architecture.md](../../docs/advanced/architecture.md)
- [jf_compat.go](../../internal/server/jf_compat.go)
- [server.go](../../internal/server/server.go)
