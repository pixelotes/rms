package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"raspberry-media-server/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

const serverName = "Raspberry Media Server"

// Deterministic server ID
const serverID = "d3adb33f-cafe-babe-f00d-deadbeef1234"

// --- System Endpoints ---

func (s *Server) jfSystemInfoPublic(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"LocalAddress":            fmt.Sprintf("http://localhost:%d", s.config.App.Port),
		"ServerName":              serverName,
		"Version":                 s.config.App.JellyfinVersion,
		"ProductName":             "Jellyfin Server",
		"Id":                      serverID,
		"StartupWizardCompleted":  true,
		"OperatingSystem":         "Linux",
		"OperatingSystemDisplayName": "Linux",
	})
}

func (s *Server) jfSystemInfo(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"LocalAddress":            fmt.Sprintf("http://localhost:%d", s.config.App.Port),
		"ServerName":              serverName,
		"Version":                 s.config.App.JellyfinVersion,
		"ProductName":                "Jellyfin Server",
		"Id":                        serverID,
		"StartupWizardCompleted":    true,
		"OperatingSystem":           "Linux",
		"OperatingSystemDisplayName": "Linux",
		"HasPendingRestart":         false,
		"HasUpdateAvailable":      false,
		"SupportsLibraryMonitor":  false,
		"CanSelfRestart":          false,
		"CanLaunchWebBrowser":     false,
	})
}

func (s *Server) jfBrandingConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"LoginDisclaimer":  "",
		"CustomCss":        "",
		"SplashscreenEnabled": false,
	})
}

// --- Auth Endpoints ---

func (s *Server) jfUsersPublic(w http.ResponseWriter, r *http.Request) {
	var users []map[string]interface{}
	if len(s.config.Users) == 0 {
		users = append(users, map[string]interface{}{
			"Name":                  "rms",
			"Id":                    stableUserID("rms"),
			"HasPassword":           true,
			"HasConfiguredPassword": true,
		})
	} else {
		for _, u := range s.config.Users {
			users = append(users, map[string]interface{}{
				"Name":                  u.Username,
				"Id":                    stableUserID(u.Username),
				"HasPassword":           true,
				"HasConfiguredPassword": true,
			})
		}
	}
	respondJSON(w, http.StatusOK, users)
}

func (s *Server) jfAuthenticateByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	username := req.Username
	if username == "" {
		username = "rms"
	}

	pw := req.Pw
	if pw == "" {
		pw = req.Password
	}

	user := s.config.AuthenticateUser(username, pw)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	username = user.Username

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":  time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
		"user": username,
	})
	tokenString, err := token.SignedString([]byte(s.config.App.JWTSecret))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	userID := stableUserID(username)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"User": map[string]interface{}{
			"Name":                  username,
			"Id":                    userID,
			"HasPassword":           true,
			"HasConfiguredPassword": true,
			"Policy": map[string]interface{}{
				"IsAdministrator": true,
				"IsDisabled":      false,
				"EnableAllFolders": true,
			},
			"Configuration": map[string]interface{}{},
		},
		"AccessToken": tokenString,
		"ServerId":    serverID,
	})
}

// --- Views (Libraries) ---

