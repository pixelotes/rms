package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type AniListClient struct {
	httpClient *http.Client
}

func NewAniListClient() *AniListClient {
	return &AniListClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *AniListClient) SearchAnime(title string) (*TVShowResult, error) {
	query := `query ($search: String) {
  Page(perPage: 5) {
    media(search: $search, type: ANIME, sort: POPULARITY_DESC) {
      id
      title { romaji english }
      description(asHtml: false)
      bannerImage
      coverImage { extraLarge large }
      episodes
      genres
      studios(isMain: true) { nodes { name } }
      startDate { year }
    }
  }
}`

	body, _ := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": map[string]string{"search": title},
	})

	req, err := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AniList API error: %s", resp.Status)
	}

	var searchResp struct {
		Data struct {
			Page struct {
				Media []struct {
					ID    int `json:"id"`
					Title struct {
						English string `json:"english"`
						Romaji  string `json:"romaji"`
					} `json:"title"`
					Description string `json:"description"`
					BannerImage string `json:"bannerImage"`
					CoverImage  struct {
						ExtraLarge string `json:"extraLarge"`
						Large      string `json:"large"`
					} `json:"coverImage"`
					Episodes int      `json:"episodes"`
					Genres   []string `json:"genres"`
					Studios  struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"studios"`
					StartDate struct {
						Year int `json:"year"`
					} `json:"startDate"`
				} `json:"media"`
			} `json:"page"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}
	if len(searchResp.Data.Page.Media) == 0 {
		return nil, fmt.Errorf("no results found for '%s'", title)
	}

	anime := searchResp.Data.Page.Media[0]
	name := anime.Title.English
	if name == "" {
		name = anime.Title.Romaji
	}

	posterURL := anime.CoverImage.ExtraLarge
	if posterURL == "" {
		posterURL = anime.CoverImage.Large
	}

	result := &TVShowResult{
		ID:          strconv.Itoa(anime.ID),
		Title:       name,
		Year:        anime.StartDate.Year,
		Overview:    anime.Description,
		PosterURL:   posterURL,
		BackdropURL: anime.BannerImage,
		Genres:      anime.Genres,
		Seasons:     make(map[int][]Episode),
	}

	if len(anime.Studios.Nodes) > 0 {
		result.Studio = anime.Studios.Nodes[0].Name
	}

	// Generate episode entries
	for i := 1; i <= anime.Episodes; i++ {
		result.Seasons[1] = append(result.Seasons[1], Episode{
			Number: i,
			Title:  fmt.Sprintf("Episode %d", i),
		})
	}

	return result, nil
}
