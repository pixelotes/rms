package subtitles

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errNoAPIKeyErr = errors.New(errNoAPIKey)

const (
	baseURL     = "https://api.opensubtitles.com/api/v1"
	downloadURL = "https://api.opensubtitles.com/api/v1/download"
	userAgent   = "rms-subcrawler v1.0"
	errNoAPIKey = "OpenSubtitles API key not configured"
)

type Client struct {
	apiKey      string
	languages   []string
	httpClient  *http.Client
	rateLimiter *rateLimiter
}

// SearchResult is a single subtitle candidate returned by OpenSubtitles,
// with the metadata needed to make an informed choice in the UI.
type SearchResult struct {
	Language        string  `json:"language"`
	FileID          int     `json:"file_id"`
	FileName        string  `json:"file_name"`
	Release         string  `json:"release"`
	Uploader        string  `json:"uploader"`
	Downloads       int     `json:"downloads"`
	Rating          float64 `json:"rating"`
	Votes           int     `json:"votes"`
	FPS             float64 `json:"fps"`
	HD              bool    `json:"hd"`
	HearingImpaired bool    `json:"hearing_impaired"`
	MovieHashMatch  bool    `json:"moviehash_match"`
	FromTrusted     bool    `json:"from_trusted"`
	AITranslated    bool    `json:"ai_translated"`
	MachineTrans    bool    `json:"machine_translated"`
	UploadDate      string  `json:"upload_date"`
}

// SearchOpts controls how subtitle candidates are looked up.
type SearchOpts struct {
	VideoPath string   // used for hash + filename fallback
	Query     string   // optional override; empty derives from VideoPath
	Languages []string // optional override; empty uses client default
}

type osResponse struct {
	Data []struct {
		Attributes struct {
			Language         string  `json:"language"`
			DownloadCount    int     `json:"download_count"`
			HearingImpaired  bool    `json:"hearing_impaired"`
			HD               bool    `json:"hd"`
			FPS              float64 `json:"fps"`
			Votes            int     `json:"votes"`
			Ratings          float64 `json:"ratings"`
			FromTrusted      bool    `json:"from_trusted"`
			UploadDate       string  `json:"upload_date"`
			AITranslated     bool    `json:"ai_translated"`
			MachineTranslated bool   `json:"machine_translated"`
			Release          string  `json:"release"`
			MovieHashMatch   bool    `json:"moviehash_match"`
			Uploader         struct {
				Name string `json:"name"`
			} `json:"uploader"`
			Files []struct {
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

// ProcessFile searches and downloads subtitles for a single video file
// (batch crawler entry point: hash-then-filename, missing languages only).
// Returns the number of subtitles downloaded, the number of missing languages, and any error.
func (c *Client) ProcessFile(videoPath string) (int, int, error) {
	if c.apiKey == "" {
		return 0, 0, errNoAPIKeyErr
	}

	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	videoDir := filepath.Dir(videoPath)

	missing := c.missingLanguages(videoDir, videoBase)
	if len(missing) == 0 {
		return 0, 0, nil
	}

	subs, err := c.Search(SearchOpts{VideoPath: videoPath, Languages: missing})
	if err != nil {
		return 0, len(missing), err
	}

	downloaded := 0
	for _, lang := range missing {
		sub := findForLanguage(subs, lang)
		if sub == nil {
			continue
		}

		destPath := filepath.Join(videoDir, videoBase+"."+lang+".srt")
		if _, err := os.Stat(destPath); err == nil {
			continue
		}

		if err := c.Download(sub.FileID, destPath); err != nil {
			fmt.Printf("  [!] Failed to download %s subtitle: %v\n", lang, err)
			continue
		}
		downloaded++
		fmt.Printf("  [+] Downloaded: %s\n", filepath.Base(destPath))
	}

	return downloaded, len(missing), nil
}

func (c *Client) missingLanguages(dir, videoBase string) []string {
	var missing []string
	for _, lang := range c.languages {
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

// Search returns candidate subtitles for a video. It tries the OSDb movie hash
// first (when VideoPath is set) and falls back to a cleaned filename query.
// If opts.Query is provided, it is used directly (no hash, no filename derivation).
func (c *Client) Search(opts SearchOpts) ([]SearchResult, error) {
	if c.apiKey == "" {
		return nil, errNoAPIKeyErr
	}

	langs := opts.Languages
	if len(langs) == 0 {
		langs = c.languages
	}
	langStr := strings.Join(langs, ",")

	// Manual query override: skip hash, search by query directly.
	if strings.TrimSpace(opts.Query) != "" {
		return c.doSearch(url.Values{
			"query":     {opts.Query},
			"languages": {langStr},
		})
	}

	if opts.VideoPath == "" {
		return nil, fmt.Errorf("Search requires VideoPath or Query")
	}

	// Hash first.
	if hash, err := computeMovieHash(opts.VideoPath); err == nil {
		subs, err := c.doSearch(url.Values{
			"moviehash": {hash},
			"languages": {langStr},
		})
		if err == nil && len(subs) > 0 {
			return subs, nil
		}
	}

	// Filename fallback.
	name := strings.TrimSuffix(filepath.Base(opts.VideoPath), filepath.Ext(opts.VideoPath))
	return c.doSearch(url.Values{
		"query":     {cleanQuery(name)},
		"languages": {langStr},
	})
}

func (c *Client) doSearch(params url.Values) ([]SearchResult, error) {
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

	results := make([]SearchResult, 0, len(osResp.Data))
	for _, item := range osResp.Data {
		a := item.Attributes
		if len(a.Files) == 0 {
			continue
		}
		results = append(results, SearchResult{
			Language:        a.Language,
			FileID:          a.Files[0].FileID,
			FileName:        a.Files[0].FileName,
			Release:         a.Release,
			Uploader:        a.Uploader.Name,
			Downloads:       a.DownloadCount,
			Rating:          a.Ratings,
			Votes:           a.Votes,
			FPS:             a.FPS,
			HD:              a.HD,
			HearingImpaired: a.HearingImpaired,
			MovieHashMatch:  a.MovieHashMatch,
			FromTrusted:     a.FromTrusted,
			AITranslated:    a.AITranslated,
			MachineTrans:    a.MachineTranslated,
			UploadDate:      a.UploadDate,
		})
	}
	return results, nil
}

// Download fetches the subtitle with the given file_id and writes it to destPath,
// overwriting any existing file. Callers are responsible for skipping duplicates
// when that is the desired behavior.
func (c *Client) Download(fileID int, destPath string) error {
	if c.apiKey == "" {
		return errNoAPIKeyErr
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

	tmpPath := destPath + ".part"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	if _, err = io.Copy(outFile, dlRespObj.Body); err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return err
	}

	outFile.Sync()
	if err := outFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", userAgent)
}

func findForLanguage(subs []SearchResult, lang string) *SearchResult {
	for i, s := range subs {
		if s.Language == lang {
			return &subs[i]
		}
	}
	return nil
}

func cleanQuery(name string) string {
	for _, sep := range []string{"[", "(", " - "} {
		if idx := strings.Index(name, sep); idx > 0 {
			name = name[:idx]
		}
	}
	name = strings.ReplaceAll(name, ".", " ")
	return strings.TrimSpace(name)
}
