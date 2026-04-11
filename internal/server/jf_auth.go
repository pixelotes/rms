package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- User DTO helpers ---

func jellyfinUserPolicy() map[string]interface{} {
	return map[string]interface{}{
		"IsAdministrator":                 true,
		"IsHidden":                        false,
		"IsDisabled":                      false,
		"EnableUserPreferenceAccess":      true,
		"EnableRemoteControlOfOtherUsers": true,
		"EnableSharedDeviceControl":       true,
		"EnableRemoteAccess":              true,
		"EnableLiveTvManagement":          true,
		"EnableLiveTvAccess":              true,
		"EnableMediaPlayback":             true,
		"EnableAudioPlaybackTranscoding":  true,
		"EnableVideoPlaybackTranscoding":  true,
		"EnablePlaybackRemuxing":          true,
		"ForceRemoteSourceTranscoding":    false,
		"EnableContentDeletion":           false,
		"EnableContentDownloading":        true,
		"EnableSyncTranscoding":           true,
		"EnableMediaConversion":           false,
		"EnableAllDevices":                true,
		"EnableAllChannels":               true,
		"EnableAllFolders":                true,
		"InvalidLoginAttemptCount":        0,
		"LoginAttemptsBeforeLockout":      -1,
		"MaxActiveSessions":               0,
		"EnablePublicSharing":             true,
		"RemoteClientBitrateLimit":        0,
		"AuthenticationProviderId":        "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
		"PasswordResetProviderId":         "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
		"SyncPlayAccess":                  "CreateAndJoinGroups",
	}
}

func jellyfinUserConfig() map[string]interface{} {
	return map[string]interface{}{
		"PlayDefaultAudioTrack":       true,
		"DisplayMissingEpisodes":      false,
		"GroupedFolders":              []string{},
		"SubtitleMode":                "Default",
		"DisplayCollectionsView":      false,
		"EnableLocalPassword":         false,
		"OrderedViews":               []string{},
		"LatestItemsExcludes":        []string{},
		"MyMediaExcludes":            []string{},
		"HidePlayedInLatest":         true,
		"RememberAudioSelections":    true,
		"RememberSubtitleSelections": true,
		"EnableNextEpisodeAutoPlay":  true,
	}
}

func jellyfinUserDTO(username, userID string) map[string]interface{} {
	return map[string]interface{}{
		"Name":                      username,
		"ServerId":                  serverID,
		"Id":                        userID,
		"HasPassword":               true,
		"HasConfiguredPassword":     true,
		"HasConfiguredEasyPassword": false,
		"Policy":                    jellyfinUserPolicy(),
		"Configuration":             jellyfinUserConfig(),
	}
}

// --- Auth Endpoints ---

func (s *Server) jfUsersPublic(w http.ResponseWriter, r *http.Request) {
	var users []map[string]interface{}
	if len(s.config.Users) == 0 {
		users = append(users, map[string]interface{}{
			"Name":                  "rms",
			"Id":                    stableUserID("rms"),
			"HasPassword":           true,
			"HasConfiguredPassword": true,
		})
	} else {
		for _, u := range s.config.Users {
			users = append(users, map[string]interface{}{
				"Name":                  u.Username,
				"Id":                    stableUserID(u.Username),
				"HasPassword":           true,
				"HasConfiguredPassword": true,
			})
		}
	}
	respondJSON(w, http.StatusOK, users)
}

func (s *Server) jfAuthenticateByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
		Password string `json:"Password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	username := req.Username
	if username == "" {
		username = "rms"
	}

	pw := req.Pw
	if pw == "" {
		pw = req.Password
	}

	user := s.config.AuthenticateUser(username, pw)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	username = user.Username

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":  time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
		"user": username,
	})
	tokenString, err := token.SignedString([]byte(s.config.App.JWTSecret))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	userID := stableUserID(username)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"User":        jellyfinUserDTO(username, userID),
		"AccessToken": tokenString,
		"ServerId":    serverID,
	})
}

func (s *Server) jfGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	username := usernameFromContext(r)
	userID := stableUserID(username)
	respondJSON(w, http.StatusOK, jellyfinUserDTO(username, userID))
}

func (s *Server) jfGetUser(w http.ResponseWriter, r *http.Request) {
	username := usernameFromContext(r)
	userID := stableUserID(username)
	respondJSON(w, http.StatusOK, jellyfinUserDTO(username, userID))
}
