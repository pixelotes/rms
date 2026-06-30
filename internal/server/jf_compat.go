package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"


	"raspberry-media-server/internal/media"
)

func (s *Server) jfNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) jfBrandingCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) jfSystemPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Jellyfin Server"))
}

func (s *Server) jfSystemEndpoint(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"IsLocal":     false,
		"IsInNetwork": false,
	})
}

func (s *Server) jfSystemConfiguration(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ServerName":                serverName,
		"UICulture":                 "en-US",
		"MetadataCountryCode":       "US",
		"PreferredMetadataLanguage": "en",
		"EnableRemoteAccess":        true,
		"IsStartupWizardCompleted":  true,
	})
}

func (s *Server) jfSystemConfigurationValue(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	switch key {
	case "branding":
		s.jfBrandingConfig(w, r)
	default:
		respondJSON(w, http.StatusOK, map[string]interface{}{})
	}
}

func (s *Server) jfMetadataOptionsDefault(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"DisabledMetadataSavers":   []interface{}{},
		"LocalMetadataReaderOrder": []interface{}{},
		"DisabledMetadataFetchers": []interface{}{},
		"MetadataFetcherOrder":     []interface{}{},
		"DisabledImageFetchers":    []interface{}{},
		"ImageFetcherOrder":        []interface{}{},
	})
}

func (s *Server) jfEmptyObject(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) jfGetUsers(w http.ResponseWriter, r *http.Request) {
	users := make([]map[string]interface{}, 0, len(s.config.Users))
	if len(s.config.Users) == 0 {
		users = append(users, jellyfinUserDTO("rms", stableUserID("rms")))
	} else {
		for _, u := range s.config.Users {
			users = append(users, jellyfinUserDTO(u.Username, stableUserID(u.Username)))
		}
	}
	respondJSON(w, http.StatusOK, users)
}

func (s *Server) jfGetRoot(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Name":     "Root",
		"Id":       "root",
		"Type":     "Folder",
		"IsFolder": true,
	})
}

func (s *Server) jfGroupingOptions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]interface{}{
		{
			"Name": "None",
			"Type": "none",
		},
	})
}

func (s *Server) jfItemCounts(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	counts := map[string]int{
		"MovieCount":      0,
		"SeriesCount":     0,
		"EpisodeCount":    0,
		"ArtistCount":     0,
		"ProgramCount":    0,
		"TrailerCount":    0,
		"SongCount":       0,
		"AlbumCount":      0,
		"MusicVideoCount": 0,
		"BoxSetCount":     0,
		"BookCount":       0,
		"ItemCount":       0,
	}

	for _, lib := range libs {
		contentType := lib.ContentType
		filepath.WalkDir(lib.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil || path == lib.Path {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if contentType == "tvseries" || contentType == "anime" {
					if filepath.Dir(path) == lib.Path && !isSeasonDir(name) {
						counts["SeriesCount"]++
						counts["ItemCount"]++
					}
				} else if media.FindVideoFile(path) != "" {
					counts["MovieCount"]++
					counts["ItemCount"]++
				}
				return nil
			}
			if !media.IsVideoFile(name) {
				return nil
			}
			if contentType == "tvseries" || contentType == "anime" {
				counts["EpisodeCount"]++
			} else if filepath.Dir(path) == lib.Path {
				counts["MovieCount"]++
			} else {
				return nil
			}
			counts["ItemCount"]++
			return nil
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"MovieCount":      counts["MovieCount"],
		"SeriesCount":     counts["SeriesCount"],
		"EpisodeCount":    counts["EpisodeCount"],
		"ArtistCount":     counts["ArtistCount"],
		"ProgramCount":    counts["ProgramCount"],
		"TrailerCount":    counts["TrailerCount"],
		"SongCount":       counts["SongCount"],
		"AlbumCount":      counts["AlbumCount"],
		"MusicVideoCount": counts["MusicVideoCount"],
		"BoxSetCount":     counts["BoxSetCount"],
		"BookCount":       counts["BookCount"],
		"ItemCount":       counts["ItemCount"],
	})
}