func (s *Server) jfGetViews(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	items := make([]map[string]interface{}, 0, len(libs))
	for i, lib := range libs {
		collectionType := "movies"
		if lib.ContentType == "tvseries" {
			collectionType = "tvshows"
		}
		items = append(items, map[string]interface{}{
			"Name":           lib.FriendlyName,
			"Id":             libraryID(i),
			"Type":           "CollectionFolder",
			"CollectionType": collectionType,
			"IsFolder":       true,
			"ImageTags":      map[string]string{},
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"TotalRecordCount": len(items),
	})
}

// --- Items ---

func (s *Server) jfGetItems(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	parentID := queryParam(r, "parentId", "ParentId")
	includeTypes := queryParam(r, "includeItemTypes", "IncludeItemTypes")
	sortBy := queryParam(r, "sortBy", "SortBy")
	limit, _ := strconv.Atoi(queryParam(r, "limit", "Limit"))
	startIndex, _ := strconv.Atoi(queryParam(r, "startIndex", "StartIndex"))
	searchTerm := queryParam(r, "searchTerm", "SearchTerm")
	recursive := queryParam(r, "recursive", "Recursive") == "true"
	ids := queryParam(r, "ids", "Ids")

	// If specific IDs requested, return those items directly
	if ids != "" {
		var allItems []map[string]interface{}
		for _, id := range strings.Split(ids, ",") {
			id = strings.TrimSpace(id)
			path, err := media.ItemPath(id)
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
			allItems = append(allItems, s.buildJellyfinItem(path, info, libIdx, libs))
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"Items":            allItems,
			"TotalRecordCount": len(allItems),
		})
		return
	}

	// Determine which directory to browse
	dir, libIndex := s.resolveJellyfinParent(parentID, libs)
	if dir == "" {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"Items":            []interface{}{},
			"TotalRecordCount": 0,
		})
		return
	}

	var allItems []map[string]interface{}

	if recursive {
		allItems = s.collectItemsRecursive(dir, libIndex, includeTypes, searchTerm, libs)
	} else {
		allItems = s.collectItems(dir, libIndex, includeTypes, libs)
	}

	// Search filter
	if searchTerm != "" && !recursive {
		searchLower := strings.ToLower(searchTerm)
		var filtered []map[string]interface{}
		for _, item := range allItems {
			name, _ := item["Name"].(string)
			if strings.Contains(strings.ToLower(name), searchLower) {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	// Sort
	if sortBy == "SortName" || sortBy == "Name" || sortBy == "" {
		sort.Slice(allItems, func(i, j int) bool {
			a, _ := allItems[i]["SortName"].(string)
			b, _ := allItems[j]["SortName"].(string)
			return strings.ToLower(a) < strings.ToLower(b)
		})
	} else if sortBy == "ProductionYear,SortName" || sortBy == "ProductionYear" {
		sort.Slice(allItems, func(i, j int) bool {
			ya, _ := allItems[i]["ProductionYear"].(int)
			yb, _ := allItems[j]["ProductionYear"].(int)
			if ya != yb {
				return ya > yb // Newest first
			}
			a, _ := allItems[i]["SortName"].(string)
			b, _ := allItems[j]["SortName"].(string)
			return strings.ToLower(a) < strings.ToLower(b)
		})
	}

	total := len(allItems)

	// Pagination
	if startIndex > 0 && startIndex < len(allItems) {
		allItems = allItems[startIndex:]
	}
	if limit > 0 && limit < len(allItems) {
		allItems = allItems[:limit]
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            allItems,
		"TotalRecordCount": total,
	})
}

func (s *Server) jfGetItem(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	itemID := mux.Vars(r)["itemId"]

	if idx, ok := parseLibraryID(itemID); ok && idx < len(libs) {
		lib := libs[idx]
		collectionType := "movies"
		if lib.ContentType == "tvseries" {
			collectionType = "tvshows"
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"Name":           lib.FriendlyName,
			"Id":             itemID,
			"Type":           "CollectionFolder",
			"CollectionType": collectionType,
			"IsFolder":       true,
		})
		return
	}

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	if !media.IsPathAllowed(path, libs) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	libIdx := findLibraryIndex(path, libs)
	item := s.buildJellyfinItem(path, info, libIdx, libs)
	respondJSON(w, http.StatusOK, item)
}

// --- Images ---

func (s *Server) jfGetItemImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itemID := vars["itemId"]
	imageType := vars["imageType"]

	var imgPath string

	if idx, ok := parseLibraryID(itemID); ok && idx < len(s.config.Libraries) {
		imgPath = media.FindImage(s.config.Libraries[idx].Path, imageType)
	} else {
		path, err := media.ItemPath(itemID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			imgPath = media.FindImage(path, imageType)
		} else {
			// Video file: try episode-specific thumbnail first
			imgPath = media.FindImageForVideo(path, imageType)
		}
	}

	if imgPath == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, imgPath)
}

// --- TV Shows ---

