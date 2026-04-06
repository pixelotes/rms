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
)

type Server struct {
	config     *config.Config
	httpServer *http.Server
	router     *mux.Router
}

func New(cfg *config.Config) *Server {
	s := &Server{config: cfg}
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
	protected.HandleFunc("/crawl/metadata", s.handleCrawlMetadata).Methods("POST")
	protected.HandleFunc("/crawl/subtitles", s.handleCrawlSubtitles).Methods("POST")
	protected.HandleFunc("/crawl/thumbnails", s.handleCrawlThumbnails).Methods("POST")

	// === Jellyfin-Compatible API ===
	jf := s.router.PathPrefix("").Subrouter()

	// Jellyfin public endpoints
	jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
	jf.HandleFunc("/Users/Public", s.jfUsersPublic).Methods("GET")
	jf.HandleFunc("/Users/AuthenticateByName", s.jfAuthenticateByName).Methods("POST")
	jf.HandleFunc("/Branding/Configuration", s.jfBrandingConfig).Methods("GET")

	// Images - public (clients load these as direct URLs without auth headers)
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.jfGetItemImage).Methods("GET", "HEAD")

	// Jellyfin protected endpoints
	jfAuth := s.router.PathPrefix("").Subrouter()
	jfAuth.Use(s.jellyfinAuthMiddleware)

	// System
	jfAuth.HandleFunc("/System/Info", s.jfSystemInfo).Methods("GET")

	// Users & Items
	jfAuth.HandleFunc("/UserViews", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Views", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items", s.jfGetItems).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}", s.jfGetItem).Methods("GET")
	jfAuth.HandleFunc("/Users/Me", s.jfGetCurrentUser).Methods("GET")
	jfAuth.HandleFunc("/Items", s.jfGetItems).Methods("GET")

	// Items sub-endpoints (must be before /Items/{itemId} catch-all)
	jfAuth.HandleFunc("/Items/Latest", s.jfGetLatest).Methods("GET")
	jfAuth.HandleFunc("/Items/Suggestions", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/Filters", s.jfGetFilters).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}", s.jfGetItem).Methods("GET")

	// TV Shows
	jfAuth.HandleFunc("/Shows/{showId}/Seasons", s.jfGetSeasons).Methods("GET")
	jfAuth.HandleFunc("/Shows/{showId}/Episodes", s.jfGetEpisodes).Methods("GET")
	jfAuth.HandleFunc("/Shows/NextUp", s.jfEmptyItems).Methods("GET")

	// Playback
	jfAuth.HandleFunc("/Items/{itemId}/PlaybackInfo", s.jfPlaybackInfo).Methods("POST", "GET")
	jfAuth.HandleFunc("/Videos/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Videos/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Audio/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Audio/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")

	// Subtitles (external delivery)
	jfAuth.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}", s.jfSubtitleStream).Methods("GET")
	jfAuth.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}", s.jfSubtitleStream).Methods("GET")

	// Session stubs
	jfAuth.HandleFunc("/Sessions", s.jfSessionsStub).Methods("GET")
	jfAuth.HandleFunc("/Sessions/Capabilities/Full", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing/Progress", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Playing/Stopped", s.jfSessionStub).Methods("POST")

	// Resume / Suggestions stubs
	jfAuth.HandleFunc("/UserItems/Resume", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/Suggestions", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/PlayingItems/{itemId}", s.jfSessionStub).Methods("POST", "DELETE")

	// User items stubs
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/UserData", s.jfUserDataStub).Methods("POST")
	jfAuth.HandleFunc("/Users/{userId}/PlayedItems/{itemId}", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Users/{userId}/FavoriteItems/{itemId}", s.jfSessionStub).Methods("POST", "DELETE")

	// Display preferences stub
	jfAuth.HandleFunc("/DisplayPreferences/{displayPrefsId}", s.jfDisplayPrefsStub).Methods("GET")

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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	})
}

func (s *Server) Start() error {
	s.startAutoScan()
	log.Printf("Starting server on port %d (UI enabled: %v)", s.config.App.Port, s.config.App.UIEnabled)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
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
