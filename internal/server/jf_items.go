package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
	"raspberry-media-server/internal/tv"
)

// --- Views (Libraries) ---

func (s *Server) jfGetViews(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	items := make([]map[string]interface{}, 0, len(libs))
	for i, lib := range libs {
		collectionType := "movies"
		if lib.ContentType == "tvseries" || lib.ContentType == "anime" {
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
		"StartIndex":       0,
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
		allItems := make([]map[string]interface{}, 0)
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
		s.patchItemsUserData(allItems, r)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"Items":            allItems,
			"StartIndex":       0,
			"TotalRecordCount": len(allItems),
		})
		return
	}

	// Determine which directory to browse
	dir, libIndex := s.resolveJellyfinParent(parentID, libs)
	if dir == "" {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"Items":            []interface{}{},
			"StartIndex":       0,
			"TotalRecordCount": 0,
		})
		return
	}

	allItems := make([]map[string]interface{}, 0)

	if recursive {
		allItems = s.collectItemsRecursive(dir, libIndex, includeTypes, searchTerm, libs)
	} else {
		allItems = s.collectItems(dir, libIndex, includeTypes, libs)
	}

	// Search filter
	if searchTerm != "" && !recursive {
		searchLower := strings.ToLower(searchTerm)
		filtered := make([]map[string]interface{}, 0)
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

	// User data filters
	if f := queryParam(r, "filters", "Filters"); f != "" {
		userID := stableUserID(usernameFromContext(r))
		filtered := make([]map[string]interface{}, 0, len(allItems))
		for _, item := range allItems {
			id, _ := item["Id"].(string)
			keep := true
			if strings.Contains(f, "IsResumable") && !s.userData.IsResumable(userID, id) {
				keep = false
			}
			if strings.Contains(f, "IsFavorite") && !s.userData.IsFavorite(userID, id) {
				keep = false
			}
			if keep {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	total := len(allItems)

	// Pagination
	if startIndex > 0 && startIndex < len(allItems) {
		allItems = allItems[startIndex:]
	}
	if limit > 0 && limit < len(allItems) {
		allItems = allItems[:limit]
	}

	s.patchItemsUserData(allItems, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            allItems,
		"StartIndex":       startIndex,
		"TotalRecordCount": total,
	})
}

func (s *Server) jfGetItem(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	itemID := mux.Vars(r)["itemId"]

	if idx, ok := s.parseLibraryID(itemID, libs); ok {
		lib := libs[idx]
		collectionType := "movies"
		if lib.ContentType == "tvseries" || lib.ContentType == "anime" {
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

	// TV channel.
	if ch, ok := tv.LookupChannel(itemID); ok {
		if !s.tvLibraryVisible(ch.LibKey, r) {
			respondError(w, http.StatusNotFound, "Item not found")
			return
		}
		item := channelItem(ch)
		item["MediaSources"] = []map[string]interface{}{channelMediaSource(ch)}
		item["UserData"] = s.userData.Get(stableUserID(usernameFromContext(r)), ch.ID)
		respondJSON(w, http.StatusOK, item)
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
	item["UserData"] = s.userData.Get(stableUserID(usernameFromContext(r)), item["Id"].(string))
	respondJSON(w, http.StatusOK, item)
}

// --- Ancestors ---

// jfGetAncestors returns the chain of parent items from the library root down
// to (but not including) the requested item. Kodi uses this to reconstruct
// Series → Season → Episode hierarchies.
func (s *Server) jfGetAncestors(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	itemID := mux.Vars(r)["itemId"]

	path, err := media.ItemPath(itemID)
	if err != nil {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	if !media.IsPathAllowed(path, libs) {
		respondError(w, http.StatusForbidden, "Access denied")
		return
	}

	libIdx := findLibraryIndex(path, libs)
	if libIdx < 0 {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	libPath, _ := filepath.Abs(libs[libIdx].Path)

	ancestors := make([]map[string]interface{}, 0)
	current := filepath.Dir(path)
	for {
		abs, _ := filepath.Abs(current)
		if !strings.HasPrefix(abs, libPath) {
			break
		}
		if abs == libPath {
			ancestors = append(ancestors, map[string]interface{}{
				"Name":     libs[libIdx].FriendlyName,
				"Id":       libraryID(libIdx),
				"Type":     "CollectionFolder",
				"IsFolder": true,
			})
			break
		}
		info, err := os.Stat(current)
		if err != nil {
			break
		}
		ancestors = append(ancestors, s.buildJellyfinItem(current, info, libIdx, libs))
		current = filepath.Dir(current)
	}

	respondJSON(w, http.StatusOK, ancestors)
}

// --- Images ---

func (s *Server) jfGetItemImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itemID := vars["itemId"]
	imageType := vars["imageType"]

	// TV channel logo: redirect to the upstream image (clients load images via
	// <img>, so cross-origin is fine — no proxy needed here).
	if ch, ok := tv.LookupChannel(itemID); ok {
		if ch.Logo == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, ch.Logo, http.StatusFound)
		return
	}

	var imgPath string

	if idx, ok := s.parseLibraryID(itemID, s.config.Libraries); ok {
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
			// Fallback: parent directory (season → show)
			if imgPath == "" {
				imgPath = media.FindImage(filepath.Dir(path), imageType)
			}
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

// --- Latest ---

func (s *Server) jfGetLatest(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	parentID := r.URL.Query().Get("parentId")

	items := make([]map[string]interface{}, 0)
	if parentID == "" {
		for i, lib := range libs {
			items = append(items, s.collectItemsRecursive(lib.Path, i, "", "", libs)...)
		}
	} else {
		dir, libIndex := s.resolveJellyfinParent(parentID, libs)
		if dir == "" {
			respondJSON(w, http.StatusOK, []interface{}{})
			return
		}
		items = s.collectItemsRecursive(dir, libIndex, "", "", libs)
	}

	sort.Slice(items, func(i, j int) bool {
		a, _ := items[i]["DateCreated"].(string)
		b, _ := items[j]["DateCreated"].(string)
		return a > b
	})

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if len(items) > limit {
		items = items[:limit]
	}

	s.patchItemsUserData(items, r)
	respondJSON(w, http.StatusOK, items)
}

func (s *Server) jfGetSuggestions(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	limit := 12
	if l, err := strconv.Atoi(queryParam(r, "limit", "Limit")); err == nil && l > 0 {
		limit = l
	}

	items := make([]map[string]interface{}, 0)
	for i, lib := range libs {
		items = append(items, s.collectItemsRecursive(lib.Path, i, "", "", libs)...)
	}
	sort.Slice(items, func(i, j int) bool {
		a, _ := items[i]["DateCreated"].(string)
		b, _ := items[j]["DateCreated"].(string)
		return a > b
	})
	if len(items) > limit {
		items = items[:limit]
	}
	s.patchItemsUserData(items, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"StartIndex":       0,
		"TotalRecordCount": len(items),
	})
}

// --- Filters ---

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

// --- Search ---

func (s *Server) jfSearchHints(w http.ResponseWriter, r *http.Request) {
	searchTerm := queryParam(r, "searchTerm", "SearchTerm")
	if searchTerm == "" {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"SearchHints":      []interface{}{},
			"TotalRecordCount": 0,
		})
		return
	}

	libs := s.librariesForRequest(r)
	searchLower := strings.ToLower(searchTerm)
	limit := 20
	if l, err := strconv.Atoi(queryParam(r, "limit", "Limit")); err == nil && l > 0 {
		limit = l
	}

	var hints []map[string]interface{}

	for i, lib := range libs {
		items := s.collectItemsRecursive(lib.Path, i, "", "", libs)
		for _, item := range items {
			name, _ := item["Name"].(string)
			if !strings.Contains(strings.ToLower(name), searchLower) {
				continue
			}
			hint := map[string]interface{}{
				"Id":           item["Id"],
				"Name":         name,
				"Type":         item["Type"],
				"IsFolder":     item["IsFolder"],
				"MediaType":    item["MediaType"],
				"RunTimeTicks": item["RunTimeTicks"],
			}
			if year, ok := item["ProductionYear"]; ok {
				hint["ProductionYear"] = year
			}
			if img, ok := item["ImageTags"]; ok {
				hint["ImageTags"] = img
			}
			hints = append(hints, hint)
			if len(hints) >= limit {
				break
			}
		}
		if len(hints) >= limit {
			break
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"SearchHints":      hints,
		"TotalRecordCount": len(hints),
	})
}

func (s *Server) jfEmptyItems(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            []interface{}{},
		"StartIndex":       0,
		"TotalRecordCount": 0,
	})
}

func (s *Server) jfGetGenres(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	parentID := queryParam(r, "parentId", "ParentId")

	genres := map[string]bool{}
	for i, lib := range libs {
		if parentID != "" && libraryID(i) != parentID {
			continue
		}
		items := s.collectItemsRecursive(lib.Path, i, "", "", libs)
		for _, item := range items {
			if gs, ok := item["Genres"].([]string); ok {
				for _, g := range gs {
					genres[g] = true
				}
			}
		}
	}

	genreItems := make([]map[string]interface{}, 0, len(genres))
	for g := range genres {
		genreItems = append(genreItems, map[string]interface{}{
			"Name": g,
			"Id":   stableID("genre-" + g),
			"Type": "Genre",
		})
	}
	sort.Slice(genreItems, func(i, j int) bool {
		a, _ := genreItems[i]["Name"].(string)
		b, _ := genreItems[j]["Name"].(string)
		return a < b
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            genreItems,
		"StartIndex":       0,
		"TotalRecordCount": len(genreItems),
	})
}