func (s *Server) jfGetSeasons(w http.ResponseWriter, r *http.Request) {
	showID := mux.Vars(r)["showId"]

	showPath, err := media.ItemPath(showID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Show not found")
		return
	}

	entries, err := os.ReadDir(showPath)
	if err != nil {
		respondError(w, http.StatusNotFound, "Show not found")
		return
	}

	var seasons []map[string]interface{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Match "Season X" or "SXX" pattern
		if !isSeasonDir(name) {
			continue
		}

		seasonPath := filepath.Join(showPath, name)
		seasonNum := extractSeasonNumber(name)

		seasons = append(seasons, map[string]interface{}{
			"Name":         name,
			"Id":           media.ItemID(seasonPath),
			"Type":         "Season",
			"IndexNumber":  seasonNum,
			"SeriesId":     showID,
			"SeriesName":   filepath.Base(showPath),
			"IsFolder":     true,
			"ImageTags":    s.imageTagsForDir(seasonPath),
			"ParentId":     showID,
		})
	}

	// Sort by season number
	sort.Slice(seasons, func(i, j int) bool {
		a, _ := seasons[i]["IndexNumber"].(int)
		b, _ := seasons[j]["IndexNumber"].(int)
		return a < b
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            seasons,
		"TotalRecordCount": len(seasons),
	})
}

func (s *Server) jfGetEpisodes(w http.ResponseWriter, r *http.Request) {
	showID := mux.Vars(r)["showId"]
	seasonID := queryParam(r, "seasonId", "SeasonId")
	parentID := queryParam(r, "parentId", "ParentId")

	// Determine season directory
	var seasonPath string
	if seasonID != "" {
		p, err := media.ItemPath(seasonID)
		if err == nil {
			seasonPath = p
		}
	}
	if seasonPath == "" && parentID != "" {
		p, err := media.ItemPath(parentID)
		if err == nil {
			seasonPath = p
		}
	}

	showPath, _ := media.ItemPath(showID)

	// If no season specified, list all episodes from all seasons
	if seasonPath == "" {
		seasonPath = showPath
	}

	var episodes []map[string]interface{}
	s.collectEpisodes(seasonPath, showID, showPath, &episodes)

	// Sort by episode number
	sort.Slice(episodes, func(i, j int) bool {
		si, _ := episodes[i]["ParentIndexNumber"].(int)
		sj, _ := episodes[j]["ParentIndexNumber"].(int)
		if si != sj {
			return si < sj
		}
		a, _ := episodes[i]["IndexNumber"].(int)
		b, _ := episodes[j]["IndexNumber"].(int)
		return a < b
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            episodes,
		"TotalRecordCount": len(episodes),
	})
}

func (s *Server) collectEpisodes(dir string, showID string, showPath string, episodes *[]map[string]interface{}) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	seasonNum := extractSeasonNumber(filepath.Base(dir))

	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			if isSeasonDir(name) {
				s.collectEpisodes(fullPath, showID, showPath, episodes)
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".mkv" && ext != ".mp4" && ext != ".avi" && ext != ".m4v" && ext != ".mov" && ext != ".webm" {
			continue
		}

		epNum := extractEpisodeNumber(name)
		epName := cleanEpisodeName(name)
		epSeasonNum := seasonNum
		var runTimeTicks int64

		// Read episode NFO for title, season, episode number, duration
		if nfo, err := media.ParseEpisodeNFO(fullPath); err == nil {
			if nfo.Title != "" {
				epName = nfo.Title
			}
			if nfo.Season > 0 {
				epSeasonNum = nfo.Season
			}
			if nfo.Episode > 0 {
				epNum = nfo.Episode
			}
			if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
				runTimeTicks = int64(nfo.StreamDetails.DurationSeconds) * 10000000
			} else if nfo.Runtime > 0 {
				runTimeTicks = int64(nfo.Runtime) * 600000000
			}
		}

		ep := map[string]interface{}{
			"Name":              epName,
			"Id":                media.ItemID(fullPath),
			"Type":              "Episode",
			"IndexNumber":       epNum,
			"ParentIndexNumber": epSeasonNum,
			"SeriesId":          showID,
			"SeriesName":        filepath.Base(showPath),
			"SeasonId":          media.ItemID(dir),
			"IsFolder":          false,
			"MediaType":         "Video",
			"ImageTags":         s.imageTagsForDir(dir),
			"VideoType":         "VideoFile",
			"LocationType":      "FileSystem",
			"Path":              fullPath,
			"MediaSources":      s.buildMediaSources(fullPath),
		}

		if runTimeTicks > 0 {
			ep["RunTimeTicks"] = runTimeTicks
		}

		*episodes = append(*episodes, ep)
	}
}

// --- Playback ---

