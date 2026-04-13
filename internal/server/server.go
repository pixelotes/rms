package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

type Server struct {
	config     *config.Config
	httpServer *http.Server
	router     *mux.Router
	userData   *UserDataStore
	syncQueue  *SyncQueueStore
}

func New(cfg *config.Config) *Server {
	s := &Server{config: cfg}
	s.userData = NewUserDataStore(cfg.App.UserdataPath)
	s.syncQueue = NewSyncQueueStore()
	s.router = mux.NewRouter()
	if cfg.App.Debug {
		s.router.Use(loggingMiddleware)
	}
	s.registerRoutes()
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.App.Port),
		Handler:      s.router,
		ReadTimeout:  0, // No timeout for streaming
		WriteTimeout: 0, // No timeout for streaming
		IdleTimeout:  120 * time.Second,
	}
	return s
}

func (s *Server) registerRoutes() {
	// Static files (Web UI) - conditional
	if s.config.App.UIEnabled {
		s.router.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(http.Dir("web/css"))))
		s.router.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(http.Dir("web/js"))))
		s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "web/index.html")
		})
	}

	// === RMS Native API ===
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Public
	api.HandleFunc("/login", s.handleLogin).Methods("POST")

	// Protected (JWT Bearer)
	protected := api.PathPrefix("").Subrouter()
	protected.Use(s.jwtMiddleware)
	protected.HandleFunc("/browse", s.handleBrowse).Methods("GET")
	protected.HandleFunc("/stream/{filePath:.+}", s.handleStream).Methods("GET")
	protected.HandleFunc("/subtitles/{filePath:.+}", s.handleSubtitles).Methods("GET")
	protected.HandleFunc("/subtitles-list/{filePath:.+}", s.handleSubtitlesList).Methods("GET")
	protected.HandleFunc("/images/{imageId}", s.handleImage).Methods("GET")
	protected.HandleFunc("/duration/{filePath:.+}", s.handleDuration).Methods("GET")
	protected.HandleFunc("/crawl/metadata", s.handleCrawlMetadata).Methods("POST")
	protected.HandleFunc("/crawl/subtitles", s.handleCrawlSubtitles).Methods("POST")
	protected.HandleFunc("/crawl/thumbnails", s.handleCrawlThumbnails).Methods("POST")
	protected.HandleFunc("/library/rescan", s.handleRescan).Methods("POST")

	// === Jellyfin-Compatible API ===
	jf := s.router.PathPrefix("").Subrouter()

	// Jellyfin public endpoints
	jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
	jf.HandleFunc("/system/info/public", s.jfSystemInfoPublic).Methods("GET")
	jf.HandleFunc("/Users/Public", s.jfUsersPublic).Methods("GET")
	jf.HandleFunc("/Users/AuthenticateByName", s.jfAuthenticateByName).Methods("POST")
	jf.HandleFunc("/Branding/Configuration", s.jfBrandingConfig).Methods("GET")
	jf.HandleFunc("/QuickConnect/Enabled", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, false)
	}).Methods("GET")
	jf.HandleFunc("/QuickConnect/Initiate", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{"Error": "QuickConnect is not available"})
	}).Methods("POST", "GET")

	// Images - public (clients load these as direct URLs without auth headers)
	// Some clients use lowercase "/items/" so register both variants.
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/items/{itemId}/Images/{imageType}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/items/{itemId}/Images/{imageType}/{imageIndex}", s.jfGetItemImage).Methods("GET", "HEAD")

	// Streaming - public (media players like ExoPlayer/AVPlayer use bare URLs without auth headers)
	jf.HandleFunc("/Videos/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Videos/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Audio/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Audio/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}", s.jfSubtitleStream).Methods("GET")
	jf.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}", s.jfSubtitleStream).Methods("GET")

	// Jellyfin protected endpoints
	jfAuth := s.router.PathPrefix("").Subrouter()
	jfAuth.Use(s.jellyfinAuthMiddleware)

	// System (register both cases — some clients use lowercase)
	jfAuth.HandleFunc("/System/Info", s.jfSystemInfo).Methods("GET")
	jfAuth.HandleFunc("/system/info", s.jfSystemInfo).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration/encoding", s.jfEncodingConfig).Methods("GET")
	if s.jfVersionAtLeast(10, 11) {
		jfAuth.HandleFunc("/System/Storage", s.jfSystemStorage).Methods("GET")
	}

	// Users & Items
	jfAuth.HandleFunc("/UserViews", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Views", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/Latest", s.jfGetLatest).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/Resume", s.jfGetResumeItems).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items", s.jfGetItems).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/Intros", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/LocalTrailers", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/SpecialFeatures", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}", s.jfGetItem).Methods("GET")
	jfAuth.HandleFunc("/Users/Me", s.jfGetCurrentUser).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}", s.jfGetUser).Methods("GET")
	jfAuth.HandleFunc("/Items", s.jfGetItems).Methods("GET")

	// Items sub-endpoints (must be before /Items/{itemId} catch-all)
	jfAuth.HandleFunc("/Items/Latest", s.jfGetLatest).Methods("GET")
	jfAuth.HandleFunc("/Items/Suggestions", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/Filters", s.jfGetFilters).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Similar", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Ancestors", s.jfGetAncestors).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Intros", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeMedia", s.jfThemeMedia).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeSongs", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeVideos", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/SpecialFeatures", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/LocalTrailers", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}", s.jfGetItem).Methods("GET")

	// TV Shows
	jfAuth.HandleFunc("/Shows/{showId}/Seasons", s.jfGetSeasons).Methods("GET")
	jfAuth.HandleFunc("/Shows/{showId}/Episodes", s.jfGetEpisodes).Methods("GET")
	jfAuth.HandleFunc("/Shows/NextUp", s.jfEmptyItems).Methods("GET")

	// Playback
	jfAuth.HandleFunc("/Items/{itemId}/PlaybackInfo", s.jfPlaybackInfo).Methods("POST", "GET")

	// Sessions
	jfAuth.HandleFunc("/Sessions", s.jfSessionsStub).Methods("GET")
	jfAuth.HandleFunc("/Sessions/Capabilities", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Capabilities/Full", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing", s.jfReportPlayback).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing/Progress", s.jfReportPlayback).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing/Stopped", s.jfReportPlaybackStopped).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing/Ping", s.jfSessionStub).Methods("POST")

	// Sessions (10.11+)
	if s.jfVersionAtLeast(10, 11) {
		jfAuth.HandleFunc("/Sessions/Playing/ReportStart", s.jfReportPlayback).Methods("POST")
		jfAuth.HandleFunc("/Sessions/Playing/ReportProgress", s.jfReportPlayback).Methods("POST")
		jfAuth.HandleFunc("/Sessions/Playing/ReportStopped", s.jfReportPlaybackStopped).Methods("POST")
		jfAuth.HandleFunc("/Sessions/Logout", s.jfSessionStub).Methods("DELETE", "POST")
	}

	// Client log stub (Moonfin sends crash reports here)
	jfAuth.HandleFunc("/ClientLog/Document", s.jfSessionStub).Methods("POST")

	// Resume
	jfAuth.HandleFunc("/UserItems/Resume", s.jfGetResumeItems).Methods("GET")

	// User items state
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/UserData", s.jfUpdateUserData).Methods("POST")
	jfAuth.HandleFunc("/Users/{userId}/PlayedItems/{itemId}", s.jfTogglePlayed).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Users/{userId}/FavoriteItems/{itemId}", s.jfToggleFavorite).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/PlayingItems/{itemId}", s.jfReportPlayback).Methods("POST", "DELETE")

	// Search
	jfAuth.HandleFunc("/Search/Hints", s.jfSearchHints).Methods("GET")

	// Genres, Persons, Studios
	jfAuth.HandleFunc("/Genres", s.jfGetGenres).Methods("GET")
	jfAuth.HandleFunc("/Persons", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Studios", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists", s.jfEmptyItems).Methods("GET")

	// Media segments (skip intro/credits)
	jfAuth.HandleFunc("/MediaSegments/{itemId}", s.jfMediaSegments).Methods("GET")

	// Cancel active transcoding
	jfAuth.HandleFunc("/Videos/ActiveEncodings", s.jfSessionStub).Methods("DELETE")

	// Display preferences stub
	jfAuth.HandleFunc("/DisplayPreferences/{displayPrefsId}", s.jfDisplayPrefsStub).Methods("GET")

	// LiveTv stub (Moonfin checks for live TV)
	jfAuth.HandleFunc("/LiveTv/Programs/Recommended", s.jfEmptyItems).Methods("GET")

	// Kodi SyncQueue plugin emulation (optional)
	if s.config.App.KodiSyncQueue {
		jf.HandleFunc("/Jellyfin.Plugin.KodiSyncQueue/GetPluginSettings", s.jfKodiSyncSettings).Methods("GET")
		jfAuth.HandleFunc("/Jellyfin.Plugin.KodiSyncQueue/{userId}/GetItems", s.jfKodiSyncGetItems).Methods("GET")
	}

	// WebSocket stub (Jellyfin clients poll this constantly)
	s.router.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Web UI static files
	if s.config.App.UIEnabled {
		s.router.PathPrefix("/web/").Handler(http.StripPrefix("/web/", http.FileServer(http.Dir("web"))))
	}

	// Catch-all for unhandled routes
	s.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.App.Debug {
			log.Printf("[UNHANDLED] %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"Not Found"}`))
	})
}

