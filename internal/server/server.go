package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

type Server struct {
	config      *config.Config
	httpServer  *http.Server
	router      *http.ServeMux
	userData    *UserDataStore
	syncQueue   *SyncQueueStore
	streamCache *streamCache

	// webhook debounce
	rescanMu    sync.Mutex
	rescanTimer *time.Timer
}

func New(cfg *config.Config) *Server {
	s := &Server{config: cfg}
	s.userData = NewUserDataStore(cfg.App.UserdataPath, cfg.App.UserdataFlushMinutes)
	s.syncQueue = NewSyncQueueStore()
	s.streamCache = newStreamCache(cfg.App.CachePath, cfg.App.CacheMaxGB)
	s.router = http.NewServeMux()
	s.registerRoutes()

	var handler http.Handler = s.router
	if cfg.App.Debug {
		handler = loggingMiddleware(handler)
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.App.Port),
		Handler:      handler,
		ReadTimeout:  0, // No timeout for streaming
		WriteTimeout: 0, // No timeout for streaming
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// jwt wraps a handler with RMS JWT/cookie authentication.
func (s *Server) jwt(h http.HandlerFunc) http.Handler { return s.jwtMiddleware(h) }

// jf wraps a handler with Jellyfin token authentication.
func (s *Server) jf(h http.HandlerFunc) http.Handler { return s.jellyfinAuthMiddleware(h) }

// route registers h for each method on pattern.
func route(mux *http.ServeMux, pattern string, h http.Handler, methods ...string) {
	for _, m := range methods {
		mux.Handle(m+" "+pattern, h)
	}
}

func (s *Server) registerRoutes() {
	mux := s.router

	// Static files (Web UI)
	if s.config.App.UIEnabled {
		withCache := func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "public, max-age=3600")
				h.ServeHTTP(w, r)
			})
		}
		mux.Handle("/css/{path...}", withCache(http.StripPrefix("/css/", http.FileServer(http.Dir("web/css")))))
		mux.Handle("/js/{path...}", withCache(http.StripPrefix("/js/", http.FileServer(http.Dir("web/js")))))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			http.ServeFile(w, r, "web/index.html")
		})
	}

	// === RMS Native API ===

	// Public
	mux.Handle("POST /api/v1/login", http.HandlerFunc(s.handleLogin))
	mux.Handle("POST /api/v1/logout", http.HandlerFunc(s.handleLogout))

	// Protected (JWT Bearer or session cookie)
	mux.Handle("GET /api/v1/me", s.jwt(s.handleMe))
	mux.Handle("GET /api/v1/config", s.jwt(s.handleClientConfig))
	mux.Handle("GET /api/v1/browse", s.jwt(s.handleBrowse))
	mux.Handle("GET /api/v1/stream/{filePath...}", s.jwt(s.handleStream))
	mux.Handle("GET /api/v1/subtitles/{filePath...}", s.jwt(s.handleSubtitles))
	mux.Handle("GET /api/v1/subtitles-list/{filePath...}", s.jwt(s.handleSubtitlesList))
	mux.Handle("POST /api/v1/subtitles-search/{filePath...}", s.jwt(s.handleSearchSubtitles))
	mux.Handle("POST /api/v1/subtitles-download/{filePath...}", s.jwt(s.handleDownloadSubtitle))
	mux.Handle("GET /api/v1/images/{imageId}", s.jwt(s.handleImage))
	if s.hasTVLibraries() {
		mux.Handle("GET /api/v1/tv/logo/{channelId}", s.jwt(s.handleTVLogo))
	}
	mux.Handle("GET /api/v1/duration/{filePath...}", s.jwt(s.handleDuration))
	mux.Handle("POST /api/v1/crawl/metadata", s.jwt(s.handleCrawlMetadata))
	mux.Handle("POST /api/v1/crawl/subtitles", s.jwt(s.handleCrawlSubtitles))
	mux.Handle("POST /api/v1/crawl/thumbnails", s.jwt(s.handleCrawlThumbnails))
	mux.Handle("POST /api/v1/library/rescan", s.jwt(s.handleRescan))

	// Webhook rescan — token-authenticated, no JWT required
	if s.config.App.WebhookToken != "" {
		mux.Handle("POST /api/v1/library/rescan-hook", http.HandlerFunc(s.handleRescanHook))
	}

	// === Jellyfin-Compatible API ===

	// Jellyfin public endpoints
	mux.Handle("GET /System/Info/Public", http.HandlerFunc(s.jfSystemInfoPublic))
	mux.Handle("GET /system/info/public", http.HandlerFunc(s.jfSystemInfoPublic))
	mux.Handle("GET /Users/Public", http.HandlerFunc(s.jfUsersPublic))
	mux.Handle("POST /Users/AuthenticateByName", http.HandlerFunc(s.jfAuthenticateByName))
	mux.Handle("GET /Branding/Configuration", http.HandlerFunc(s.jfBrandingConfig))
	mux.Handle("GET /Branding/Css", http.HandlerFunc(s.jfBrandingCSS))
	mux.Handle("GET /Branding/Css.css", http.HandlerFunc(s.jfBrandingCSS))
	route(mux, "/Branding/Splashscreen", http.HandlerFunc(s.jfNotFound), "GET", "POST", "DELETE")
	mux.Handle("GET /QuickConnect/Enabled", http.HandlerFunc(s.jfQuickConnectEnabled))
	route(mux, "/QuickConnect/Initiate", http.HandlerFunc(s.jfQuickConnectInitiate), "POST", "GET")
	mux.Handle("POST /QuickConnect/Authorize", http.HandlerFunc(s.jfQuickConnectUnavailable))
	mux.Handle("GET /QuickConnect/Connect", http.HandlerFunc(s.jfQuickConnectUnavailable))

	// Images — public (clients load these as direct URLs without auth headers)
	route(mux, "/Items/{itemId}/Images/{imageType}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")
	route(mux, "/Items/{itemId}/Images/{imageType}/{imageIndex}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")
	route(mux, "/Items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")
	route(mux, "/items/{itemId}/Images/{imageType}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")
	route(mux, "/items/{itemId}/Images/{imageType}/{imageIndex}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")
	route(mux, "/items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}", http.HandlerFunc(s.jfGetItemImage), "GET", "HEAD")

	// Streaming — public (media players use bare URLs without auth headers)
	route(mux, "/Videos/{itemId}/stream", http.HandlerFunc(s.jfVideoStream), "GET", "HEAD")
	route(mux, "/Videos/{itemId}/stream.{container}", http.HandlerFunc(s.jfVideoStream), "GET", "HEAD")
	route(mux, "/Audio/{itemId}/stream", http.HandlerFunc(s.jfVideoStream), "GET", "HEAD")
	route(mux, "/Audio/{itemId}/stream.{container}", http.HandlerFunc(s.jfVideoStream), "GET", "HEAD")
	route(mux, "/Audio/{itemId}/universal", http.HandlerFunc(s.jfVideoStream), "GET", "HEAD")
	mux.Handle("GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}", http.HandlerFunc(s.jfSubtitleStream))
	mux.Handle("GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}", http.HandlerFunc(s.jfSubtitleStream))

	// === Jellyfin protected endpoints ===

	// System
	mux.Handle("GET /System/Info", s.jf(s.jfSystemInfo))
	mux.Handle("GET /system/info", s.jf(s.jfSystemInfo))
	mux.Handle("GET /System/Endpoint", s.jf(s.jfSystemEndpoint))
	route(mux, "/System/Ping", s.jf(s.jfSystemPing), "GET", "POST")
	mux.Handle("GET /System/ActivityLog/Entries", s.jf(s.jfEmptyItems))
	mux.Handle("GET /System/Logs", s.jf(s.jfEmptyArray))
	mux.Handle("GET /System/Logs/Log", s.jf(s.jfNotFound))
	mux.Handle("GET /System/Configuration", s.jf(s.jfSystemConfiguration))
	mux.Handle("POST /System/Configuration", s.jf(s.jfSessionStub))
	mux.Handle("POST /System/Configuration/Branding", s.jf(s.jfSessionStub))
	mux.Handle("GET /System/Configuration/encoding", s.jf(s.jfEncodingConfig))
	mux.Handle("GET /System/Configuration/MetadataOptions/Default", s.jf(s.jfMetadataOptionsDefault))
	mux.Handle("GET /System/Configuration/{key}", s.jf(s.jfSystemConfigurationValue))
	mux.Handle("POST /System/Configuration/{key}", s.jf(s.jfSessionStub))
	mux.Handle("POST /System/Restart", s.jf(s.jfSessionStub))
	mux.Handle("POST /System/Shutdown", s.jf(s.jfSessionStub))
	if s.jfVersionAtLeast(10, 11) {
		mux.Handle("GET /System/Storage", s.jf(s.jfSystemStorage))
		mux.Handle("GET /System/Info/Storage", s.jf(s.jfSystemStorage))
	}

	// Users & Items
	mux.Handle("GET /Users", s.jf(s.jfGetUsers))
	mux.Handle("POST /Users", s.jf(s.jfGetCurrentUser))
	mux.Handle("GET /UserViews", s.jf(s.jfGetViews))
	mux.Handle("GET /UserViews/GroupingOptions", s.jf(s.jfGroupingOptions))
	mux.Handle("GET /Users/{userId}/Views", s.jf(s.jfGetViews))
	mux.Handle("GET /Users/{userId}/Items/Latest", s.jf(s.jfGetLatest))
	mux.Handle("GET /Users/{userId}/Items/Resume", s.jf(s.jfGetResumeItems))
	mux.Handle("GET /Users/{userId}/Items", s.jf(s.jfGetItems))
	mux.Handle("GET /Users/{userId}/Items/{itemId}/Intros", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Users/{userId}/Items/{itemId}/LocalTrailers", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Users/{userId}/Items/{itemId}/SpecialFeatures", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Users/{userId}/Items/{itemId}", s.jf(s.jfGetItem))
	mux.Handle("GET /Users/Me", s.jf(s.jfGetCurrentUser))
	mux.Handle("GET /Users/{userId}", s.jf(s.jfGetUser))
	mux.Handle("DELETE /Users/{userId}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Users/{userId}/Policy", s.jf(s.jfSessionStub))
	mux.Handle("POST /Users/AuthenticateWithQuickConnect", s.jf(s.jfQuickConnectUnavailable))
	mux.Handle("POST /Users/Configuration", s.jf(s.jfSessionStub))
	mux.Handle("POST /Users/ForgotPassword", s.jf(s.jfForgotPassword))
	mux.Handle("POST /Users/ForgotPassword/Pin", s.jf(s.jfForgotPasswordPin))
	mux.Handle("POST /Users/New", s.jf(s.jfGetCurrentUser))
	mux.Handle("POST /Users/Password", s.jf(s.jfSessionStub))
	mux.Handle("GET /Items", s.jf(s.jfGetItems))

	// Items sub-endpoints (must be before /Items/{itemId} catch-all)
	mux.Handle("GET /Items/Latest", s.jf(s.jfGetLatest))
	mux.Handle("GET /Items/Root", s.jf(s.jfGetRoot))
	mux.Handle("GET /Items/Suggestions", s.jf(s.jfGetSuggestions))
	mux.Handle("GET /Items/Filters", s.jf(s.jfGetFilters))
	mux.Handle("GET /Items/Filters2", s.jf(s.jfGetFilters))
	mux.Handle("GET /Items/Counts", s.jf(s.jfItemCounts))
	mux.Handle("GET /Items/{itemId}/Images", s.jf(s.jfItemImages))
	route(mux, "/Items/{itemId}/Images/{imageType}", s.jf(s.jfSessionStub), "POST", "DELETE")
	route(mux, "/Items/{itemId}/Images/{imageType}/{imageIndex}", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("POST /Items/{itemId}/Images/{imageType}/{imageIndex}/Index", s.jf(s.jfSessionStub))
	mux.Handle("GET /Items/{itemId}/InstantMix", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/ExternalIdInfos", s.jf(s.jfExternalIdInfos))
	mux.Handle("GET /Items/{itemId}/MetadataEditor", s.jf(s.jfMetadataEditor))
	mux.Handle("GET /Items/{itemId}/CriticReviews", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/RemoteImages", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/RemoteImages/Providers", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/{itemId}/RemoteImages/Download", s.jf(s.jfSessionStub))
	mux.Handle("GET /Items/{itemId}/RemoteSearch/Subtitles/{language}", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/{itemId}/RemoteSearch/Subtitles/{subtitleId}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Items/RemoteSearch/Apply/{itemId}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Items/RemoteSearch/Book", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/BoxSet", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/Movie", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/MusicAlbum", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/MusicArtist", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/MusicVideo", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/Person", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/Series", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/RemoteSearch/Trailer", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Items/{itemId}/Refresh", s.jf(s.jfRefreshLibrary))
	mux.Handle("POST /Items/{itemId}/ContentType", s.jf(s.jfSessionStub))
	route(mux, "/Items/{itemId}/Download", s.jf(s.jfVideoStream), "GET", "HEAD")
	route(mux, "/Items/{itemId}/File", s.jf(s.jfVideoStream), "GET", "HEAD")
	mux.Handle("GET /Items/{itemId}/Similar", s.jf(s.jfSimilarItems))
	mux.Handle("GET /Items/{itemId}/Ancestors", s.jf(s.jfGetAncestors))
	mux.Handle("GET /Items/{itemId}/Intros", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/ThemeMedia", s.jf(s.jfThemeMedia))
	mux.Handle("GET /Items/{itemId}/ThemeSongs", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/ThemeVideos", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}/SpecialFeatures", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Items/{itemId}/LocalTrailers", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Items/{itemId}/Collections", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Items/{itemId}", s.jf(s.jfGetItem))
	route(mux, "/Items/{itemId}", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("DELETE /Items", s.jf(s.jfSessionStub))

	// TV Shows
	mux.Handle("GET /Shows/{showId}/Seasons", s.jf(s.jfGetSeasons))
	mux.Handle("GET /Shows/{showId}/Episodes", s.jf(s.jfGetEpisodes))
	mux.Handle("GET /Shows/NextUp", s.jf(s.jfNextUp))
	mux.Handle("GET /Shows/Upcoming", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Shows/{itemId}/Similar", s.jf(s.jfSimilarItems))
	mux.Handle("GET /Movies/{itemId}/Similar", s.jf(s.jfSimilarItems))
	mux.Handle("GET /Movies/Recommendations", s.jf(s.jfMovieRecommendations))
	mux.Handle("GET /Videos/{itemId}/AdditionalParts", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Videos/{videoId}/{mediaSourceId}/Attachments/{index}", s.jf(s.jfNotFound))
	mux.Handle("POST /Videos/{itemId}/Subtitles", s.jf(s.jfSessionStub))
	mux.Handle("DELETE /Videos/{itemId}/Subtitles/{index}", s.jf(s.jfSessionStub))
	mux.Handle("DELETE /Videos/{itemId}/AlternateSources", s.jf(s.jfSessionStub))
	mux.Handle("POST /Videos/MergeVersions", s.jf(s.jfSessionStub))
	mux.Handle("GET /Audio/{itemId}/Lyrics", s.jf(s.jfEmptyObject))
	route(mux, "/Audio/{itemId}/Lyrics", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("GET /Audio/{itemId}/RemoteSearch/Lyrics", s.jf(s.jfEmptyArray))
	mux.Handle("POST /Audio/{itemId}/RemoteSearch/Lyrics/{lyricId}", s.jf(s.jfSessionStub))

	// Playback
	route(mux, "/Items/{itemId}/PlaybackInfo", s.jf(s.jfPlaybackInfo), "POST", "GET")
	mux.Handle("GET /Playback/BitrateTest", s.jf(s.jfBitrateTest))

	// Sessions
	mux.Handle("GET /Sessions", s.jf(s.jfSessionsStub))
	mux.Handle("POST /Sessions/{sessionId}/Command", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/{sessionId}/Command/{command}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/{sessionId}/Message", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/{sessionId}/Playing", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/{sessionId}/Playing/{command}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/{sessionId}/System/{command}", s.jf(s.jfSessionStub))
	route(mux, "/Sessions/{sessionId}/User/{userId}", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("POST /Sessions/{sessionId}/Viewing", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/Viewing", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/Capabilities", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/Capabilities/Full", s.jf(s.jfSessionStub))
	mux.Handle("POST /Sessions/Playing", s.jf(s.jfReportPlayback))
	mux.Handle("POST /Sessions/Playing/Progress", s.jf(s.jfReportPlayback))
	mux.Handle("POST /Sessions/Playing/Stopped", s.jf(s.jfReportPlaybackStopped))
	mux.Handle("POST /Sessions/Playing/Ping", s.jf(s.jfSessionStub))
	if s.jfVersionAtLeast(10, 11) {
		mux.Handle("POST /Sessions/Playing/ReportStart", s.jf(s.jfReportPlayback))
		mux.Handle("POST /Sessions/Playing/ReportProgress", s.jf(s.jfReportPlayback))
		mux.Handle("POST /Sessions/Playing/ReportStopped", s.jf(s.jfReportPlaybackStopped))
		route(mux, "/Sessions/Logout", s.jf(s.jfSessionStub), "DELETE", "POST")
	}

	// Client log stub
	mux.Handle("POST /ClientLog/Document", s.jf(s.jfSessionStub))

	// Resume
	mux.Handle("GET /UserItems/Resume", s.jf(s.jfGetResumeItems))

	// User items state
	mux.Handle("GET /UserItems/{itemId}/UserData", s.jf(s.jfUserData))
	mux.Handle("POST /UserItems/{itemId}/UserData", s.jf(s.jfUpdateUserData))
	mux.Handle("GET /Users/{userId}/Items/{itemId}/UserData", s.jf(s.jfUserData))
	mux.Handle("POST /Users/{userId}/Items/{itemId}/UserData", s.jf(s.jfUpdateUserData))
	route(mux, "/UserItems/{itemId}/Rating", s.jf(s.jfSessionStub), "POST", "DELETE")
	route(mux, "/Users/{userId}/PlayedItems/{itemId}", s.jf(s.jfTogglePlayed), "POST", "DELETE")
	route(mux, "/Users/{userId}/FavoriteItems/{itemId}", s.jf(s.jfToggleFavorite), "POST", "DELETE")
	route(mux, "/UserPlayedItems/{itemId}", s.jf(s.jfTogglePlayed), "POST", "DELETE")
	route(mux, "/UserFavoriteItems/{itemId}", s.jf(s.jfToggleFavorite), "POST", "DELETE")
	route(mux, "/PlayingItems/{itemId}", s.jf(s.jfReportPlayback), "POST", "DELETE")
	mux.Handle("POST /PlayingItems/{itemId}/Progress", s.jf(s.jfReportPlayback))

	// Search
	mux.Handle("GET /Search/Hints", s.jf(s.jfSearchHints))

	// Genres, Persons, Studios
	mux.Handle("GET /Genres", s.jf(s.jfGetGenres))
	mux.Handle("GET /Genres/{genreName}", s.jf(s.jfNamedStubItem))
	route(mux, "/Genres/{name}/Images/{imageType}", s.jf(s.jfNotFound), "GET", "HEAD")
	route(mux, "/Genres/{name}/Images/{imageType}/{imageIndex}", s.jf(s.jfNotFound), "GET", "HEAD")
	mux.Handle("GET /Persons", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Persons/{name}", s.jf(s.jfNamedStubItem))
	route(mux, "/Persons/{name}/Images/{imageType}", s.jf(s.jfNotFound), "GET", "HEAD")
	route(mux, "/Persons/{name}/Images/{imageType}/{imageIndex}", s.jf(s.jfNotFound), "GET", "HEAD")
	mux.Handle("GET /Studios", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Studios/{name}", s.jf(s.jfNamedStubItem))
	route(mux, "/Studios/{name}/Images/{imageType}", s.jf(s.jfNotFound), "GET", "HEAD")
	route(mux, "/Studios/{name}/Images/{imageType}/{imageIndex}", s.jf(s.jfNotFound), "GET", "HEAD")
	mux.Handle("GET /Artists", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Artists/InstantMix", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Artists/AlbumArtists", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Artists/{itemId}/InstantMix", s.jf(s.jfEmptyItems))
	mux.Handle("GET /Artists/{itemId}/Similar", s.jf(s.jfEmptyItems))
	route(mux, "/Artists/{name}/Images/{imageType}/{imageIndex}", s.jf(s.jfNotFound), "GET", "HEAD")
	mux.Handle("GET /Artists/{name}", s.jf(s.jfNamedStubItem))

	// Media segments (skip intro/credits)
	mux.Handle("GET /MediaSegments/{itemId}", s.jf(s.jfMediaSegments))

	// Cancel active transcoding
	mux.Handle("DELETE /Videos/ActiveEncodings", s.jf(s.jfSessionStub))

	// Display preferences stub
	mux.Handle("GET /DisplayPreferences/{displayPrefsId}", s.jf(s.jfDisplayPrefsStub))
	mux.Handle("POST /DisplayPreferences/{displayPrefsId}", s.jf(s.jfSessionStub))

	// LiveTv
	if s.hasTVLibraries() {
		mux.Handle("GET /LiveTv/Info", s.jf(s.jfLiveTVInfo))
		mux.Handle("GET /LiveTv/Channels", s.jf(s.jfLiveTvChannels))
		mux.Handle("GET /LiveTv/Channels/{channelId}", s.jf(s.jfLiveTvChannel))
		mux.Handle("GET /LiveTv/Programs/Recommended", s.jf(s.jfEmptyItems))
		route(mux, "/LiveTv/Programs", s.jf(s.jfEmptyItems), "GET", "POST")
		mux.Handle("GET /LiveTv/GuideInfo", s.jf(s.jfEmptyObject))
		mux.Handle("POST /LiveStreams/Open", s.jf(s.jfOpenLiveStream))
		mux.Handle("POST /LiveStreams/Close", s.jf(s.jfCloseLiveStream))
	}

	// Compatibility stubs
	mux.Handle("GET /Plugins", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Packages", s.jf(s.jfEmptyArray))
	mux.Handle("GET /ScheduledTasks", s.jf(s.jfScheduledTasks))
	mux.Handle("GET /Devices", s.jf(s.jfDevices))
	mux.Handle("DELETE /Devices", s.jf(s.jfSessionStub))
	mux.Handle("GET /Devices/Info", s.jf(s.jfDeviceInfo))
	mux.Handle("GET /Devices/Options", s.jf(s.jfDeviceOptions))
	mux.Handle("POST /Devices/Options", s.jf(s.jfSessionStub))
	mux.Handle("GET /Library/MediaFolders", s.jf(s.jfGetViews))
	mux.Handle("GET /Library/VirtualFolders", s.jf(s.jfVirtualFolders))
	mux.Handle("GET /Library/PhysicalPaths", s.jf(s.jfPhysicalPaths))
	mux.Handle("GET /Localization/Countries", s.jf(s.jfLocalizationCountries))
	mux.Handle("GET /Localization/Cultures", s.jf(s.jfLocalizationCultures))
	mux.Handle("GET /Localization/Options", s.jf(s.jfLocalizationOptions))
	mux.Handle("GET /Localization/ParentalRatings", s.jf(s.jfEmptyArray))
	mux.Handle("GET /Auth/Providers", s.jf(s.jfAuthProviders))
	mux.Handle("GET /Auth/PasswordResetProviders", s.jf(s.jfPasswordResetProviders))
	mux.Handle("GET /Auth/Keys", s.jf(s.jfEmptyItems))
	mux.Handle("POST /Auth/Keys", s.jf(s.jfAuthKey))
	mux.Handle("DELETE /Auth/Keys/{key}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Startup/Complete", s.jf(s.jfSessionStub))
	mux.Handle("GET /Startup/Configuration", s.jf(s.jfStartupConfiguration))
	mux.Handle("POST /Startup/Configuration", s.jf(s.jfSessionStub))
	mux.Handle("GET /Startup/FirstUser", s.jf(s.jfStartupUser))
	mux.Handle("POST /Startup/RemoteAccess", s.jf(s.jfSessionStub))
	mux.Handle("GET /Startup/User", s.jf(s.jfStartupUser))
	mux.Handle("POST /Startup/User", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Media/Updated", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Movies/Added", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Movies/Updated", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Series/Added", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Series/Updated", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/Refresh", s.jf(s.jfRefreshLibrary))
	route(mux, "/Library/VirtualFolders", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("POST /Library/VirtualFolders/LibraryOptions", s.jf(s.jfSessionStub))
	mux.Handle("POST /Library/VirtualFolders/Name", s.jf(s.jfSessionStub))
	route(mux, "/Library/VirtualFolders/Paths", s.jf(s.jfSessionStub), "POST", "DELETE")
	mux.Handle("POST /Library/VirtualFolders/Paths/Update", s.jf(s.jfSessionStub))
	mux.Handle("GET /ScheduledTasks/{taskId}", s.jf(s.jfScheduledTask))
	mux.Handle("POST /ScheduledTasks/{taskId}/Triggers", s.jf(s.jfSessionStub))
	mux.Handle("POST /ScheduledTasks/Running/{taskId}", s.jf(s.jfRunScheduledTask))
	mux.Handle("DELETE /ScheduledTasks/Running/{taskId}", s.jf(s.jfSessionStub))
	mux.Handle("GET /Packages/{name}", s.jf(s.jfPackageInfo))
	mux.Handle("POST /Packages/Installed/{name}", s.jf(s.jfSessionStub))
	mux.Handle("DELETE /Packages/Installing/{packageId}", s.jf(s.jfSessionStub))
	mux.Handle("DELETE /Plugins/{pluginId}", s.jf(s.jfSessionStub))
	mux.Handle("DELETE /Plugins/{pluginId}/{version}", s.jf(s.jfSessionStub))
	mux.Handle("POST /Plugins/{pluginId}/{version}/Disable", s.jf(s.jfSessionStub))
	mux.Handle("POST /Plugins/{pluginId}/{version}/Enable", s.jf(s.jfSessionStub))
	mux.Handle("GET /Plugins/{pluginId}/{version}/Image", s.jf(s.jfNotFound))
	mux.Handle("GET /Plugins/{pluginId}/Configuration", s.jf(s.jfEmptyObject))
	mux.Handle("POST /Plugins/{pluginId}/Configuration", s.jf(s.jfSessionStub))
	mux.Handle("POST /Plugins/{pluginId}/Manifest", s.jf(s.jfSessionStub))

	// Kodi SyncQueue plugin emulation (optional)
	if s.config.App.KodiSyncQueue {
		mux.Handle("GET /Jellyfin.Plugin.KodiSyncQueue/GetPluginSettings", http.HandlerFunc(s.jfKodiSyncSettings))
		mux.Handle("GET /Jellyfin.Plugin.KodiSyncQueue/{userId}/GetItems", s.jf(s.jfKodiSyncGetItems))
	}

	// WebSocket stub
	mux.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Web UI static files (Jellyfin web path)
	if s.config.App.UIEnabled {
		mux.Handle("/web/{path...}", http.StripPrefix("/web/", http.FileServer(http.Dir("web"))))
	}

	// Catch-all for unhandled routes
	mux.HandleFunc("/{rest...}", func(w http.ResponseWriter, r *http.Request) {
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

// hasTVLibraries reports whether any content_type: "tv" library is configured.
func (s *Server) hasTVLibraries() bool {
	for _, lib := range s.config.Libraries {
		if lib.ContentType == "tv" {
			return true
		}
	}
	return false
}

func (s *Server) Start() error {
	added, _ := media.PopulateIDStore(s.config.Libraries)
	log.Printf("Item ID store populated for %d libraries (%d items registered)", len(s.config.Libraries), len(added))
	s.refreshTVChannels()
	s.startAutoScan()
	s.startIndexRefresh()
	s.startTVRefresh()
	s.runBootScan()
	log.Printf("Starting server on port %d (UI enabled: %v)", s.config.App.Port, s.config.App.UIEnabled)
	return s.httpServer.ListenAndServe()
}

// runBootScan triggers an auto-scan in the background on startup if enabled.
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