func (s *Server) jfPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	// If it's a directory (movie folder), find the video file inside
	info, err := os.Stat(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			respondError(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"MediaSources": s.buildMediaSources(path),
		"PlaySessionId": fmt.Sprintf("ps-%s", itemID[:8]),
	})
}

func (s *Server) jfVideoStream(w http.ResponseWriter, r *http.Request) {
	itemID := mux.Vars(r)["itemId"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		respondError(w, http.StatusNotFound, "Item not found")
		return
	}
	if info.IsDir() {
		videoPath := media.FindVideoFile(path)
		if videoPath == "" {
			respondError(w, http.StatusNotFound, "No video file found")
			return
		}
		path = videoPath
	}

	isStatic := queryParam(r, "static", "Static") == "true"
	if isStatic {
		s.streamDirect(w, r, path)
	} else {
		// Try remux by default for non-static requests
		s.streamRemux(w, r, path)
	}
}

// --- Session Stubs ---

func (s *Server) jfSubtitleStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itemID := vars["itemId"]
	subtitleIndex, _ := strconv.Atoi(vars["index"])
	format := vars["format"] // "srt" or "vtt"

	path, err := media.ItemPath(itemID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	videoPath := path
	if info.IsDir() {
		videoPath = media.FindVideoFile(path)
		if videoPath == "" {
			http.NotFound(w, r)
			return
		}
	}

	subs := media.FindSubtitles(videoPath)

	// The subtitle index in MediaStreams starts after video (0) and audio (1) streams,
	// so subtitle index 2 = first subtitle, 3 = second, etc.
	subIdx := subtitleIndex - 2
	if subIdx < 0 || subIdx >= len(subs) {
		http.NotFound(w, r)
		return
	}

	sub := subs[subIdx]

	if format == "vtt" {
		vttContent, err := media.ConvertSRTToVTT(sub.FilePath)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to convert subtitle")
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeContent(w, r, "subtitles.vtt", time.Now(), vttContent)
	} else {
		w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, sub.FilePath)
	}
}

func (s *Server) jfSessionStub(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfSessionsStub(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	username := usernameFromContext(r)
	userID := stableUserID(username)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Name":                  username,
		"Id":                    userID,
		"HasPassword":           true,
		"HasConfiguredPassword": true,
		"Policy": map[string]interface{}{
			"IsAdministrator":  true,
			"IsDisabled":       false,
			"EnableAllFolders": true,
		},
		"Configuration": map[string]interface{}{},
	})
}

func (s *Server) jfGetLatest(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	parentID := r.URL.Query().Get("parentId")
	dir, libIndex := s.resolveJellyfinParent(parentID, libs)
	if dir == "" {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	items := s.collectItemsRecursive(dir, libIndex, "", "", libs)

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if len(items) > limit {
		items = items[:limit]
	}

	respondJSON(w, http.StatusOK, items)
}

func (s *Server) jfGetFilters(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	parentID := r.URL.Query().Get("parentId")
	dir, libIndex := s.resolveJellyfinParent(parentID, libs)

	genres := map[string]bool{}
	years := map[int]bool{}

	if dir != "" {
		items := s.collectItemsRecursive(dir, libIndex, "", "", libs)
		for _, item := range items {
			if gs, ok := item["Genres"].([]string); ok {
				for _, g := range gs {
					genres[g] = true
				}
			}
			if y, ok := item["ProductionYear"].(int); ok && y > 0 {
				years[y] = true
			}
		}
	}

	genreList := make([]string, 0, len(genres))
	for g := range genres {
		genreList = append(genreList, g)
	}
	sort.Strings(genreList)

	yearList := make([]int, 0, len(years))
	for y := range years {
		yearList = append(yearList, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(yearList)))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Genres": genreList,
		"Years":  yearList,
		"Tags":   []string{},
	})
}

func (s *Server) jfEmptyItems(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            []interface{}{},
		"TotalRecordCount": 0,
	})
}

func (s *Server) jfUserDataStub(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
	})
}

func (s *Server) jfDisplayPrefsStub(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Id":                mux.Vars(r)["displayPrefsId"],
		"SortBy":            "SortName",
		"SortOrder":         "Ascending",
		"RememberIndexing":  false,
		"RememberSorting":   false,
		"CustomPrefs":       map[string]string{},
	})
}