func (s *Server) jfItemImages(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemId")
	images := make([]map[string]interface{}, 0, 2)
	addImage := func(imageType, tag string) {
		images = append(images, map[string]interface{}{
			"ImageType":  imageType,
			"ImageIndex": 0,
			"ImageTag":   tag,
			"Path":       "/Items/" + itemID + "/Images/" + imageType,
		})
	}
	if idx, ok := s.parseLibraryID(itemID, s.config.Libraries); ok {
		if media.FindImage(s.config.Libraries[idx].Path, "Primary") != "" {
			addImage("Primary", "primary")
		}
		if media.FindImage(s.config.Libraries[idx].Path, "Backdrop") != "" {
			addImage("Backdrop", "backdrop")
		}
	} else if path, err := media.ItemPath(itemID); err == nil {
		if info, statErr := os.Stat(path); statErr == nil {
			if info.IsDir() {
				if media.FindImage(path, "Primary") != "" || media.FindImage(filepath.Dir(path), "Primary") != "" {
					addImage("Primary", "primary")
				}
				if media.FindImage(path, "Backdrop") != "" || media.FindImage(filepath.Dir(path), "Backdrop") != "" {
					addImage("Backdrop", "backdrop")
				}
			} else {
				if media.FindImageForVideo(path, "Primary") != "" {
					addImage("Primary", "primary")
				}
			}
		}
	}
	if len(images) == 0 {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}
	respondJSON(w, http.StatusOK, images)
}

func (s *Server) jfMetadataEditor(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemId")
	item := map[string]interface{}{"Id": itemID}
	libs := s.librariesForRequest(r)
	if idx, ok := s.parseLibraryID(itemID, libs); ok {
		item = map[string]interface{}{
			"Name":     libs[idx].FriendlyName,
			"Id":       itemID,
			"Type":     "CollectionFolder",
			"IsFolder": true,
		}
	} else if path, err := media.ItemPath(itemID); err == nil && media.IsPathAllowed(path, libs) {
		if info, statErr := os.Stat(path); statErr == nil {
			item = s.buildJellyfinItem(path, info, findLibraryIndex(path, libs), libs)
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Item":                  item,
		"ParentalRatingOptions": []interface{}{},
		"Countries":             []interface{}{},
		"Cultures":              []interface{}{},
		"ExternalIdInfos":       externalIDInfoList(),
	})
}

func (s *Server) jfExternalIdInfos(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, externalIDInfoList())
}

func externalIDInfoList() []map[string]interface{} {
	return []map[string]interface{}{
		{"Name": "TheMovieDb", "Key": "Tmdb", "Type": "Movie", "UrlFormatString": "https://www.themoviedb.org/movie/{0}"},
		{"Name": "IMDb", "Key": "Imdb", "Type": "Movie", "UrlFormatString": "https://www.imdb.com/title/{0}"},
		{"Name": "TheTVDB", "Key": "Tvdb", "Type": "Series", "UrlFormatString": "https://thetvdb.com/dereferrer/series/{0}"},
		{"Name": "AniList", "Key": "AniList", "Type": "Series", "UrlFormatString": "https://anilist.co/anime/{0}"},
	}
}

func (s *Server) jfNamedStubItem(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		name = r.PathValue("genreName")
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Name": name,
		"Id":   stableID("named-" + name),
	})
}

