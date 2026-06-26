package media

import (
	"os"
	"path/filepath"
	"strings"

	"raspberry-media-server/internal/config"
)

// BrowseMeta holds human-readable metadata for a file or folder.
type BrowseMeta struct {
	Title         string   `json:"title,omitempty"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Year          int      `json:"year,omitempty"`
	Plot          string   `json:"plot,omitempty"`
	Rating        float64  `json:"rating,omitempty"`
	Runtime       int      `json:"runtime,omitempty"` // minutes
	Studio        string   `json:"studio,omitempty"`
	Genres        []string `json:"genres,omitempty"`
}

// BrowseFolder describes the currently open directory.
type BrowseFolder struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Backdrop string      `json:"backdrop,omitempty"` // URL
	Logo     string      `json:"logo,omitempty"`     // URL
	Metadata *BrowseMeta `json:"metadata,omitempty"`
}

// BrowseItem represents one entry returned by the browse API.
type BrowseItem struct {
	Name         string      `json:"name"`
	FriendlyName string      `json:"friendly_name,omitempty"`
	Path         string      `json:"path"`
	IsDir        bool        `json:"is_dir"`
	Icon         string      `json:"icon,omitempty"`        // directory poster URL
	Thumbnail    string      `json:"thumbnail,omitempty"`   // video thumb URL
	StreamType   string      `json:"stream_type,omitempty"` // "hls" for live TV channels; empty = file/VOD
	Metadata     *BrowseMeta `json:"metadata,omitempty"`
}

// BrowseResponse is the JSON shape returned by GET /api/v1/browse.
type BrowseResponse struct {
	CurrentFolder *BrowseFolder `json:"current_folder"`
	Items         []BrowseItem  `json:"items"`
}

// BrowseLibraries returns the top-level list of configured libraries.
func BrowseLibraries(libs []config.Library) BrowseResponse {
	items := make([]BrowseItem, 0, len(libs))
	for _, lib := range libs {
		item := BrowseItem{
			Name:         lib.FriendlyName,
			FriendlyName: lib.FriendlyName,
			Path:         lib.Path,
			IsDir:        true,
		}
		if FindImage(lib.Path, "Primary") != "" {
			item.Icon = jellyfinImageURL(lib.Path, "Primary")
		}
		items = append(items, item)
	}
	return BrowseResponse{Items: items}
}

// BrowseDirectory returns the contents of a directory along with its metadata.
// Returns an empty BrowseResponse (CurrentFolder == nil, Items empty) when path
// is not within any allowed library.
func BrowseDirectory(path string, libs []config.Library) BrowseResponse {
	if !IsPathAllowed(path, libs) {
		return BrowseResponse{}
	}

	folder := &BrowseFolder{
		Path: path,
		Name: filepath.Base(path),
	}

	if nfo, err := ParseNFO(path); err == nil {
		folder.Metadata = nfoToMeta(nfo)
	}
	if FindImage(path, "Backdrop") != "" {
		folder.Backdrop = jellyfinImageURL(path, "Backdrop")
	}
	if FindImage(path, "Logo") != "" {
		folder.Logo = jellyfinImageURL(path, "Logo")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return BrowseResponse{CurrentFolder: folder, Items: []BrowseItem{}}
	}

	items := make([]BrowseItem, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(path, name)

		if e.IsDir() {
			item := BrowseItem{Name: name, Path: fullPath, IsDir: true}
			if FindImage(fullPath, "Primary") != "" {
				item.Icon = jellyfinImageURL(fullPath, "Primary")
			}
			if nfo, err := ParseNFO(fullPath); err == nil {
				item.Metadata = nfoToMeta(nfo)
			}
			items = append(items, item)
		} else if IsVideoFile(name) {
			item := BrowseItem{Name: name, Path: fullPath, IsDir: false}
			if FindImageForVideo(fullPath, "Primary") != "" {
				item.Thumbnail = jellyfinImageURL(fullPath, "Primary")
			}
			if nfo, err := ParseEpisodeNFO(fullPath); err == nil && (nfo.Title != "" || nfo.Runtime > 0) {
				item.Metadata = episodeNFOToMeta(nfo)
			}
			items = append(items, item)
		}
	}

	return BrowseResponse{CurrentFolder: folder, Items: items}
}

// jellyfinImageURL returns a Jellyfin-compatible image URL for an item.
// The URL is resolved by jfGetItemImage via media.ItemPath(itemId).
func jellyfinImageURL(itemPath, imageType string) string {
	return "/Items/" + ItemID(itemPath) + "/Images/" + imageType
}

func nfoToMeta(nfo NFOData) *BrowseMeta {
	if nfo.Title == "" && nfo.Plot == "" && nfo.Year == 0 {
		return nil
	}
	return &BrowseMeta{
		Title:         nfo.Title,
		OriginalTitle: nfo.OriginalTitle,
		Year:          nfo.Year,
		Plot:          nfo.Plot,
		Rating:        nfo.Rating,
		Runtime:       nfo.Runtime,
		Studio:        nfo.Studio,
		Genres:        nfo.Genres,
	}
}

func episodeNFOToMeta(nfo EpisodeNFOData) *BrowseMeta {
	m := &BrowseMeta{
		Title:   nfo.Title,
		Year:    nfo.Year,
		Plot:    nfo.Plot,
		Rating:  nfo.Rating,
		Runtime: nfo.Runtime,
		Genres:  nfo.Genres,
	}
	if nfo.StreamDetails != nil && nfo.StreamDetails.DurationSeconds > 0 {
		m.Runtime = nfo.StreamDetails.DurationSeconds / 60
	}
	return m
}