// --- Helper Functions ---

// queryParam returns the first non-empty value for any of the given param names.
// Jellyfin clients are inconsistent with casing (parentId vs ParentId).
func queryParam(r *http.Request, names ...string) string {
	for _, name := range names {
		if v := r.URL.Query().Get(name); v != "" {
			return v
		}
	}
	return ""
}

func libraryID(index int) string {
	return fmt.Sprintf("library-%d", index)
}

func parseLibraryID(id string) (int, bool) {
	if !strings.HasPrefix(id, "library-") {
		return 0, false
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(id, "library-"))
	if err != nil {
		return 0, false
	}
	return idx, true
}

func (s *Server) resolveJellyfinParent(parentID string, libs []config.Library) (string, int) {
	if parentID == "" {
		return "", -1
	}

	if idx, ok := parseLibraryID(parentID); ok && idx < len(libs) {
		return libs[idx].Path, idx
	}

	path, err := media.ItemPath(parentID)
	if err != nil {
		return "", -1
	}
	if !media.IsPathAllowed(path, libs) {
		return "", -1
	}

	return path, findLibraryIndex(path, libs)
}

func findLibraryIndex(path string, libs []config.Library) int {
	absPath, _ := filepath.Abs(path)
	for i, lib := range libs {
		absLib, _ := filepath.Abs(lib.Path)
		if strings.HasPrefix(absPath, absLib) {
			return i
		}
	}
	return -1
}

func (s *Server) collectItems(dir string, libIndex int, includeTypes string, libs []config.Library) []map[string]interface{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var items []map[string]interface{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}

		item := s.buildJellyfinItem(fullPath, info, libIndex, libs)
		itemType, _ := item["Type"].(string)

		// Filter by IncludeItemTypes
		if includeTypes != "" && !strings.Contains(includeTypes, itemType) {
			continue
		}

		items = append(items, item)
	}

	return items
}

func (s *Server) collectItemsRecursive(dir string, libIndex int, includeTypes string, searchTerm string, libs []config.Library) []map[string]interface{} {
	var items []map[string]interface{}
	searchLower := strings.ToLower(searchTerm)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			info, err := e.Info()
			if err != nil {
				continue
			}

			item := s.buildJellyfinItem(fullPath, info, libIndex, libs)
			itemType, _ := item["Type"].(string)

			typeMatch := includeTypes == "" || strings.Contains(includeTypes, itemType)
			nameMatch := searchTerm == "" || strings.Contains(strings.ToLower(name), searchLower)

			if typeMatch && nameMatch {
				items = append(items, item)
			}
		}
	}

	return items
}

func (s *Server) buildJellyfinItem(path string, info os.FileInfo, libIndex int, libs []config.Library) map[string]interface{} {
	name := info.Name()
	id := media.ItemID(path)

	if info.IsDir() {
		return s.buildFolderItem(path, name, id, libIndex, libs)
	}
	return s.buildVideoItem(path, name, id, libIndex)
}

