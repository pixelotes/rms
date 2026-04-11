package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

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

	seasons := make([]map[string]interface{}, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !isSeasonDir(name) {
			continue
		}

		seasonPath := filepath.Join(showPath, name)
		seasonNum := extractSeasonNumber(name)
		seasonID := media.ItemID(seasonPath)

		seasons = append(seasons, map[string]interface{}{
			"Name":        name,
			"Id":          seasonID,
			"Type":        "Season",
			"IndexNumber": seasonNum,
			"SeriesId":    showID,
			"SeriesName":  filepath.Base(showPath),
			"IsFolder":    true,
			"ImageTags":   s.imageTagsForDir(seasonPath),
			"ParentId":    showID,
			"UserData":    defaultUserData(seasonID),
		})
	}

	sort.Slice(seasons, func(i, j int) bool {
		a, _ := seasons[i]["IndexNumber"].(int)
		b, _ := seasons[j]["IndexNumber"].(int)
		return a < b
	})

	s.patchItemsUserData(seasons, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            seasons,
		"StartIndex":       0,
		"TotalRecordCount": len(seasons),
	})
}

func (s *Server) jfGetEpisodes(w http.ResponseWriter, r *http.Request) {
	showID := mux.Vars(r)["showId"]
	seasonID := queryParam(r, "seasonId", "SeasonId")
	parentID := queryParam(r, "parentId", "ParentId")

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

	if seasonPath == "" {
		seasonPath = showPath
	}

	episodes := make([]map[string]interface{}, 0)
	s.collectEpisodes(seasonPath, showID, showPath, &episodes)

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

	s.patchItemsUserData(episodes, r)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            episodes,
		"StartIndex":       0,
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

		epID := media.ItemID(fullPath)
		ep := map[string]interface{}{
			"Name":              epName,
			"Id":                epID,
			"Type":              "Episode",
			"IndexNumber":       epNum,
			"ParentIndexNumber": epSeasonNum,
			"SeriesId":          showID,
			"SeriesName":        filepath.Base(showPath),
			"SeasonId":          media.ItemID(dir),
			"IsFolder":          false,
			"MediaType":         "Video",
			"ImageTags":         s.imageTagsForVideo(fullPath),
			"VideoType":         "VideoFile",
			"LocationType":      "FileSystem",
			"Path":              fullPath,
			"MediaSources":      s.buildMediaSources(fullPath),
			"UserData":          defaultUserData(epID),
			"DateCreated":       fileDateCreated(fullPath),
		}

		if runTimeTicks > 0 {
			ep["RunTimeTicks"] = runTimeTicks
		}

		*episodes = append(*episodes, ep)
	}
}
