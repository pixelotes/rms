package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

type Server struct {
	config      *config.Config
	httpServer  *http.Server
	router      *mux.Router
	userData    *UserDataStore
	syncQueue   *SyncQueueStore
	streamCache *streamCache

	// webhook debounce (Paso 2)
	rescanMu    sync.Mutex
	rescanTimer *time.Timer
}

func New(cfg *config.Config) *Server {
	s := &Server{config: cfg}
	s.userData = NewUserDataStore(cfg.App.UserdataPath)
	s.syncQueue = NewSyncQueueStore()
	s.streamCache = newStreamCache(cfg.App.CachePath, cfg.App.CacheMaxGB)
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
		noCache := func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-cache, must-revalidate")
				h.ServeHTTP(w, r)
			})
		}
		s.router.PathPrefix("/css/").Handler(noCache(http.StripPrefix("/css/", http.FileServer(http.Dir("web/css")))))
		s.router.PathPrefix("/js/").Handler(noCache(http.StripPrefix("/js/", http.FileServer(http.Dir("web/js")))))
		s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			http.ServeFile(w, r, "web/index.html")
		})
	}

	// === RMS Native API ===
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Public
	api.HandleFunc("/login", s.handleLogin).Methods("POST")
	api.HandleFunc("/logout", s.handleLogout).Methods("POST")

	// Protected (JWT Bearer or session cookie)
	protected := api.PathPrefix("").Subrouter()
	protected.Use(s.jwtMiddleware)
	protected.HandleFunc("/me", s.handleMe).Methods("GET")
	protected.HandleFunc("/config", s.handleClientConfig).Methods("GET")
	protected.HandleFunc("/browse", s.handleBrowse).Methods("GET")
	protected.HandleFunc("/stream/{filePath:.+}", s.handleStream).Methods("GET")
	protected.HandleFunc("/subtitles/{filePath:.+}", s.handleSubtitles).Methods("GET")
	protected.HandleFunc("/subtitles-list/{filePath:.+}", s.handleSubtitlesList).Methods("GET")
	protected.HandleFunc("/subtitles-search/{filePath:.+}", s.handleSearchSubtitles).Methods("POST")
	protected.HandleFunc("/subtitles-download/{filePath:.+}", s.handleDownloadSubtitle).Methods("POST")
	protected.HandleFunc("/images/{imageId}", s.handleImage).Methods("GET")
	protected.HandleFunc("/tv/logo/{channelId}", s.handleTVLogo).Methods("GET")
	protected.HandleFunc("/duration/{filePath:.+}", s.handleDuration).Methods("GET")
	protected.HandleFunc("/crawl/metadata", s.handleCrawlMetadata).Methods("POST")
	protected.HandleFunc("/crawl/subtitles", s.handleCrawlSubtitles).Methods("POST")
	protected.HandleFunc("/crawl/thumbnails", s.handleCrawlThumbnails).Methods("POST")
	protected.HandleFunc("/library/rescan", s.handleRescan).Methods("POST")

	// Webhook rescan — token-authenticated, no JWT required (for Sonarr/Radarr/scripts)
	if s.config.App.WebhookToken != "" {
		api.HandleFunc("/library/rescan-hook", s.handleRescanHook).Methods("POST")
	}

	// === Jellyfin-Compatible API ===
	jf := s.router.PathPrefix("").Subrouter()

	// Jellyfin public endpoints
	jf.HandleFunc("/System/Info/Public", s.jfSystemInfoPublic).Methods("GET")
	jf.HandleFunc("/system/info/public", s.jfSystemInfoPublic).Methods("GET")
	jf.HandleFunc("/Users/Public", s.jfUsersPublic).Methods("GET")
	jf.HandleFunc("/Users/AuthenticateByName", s.jfAuthenticateByName).Methods("POST")
	jf.HandleFunc("/Branding/Configuration", s.jfBrandingConfig).Methods("GET")
	jf.HandleFunc("/Branding/Css", s.jfBrandingCSS).Methods("GET")
	jf.HandleFunc("/Branding/Css.css", s.jfBrandingCSS).Methods("GET")
	jf.HandleFunc("/Branding/Splashscreen", s.jfNotFound).Methods("GET", "POST", "DELETE")
	jf.HandleFunc("/QuickConnect/Enabled", s.jfQuickConnectEnabled).Methods("GET")
	jf.HandleFunc("/QuickConnect/Initiate", s.jfQuickConnectInitiate).Methods("POST", "GET")
	jf.HandleFunc("/QuickConnect/Authorize", s.jfQuickConnectUnavailable).Methods("POST")
	jf.HandleFunc("/QuickConnect/Connect", s.jfQuickConnectUnavailable).Methods("GET")

	// Images - public (clients load these as direct URLs without auth headers)
	// Some clients use lowercase "/items/" so register both variants.
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/items/{itemId}/Images/{imageType}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/items/{itemId}/Images/{imageType}/{imageIndex}", s.jfGetItemImage).Methods("GET", "HEAD")
	jf.HandleFunc("/items/{itemId}/Images/{imageType}/{imageIndex}/{tag}/{format}/{maxWidth}/{maxHeight}/{percentPlayed}/{unplayedCount}", s.jfGetItemImage).Methods("GET", "HEAD")

	// Streaming - public (media players like ExoPlayer/AVPlayer use bare URLs without auth headers)
	jf.HandleFunc("/Videos/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Videos/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Audio/{itemId}/stream", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Audio/{itemId}/stream.{container}", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Audio/{itemId}/universal", s.jfVideoStream).Methods("GET", "HEAD")
	jf.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/{tick}/Stream.{format}", s.jfSubtitleStream).Methods("GET")
	jf.HandleFunc("/Videos/{itemId}/{sourceId}/Subtitles/{index}/Stream.{format}", s.jfSubtitleStream).Methods("GET")

	// Jellyfin protected endpoints
	jfAuth := s.router.PathPrefix("").Subrouter()
	jfAuth.Use(s.jellyfinAuthMiddleware)

	// System (register both cases — some clients use lowercase)
	jfAuth.HandleFunc("/System/Info", s.jfSystemInfo).Methods("GET")
	jfAuth.HandleFunc("/system/info", s.jfSystemInfo).Methods("GET")
	jfAuth.HandleFunc("/System/Endpoint", s.jfSystemEndpoint).Methods("GET")
	jfAuth.HandleFunc("/System/Ping", s.jfSystemPing).Methods("GET", "POST")
	jfAuth.HandleFunc("/System/ActivityLog/Entries", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/System/Logs", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/System/Logs/Log", s.jfNotFound).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration", s.jfSystemConfiguration).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/System/Configuration/Branding", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/System/Configuration/encoding", s.jfEncodingConfig).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration/MetadataOptions/Default", s.jfMetadataOptionsDefault).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration/{key}", s.jfSystemConfigurationValue).Methods("GET")
	jfAuth.HandleFunc("/System/Configuration/{key}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/System/Restart", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/System/Shutdown", s.jfSessionStub).Methods("POST")
	if s.jfVersionAtLeast(10, 11) {
		jfAuth.HandleFunc("/System/Storage", s.jfSystemStorage).Methods("GET")
		jfAuth.HandleFunc("/System/Info/Storage", s.jfSystemStorage).Methods("GET")
	}

	// Users & Items
	jfAuth.HandleFunc("/Users", s.jfGetUsers).Methods("GET")
	jfAuth.HandleFunc("/Users", s.jfGetCurrentUser).Methods("POST")
	jfAuth.HandleFunc("/UserViews", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/UserViews/GroupingOptions", s.jfGroupingOptions).Methods("GET")
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
	jfAuth.HandleFunc("/Users/{userId}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Users/{userId}/Policy", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Users/AuthenticateWithQuickConnect", s.jfQuickConnectUnavailable).Methods("POST")
	jfAuth.HandleFunc("/Users/Configuration", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Users/ForgotPassword", s.jfForgotPassword).Methods("POST")
	jfAuth.HandleFunc("/Users/ForgotPassword/Pin", s.jfForgotPasswordPin).Methods("POST")
	jfAuth.HandleFunc("/Users/New", s.jfGetCurrentUser).Methods("POST")
	jfAuth.HandleFunc("/Users/Password", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items", s.jfGetItems).Methods("GET")

	// Items sub-endpoints (must be before /Items/{itemId} catch-all)
	jfAuth.HandleFunc("/Items/Latest", s.jfGetLatest).Methods("GET")
	jfAuth.HandleFunc("/Items/Root", s.jfGetRoot).Methods("GET")
	jfAuth.HandleFunc("/Items/Suggestions", s.jfGetSuggestions).Methods("GET")
	jfAuth.HandleFunc("/Items/Filters", s.jfGetFilters).Methods("GET")
	jfAuth.HandleFunc("/Items/Filters2", s.jfGetFilters).Methods("GET")
	jfAuth.HandleFunc("/Items/Counts", s.jfItemCounts).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Images", s.jfItemImages).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Images/{imageType}", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Items/{itemId}/Images/{imageType}/{imageIndex}/Index", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items/{itemId}/InstantMix", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ExternalIdInfos", s.jfExternalIdInfos).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/MetadataEditor", s.jfMetadataEditor).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/CriticReviews", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/RemoteImages", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/RemoteImages/Providers", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/RemoteImages/Download", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items/{itemId}/RemoteSearch/Subtitles/{language}", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/RemoteSearch/Subtitles/{subtitleId}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Apply/{itemId}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Book", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/BoxSet", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Movie", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/MusicAlbum", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/MusicArtist", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/MusicVideo", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Person", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Series", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/RemoteSearch/Trailer", s.jfEmptyArray).Methods("POST")
	jfAuth.HandleFunc("/Items/{itemId}/Refresh", s.jfRefreshLibrary).Methods("POST")
	jfAuth.HandleFunc("/Items/{itemId}/ContentType", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Items/{itemId}/Download", s.jfVideoStream).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Items/{itemId}/File", s.jfVideoStream).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Items/{itemId}/Similar", s.jfSimilarItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Ancestors", s.jfGetAncestors).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/Intros", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeMedia", s.jfThemeMedia).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeSongs", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/ThemeVideos", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/SpecialFeatures", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}/LocalTrailers", s.jfEmptyArray).Methods("GET")
	// Jellyfin 12.0+: collections containing the item. RMS has no collections (no DB) → empty result.
	jfAuth.HandleFunc("/Items/{itemId}/Collections", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}", s.jfGetItem).Methods("GET")
	jfAuth.HandleFunc("/Items/{itemId}", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Items", s.jfSessionStub).Methods("DELETE")

	// TV Shows
	jfAuth.HandleFunc("/Shows/{showId}/Seasons", s.jfGetSeasons).Methods("GET")
	jfAuth.HandleFunc("/Shows/{showId}/Episodes", s.jfGetEpisodes).Methods("GET")
	jfAuth.HandleFunc("/Shows/NextUp", s.jfNextUp).Methods("GET")
	jfAuth.HandleFunc("/Shows/Upcoming", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Shows/{itemId}/Similar", s.jfSimilarItems).Methods("GET")
	jfAuth.HandleFunc("/Movies/{itemId}/Similar", s.jfSimilarItems).Methods("GET")
	jfAuth.HandleFunc("/Movies/Recommendations", s.jfMovieRecommendations).Methods("GET")
	jfAuth.HandleFunc("/Videos/{itemId}/AdditionalParts", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Videos/{videoId}/{mediaSourceId}/Attachments/{index}", s.jfNotFound).Methods("GET")
	jfAuth.HandleFunc("/Videos/{itemId}/Subtitles", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Videos/{itemId}/Subtitles/{index}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Videos/{itemId}/AlternateSources", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Videos/MergeVersions", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Audio/{itemId}/Lyrics", s.jfEmptyObject).Methods("GET")
	jfAuth.HandleFunc("/Audio/{itemId}/Lyrics", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Audio/{itemId}/RemoteSearch/Lyrics", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Audio/{itemId}/RemoteSearch/Lyrics/{lyricId}", s.jfSessionStub).Methods("POST")

	// Playback
	jfAuth.HandleFunc("/Items/{itemId}/PlaybackInfo", s.jfPlaybackInfo).Methods("POST", "GET")
	jfAuth.HandleFunc("/Playback/BitrateTest", s.jfBitrateTest).Methods("GET")

	// Sessions
	jfAuth.HandleFunc("/Sessions", s.jfSessionsStub).Methods("GET")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Command", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Command/{command}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Message", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Playing", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Playing/{command}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/System/{command}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/{sessionId}/User/{userId}", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Sessions/{sessionId}/Viewing", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Sessions/Viewing", s.jfSessionStub).Methods("POST")
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
	jfAuth.HandleFunc("/UserItems/{itemId}/UserData", s.jfUserData).Methods("GET")
	jfAuth.HandleFunc("/UserItems/{itemId}/UserData", s.jfUpdateUserData).Methods("POST")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/UserData", s.jfUserData).Methods("GET")
	jfAuth.HandleFunc("/Users/{userId}/Items/{itemId}/UserData", s.jfUpdateUserData).Methods("POST")
	jfAuth.HandleFunc("/UserItems/{itemId}/Rating", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Users/{userId}/PlayedItems/{itemId}", s.jfTogglePlayed).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Users/{userId}/FavoriteItems/{itemId}", s.jfToggleFavorite).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/UserPlayedItems/{itemId}", s.jfTogglePlayed).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/UserFavoriteItems/{itemId}", s.jfToggleFavorite).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/PlayingItems/{itemId}", s.jfReportPlayback).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/PlayingItems/{itemId}/Progress", s.jfReportPlayback).Methods("POST")

	// Search
	jfAuth.HandleFunc("/Search/Hints", s.jfSearchHints).Methods("GET")

	// Genres, Persons, Studios
	jfAuth.HandleFunc("/Genres", s.jfGetGenres).Methods("GET")
	jfAuth.HandleFunc("/Genres/{genreName}", s.jfNamedStubItem).Methods("GET")
	jfAuth.HandleFunc("/Genres/{name}/Images/{imageType}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Genres/{name}/Images/{imageType}/{imageIndex}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Persons", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Persons/{name}", s.jfNamedStubItem).Methods("GET")
	jfAuth.HandleFunc("/Persons/{name}/Images/{imageType}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Persons/{name}/Images/{imageType}/{imageIndex}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Studios", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Studios/{name}", s.jfNamedStubItem).Methods("GET")
	jfAuth.HandleFunc("/Studios/{name}/Images/{imageType}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Studios/{name}/Images/{imageType}/{imageIndex}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Artists", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists/InstantMix", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists/AlbumArtists", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists/{itemId}/InstantMix", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists/{itemId}/Similar", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Artists/{name}/Images/{imageType}/{imageIndex}", s.jfNotFound).Methods("GET", "HEAD")
	jfAuth.HandleFunc("/Artists/{name}", s.jfNamedStubItem).Methods("GET")

	// Media segments (skip intro/credits)
	jfAuth.HandleFunc("/MediaSegments/{itemId}", s.jfMediaSegments).Methods("GET")

	// Cancel active transcoding
	jfAuth.HandleFunc("/Videos/ActiveEncodings", s.jfSessionStub).Methods("DELETE")

	// Display preferences stub
	jfAuth.HandleFunc("/DisplayPreferences/{displayPrefsId}", s.jfDisplayPrefsStub).Methods("GET")
	jfAuth.HandleFunc("/DisplayPreferences/{displayPrefsId}", s.jfSessionStub).Methods("POST")

	// LiveTv: channels are served from the M3U/IPTV channel store (ADR-015).
	// EPG (Programs/Guide) and DVR remain unimplemented — empty stubs.
	jfAuth.HandleFunc("/LiveTv/Info", s.jfLiveTVInfo).Methods("GET")
	jfAuth.HandleFunc("/LiveTv/Channels", s.jfLiveTvChannels).Methods("GET")
	jfAuth.HandleFunc("/LiveTv/Channels/{channelId}", s.jfLiveTvChannel).Methods("GET")
	jfAuth.HandleFunc("/LiveTv/Programs/Recommended", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/LiveTv/Programs", s.jfEmptyItems).Methods("GET", "POST")
	jfAuth.HandleFunc("/LiveTv/GuideInfo", s.jfEmptyObject).Methods("GET")
	jfAuth.HandleFunc("/LiveStreams/Open", s.jfOpenLiveStream).Methods("POST")
	jfAuth.HandleFunc("/LiveStreams/Close", s.jfCloseLiveStream).Methods("POST")

	// Lightweight compatibility stubs used by several official and third-party clients at startup.
	jfAuth.HandleFunc("/Plugins", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Packages", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/ScheduledTasks", s.jfScheduledTasks).Methods("GET")
	jfAuth.HandleFunc("/Devices", s.jfDevices).Methods("GET")
	jfAuth.HandleFunc("/Devices", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Devices/Info", s.jfDeviceInfo).Methods("GET")
	jfAuth.HandleFunc("/Devices/Options", s.jfDeviceOptions).Methods("GET")
	jfAuth.HandleFunc("/Devices/Options", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/MediaFolders", s.jfGetViews).Methods("GET")
	jfAuth.HandleFunc("/Library/VirtualFolders", s.jfVirtualFolders).Methods("GET")
	jfAuth.HandleFunc("/Library/PhysicalPaths", s.jfPhysicalPaths).Methods("GET")
	jfAuth.HandleFunc("/Localization/Countries", s.jfLocalizationCountries).Methods("GET")
	jfAuth.HandleFunc("/Localization/Cultures", s.jfLocalizationCultures).Methods("GET")
	jfAuth.HandleFunc("/Localization/Options", s.jfLocalizationOptions).Methods("GET")
	jfAuth.HandleFunc("/Localization/ParentalRatings", s.jfEmptyArray).Methods("GET")
	jfAuth.HandleFunc("/Auth/Providers", s.jfAuthProviders).Methods("GET")
	jfAuth.HandleFunc("/Auth/PasswordResetProviders", s.jfPasswordResetProviders).Methods("GET")
	jfAuth.HandleFunc("/Auth/Keys", s.jfEmptyItems).Methods("GET")
	jfAuth.HandleFunc("/Auth/Keys", s.jfAuthKey).Methods("POST")
	jfAuth.HandleFunc("/Auth/Keys/{key}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Startup/Complete", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Startup/Configuration", s.jfStartupConfiguration).Methods("GET")
	jfAuth.HandleFunc("/Startup/Configuration", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Startup/FirstUser", s.jfStartupUser).Methods("GET")
	jfAuth.HandleFunc("/Startup/RemoteAccess", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Startup/User", s.jfStartupUser).Methods("GET")
	jfAuth.HandleFunc("/Startup/User", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Media/Updated", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Movies/Added", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Movies/Updated", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Series/Added", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Series/Updated", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/Refresh", s.jfRefreshLibrary).Methods("POST")
	jfAuth.HandleFunc("/Library/VirtualFolders", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Library/VirtualFolders/LibraryOptions", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/VirtualFolders/Name", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Library/VirtualFolders/Paths", s.jfSessionStub).Methods("POST", "DELETE")
	jfAuth.HandleFunc("/Library/VirtualFolders/Paths/Update", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/ScheduledTasks/{taskId}", s.jfScheduledTask).Methods("GET")
	jfAuth.HandleFunc("/ScheduledTasks/{taskId}/Triggers", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/ScheduledTasks/Running/{taskId}", s.jfRunScheduledTask).Methods("POST")
	jfAuth.HandleFunc("/ScheduledTasks/Running/{taskId}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Packages/{name}", s.jfPackageInfo).Methods("GET")
	jfAuth.HandleFunc("/Packages/Installed/{name}", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Packages/Installing/{packageId}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Plugins/{pluginId}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Plugins/{pluginId}/{version}", s.jfSessionStub).Methods("DELETE")
	jfAuth.HandleFunc("/Plugins/{pluginId}/{version}/Disable", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Plugins/{pluginId}/{version}/Enable", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Plugins/{pluginId}/{version}/Image", s.jfNotFound).Methods("GET")
	jfAuth.HandleFunc("/Plugins/{pluginId}/Configuration", s.jfEmptyObject).Methods("GET")
	jfAuth.HandleFunc("/Plugins/{pluginId}/Configuration", s.jfSessionStub).Methods("POST")
	jfAuth.HandleFunc("/Plugins/{pluginId}/Manifest", s.jfSessionStub).Methods("POST")

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
	// Boot population is NOT pushed to the Kodi sync queue.
	// Kodi sees an empty queue on first connect and performs a full scan (default).
	// Only subsequent rescans record deltas for incremental sync.
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
