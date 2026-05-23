package server

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

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

// libraryID generates a deterministic UUID for a library by index.
func libraryID(index int) string {
	return stableID(fmt.Sprintf("rms-library-%d", index))
}

// parseLibraryID checks if the given ID matches any library and returns its index.
func (s *Server) parseLibraryID(id string, libs []config.Library) (int, bool) {
	for i := range libs {
		if libraryID(i) == id {
			return i, true
		}
	}
	// Legacy format: "library-0", "library-1", etc.
	if strings.HasPrefix(id, "library-") {
		idx, err := strconv.Atoi(strings.TrimPrefix(id, "library-"))
		if err == nil && idx >= 0 && idx < len(libs) {
			return idx, true
		}
	}
	return 0, false
}

// stableID generates a deterministic UUID-like string from an arbitrary key.
func stableID(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func (s *Server) resolveJellyfinParent(parentID string, libs []config.Library) (string, int) {
	if parentID == "" {
		return "", -1
	}

	if idx, ok := s.parseLibraryID(parentID, libs); ok {
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
		return make([]map[string]interface{}, 0)
	}

	items := make([]map[string]interface{}, 0)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Skip non-video files — only directories and video files are Jellyfin items
		if !e.IsDir() && !media.IsVideoFile(name) {
			continue
		}

		fullPath := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}

		item := s.buildJellyfinItem(fullPath, info, libIndex, libs)
		itemType, _ := item["Type"].(string)

		if includeTypes != "" && !strings.Contains(includeTypes, itemType) {
			continue
		}

		items = append(items, item)
	}

	return items
}

func (s *Server) collectItemsRecursive(dir string, libIndex int, includeTypes string, searchTerm string, libs []config.Library) []map[string]interface{} {
	items := make([]map[string]interface{}, 0)
	searchLower := strings.ToLower(searchTerm)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return items
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
			items = append(items, s.collectItemsRecursive(fullPath, libIndex, includeTypes, searchTerm, libs)...)
			continue
		}

		if !media.IsVideoFile(name) {
			continue
		}
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

	return items
}

func (s *Server) buildJellyfinItem(path string, info os.FileInfo, libIndex int, libs []config.Library) map[string]interface{} {
	name := info.Name()
	id := media.ItemID(path)

	if info.IsDir() {
		return s.buildFolderItem(path, name, id, libIndex, libs)
	}
	return s.buildVideoItem(path, name, id, libIndex, libs)
}

func (s *Server) buildFolderItem(path, name, id string, libIndex int, libs []config.Library) map[string]interface{} {
	itemType := "Movie"
	if libIndex >= 0 && libIndex < len(libs) {
		ct := libs[libIndex].ContentType
		if ct == "tvseries" || ct == "anime" {
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
		"UserData":     defaultUserData(id),
		"People":       []interface{}{},
		"ExternalUrls": []interface{}{},
		"DateCreated":  dirDateCreated(path),
	}

	if media.FindImage(path, "Backdrop") != "" {
		item["BackdropImageTags"] = []string{"backdrop"}
	}

	if itemType == "Season" {
		showPath := filepath.Dir(path)
		seriesID := media.ItemID(showPath)
		item["SeriesId"] = seriesID
		item["SeriesName"] = filepath.Base(showPath)
		item["ParentId"] = seriesID
		item["IndexNumber"] = extractSeasonNumber(name)
		item["ChildCount"] = countVideoFiles(path)
	}

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
			item["RunTimeTicks"] = int64(nfo.Runtime) * 600000000
		}
		if nfo.Studio != "" {
			item["Studios"] = []map[string]interface{}{{"Name": nfo.Studio, "Id": stableID("studio-" + nfo.Studio)}}
		}
		if len(nfo.Genres) > 0 {
			item["Genres"] = nfo.Genres
			genreItems := make([]map[string]interface{}, len(nfo.Genres))
			for i, g := range nfo.Genres {
				genreItems[i] = map[string]interface{}{"Name": g, "Id": stableID("genre-" + g)}
			}
			item["GenreItems"] = genreItems
		}
		if nfo.MPAA != "" {
			item["OfficialRating"] = nfo.MPAA
		}
		if nfo.Tagline != "" {
			item["Taglines"] = []string{nfo.Tagline}
		}

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

	if itemType == "Movie" {
		if videoPath := media.FindVideoFile(path); videoPath != "" {
			item["MediaSources"] = s.buildMediaSources(videoPath)
			item["MediaType"] = "Video"
			item["VideoType"] = "VideoFile"
		}
	}

	return item
}

func (s *Server) buildVideoItem(path, name, id string, libIndex int, libs []config.Library) map[string]interface{} {
	itemType := "Video"
	if libIndex >= 0 && libIndex < len(libs) {
		ct := libs[libIndex].ContentType
		if ct == "tvseries" || ct == "anime" {
			itemType = "Episode"
		}
	}

	item := map[string]interface{}{
		"Name":         cleanEpisodeName(name),
		"SortName":     name,
		"Id":           id,
		"Type":         itemType,
		"IsFolder":     false,
		"MediaType":    "Video",
		"VideoType":    "VideoFile",
		"LocationType": "FileSystem",
		"Path":         path,
		"MediaSources": s.buildMediaSources(path),
		"ImageTags":    s.imageTagsForVideo(path),
		"UserData":     defaultUserData(id),
		"People":       []interface{}{},
		"ExternalUrls": []interface{}{},
		"DateCreated":  fileDateCreated(path),
	}

	if itemType == "Episode" {
		seasonPath := filepath.Dir(path)
		showPath := filepath.Dir(seasonPath)
		seasonNumber := extractSeasonNumber(filepath.Base(seasonPath))
		if !isSeasonDir(filepath.Base(seasonPath)) {
			showPath = seasonPath
			seasonNumber = 1
		}
		seriesID := media.ItemID(showPath)
		seasonID := media.ItemID(seasonPath)
		item["SeriesId"] = seriesID
		item["SeriesName"] = filepath.Base(showPath)
		item["SeasonId"] = seasonID
		item["ParentId"] = seasonID
		item["ParentIndexNumber"] = seasonNumber
		item["IndexNumber"] = extractEpisodeNumber(name)
	}

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

func (s *Server) imageTagsForDir(dir string) map[string]string {
	tags := map[string]string{}
	if media.FindImage(dir, "Primary") != "" {
		tags["Primary"] = "primary"
	} else if media.FindImage(filepath.Dir(dir), "Primary") != "" {
		// Fallback: use parent directory artwork (season → show)
		tags["Primary"] = "primary"
	}
	return tags
}

func (s *Server) imageTagsForVideo(videoPath string) map[string]string {
	tags := map[string]string{}
	if media.FindImageForVideo(videoPath, "Primary") != "" {
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

func countVideoFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && media.IsVideoFile(entry.Name()) {
			count++
		}
	}
	return count
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

func dirDateCreated(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "2020-01-01T00:00:00.0000000Z"
	}
	return info.ModTime().UTC().Format("2006-01-02T15:04:05.0000000Z")
}

func fileDateCreated(path string) string {
	return dirDateCreated(path)
}
