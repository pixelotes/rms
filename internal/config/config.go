package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

type Config struct {
	App       AppConfig     `yaml:"app"`
	Player    PlayerConfig  `yaml:"player"`
	Libraries []Library     `yaml:"libraries"`
	Users     []User        `yaml:"users"`
	Crawlers  CrawlerConfig `yaml:"crawlers"`
}

type CrawlerConfig struct {
	Subtitles SubCrawlerConfig  `yaml:"subtitles"`
	Metadata  MetaCrawlerConfig `yaml:"metadata"`
	AutoScan  AutoScanConfig    `yaml:"auto_scan"`
}

type AutoScanConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Schedule      string `yaml:"schedule"`       // "HH:MM" daily schedule (e.g. "03:00")
	IntervalHours int    `yaml:"interval_hours"` // fallback if schedule not set
	Metadata      bool   `yaml:"metadata"`
	Subtitles     bool   `yaml:"subtitles"`
	Thumbnails    bool   `yaml:"thumbnails"`
}

type SubCrawlerConfig struct {
	APIKey    string   `yaml:"api_key"`
	Languages []string `yaml:"languages"`
}

type MetaCrawlerConfig struct {
	TMDBKey             string   `yaml:"tmdb_api_key"`
	TraktID             string   `yaml:"trakt_client_id"`
	AnimeProviders      []string `yaml:"anime_providers"`
	MovieProviders      []string `yaml:"movie_providers"`
	TVSeriesProviders   []string `yaml:"tvseries_providers"`
}

type User struct {
	Username  string   `yaml:"username"`
	Password  string   `yaml:"password"`
	Libraries []string `yaml:"libraries"` // friendly_name list; empty = all
}

type AppConfig struct {
	Port            int    `yaml:"port"`
	UIEnabled       bool   `yaml:"ui_enabled"`
	UIPassword      string `yaml:"ui_password"`
	JWTSecret       string `yaml:"jwt_secret"`
	JellyfinVersion string `yaml:"jellyfin_version"`
	Debug           bool   `yaml:"debug"`
}

type PlayerConfig struct {
	StreamStrategy []string `yaml:"stream_strategy"`
}

type Library struct {
	FriendlyName      string   `yaml:"friendly_name"`
	Path              string   `yaml:"path"`
	MetadataLang      string   `yaml:"metadata_lang"`
	DownloadMetadata  YAMLBool `yaml:"download_metadata"`
	DownloadSubtitles YAMLBool `yaml:"download_subtitles"`
	ContentType       string   `yaml:"content_type"` // "movies" | "tvseries" | "anime"
	Providers         []string `yaml:"providers,omitempty"`
}

// YAMLBool handles YAML booleans that may be strings ("true"/"false") or native bools.
type YAMLBool bool

func (b *YAMLBool) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var boolVal bool
	if err := unmarshal(&boolVal); err == nil {
		*b = YAMLBool(boolVal)
		return nil
	}
	var strVal string
	if err := unmarshal(&strVal); err == nil {
		*b = YAMLBool(strVal == "true" || strVal == "yes" || strVal == "1")
		return nil
	}
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables: ${VAR} and $VAR
	expanded := envVarRegex.ReplaceAllStringFunc(string(data), func(match string) string {
		// Extract variable name from ${VAR} or $VAR
		groups := envVarRegex.FindStringSubmatch(match)
		name := groups[1] // ${VAR}
		if name == "" {
			name = groups[2] // $VAR
		}
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match // keep original if env var not set
	})

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.setDefaults()
	cfg.resolvePaths()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) resolvePaths() {
	for i, lib := range c.Libraries {
		if abs, err := filepath.Abs(lib.Path); err == nil {
			c.Libraries[i].Path = abs
		}
	}
}

func (c *Config) setDefaults() {
	if c.App.Port == 0 {
		c.App.Port = 8082
	}
	if c.App.JWTSecret == "" {
		c.App.JWTSecret = "change-me-in-production"
	}
	if c.App.JellyfinVersion == "" {
		c.App.JellyfinVersion = "10.10.7"
	}
	if len(c.Player.StreamStrategy) == 0 {
		c.Player.StreamStrategy = []string{"direct", "remux", "transcode"}
	}
	if c.Crawlers.AutoScan.IntervalHours == 0 {
		c.Crawlers.AutoScan.IntervalHours = 24
	}
	if len(c.Crawlers.Metadata.AnimeProviders) == 0 {
		c.Crawlers.Metadata.AnimeProviders = []string{"anilist"}
	}
	if len(c.Crawlers.Metadata.MovieProviders) == 0 {
		c.Crawlers.Metadata.MovieProviders = []string{"tmdb"}
	}
	if len(c.Crawlers.Metadata.TVSeriesProviders) == 0 {
		c.Crawlers.Metadata.TVSeriesProviders = []string{"tvmaze"}
	}
}

// AuthenticateUser validates credentials and returns the user.
// Falls back to the default "rms" user with ui_password if no users are configured.
func (c *Config) AuthenticateUser(username, password string) *User {
	if len(c.Users) == 0 {
		// Fallback: single default user with access to all libraries
		if password == c.App.UIPassword {
			return &User{Username: "rms", Password: c.App.UIPassword}
		}
		return nil
	}

	for _, u := range c.Users {
		if u.Username == username && u.Password == password {
			return &u
		}
	}
	return nil
}

// LibrariesForUser returns the libraries accessible to a user.
// Empty Libraries list means access to all.
func (c *Config) LibrariesForUser(username string) []Library {
	user := c.FindUser(username)
	if user == nil || len(user.Libraries) == 0 {
		return c.Libraries
	}

	allowed := make(map[string]bool, len(user.Libraries))
	for _, name := range user.Libraries {
		allowed[name] = true
	}

	var libs []Library
	for _, lib := range c.Libraries {
		if allowed[lib.FriendlyName] {
			libs = append(libs, lib)
		}
	}
	return libs
}

// FindUser returns a user by username, or nil.
func (c *Config) FindUser(username string) *User {
	if len(c.Users) == 0 {
		return &User{Username: "rms"}
	}
	for _, u := range c.Users {
		if u.Username == username {
			return &u
		}
	}
	return nil
}

func (c *Config) validate() error {
	if len(c.Users) == 0 && c.App.UIPassword == "" {
		return fmt.Errorf("either app.ui_password or users must be configured")
	}
	if len(c.Libraries) == 0 {
		return fmt.Errorf("at least one library must be configured")
	}
	for i, lib := range c.Libraries {
		if lib.Path == "" {
			return fmt.Errorf("library[%d].path is required", i)
		}
		if lib.FriendlyName == "" {
			return fmt.Errorf("library[%d].friendly_name is required", i)
		}
	}
	return nil
}