func (s *Server) buildFolderItem(path, name, id string, libIndex int, libs []config.Library) map[string]interface{} {
	itemType := "Movie"
	if libIndex >= 0 && libIndex < len(libs) {
		if libs[libIndex].ContentType == "tvseries" {
			// Check if it's a season folder
			if isSeasonDir(name) {
				itemType = "Season"
			} else {
				itemType = "Series"
			}
		}
	}

	item := map[string]interface{}{
		"Name":         name,
		"SortName":     name,
		"Id":           id,
		"Type":         itemType,
		"IsFolder":     true,
		"ImageTags":    s.imageTagsForDir(path),
		"LocationType": "FileSystem",
		"Path":         path,
	}

	// Add backdrop image tags
	if media.FindImage(path, "Backdrop") != "" {
		item["BackdropImageTags"] = []string{"backdrop"}
	}

	// Read NFO for metadata
	if nfo, err := media.ParseNFO(path); err == nil {
		if nfo.Title != "" {
			item["Name"] = nfo.Title
			item["SortName"] = nfo.Title
		}
		if nfo.OriginalTitle != "" {
			item["OriginalTitle"] = nfo.OriginalTitle
		}
		if nfo.Year > 0 {
			item["ProductionYear"] = nfo.Year
			item["PremiereDate"] = fmt.Sprintf("%d-01-01T00:00:00.0000000Z", nfo.Year)
		}
		if nfo.Plot != "" {
			item["Overview"] = nfo.Plot
		}
		if nfo.Rating > 0 {
			item["CommunityRating"] = nfo.Rating
		}
		if nfo.Runtime > 0 {
			item["RunTimeTicks"] = int64(nfo.Runtime) * 600000000 // minutes to ticks
		}
		if nfo.Studio != "" {
			item["Studios"] = []map[string]string{{"Name": nfo.Studio}}
		}
		if len(nfo.Genres) > 0 {
			item["Genres"] = nfo.Genres
			genreItems := make([]map[string]string, len(nfo.Genres))
			for i, g := range nfo.Genres {
				genreItems[i] = map[string]string{"Name": g}
			}
			item["GenreItems"] = genreItems
		}
		if nfo.MPAA != "" {
			item["OfficialRating"] = nfo.MPAA
		}
		if nfo.Tagline != "" {
			item["Taglines"] = []string{nfo.Tagline}
		}

		// ProviderIds from NFO uniqueid
		providerIDs := map[string]string{}
		for _, uid := range nfo.UniqueID {
			if uid.Value != "" {
				providerIDs[uid.Type] = uid.Value
			}
		}
		if len(providerIDs) > 0 {
			item["ProviderIds"] = providerIDs
		}
	}

	// For movies, add media sources from the video file inside
	if itemType == "Movie" {
		if videoPath := media.FindVideoFile(path); videoPath != "" {
			item["MediaSources"] = s.buildMediaSources(videoPath)
			item["MediaType"] = "Video"
			item["VideoType"] = "VideoFile"
		}
	}

	return item
}

func (s *Server) buildVideoItem(path, name, id string, libIndex int) map[string]interface{} {
	item := map[string]interface{}{
		"Name":         cleanEpisodeName(name),
		"SortName":     name,
		"Id":           id,
		"Type":         "Video",
		"IsFolder":     false,
		"MediaType":    "Video",
		"VideoType":    "VideoFile",
		"LocationType": "FileSystem",
		"Path":         path,
		"MediaSources": s.buildMediaSources(path),
		"ImageTags":    s.imageTagsForDir(filepath.Dir(path)),
	}

	// Read episode NFO (same name as video but .nfo)
	if nfo, err := media.ParseEpisodeNFO(path); err == nil {
		if nfo.Title != "" {
			item["Name"] = nfo.Title
		}
		if nfo.Season > 0 {
			item["ParentIndexNumber"] = nfo.Season
		}
		if nfo.Episode > 0 {
			item["IndexNumber"] = nfo.Episode
		}
		if nfo.Plot != "" {
			item["Overview"] = nfo.Plot
		}
		if nfo.Rating > 0 {
			item["CommunityRating"] = nfo.Rating
		}
		if nfo.Year > 0 {
			item["ProductionYear"] = nfo.Year
		}
		if nfo.Aired != "" {
			item["PremiereDate"] = nfo.Aired + "T00:00:00.0000000Z"
		}
		if nfo.Runtime > 0 {
			item["RunTimeTicks"] = int64(nfo.Runtime) * 600000000
		}
		if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
			item["RunTimeTicks"] = int64(nfo.StreamDetails.DurationSeconds) * 10000000
		}
		if nfo.ShowTitle != "" {
			item["SeriesName"] = nfo.ShowTitle
		}
		if len(nfo.Genres) > 0 {
			item["Genres"] = nfo.Genres
		}
	}

	return item
}