func (s *Server) jfSimilarItems(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemId")
	libs := s.librariesForRequest(r)
	path, err := media.ItemPath(itemID)
	if err != nil || !media.IsPathAllowed(path, libs) {
		s.jfEmptyItems(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		s.jfEmptyItems(w, r)
		return
	}

	libIndex := findLibraryIndex(path, libs)
	if libIndex < 0 {
		s.jfEmptyItems(w, r)
		return
	}
	base := s.buildJellyfinItem(path, info, libIndex, libs)
	baseType, _ := base["Type"].(string)
	baseGenres := stringSetFromItem(base["Genres"])
	limit := queryLimit(r, 12)

	candidates := s.collectItemsRecursive(libs[libIndex].Path, libIndex, baseType, "", libs)
	items := make([]map[string]interface{}, 0, limit)
	for _, item := range candidates {
		if item["Id"] == itemID {
			continue
		}
		if len(baseGenres) > 0 && !hasAnyString(baseGenres, item["Genres"]) {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	s.patchItemsUserData(items, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"StartIndex":       0,
		"TotalRecordCount": len(items),
	})
}

func (s *Server) jfMovieRecommendations(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	limit := queryLimit(r, 12)
	items := make([]map[string]interface{}, 0, limit)
	for i, lib := range libs {
		if lib.ContentType == "tvseries" || lib.ContentType == "anime" {
			continue
		}
		for _, item := range s.collectItemsRecursive(lib.Path, i, "Movie", "", libs) {
			items = append(items, item)
			if len(items) >= limit {
				break
			}
		}
		if len(items) >= limit {
			break
		}
	}
	s.patchItemsUserData(items, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":              items,
		"RecommendationType": "SimilarToRecentlyPlayed",
	})
}

func (s *Server) jfNextUp(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	limit := queryLimit(r, 24)
	seriesID := queryParam(r, "seriesId", "SeriesId")
	items := make([]map[string]interface{}, 0, limit)
	for _, lib := range libs {
		if lib.ContentType != "tvseries" && lib.ContentType != "anime" {
			continue
		}
		entries, err := os.ReadDir(lib.Path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			showPath := filepath.Join(lib.Path, entry.Name())
			showID := media.ItemID(showPath)
			if seriesID != "" && seriesID != showID {
				continue
			}
			episodes := make([]map[string]interface{}, 0)
			s.collectEpisodes(showPath, showID, showPath, &episodes)
			if len(episodes) == 0 {
				continue
			}
			s.patchItemsUserData(episodes, r)
			for _, ep := range episodes {
				if ud, ok := ep["UserData"].(map[string]interface{}); ok {
					if played, _ := ud["Played"].(bool); played {
						continue
					}
				}
				items = append(items, ep)
				break
			}
			if len(items) >= limit {
				break
			}
		}
		if len(items) >= limit {
			break
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"StartIndex":       0,
		"TotalRecordCount": len(items),
	})
}

// jfLiveTVInfo gates the Live TV section in clients. It is enabled only when
// the user can see at least one TV library with parsed channels; the non-empty
// Services list is what makes clients reveal their Live TV UI.
func (s *Server) jfLiveTVInfo(w http.ResponseWriter, r *http.Request) {
	enabled := len(s.visibleTVLibraries(r)) > 0
	services := []interface{}{}
	if enabled {
		services = append(services, map[string]interface{}{
			"Name":               serverName,
			"HomePageUrl":        "",
			"Status":             "Ok",
			"StatusMessage":      "",
			"Version":            "1.0",
			"HasUpdateAvailable": false,
			"IsVisible":          true,
			"Tuners":             []interface{}{},
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Services":     services,
		"IsEnabled":    enabled,
		"EnabledUsers": []interface{}{},
	})
}

func (s *Server) jfDevices(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            []interface{}{},
		"TotalRecordCount": 0,
		"StartIndex":       0,
	})
}

func (s *Server) jfDeviceInfo(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) jfDeviceOptions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"CustomName": "",
	})
}

func (s *Server) jfBitrateTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) jfVirtualFolders(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	items := make([]map[string]interface{}, 0, len(libs))
	for i, lib := range libs {
		items = append(items, map[string]interface{}{
			"Name":            lib.FriendlyName,
			"Locations":       []string{lib.Path},
			"CollectionType":  lib.ContentType,
			"ItemId":          libraryID(i),
			"RefreshProgress": 0,
		})
	}
	respondJSON(w, http.StatusOK, items)
}

func (s *Server) jfPhysicalPaths(w http.ResponseWriter, r *http.Request) {
	libs := s.librariesForRequest(r)
	paths := make([]string, 0, len(libs))
	for _, lib := range libs {
		paths = append(paths, lib.Path)
	}
	respondJSON(w, http.StatusOK, paths)
}

func (s *Server) jfLocalizationCountries(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]string{{"Name": "United States", "DisplayName": "United States", "TwoLetterISORegionName": "US", "ThreeLetterISORegionName": "USA"}})
}

func (s *Server) jfLocalizationCultures(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]string{{"Name": "en-US", "DisplayName": "English (United States)", "ThreeLetterISOLanguageName": "eng", "TwoLetterISOLanguageName": "en"}})
}

