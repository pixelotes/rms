package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type KitsuClient struct {
	httpClient *http.Client
}

func NewKitsuClient() *KitsuClient {
	return &KitsuClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (k *KitsuClient) SearchAnime(title string) (*TVShowResult, error) {
	searchURL := "https://kitsu.io/api/edge/anime?filter[text]=" + url.QueryEscape(title) + "&page[limit]=5&include=genres"

	var searchResp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				CanonicalTitle string `json:"canonicalTitle"`
				Titles         struct {
					En   string `json:"en"`
					EnJp string `json:"en_jp"`
				} `json:"titles"`
				Synopsis      string  `json:"synopsis"`
				StartDate     string  `json:"startDate"`
				Status        string  `json:"status"`
				EpisodeCount  int     `json:"episodeCount"`
				EpisodeLength int     `json:"episodeLength"`
				AverageRating string  `json:"averageRating"`
				PosterImage   *struct {
					Large    string `json:"large"`
					Original string `json:"original"`
				} `json:"posterImage"`
				CoverImage *struct {
					Large    string `json:"large"`
					Original string `json:"original"`
				} `json:"coverImage"`
			} `json:"attributes"`
		} `json:"data"`
		Included []struct {
			Type       string `json:"type"`
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"included"`
	}

	if err := k.get(searchURL, &searchResp); err != nil {
		return nil, err
	}
	if len(searchResp.Data) == 0 {
		return nil, fmt.Errorf("no results found for '%s'", title)
	}

	anime := searchResp.Data[0]
	attr := anime.Attributes

	name := attr.Titles.En
	if name == "" {
		name = attr.CanonicalTitle
	}

	var posterURL string
	if attr.PosterImage != nil {
		posterURL = attr.PosterImage.Large
		if posterURL == "" {
			posterURL = attr.PosterImage.Original
		}
	}

	var backdropURL string
	if attr.CoverImage != nil {
		backdropURL = attr.CoverImage.Large
		if backdropURL == "" {
			backdropURL = attr.CoverImage.Original
		}
	}

	var rating float64
	if attr.AverageRating != "" {
		if r, err := strconv.ParseFloat(attr.AverageRating, 64); err == nil {
			rating = r / 10.0
		}
	}

	result := &TVShowResult{
		ID:          anime.ID,
		Title:       name,
		Overview:    attr.Synopsis,
		PosterURL:   posterURL,
		BackdropURL: backdropURL,
		Rating:      rating,
		Status:      attr.Status,
		Seasons:     make(map[int][]Episode),
	}

	if attr.StartDate != "" {
		if t, err := time.Parse("2006-01-02", attr.StartDate); err == nil {
			result.Year = t.Year()
		}
	}

	for _, inc := range searchResp.Included {
		if inc.Type == "genres" {
			result.Genres = append(result.Genres, inc.Attributes.Name)
		}
	}

	// Fetch episodes
	k.fetchEpisodes(anime.ID, attr.EpisodeCount, result)

	return result, nil
}

func (k *KitsuClient) fetchEpisodes(animeID string, episodeCount int, result *TVShowResult) {
	const pageSize = 20
	offset := 0

	for {
		epURL := fmt.Sprintf("https://kitsu.io/api/edge/anime/%s/episodes?page[limit]=%d&page[offset]=%d", animeID, pageSize, offset)

		var epResp struct {
			Data []struct {
				Attributes struct {
					Number         int    `json:"number"`
					SeasonNumber   int    `json:"seasonNumber"`
					CanonicalTitle string `json:"canonicalTitle"`
					Airdate        string `json:"airdate"`
					Length         int    `json:"length"`
				} `json:"attributes"`
			} `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}

		if err := k.get(epURL, &epResp); err != nil || len(epResp.Data) == 0 {
			break
		}

		for _, ep := range epResp.Data {
			season := ep.Attributes.SeasonNumber
			if season == 0 {
				season = 1
			}
			title := ep.Attributes.CanonicalTitle
			if title == "" {
				title = fmt.Sprintf("Episode %d", ep.Attributes.Number)
			}
			result.Seasons[season] = append(result.Seasons[season], Episode{
				Number:  ep.Attributes.Number,
				Title:   title,
				AirDate: ep.Attributes.Airdate,
				Runtime: ep.Attributes.Length,
			})
		}

		offset += pageSize
		if offset >= epResp.Meta.Count {
			break
		}
	}
}

func (k *KitsuClient) get(rawURL string, target interface{}) error {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.api+json")

	resp, err := k.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Kitsu API error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