func (s *Server) buildMediaSources(videoPath string) []map[string]interface{} {
	info, err := os.Stat(videoPath)
	if err != nil {
		return nil
	}

	ext := strings.ToLower(filepath.Ext(videoPath))
	container := strings.TrimPrefix(ext, ".")

	// Infer codec from container (best effort without ffprobe)
	videoCodec := "h264"
	audioCodec := "aac"
	if container == "mkv" || container == "webm" {
		videoCodec = "h264"
		audioCodec = "aac"
	}

	streamIndex := 0
	mediaStreams := []map[string]interface{}{
		{
			"Type":       "Video",
			"Index":      streamIndex,
			"Codec":      videoCodec,
			"IsDefault":  true,
			"IsExternal": false,
		},
	}
	streamIndex++
	mediaStreams = append(mediaStreams, map[string]interface{}{
		"Type":       "Audio",
		"Index":      streamIndex,
		"Codec":      audioCodec,
		"IsDefault":  true,
		"IsExternal": false,
		"Language":   "und",
	})
	streamIndex++

	// Discover external subtitle files
	subtitles := media.FindSubtitles(videoPath)
	for i, sub := range subtitles {
		mediaStreams = append(mediaStreams, map[string]interface{}{
			"Type":               "Subtitle",
			"Index":              streamIndex,
			"Codec":              "srt",
			"Language":           sub.Language,
			"DisplayTitle":       sub.Label,
			"IsDefault":         i == 0,
			"IsForced":          false,
			"IsExternal":        true,
			"SupportsExternalStream": true,
			"DeliveryMethod":    "External",
			"DeliveryUrl":       fmt.Sprintf("/Videos/%s/%s/Subtitles/%d/0/Stream.srt", media.ItemID(videoPath), media.ItemID(videoPath), streamIndex),
			"Path":              sub.FilePath,
		})
		streamIndex++
	}

	sourceID := media.ItemID(videoPath)

	// Try to get runtime: episode NFO (durationinseconds) > episode NFO (runtime) > folder NFO > ffprobe
	var runTimeTicks int64
	if nfo, err := media.ParseEpisodeNFO(videoPath); err == nil {
		if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
			runTimeTicks = int64(nfo.StreamDetails.DurationSeconds) * 10000000
		} else if nfo.Runtime > 0 {
			runTimeTicks = int64(nfo.Runtime) * 600000000
		}
	}
	if runTimeTicks == 0 {
		if nfo, err := media.ParseNFO(filepath.Dir(videoPath)); err == nil && nfo.Runtime > 0 {
			runTimeTicks = int64(nfo.Runtime) * 600000000
		}
	}
	if runTimeTicks == 0 {
		if seconds := media.ProbeDuration(videoPath); seconds > 0 {
			runTimeTicks = int64(seconds * 10000000)
		}
	}

	source := map[string]interface{}{
		"Id":                    sourceID,
		"Path":                  videoPath,
		"Protocol":              "File",
		"Type":                  "Default",
		"Container":             container,
		"Size":                  info.Size(),
		"Name":                  filepath.Base(videoPath),
		"IsRemote":              false,
		"SupportsDirectPlay":    true,
		"SupportsDirectStream":  true,
		"SupportsTranscoding":   true,
		"ReadAtNativeFramerate": false,
		"MediaStreams":          mediaStreams,
		"DirectStreamUrl":       fmt.Sprintf("/Videos/%s/stream?static=true&mediaSourceId=%s", sourceID, sourceID),
	}

	if runTimeTicks > 0 {
		source["RunTimeTicks"] = runTimeTicks
	}

	return []map[string]interface{}{source}
}

func (s *Server) imageTagsForDir(dir string) map[string]string {
	tags := map[string]string{}
	if media.FindImage(dir, "Primary") != "" {
		tags["Primary"] = "primary"
	}
	return tags
}

func isSeasonDir(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "season ") {
		return true
	}
	if strings.HasPrefix(lower, "season") && len(lower) > 6 {
		_, err := strconv.Atoi(lower[6:])
		return err == nil
	}
	if len(lower) >= 2 && lower[0] == 's' {
		_, err := strconv.Atoi(lower[1:])
		return err == nil
	}
	// Also match "Specials"
	return lower == "specials" || lower == "extras"
}

func extractSeasonNumber(name string) int {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "specials" || lower == "extras" {
		return 0
	}
	if strings.HasPrefix(lower, "season ") {
		n, _ := strconv.Atoi(strings.TrimSpace(lower[7:]))
		return n
	}
	if strings.HasPrefix(lower, "season") {
		n, _ := strconv.Atoi(lower[6:])
		return n
	}
	if len(lower) >= 2 && lower[0] == 's' {
		n, _ := strconv.Atoi(lower[1:])
		return n
	}
	return 0
}

func extractEpisodeNumber(filename string) int {
	return media.ExtractEpisodeNumber(filename)
}

func cleanEpisodeName(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	return name
}

func (s *Server) logDebug(format string, args ...interface{}) {
	if s.config.App.Debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}
