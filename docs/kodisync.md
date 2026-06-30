# Kodi Sync Queue Implementation

This document describes the implementation and logic of the Kodi Sync Queue emulation in RMS.

## Purpose
The Kodi Sync Queue emulates the Jellyfin `KodiSyncQueue` server plugin. Its goal is to allow Kodi clients to perform **incremental library updates** instead of performing full library scans whenever they connect. This reduces network traffic and server load.

## Core Components

### `SyncQueueStore` (`internal/server/jf_kodi_sync.go`)
The store is an in-memory structure that tracks library modifications.

*   **Data Structure**:
    *   `added []syncChange`: A chronological list of items added to the library.
    *   `removed []syncChange`: A chronological list of items removed from the library.
    *   `syncChange`: A struct containing the `ItemID` and a `Timestamp` (UTC).

*   **Operations**:
    *   `RecordAdded(ids []string)`: Appends multiple item IDs to the `added` slice, each with the current UTC timestamp.
    *   `RecordRemoved(ids []string)`: Appends items to the `removed` slice.
    *   `Since(t time.Time)`: Filters the `added` and `removed` slices, returning only those items whose `Timestamp` is strictly after the provided time `t`.

### Server Integration (`internal/server/server.go`)
*   **Initialization**: The `SyncQueueStore` is initialized during server startup in `New()`.
*   **Boot Behavior**: 
    *   The initial population of the item index during startup is **not** recorded in the sync queue.
    *   Upon the first connection, Kodi receives an empty queue and defaults to a full library scan.
*   **Incremental Sync Lifecycle**:
    1.  The client (Kodi) connects and provides its last successful sync timestamp.
    2.  The server calls `Since(timestamp)` on the `SyncQueueStore`.
    3.  The server returns the list of items added or removed since that time.
    4.  If no changes are found, the client performs no updates.

## Expected Behavior for New Content
For a new series to be synced to Kodi:
1.  The series must be identified as a "new addition" during a library scan or refresh operation.
2.  The server's library scanning logic **MUST** explicitly call `syncQueue.RecordAdded([]string{seriesID})`.
3.  The timestamp of this addition must be later than the client's last sync timestamp.
