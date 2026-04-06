package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"raspberry-media-server/internal/config"
)

type contextKey string

const contextKeyUser contextKey = "username"

// --- RMS Native Auth (JWT Bearer) ---

type loginRequest struct {
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	// Try authenticating with any configured user using just password
	user := s.config.AuthenticateUser("", req.Password)
	if user == nil {
		// Try all users with this password (web UI only sends password)
		for _, u := range s.config.Users {
			if u.Password == req.Password {
				user = &u
				break
			}
		}
	}
	if user == nil {
		respondError(w, http.StatusUnauthorized, "Incorrect password")
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
		"user": user.Username,
	})

	tokenString, err := token.SignedString([]byte(s.config.App.JWTSecret))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	respondJSON(w, http.StatusOK, loginResponse{Token: tokenString})
}

func (s *Server) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			respondError(w, http.StatusUnauthorized, "Missing or invalid token")
			return
		}

		tokenString := strings.TrimPrefix(auth, "Bearer ")
		username := s.parseToken(tokenString)
		if username == "" {
			respondError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- Jellyfin-Compatible Auth ---

func (s *Server) jellyfinAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ""

		if t := r.Header.Get("X-Emby-Token"); t != "" {
			tokenStr = t
		} else if t := r.Header.Get("X-MediaBrowser-Token"); t != "" {
			tokenStr = t
		}

		if tokenStr == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "MediaBrowser ") {
				for _, part := range strings.Split(auth[len("MediaBrowser "):], ",") {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, "Token=") {
						tokenStr = strings.Trim(strings.TrimPrefix(part, "Token="), "\"")
						break
					}
				}
			}
		}

		if tokenStr == "" {
			tokenStr = r.URL.Query().Get("api_key")
		}

		if tokenStr == "" {
			respondError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		username := s.parseToken(tokenStr)
		if username == "" {
			respondError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseToken validates a JWT and returns the username, or "" if invalid.
func (s *Server) parseToken(tokenString string) string {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.config.App.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	username, _ := claims["user"].(string)
	if username == "" {
		username = "rms"
	}
	return username
}

// usernameFromContext extracts the username from the request context.
func usernameFromContext(r *http.Request) string {
	if u, ok := r.Context().Value(contextKeyUser).(string); ok {
		return u
	}
	return "rms"
}

// librariesForRequest returns the libraries the current user can access.
func (s *Server) librariesForRequest(r *http.Request) []config.Library {
	return s.config.LibrariesForUser(usernameFromContext(r))
}

// stableUserID generates a deterministic UUID-like string from a username.
func stableUserID(username string) string {
	h := sha256.Sum256([]byte("rms-user-" + username))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
