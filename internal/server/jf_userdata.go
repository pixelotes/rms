package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

// UserDataStore holds per-user item state in memory.
// When filePath is set, data is persisted to a JSON file on disk.
type UserDataStore struct {
	mu            sync.RWMutex
	data          map[string]map[string]*UserDataEntry
	filePath      string
	flushInterval time.Duration
	dirty         bool
	stopCh        chan struct{}
}

type UserDataEntry struct {
	PlaybackPositionTicks int64  `json:"position_ticks"`
	PlayCount             int    `json:"play_count"`
	IsFavorite            bool   `json:"is_favorite"`
	Played                bool   `json:"played"`
	LastPlayedDate        string `json:"last_played_date,omitempty"`
}

// NewUserDataStore creates a new store. If filePath is non-empty, data is
// loaded from disk on startup and flushed periodically + on shutdown.
func NewUserDataStore(filePath string, flushMinutes int) *UserDataStore {
	interval := time.Duration(flushMinutes) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	store := &UserDataStore{
		data:          make(map[string]map[string]*UserDataEntry),
		filePath:      filePath,
		flushInterval: interval,
		stopCh:        make(chan struct{}),
	}
	if filePath != "" {
		store.load()
		go store.flushLoop()
	}
	return store
}

// Shutdown flushes pending data to disk and stops the background loop.
func (store *UserDataStore) Shutdown() {
	if store.filePath == "" {
		return
	}
	close(store.stopCh)
	store.flush()
}

func (store *UserDataStore) Get(userID, itemID string) map[string]interface{} {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.toDTO(itemID, store.getEntry(userID, itemID))
}

func (store *UserDataStore) getEntry(userID, itemID string) *UserDataEntry {
	if items, ok := store.data[userID]; ok {
		if e, ok := items[itemID]; ok {
			return e
		}
	}
	return nil
}

func (store *UserDataStore) toDTO(itemID string, entry *UserDataEntry) map[string]interface{} {
	dto := map[string]interface{}{
		"PlaybackPositionTicks": int64(0),
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
		"Key":                   itemID,
		"ItemId":                itemID,
		"UnplayedItemCount":     0,
	}
	if entry != nil {
		dto["PlaybackPositionTicks"] = entry.PlaybackPositionTicks
		dto["PlayCount"] = entry.PlayCount
		dto["IsFavorite"] = entry.IsFavorite
		dto["Played"] = entry.Played
		if entry.LastPlayedDate != "" {
			dto["LastPlayedDate"] = entry.LastPlayedDate
		}
	}
	return dto
}

func (store *UserDataStore) SetPosition(userID, itemID string, ticks int64) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.ensureEntry(userID, itemID)
	entry.PlaybackPositionTicks = ticks
	entry.LastPlayedDate = time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z")
	store.dirty = true
}

func (store *UserDataStore) SetPlayed(userID, itemID string, played bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.ensureEntry(userID, itemID)
	entry.Played = played
	if played {
		entry.PlayCount++
		entry.PlaybackPositionTicks = 0
	}
	entry.LastPlayedDate = time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z")
	store.dirty = true
}

func (store *UserDataStore) SetFavorite(userID, itemID string, favorite bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.ensureEntry(userID, itemID)
	entry.IsFavorite = favorite
	store.dirty = true
}

func (store *UserDataStore) ensureEntry(userID, itemID string) *UserDataEntry {
	if _, ok := store.data[userID]; !ok {
		store.data[userID] = make(map[string]*UserDataEntry)
	}
	if _, ok := store.data[userID][itemID]; !ok {
		store.data[userID][itemID] = &UserDataEntry{}
	}
	return store.data[userID][itemID]
}

// --- Persistence ---

func (store *UserDataStore) load() {
	f, err := os.Open(store.filePath)
	if err != nil {
		return // file doesn't exist yet, start fresh
	}
	defer f.Close()

	var data map[string]map[string]*UserDataEntry
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		log.Printf("Warning: failed to parse userdata file %s: %v", store.filePath, err)
		return
	}
	store.data = data
	log.Printf("Loaded userdata from %s", store.filePath)
}

func (store *UserDataStore) flush() {
	store.mu.RLock()
	if !store.dirty {
		store.mu.RUnlock()
		return
	}
	// Snapshot the data under read lock
	raw, err := json.Marshal(store.data)
	store.mu.RUnlock()
	if err != nil {
		log.Printf("Warning: failed to marshal userdata: %v", err)
		return
	}

	// Write to temp file then rename for atomicity
	tmp := store.filePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		log.Printf("Warning: failed to write userdata file: %v", err)
		return
	}
	if err := os.Rename(tmp, store.filePath); err != nil {
		log.Printf("Warning: failed to rename userdata file: %v", err)
		return
	}

	store.mu.Lock()
	store.dirty = false
	store.mu.Unlock()
}

