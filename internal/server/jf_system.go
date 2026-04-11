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
		"LocalAddress":               fmt.Sprintf("http://localhost:%d", s.config.App.Port),
		"ServerName":                 serverName,
		"Version":                    s.config.App.JellyfinVersion,
		"ProductName":                "Jellyfin Server",
		"Id":                         serverID,
		"StartupWizardCompleted":     true,
		"OperatingSystem":            "Linux",
		"OperatingSystemDisplayName": "Linux",
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

func (s *Server) jfSystemStorage(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, []interface{}{})
}

func (s *Server) jfEncodingConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"EncodingThreadCount":           0,
		"TranscodingTempPath":           "/tmp",
		"EnableFallbackFont":            false,
		"EnableThrottling":              false,
		"ThrottleDelaySeconds":          180,
		"HardwareAccelerationType":      "",
		"EnableHardwareEncoding":        false,
		"EnableSubtitleExtraction":      true,
		"EnableTonemapping":             false,
		"EnableDeinterlace":             true,
		"DeinterlaceMethod":             "yadif",
		"AllowHevcEncoding":             false,
		"AllowAv1Encoding":              false,
		"EnableIntelLowPowerH264HwEncoder": false,
		"EnableIntelLowPowerHevcHwEncoder": false,
		"DownMixAudioBoost":             2,
		"DownMixStereoAlgorithm":        "None",
		"MaxMuxingQueueSize":            2048,
		"EnableAudioVbr":                false,
		"H264Crf":                       23,
		"H265Crf":                       28,
	})
}
