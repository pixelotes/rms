package metadata

// MovieResult holds metadata for a movie from any provider.
type MovieResult struct {
	ID          string
	Title       string
	Year        int
	Overview    string
	PosterURL   string
	BackdropURL string
	Rating      float64
	Genres      []string
	Runtime     int
	Studio      string
	Tagline     string
	IMDBID      string
	TMDBID      string
}

// TVShowResult holds metadata for a TV show from any provider.
type TVShowResult struct {
	ID          string
	Title       string
	Year        int
	Overview    string
	PosterURL   string
	BackdropURL string
	Rating      float64
	Status      string
	Genres      []string
	Studio      string
	TMDBID      string
	Seasons     map[int][]Episode
}

// Episode holds metadata for a single episode.
type Episode struct {
	Number  int
	Title   string
	AirDate string
	Runtime int
}
