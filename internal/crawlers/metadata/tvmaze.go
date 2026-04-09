package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type TVmazeClient struct {
	httpClient *http.Client
}

func NewTVmazeClient() *TVmazeClient {
	return &TVmazeClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *TVmazeClient) SearchTVShow(title string) (*TVShowResult, error) {
	searchURL := "https://api.tvmaze.com/search/shows?q=" + url.QueryEscape(title)

	var searchData []struct {
		Show struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			Premiered string `json:"premiered"`
			Status    string `json:"status"`
			Summary   string `json:"summary"`
			Image     struct {
				Original string `json:"original"`
			} `json:"image"`
			Rating struct {
				Average float64 `json:"average"`
			} `json:"rating"`
			Genres []string `json:"genres"`
			Network *struct {
				Name string `json:"name"`
			} `json:"network"`
		} `json:"show"`
	}

	resp, err := t.httpClient.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TVmaze search failed: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchData); err != nil {
		return nil, err
	}
	if len(searchData) == 0 {
		return nil, fmt.Errorf("no results found for '%s'", title)
	}

	show := searchData[0].Show

	// Fetch episodes
	infoURL := fmt.Sprintf("https://api.tvmaze.com/shows/%d?embed=episodes", show.ID)
	resp2, err := t.httpClient.Get(infoURL)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	var showFull struct {
		Embedded struct {
			Episodes []struct {
				Season  int    `json:"season"`
				Number  int    `json:"number"`
				Name    string `json:"name"`
				Airdate string `json:"airdate"`
				Runtime int    `json:"runtime"`
			} `json:"episodes"`
		} `json:"_embedded"`
	}

	if resp2.StatusCode == http.StatusOK {
		json.NewDecoder(resp2.Body).Decode(&showFull)
	}

	result := &TVShowResult{
		ID:        fmt.Sprintf("%d", show.ID),
		Title:     show.Name,
		Overview:  stripHTML(show.Summary),
		PosterURL: show.Image.Original,
		Rating:    show.Rating.Average,
		Status:    show.Status,
		Genres:    show.Genres,
		Seasons:   make(map[int][]Episode),
	}

	if show.Network != nil {
		result.Studio = show.Network.Name
	}

	if show.Premiered != "" {
		if t, err := time.Parse("2006-01-02", show.Premiered); err == nil {
			result.Year = t.Year()
		}
	}

	for _, ep := range showFull.Embedded.Episodes {
		result.Seasons[ep.Season] = append(result.Seasons[ep.Season], Episode{
			Number:  ep.Number,
			Title:   ep.Name,
			AirDate: ep.Airdate,
			Runtime: ep.Runtime,
		})
	}

	return result, nil
}

func stripHTML(s string) string {
	// Simple HTML tag stripper
	out := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
		} else if s[i] == '>' {
			inTag = false
		} else if !inTag {
			out = append(out, s[i])
		}
	}
	return string(out)
}
