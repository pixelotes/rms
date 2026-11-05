package server

import (
	"fmt"
	"net/http"
)

const serverName = "Raspberry Media Server"

// Deterministic server ID
const serverID = "d3adb33f-cafe-babe-f00d-deadbeef1234"

func (s *Server) jfSystemInfoPublic(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"LocalAddress":           fmt.Sprintf("http://localhost:%d", s.config.App.Port),
		"ServerName":             serverName,
		"Version":                s.config.App.JellyfinVersion,
		"ProductName":            "Jellyfin Server",
		"Id":                     serverID,
		"StartupWizardCompleted": true,
		"OperatingSystem":        "Linux",
	})
}

func (s *Server) jfSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"LocalAddress":               fmt.Sprintf("http://localhost:%d", s.config.App.Port),
		"ServerName":                 serverName,
		"Version":                    s.config.App.JellyfinVersion,
		"ProductName":                "Jellyfin Server",
		"Id":                         serverID,
		"StartupWizardCompleted":     true,
		"OperatingSystem":            "Linux",
		"OperatingSystemDisplayName": "Linux",
		"HasPendingRestart":          false,
		"HasUpdateAvailable":         false,
		"SupportsLibraryMonitor":     false,
		"CanSelfRestart":             true,
		"CanLaunchWebBrowser":        false,
	}
	respondJSON(w, http.StatusOK, info)
}

func (s *Server) jfBrandingConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"LoginDisclaimer":     "",
		"CustomCss":           "",
		"SplashscreenEnabled": false,
	})
}

// jfSystemStorage returns a SystemStorageDto (GET /System/Info/Storage).
// RMS has no cache/metadata folders of its own, so every folder entry points
// at the working directory and the libraries list mirrors the configured ones.
func (s *Server) jfSystemStorage(w http.ResponseWriter, r *http.Request) {
	folder := func(path string) map[string]interface{} {
		return map[string]interface{}{
			"Path":      path,
			"FreeSpace": int64(0),
			"UsedSpace": int64(0),
		}
	}
	libs := s.librariesForRequest(r)
	libraries := make([]map[string]interface{}, 0, len(libs))
	for i, lib := range libs {
		libraries = append(libraries, map[string]interface{}{
			"Id":      libraryID(i),
			"Name":    lib.FriendlyName,
			"Folders": []map[string]interface{}{folder(lib.Path)},
		})
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ProgramDataFolder":      folder("."),
		"WebFolder":              folder("web"),
		"ImageCacheFolder":       folder("."),
		"CacheFolder":            folder("."),
		"LogFolder":              folder("."),
		"InternalMetadataFolder": folder("."),
		"TranscodingTempFolder":  folder("/tmp"),
		"Libraries":              libraries,
	})
}

func (s *Server) jfEncodingConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"EncodingThreadCount":              0,
		"TranscodingTempPath":              "/tmp",
		"EnableFallbackFont":               false,
		"EnableThrottling":                 false,
		"ThrottleDelaySeconds":             180,
		"HardwareAccelerationType":         "none",
		"EnableHardwareEncoding":           false,
		"EnableSubtitleExtraction":         true,
		"EnableTonemapping":                false,
		"DeinterlaceMethod":                "yadif",
		"AllowHevcEncoding":                false,
		"AllowAv1Encoding":                 false,
		"EnableIntelLowPowerH264HwEncoder": false,
		"EnableIntelLowPowerHevcHwEncoder": false,
		"DownMixAudioBoost":                2,
		"DownMixStereoAlgorithm":           "None",
		"MaxMuxingQueueSize":               2048,
		"EnableAudioVbr":                   false,
		"H264Crf":                          23,
		"H265Crf":                          28,
	})
}
