package server

import (
	"net/http"
	"sync"
	"time"
)

// Emulates the Jellyfin.Plugin.KodiSyncQueue server plugin.
// The Kodi Jellyfin addon checks for this plugin on connect and warns
// if it's missing. When kodi_sync_queue is enabled, RMS tracks item
// additions during rescans so Kodi can do incremental syncs instead
// of full library scans.

type syncChange struct {
	ItemID    string
	Timestamp time.Time
}

// SyncQueueStore tracks library changes with timestamps for Kodi's
// incremental sync.
type SyncQueueStore struct {
	mu      sync.RWMutex
	added   []syncChange
	removed []syncChange
}

func NewSyncQueueStore() *SyncQueueStore {
	return &SyncQueueStore{}
}

func (q *SyncQueueStore) RecordAdded(ids []string) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range ids {
		q.added = append(q.added, syncChange{ItemID: id, Timestamp: now})
	}
}

func (q *SyncQueueStore) RecordRemoved(ids []string) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range ids {
		q.removed = append(q.removed, syncChange{ItemID: id, Timestamp: now})
	}
}

func (q *SyncQueueStore) Since(t time.Time) (added, removed []string) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	for _, c := range q.added {
		if c.Timestamp.After(t) {
			added = append(added, c.ItemID)
		}
	}
	for _, c := range q.removed {
		if c.Timestamp.After(t) {
			removed = append(removed, c.ItemID)
		}
	}
	return
}

func (s *Server) jfKodiSyncSettings(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"IsEnabled":              true,
		"RetentionDays":          365,
		"TrackUserDataChanges":   true,
		"TrackFolderChanges":     true,
		"TrackOnlyFolderChanges": false,
	})
}

func (s *Server) jfKodiSyncGetItems(w http.ResponseWriter, r *http.Request) {
	lastUpdate := r.URL.Query().Get("LastUpdateDT")
	since := time.Time{}
	if lastUpdate != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, lastUpdate); err == nil {
				since = t
				break
			}
		}
	}

	added, removed := s.syncQueue.Since(since)
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ItemsAdded":      added,
		"ItemsUpdated":    []interface{}{},
		"ItemsRemoved":    removed,
		"UserDataChanged": []interface{}{},
	})
}
