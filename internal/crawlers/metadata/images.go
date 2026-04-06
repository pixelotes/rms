package metadata

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// imageSpec maps artwork type to filename.
var imageSpecs = []struct {
	Name string // filename to save as
	Type string // "poster", "backdrop", "logo"
}{
	{"poster.jpg", "poster"},
	{"folder.jpg", "poster"},
	{"fanart.jpg", "backdrop"},
	{"backdrop.jpg", "backdrop"},
	{"logo.png", "logo"},
}

// DownloadImages downloads poster, backdrop, and logo for a movie or show directory.
// It skips images that already exist.
func DownloadImages(dir string, posterURL, backdropURL, logoURL string) int {
	urls := map[string]string{
		"poster":   posterURL,
		"backdrop": backdropURL,
		"logo":     logoURL,
	}

	downloaded := 0
	for _, spec := range imageSpecs {
		dest := filepath.Join(dir, spec.Name)
		if _, err := os.Stat(dest); err == nil {
			continue // Already exists
		}

		imgURL := urls[spec.Type]
		if imgURL == "" {
			continue
		}

		if err := downloadFile(imgURL, dest); err != nil {
			fmt.Printf("  [!] Failed to download %s: %v\n", spec.Name, err)
			continue
		}
		downloaded++
		fmt.Printf("  [+] Downloaded: %s\n", spec.Name)
	}

	return downloaded
}

func downloadFile(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dest)
		return err
	}

	f.Sync()
	return f.Close()
}