func (store *UserDataStore) flushLoop() {
	ticker := time.NewTicker(store.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			store.flush()
		case <-store.stopCh:
			return
		}
	}
}

func (store *UserDataStore) IsResumable(userID, itemID string) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	entry := store.getEntry(userID, itemID)
	return entry != nil && entry.PlaybackPositionTicks > 0 && !entry.Played
}

func (store *UserDataStore) IsFavorite(userID, itemID string) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	entry := store.getEntry(userID, itemID)
	return entry != nil && entry.IsFavorite
}

type resumableItem struct {
	ItemID         string
	PositionTicks  int64
	LastPlayedDate string
}

func (store *UserDataStore) GetResumable(userID string) []resumableItem {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var result []resumableItem
	if items, ok := store.data[userID]; ok {
		for itemID, entry := range items {
			if entry.PlaybackPositionTicks > 0 && !entry.Played {
				result = append(result, resumableItem{
					ItemID:         itemID,
					PositionTicks:  entry.PlaybackPositionTicks,
					LastPlayedDate: entry.LastPlayedDate,
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastPlayedDate > result[j].LastPlayedDate
	})
	return result
}

// --- HTTP Handlers ---

func (s *Server) jfReportPlayback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemId        string `json:"ItemId"`
		PositionTicks int64  `json:"PositionTicks"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ItemId == "" {
		req.ItemId = mux.Vars(r)["itemId"]
	}
	if req.ItemId != "" {
		userID := stableUserID(usernameFromContext(r))
		s.userData.SetPosition(userID, req.ItemId, req.PositionTicks)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfReportPlaybackStopped(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemId        string `json:"ItemId"`
		PositionTicks int64  `json:"PositionTicks"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ItemId != "" {
		userID := stableUserID(usernameFromContext(r))
		s.userData.SetPosition(userID, req.ItemId, req.PositionTicks)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfTogglePlayed(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]
	userID := stableUserID(usernameFromContext(r))
	played := r.Method == "POST"
	s.userData.SetPlayed(userID, itemID, played)
	respondJSON(w, http.StatusOK, s.userData.Get(userID, itemID))
}

func (s *Server) jfToggleFavorite(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]
	userID := stableUserID(usernameFromContext(r))
	favorite := r.Method == "POST"
	s.userData.SetFavorite(userID, itemID, favorite)
	respondJSON(w, http.StatusOK, s.userData.Get(userID, itemID))
}

func (s *Server) jfUpdateUserData(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]
	userID := stableUserID(usernameFromContext(r))

	var req struct {
		PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
		PlayCount             int   `json:"PlayCount"`
		IsFavorite            bool  `json:"IsFavorite"`
		Played                bool  `json:"Played"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	s.userData.SetPosition(userID, itemID, req.PlaybackPositionTicks)
	if req.Played {
		s.userData.SetPlayed(userID, itemID, true)
	}
	s.userData.SetFavorite(userID, itemID, req.IsFavorite)

	respondJSON(w, http.StatusOK, s.userData.Get(userID, itemID))
}

func (s *Server) jfUserData(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]
	userID := stableUserID(usernameFromContext(r))
	respondJSON(w, http.StatusOK, s.userData.Get(userID, itemID))
}

func (s *Server) jfGetResumeItems(w http.ResponseWriter, r *http.Request) {
	userID := stableUserID(usernameFromContext(r))
	libs := s.librariesForRequest(r)
	limit := 12
	if l, _ := strconv.Atoi(queryParam(r, "limit", "Limit")); l > 0 {
		limit = l
	}

	resumable := s.userData.GetResumable(userID)
	items := make([]map[string]interface{}, 0)

	for _, entry := range resumable {
		if len(items) >= limit {
			break
		}
		path, err := media.ItemPath(entry.ItemID)
		if err != nil {
			continue
		}
		if !media.IsPathAllowed(path, libs) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		libIdx := findLibraryIndex(path, libs)
		item := s.buildJellyfinItem(path, info, libIdx, libs)
		item["UserData"] = s.userData.Get(userID, entry.ItemID)
		items = append(items, item)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"StartIndex":       0,
		"TotalRecordCount": len(items),
	})
}

func (s *Server) patchItemsUserData(items []map[string]interface{}, r *http.Request) {
	userID := stableUserID(usernameFromContext(r))
	for _, item := range items {
		if id, ok := item["Id"].(string); ok {
			item["UserData"] = s.userData.Get(userID, id)
		}
	}
}