func (s *Server) jfLocalizationOptions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Countries": []map[string]string{{"Name": "United States", "DisplayName": "United States", "TwoLetterISORegionName": "US", "ThreeLetterISORegionName": "USA"}},
		"Cultures":  []map[string]string{{"Name": "en-US", "DisplayName": "English (United States)", "ThreeLetterISOLanguageName": "eng", "TwoLetterISOLanguageName": "en"}},
	})
}

func (s *Server) jfAuthProviders(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]string{{
		"Name": "Default",
		"Id":   "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
	}})
}

func (s *Server) jfPasswordResetProviders(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]string{{
		"Name": "Default",
		"Id":   "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
	}})
}

func (s *Server) jfQuickConnectEnabled(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, false)
}

func (s *Server) jfQuickConnectInitiate(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Error": "QuickConnect is not available",
	})
}

func (s *Server) jfQuickConnectUnavailable(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Authenticated": false,
		"Error":         "QuickConnect is not available",
	})
}

func (s *Server) jfForgotPassword(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Action": "ContactAdmin",
	})
}

func (s *Server) jfForgotPasswordPin(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Success": false,
	})
}

func (s *Server) jfAuthKey(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"AccessToken": "",
		"Id":          stableID("auth-key"),
	})
}

func (s *Server) jfStartupConfiguration(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ServerName":                serverName,
		"UICulture":                 "en-US",
		"MetadataCountryCode":       "US",
		"PreferredMetadataLanguage": "en",
		"EnableRemoteAccess":        true,
		"IsStartupWizardCompleted":  true,
	})
}

func (s *Server) jfStartupUser(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Name":     usernameFromContext(r),
		"Password": "",
	})
}

func (s *Server) jfScheduledTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Id":                        taskID,
		"Name":                      taskID,
		"State":                     "Idle",
		"CurrentProgressPercentage": nil,
		"Triggers":                  []interface{}{},
	})
}

func (s *Server) jfScheduledTasks(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []map[string]interface{}{
		s.scheduledTaskDTO("ScanLibrary", "Scan Media Library"),
	})
}

func (s *Server) scheduledTaskDTO(id, name string) map[string]interface{} {
	return map[string]interface{}{
		"Id":                        id,
		"Name":                      name,
		"Category":                  "Library",
		"State":                     "Idle",
		"CurrentProgressPercentage": nil,
		"Triggers":                  []interface{}{},
		"Description":               "Refreshes the RMS media item index.",
		"IsHidden":                  false,
		"Key":                       id,
	}
}

func (s *Server) jfRunScheduledTask(w http.ResponseWriter, r *http.Request) {
	s.jfRunScheduledTaskByID(w, r, r.PathValue("taskId"))
}

func (s *Server) jfRunScheduledTaskByID(w http.ResponseWriter, _ *http.Request, taskID string) {
	if strings.EqualFold(taskID, "ScanLibrary") || strings.EqualFold(taskID, "RefreshLibrary") {
		s.rescanLibraries()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfRefreshLibrary(w http.ResponseWriter, r *http.Request) {
	s.rescanLibraries()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) jfPackageInfo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Name":        name,
		"Description": "",
		"Versions":    []interface{}{},
	})
}

func queryLimit(r *http.Request, fallback int) int {
	for _, name := range []string{"limit", "Limit"} {
		if raw := r.URL.Query().Get(name); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				return n
			}
		}
	}
	return fallback
}

func stringSetFromItem(value interface{}) map[string]bool {
	set := map[string]bool{}
	switch v := value.(type) {
	case []string:
		for _, s := range v {
			set[s] = true
		}
	case []interface{}:
		for _, raw := range v {
			if s, ok := raw.(string); ok {
				set[s] = true
			}
		}
	}
	return set
}

func hasAnyString(set map[string]bool, value interface{}) bool {
	switch v := value.(type) {
	case []string:
		for _, s := range v {
			if set[s] {
				return true
			}
		}
	case []interface{}:
		for _, raw := range v {
			if s, ok := raw.(string); ok && set[s] {
				return true
			}
		}
	}
	return false
}
