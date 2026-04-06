package subtitles

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	baseURL     = "https://api.opensubtitles.com/api/v1"
	downloadURL = "https://api.opensubtitles.com/api/v1/download"
	userAgent   = "rms-subcrawler v1.0"
)

type Client struct {
	apiKey      string
	languages   []string
	httpClient  *http.Client
	rateLimiter *rateLimiter
}

type subtitle struct {
	Language string
	FileID   int
	FileName string
}

type osResponse struct {
	Data []struct {
		Attributes struct {
			Language string `json:"language"`
			Files    []struct {
				FileID   int    `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

type downloadResponse struct {
	Link string `json:"link"`
}

func NewClient(apiKey string, languages []string) *Client {
	if len(languages) == 0 {
		languages = []string{"en"}
	}
	return &Client{
		apiKey:      apiKey,
		languages:   languages,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		rateLimiter: newRateLimiter(5, 1*time.Minute),
	}
}

// ProcessFile searches and downloads subtitles for a single video file.
// Returns the number of subtitles downloaded.
func (c *Client) ProcessFile(videoPath string) (int, error) {
	if c.apiKey == "" {
		return 0, fmt.Errorf("OpenSubtitles API key not configured")
	}

	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	videoDir := filepath.Dir(videoPath)

	// Check which languages we already have
	missing := c.missingLanguages(videoDir, videoBase)
	if len(missing) == 0 {
		return 0, nil
	}

	// Search by hash first, then by filename
	subs, err := c.search(videoPath, missing)
	if err != nil {
		return 0, err
	}

	// Download one subtitle per missing language
	downloaded := 0
	for _, lang := range missing {
		sub := findForLanguage(subs, lang)
		if sub == nil {
			continue
		}

		ext := ".srt"
		destPath := filepath.Join(videoDir, videoBase+"."+lang+ext)

		if err := c.download(sub.FileID, destPath); err != nil {
			fmt.Printf("  [!] Failed to download %s subtitle: %v\n", lang, err)
			continue
		}
		downloaded++
		fmt.Printf("  [+] Downloaded: %s\n", filepath.Base(destPath))
	}

	return downloaded, nil
}

func (c *Client) missingLanguages(dir, videoBase string) []string {
	var missing []string
	for _, lang := range c.languages {
		// Check common patterns: video.en.srt, video-en.srt, video.eng.srt
		patterns := []string{
			videoBase + "." + lang + ".srt",
			videoBase + "-" + lang + ".srt",
		}
		found := false
		for _, p := range patterns {
			if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, lang)
		}
	}
	return missing
}

func (c *Client) search(videoPath string, languages []string) ([]subtitle, error) {
	langStr := strings.Join(languages, ",")

	// Try hash search first
	hash, err := computeMovieHash(videoPath)
	if err == nil {
		subs, err := c.doSearch(url.Values{
			"moviehash": {hash},
			"languages": {langStr},
		})
		if err == nil && len(subs) > 0 {
			return subs, nil
		}
	}

	// Fallback: search by filename
	name := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	// Clean release info from filename for better search
	query := cleanQuery(name)

	return c.doSearch(url.Values{
		"query":     {query},
		"languages": {langStr},
	})
}

func (c *Client) doSearch(params url.Values) ([]subtitle, error) {
	c.rateLimiter.wait()

	req, err := http.NewRequest("GET", baseURL+"/subtitles?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %s: %s", resp.Status, string(body))
	}

	var osResp osResponse
	if err := json.NewDecoder(resp.Body).Decode(&osResp); err != nil {
		return nil, err
	}

	var results []subtitle
	for _, item := range osResp.Data {
		if len(item.Attributes.Files) == 0 {
			continue
		}
		results = append(results, subtitle{
			Language: item.Attributes.Language,
			FileID:   item.Attributes.Files[0].FileID,
			FileName: item.Attributes.Files[0].FileName,
		})
	}
	return results, nil
}

func (c *Client) download(fileID int, destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return nil // Already exists
	}

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
		if err := c.downloadAttempt(fileID, destPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (c *Client) downloadAttempt(fileID int, destPath string) error {
	c.rateLimiter.wait()

	payload, _ := json.Marshal(map[string]int{"file_id": fileID})
	req, err := http.NewRequest("POST", downloadURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download link failed: %s - %s", resp.Status, string(body))
	}

	var dlResp downloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&dlResp); err != nil {
		return err
	}

	dlRespObj, err := c.httpClient.Get(dlResp.Link)
	if err != nil {
		return err
	}
	defer dlRespObj.Body.Close()

	if dlRespObj.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", dlRespObj.Status)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}

	if _, err = io.Copy(outFile, dlRespObj.Body); err != nil {
		outFile.Close()
		os.Remove(destPath)
		return err
	}

	outFile.Sync()
	return outFile.Close()
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
}

func findForLanguage(subs []subtitle, lang string) *subtitle {
	for i, s := range subs {
		if s.Language == lang {
			return &subs[i]
		}
	}
	return nil
}

func cleanQuery(name string) string {
	// Remove common release tags for better search
	for _, sep := range []string{"[", "(", " - "} {
		if idx := strings.Index(name, sep); idx > 0 {
			name = name[:idx]
		}
	}
	// Replace dots with spaces (scene naming)
	name = strings.ReplaceAll(name, ".", " ")
	return strings.TrimSpace(name)
}