// jfVersionAtLeast returns true if the configured Jellyfin version is >= major.minor.
func (s *Server) jfVersionAtLeast(major, minor int) bool {
	m, n := s.config.App.JellyfinMajorMinor()
	return m > major || (m == major && n >= minor)
}

func (s *Server) Start() error {
	added := media.PopulateIDStore(s.config.Libraries)
	log.Printf("Item ID store populated for %d libraries (%d items registered)", len(s.config.Libraries), len(added))
	// Note: initial boot population is NOT pushed to the Kodi sync queue.
	// Kodi will see an empty queue on first connect and perform a full library
	// scan (its default behavior). Only subsequent changes (rescans, auto-scans)
	// are recorded as deltas for incremental sync.
	s.startAutoScan()
	s.runBootScan()
	log.Printf("Starting server on port %d (UI enabled: %v)", s.config.App.Port, s.config.App.UIEnabled)
	return s.httpServer.ListenAndServe()
}

// runBootScan triggers an auto-scan in the background on startup if enabled.
// Runs in a goroutine to avoid delaying the HTTP listener.
func (s *Server) runBootScan() {
	if !s.config.Crawlers.AutoScan.Enabled {
		return
	}
	go func() {
		log.Println("Boot scan: starting in background...")
		s.runAutoScan()
	}()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.userData.Shutdown()
	return s.httpServer.Shutdown(ctx)
}

// --- Helpers ---

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		log.Printf("[%d] %s %s", rec.status, r.Method, r.URL.String())
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
