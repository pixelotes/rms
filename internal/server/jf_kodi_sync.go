package server

import (
	"net/http"
)

// Emulates the Jellyfin.Plugin.KodiSyncQueue server plugin.
// The Kodi Jellyfin addon checks for this plugin on connect and warns
// if it's missing. These endpoints satisfy that check and return empty
// deltas so Kodi falls back to full library scans.
//
// Enable with `kodi_sync_queue: true` in the app config.

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
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ItemsAdded":      []interface{}{},
		"ItemsUpdated":    []interface{}{},
		"ItemsRemoved":    []interface{}{},
		"UserDataChanged": []interface{}{},
	})
}
