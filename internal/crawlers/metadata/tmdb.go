package metadata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type TMDBClient struct {
	apiKey     string
	language   string
	httpClient *http.Client
}

func NewTMDBClient(apiKey, language string) *TMDBClient {
	if language == "" {
		language = "en"
	}
	return &TMDBClient{
		apiKey:     apiKey,
		language:   language,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *TMDBClient) SearchMovie(title string, year int) (*MovieResult, error) {
	params := url.Values{
		"api_key":  {t.apiKey},
		"language": {t.language},
		"query":    {title},
	}
	if year > 0 {
		params.Add("year", strconv.Itoa(year))
	}

	var searchResp struct {
		Results []struct {
			ID          int     `json:"id"`
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			Overview    string  `json:"overview"`
			PosterPath  string  `json:"poster_path"`
			VoteAverage float64 `json:"vote_average"`
			GenreIDs    []int   `json:"genre_ids"`
		} `json:"results"`
	}

	if err := t.get("/search/movie", params, &searchResp); err != nil {
		return nil, err
	}
	if len(searchResp.Results) == 0 {
		return nil, fmt.Errorf("no results found for '%s'", title)
	}

	r := searchResp.Results[0]

	// Get full details for runtime, genres, tagline, etc.
	movie := &MovieResult{
		TMDBID:    strconv.Itoa(r.ID),
		Title:     r.Title,
		Overview:  r.Overview,
		PosterURL: tmdbImageURL(r.PosterPath, "w500"),
		Rating:    r.VoteAverage,
	}
	if r.ReleaseDate != "" {
		if t, err := time.Parse("2006-01-02", r.ReleaseDate); err == nil {
			movie.Year = t.Year()
		}
	}

	// Fetch full movie details
	t.enrichMovie(r.ID, movie)

	return movie, nil
}

func (t *TMDBClient) enrichMovie(id int, movie *MovieResult) {
	var details struct {
		Runtime int    `json:"runtime"`
		Tagline string `json:"tagline"`
		IMDBID  string `json:"imdb_id"`
		Genres  []struct {
			Name string `json:"name"`
		} `json:"genres"`
		ProductionCompanies []struct {
			Name string `json:"name"`
		} `json:"production_companies"`
	}

	path := fmt.Sprintf("/movie/%d", id)
	if err := t.get(path, url.Values{"api_key": {t.apiKey}, "language": {t.language}}, &details); err != nil {
		return
	}

	movie.Runtime = details.Runtime
	movie.Tagline = details.Tagline
	movie.IMDBID = details.IMDBID
	for _, g := range details.Genres {
		movie.Genres = append(movie.Genres, g.Name)
	}
	if len(details.ProductionCompanies) > 0 {
		movie.Studio = details.ProductionCompanies[0].Name
	}
}

// GetMovieImages returns poster, backdrop, and logo URLs.
func (t *TMDBClient) GetMovieImages(tmdbID int) (poster, backdrop, logo string) {
	var imgResp struct {
		Backdrops []struct {
			FilePath string `json:"file_path"`
		} `json:"backdrops"`
		Logos []struct {
			FilePath string `json:"file_path"`
		} `json:"logos"`
		Posters []struct {
			FilePath string `json:"file_path"`
		} `json:"posters"`
	}

	path := fmt.Sprintf("/movie/%d/images", tmdbID)
	params := url.Values{
		"api_key":                {t.apiKey},
		"include_image_language": {t.language + ",null"},
	}
	if err := t.get(path, params, &imgResp); err != nil {
		return
	}

	if len(imgResp.Posters) > 0 {
		poster = tmdbImageURL(imgResp.Posters[0].FilePath, "w500")
	}
	if len(imgResp.Backdrops) > 0 {
		backdrop = tmdbImageURL(imgResp.Backdrops[0].FilePath, "w1280")
	}
	if len(imgResp.Logos) > 0 {
		logo = tmdbImageURL(imgResp.Logos[0].FilePath, "w500")
	}
	return
}

func (t *TMDBClient) get(path string, params url.Values, target interface{}) error {
	u := "https://api.themoviedb.org/3" + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	resp, err := t.httpClient.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB API error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func tmdbImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	return "https://image.tmdb.org/t/p/" + size + path
}
