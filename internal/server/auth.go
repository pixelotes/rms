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
const sessionCookieName = "rms_token"
const sessionTTL = 30 * 24 * time.Hour

// --- RMS Native Auth (JWT Bearer + Cookie) ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	username := req.Username
	if username == "" {
		username = "rms"
	}

	user := s.config.AuthenticateUser(username, req.Password)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	tokenString, err := s.issueToken(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	setSessionCookie(w, r, tokenString)
	respondJSON(w, http.StatusOK, loginResponse{Token: tokenString, Username: user.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"username": usernameFromContext(r)})
}

func (s *Server) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"username":        usernameFromContext(r),
		"stream_strategy": s.config.Player.StreamStrategy,
	})
}

func (s *Server) issueToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":  time.Now().Add(sessionTTL).Unix(),
		"iat":  time.Now().Unix(),
		"user": username,
	})
	return token.SignedString([]byte(s.config.App.JWTSecret))
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tokenString string

		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			tokenString = strings.TrimPrefix(auth, "Bearer ")
		} else if t := r.URL.Query().Get("token"); t != "" {
			tokenString = t
		} else if c, err := r.Cookie(sessionCookieName); err == nil {
			tokenString = c.Value
		}

		if tokenString == "" {
			respondError(w, http.StatusUnauthorized, "Missing or invalid token")
			return
		}
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
