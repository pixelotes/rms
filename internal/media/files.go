package media

import (
	"os"
	"path/filepath"
	"strings"

	"raspberry-media-server/internal/config"
)

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".m4v": true, ".ts": true, ".m2ts": true, ".wmv": true,
	".flv": true, ".webm": true, ".ogv": true, ".3gp": true,
	".mpg": true, ".mpeg": true, ".divx": true, ".rmvb": true,
	".vob": true, ".iso": true,
}

// IsVideoFile returns true when the filename has a known video extension.
func IsVideoFile(name string) bool {
	return videoExts[strings.ToLower(filepath.Ext(name))]
}

// IsPathAllowed returns true if path is under at least one of the given libraries.
func IsPathAllowed(path string, libs []config.Library) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, lib := range libs {
		absLib, err := filepath.Abs(lib.Path)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absLib+"/") || absPath == absLib {
			return true
		}
	}
	return false
}

// FindVideoFile returns the path of the first video file in dir, or "".
func FindVideoFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && IsVideoFile(e.Name()) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// imageSearchPaths returns candidate filenames for a given Jellyfin image type.
func imageSearchPaths(imageType string) []string {
	switch strings.ToLower(imageType) {
	case "primary", "poster":
		return []string{
			"poster.jpg", "poster.jpeg", "poster.png", "poster.webp",
			"folder.jpg", "folder.jpeg", "folder.png",
			"movie.jpg", "movie.png",
			"default.jpg", "default.png",
		}
	case "backdrop", "fanart", "background":
		return []string{
			"fanart.jpg", "fanart.jpeg", "fanart.png",
			"backdrop.jpg", "backdrop.png",
			"background.jpg", "background.png",
		}
	case "logo", "clearlogo":
		return []string{
			"clearlogo.png", "logo.png", "clearlogo.svg", "logo.svg",
		}
	case "thumb", "landscape":
		return []string{
			"thumb.jpg", "thumb.png", "landscape.jpg", "landscape.png",
		}
	case "banner":
		return []string{"banner.jpg", "banner.png"}
	default:
		return []string{}
	}
}

// FindImage returns the path of the first matching image for the given type in dir, or "".
func FindImage(dir string, imageType string) string {
	for _, name := range imageSearchPaths(imageType) {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// FindImageForVideo returns a thumbnail image alongside a video file, or "".
// Kodi stores thumbnails as <videoname>-thumb.jpg next to the video file.
func FindImageForVideo(videoPath string, imageType string) string {
	if strings.ToLower(imageType) != "primary" && strings.ToLower(imageType) != "thumb" {
		return ""
	}
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	candidates := []string{
		base + "-thumb.jpg",
		base + "-thumb.jpeg",
		base + "-thumb.png",
		base + ".jpg",
		base + ".jpeg",
		base + ".png",
	}
	for _, c := range candidates {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
