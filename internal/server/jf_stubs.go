package server

import (
	"net/http"

)

func (s *Server) jfSessionStub(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfSessionsStub(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfMediaSegments(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items": []interface{}{},
	})
}

func (s *Server) jfThemeMedia(w http.ResponseWriter, r *http.Request) {
	empty := map[string]interface{}{
		"Items":            []interface{}{},
		"StartIndex":       0,
		"TotalRecordCount": 0,
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ThemeSongsResult":      empty,
		"ThemeVideosResult":     empty,
		"SoundtrackSongsResult": empty,
	})
}

func (s *Server) jfEmptyArray(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfDisplayPrefsStub(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Id":               r.PathValue("displayPrefsId"),
		"SortBy":           "SortName",
		"SortOrder":        "Ascending",
		"RememberIndexing": false,
		"RememberSorting":  false,
		"CustomPrefs":      map[string]string{},
	})
}

// defaultUserData returns a stub UserData object that Jellyfin clients expect
// on every item DTO. Without it clients like Findroid hang on the detail view.
func defaultUserData(itemID string) map[string]interface{} {
	return map[string]interface{}{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
		"Key":                   itemID,
		"ItemId":                itemID,
		"UnplayedItemCount":     0,
	}
}
