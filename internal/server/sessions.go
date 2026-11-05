package server

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/media"
)

// In-memory Jellyfin sessions. A session is one client device (DeviceId from
// the MediaBrowser authorization header) for one user. It is created/touched
// by every authenticated request, filled by Capabilities and Playing reports,
// listed by GET /Sessions and, when the device holds a WebSocket, used as the
// target of remote-control commands (Play, Playstate, GeneralCommand).

const (
	sessionIdleTimeout = 15 * time.Minute // sessions without a socket vanish after this
	jfTimeFormat       = "2006-01-02T15:04:05.0000000Z"
)

type contextKeyDevice struct{}

// deviceInfo is what the MediaBrowser header tells us about the client.
type deviceInfo struct {
	Client, Device, DeviceID, Version string
}

// parseMediaBrowserHeader splits `MediaBrowser Client="x", Device="y", ...`
// into its key/value pairs (keys lower-cased). nil when it is not one.
func parseMediaBrowserHeader(value string) map[string]string {
	const prefix = "MediaBrowser "
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(value[len(prefix):], ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), "\"")
	}
	return out
}

// deviceFromRequest reads the client identity headers. Clients that send only
// a token get a synthetic device derived from it so they still own a session.
func deviceFromRequest(r *http.Request) deviceInfo {
	var d deviceInfo
	for _, h := range []string{"Authorization", "X-Emby-Authorization"} {
		if kv := parseMediaBrowserHeader(r.Header.Get(h)); kv != nil {
			d = deviceInfo{Client: kv["client"], Device: kv["device"], DeviceID: kv["deviceid"], Version: kv["version"]}
			break
		}
	}
	if d.DeviceID == "" {
		if t := tokenFromRequest(r); t != "" {
			d.DeviceID = stableID("device-" + t)
		}
	}
	if d.Client == "" {
		d.Client = "Unknown"
	}
	if d.Device == "" {
		d.Device = d.Client
	}
	return d
}

type session struct {
	ID              string
	UserID          string
	UserName        string
	Client          string
	DeviceName      string
	DeviceID        string
	AppVersion      string
	RemoteEndPoint  string
	LastActivity    time.Time
	LastPlaybackAt  time.Time
	MediaTypes      []string
	Commands        []string
	SupportsControl bool
	NowPlaying      map[string]interface{} // BaseItemDto or nil
	PlayState       map[string]interface{}
	ws              *wsConn
}

type sessionStore struct {
	mu   sync.Mutex
	byID map[string]*session // session id -> session
}

func newSessionStore() *sessionStore { return &sessionStore{byID: map[string]*session{}} }

func sessionIDFor(deviceID, userID string) string {
	return stableID("session|" + deviceID + "|" + userID)
}

// touch records activity for the request's device/user and returns its session.
func (st *sessionStore) touch(r *http.Request, username string) *session {
	d := deviceFromRequest(r)
	userID := stableUserID(username)
	id := sessionIDFor(d.DeviceID, userID)
	st.mu.Lock()
	defer st.mu.Unlock()
	se, ok := st.byID[id]
	if !ok {
		se = &session{
			ID: id, UserID: userID, UserName: username, DeviceID: d.DeviceID,
			MediaTypes: []string{}, Commands: []string{},
			PlayState: map[string]interface{}{
				"CanSeek": false, "IsPaused": false, "IsMuted": false,
				"RepeatMode": "RepeatNone", "PlaybackOrder": "Default",
			},
		}
		st.byID[id] = se
	}
	se.Client, se.DeviceName, se.AppVersion = d.Client, d.Device, d.Version
	se.RemoteEndPoint = r.RemoteAddr
	se.LastActivity = time.Now().UTC()
	return se
}

// find matches a session by id, or by device id for clients that pass their
// own DeviceId where a session id is expected.
func (st *sessionStore) find(idOrDevice string) *session {
	st.mu.Lock()
	defer st.mu.Unlock()
	if se, ok := st.byID[idOrDevice]; ok {
		return se
	}
	for _, se := range st.byID {
		if sameGUID(se.ID, idOrDevice) || se.DeviceID == idOrDevice {
			return se
		}
	}
	return nil
}

func (st *sessionStore) remove(id string) {
	st.mu.Lock()
	delete(st.byID, id)
	st.mu.Unlock()
}

// list returns live sessions, dropping idle ones without a socket.
func (st *sessionStore) list() []*session {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	out := make([]*session, 0, len(st.byID))
	for id, se := range st.byID {
		if se.ws == nil && now.Sub(se.LastActivity) > sessionIdleTimeout {
			delete(st.byID, id)
			continue
		}
		out = append(out, se)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActivity.After(out[j].LastActivity) })
	return out
}

// sessionFromContext returns the session touched by the auth middleware.
func sessionFromContext(r *http.Request) *session {
	se, _ := r.Context().Value(contextKeyDevice{}).(*session)
	return se
}

// dto renders a SessionInfoDto. Fields removed in 12.0 are only emitted for
// older configured versions.
func (s *Server) sessionDTO(se *session) map[string]interface{} {
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	lastPlayback := se.LastPlaybackAt
	if lastPlayback.IsZero() {
		lastPlayback = se.LastActivity
	}
	dto := map[string]interface{}{
		"Id":                    se.ID,
		"UserId":                se.UserID,
		"UserName":              se.UserName,
		"Client":                se.Client,
		"DeviceName":            se.DeviceName,
		"DeviceId":              se.DeviceID,
		"ApplicationVersion":    se.AppVersion,
		"RemoteEndPoint":        se.RemoteEndPoint,
		"ServerId":              serverID,
		"LastActivityDate":      se.LastActivity.Format(jfTimeFormat),
		"LastPlaybackCheckIn":   lastPlayback.Format(jfTimeFormat),
		"IsActive":              true,
		"HasCustomDeviceName":   false,
		"SupportsMediaControl":  se.SupportsControl,
		"SupportsRemoteControl": se.SupportsControl && se.ws != nil,
		"PlayableMediaTypes":    se.MediaTypes,
		"SupportedCommands":     se.Commands,
		"AdditionalUsers":       []interface{}{},
		"NowPlayingQueue":       []interface{}{},
		"PlayState":             se.PlayState,
		"Capabilities": map[string]interface{}{
			"PlayableMediaTypes":           se.MediaTypes,
			"SupportedCommands":            se.Commands,
			"SupportsMediaControl":         se.SupportsControl,
			"SupportsPersistentIdentifier": true,
		},
	}
	if se.NowPlaying != nil {
		dto["NowPlayingItem"] = se.NowPlaying
	}
	if !s.jfVersionAtLeast(12, 0) {
		dto["NowPlayingQueueFullItems"] = []interface{}{}
	}
	return dto
}

func (s *Server) sessionDTOs(sessions []*session) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, se := range sessions {
		out = append(out, s.sessionDTO(se))
	}
	return out
}

// ------------------------------------------------------------------ handlers

// jfSessions handles GET /Sessions with controllableByUserId, deviceId and
// activeWithinSeconds.
func (s *Server) jfSessions(w http.ResponseWriter, r *http.Request) {
	controllable := queryParam(r, "controllableByUserId", "ControllableByUserId")
	deviceID := queryParam(r, "deviceId", "DeviceId")
	within, _ := strconv.Atoi(queryParam(r, "activeWithinSeconds", "ActiveWithinSeconds"))
	self := sessionFromContext(r)

	var out []*session
	for _, se := range s.sessions.list() {
		if deviceID != "" && se.DeviceID != deviceID {
			continue
		}
		if within > 0 && time.Since(se.LastActivity) > time.Duration(within)*time.Second {
			continue
		}
		if controllable != "" {
			// Jellyfin: sessions the user may control, excluding the caller's own.
			if !se.SupportsControl || (self != nil && se.ID == self.ID) {
				continue
			}
			if !sameGUID(se.UserID, controllable) {
				continue
			}
		}
		out = append(out, se)
	}
	respondJSON(w, http.StatusOK, s.sessionDTOs(out))
}

// jfSessionCapabilities handles POST /Sessions/Capabilities (query params)
// and POST /Sessions/Capabilities/Full (ClientCapabilitiesDto body).
func (s *Server) jfSessionCapabilities(w http.ResponseWriter, r *http.Request) {
	se := sessionFromContext(r)
	if se == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var caps struct {
		PlayableMediaTypes   []string `json:"PlayableMediaTypes"`
		SupportedCommands    []string `json:"SupportedCommands"`
		SupportsMediaControl bool     `json:"SupportsMediaControl"`
	}
	if strings.HasSuffix(r.URL.Path, "/Full") {
		json.NewDecoder(r.Body).Decode(&caps)
	} else {
		caps.PlayableMediaTypes = splitList(queryParam(r, "playableMediaTypes", "PlayableMediaTypes"))
		caps.SupportedCommands = splitList(queryParam(r, "supportedCommands", "SupportedCommands"))
		caps.SupportsMediaControl = strings.EqualFold(queryParam(r, "supportsMediaControl", "SupportsMediaControl"), "true")
	}
	if caps.PlayableMediaTypes == nil {
		caps.PlayableMediaTypes = []string{}
	}
	if caps.SupportedCommands == nil {
		caps.SupportedCommands = []string{}
	}
	s.sessions.mu.Lock()
	se.MediaTypes, se.Commands, se.SupportsControl = caps.PlayableMediaTypes, caps.SupportedCommands, caps.SupportsMediaControl
	s.sessions.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// jfSessionLogout handles POST/DELETE /Sessions/Logout.
func (s *Server) jfSessionLogout(w http.ResponseWriter, r *http.Request) {
	if se := sessionFromContext(r); se != nil {
		s.sessions.remove(se.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// playbackReport is the subset of PlaybackStartInfo/ProgressInfo/StopInfo
// that feeds the session's PlayState.
type playbackReport struct {
	ItemId              string  `json:"ItemId"`
	MediaSourceId       *string `json:"MediaSourceId"`
	PositionTicks       *int64  `json:"PositionTicks"`
	IsPaused            bool    `json:"IsPaused"`
	IsMuted             bool    `json:"IsMuted"`
	CanSeek             bool    `json:"CanSeek"`
	VolumeLevel         *int    `json:"VolumeLevel"`
	AudioStreamIndex    *int    `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int    `json:"SubtitleStreamIndex"`
	PlayMethod          *string `json:"PlayMethod"`
	RepeatMode          string  `json:"RepeatMode"`
	PlaybackOrder       string  `json:"PlaybackOrder"`
	LiveStreamId        *string `json:"LiveStreamId"`
}

// updateSessionPlayback mirrors a playback report into the caller's session.
// stopped clears NowPlayingItem.
func (s *Server) updateSessionPlayback(r *http.Request, rep playbackReport, stopped bool) {
	se := sessionFromContext(r)
	if se == nil {
		return
	}
	var nowPlaying map[string]interface{}
	if !stopped && rep.ItemId != "" {
		nowPlaying = s.nowPlayingItem(r, rep.ItemId)
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	se.LastPlaybackAt = time.Now().UTC()
	if stopped {
		se.NowPlaying = nil
		se.PlayState = map[string]interface{}{
			"CanSeek": false, "IsPaused": false, "IsMuted": false,
			"RepeatMode": "RepeatNone", "PlaybackOrder": "Default",
		}
		return
	}
	if nowPlaying != nil {
		se.NowPlaying = nowPlaying
	}
	ps := map[string]interface{}{
		"CanSeek":       rep.CanSeek,
		"IsPaused":      rep.IsPaused,
		"IsMuted":       rep.IsMuted,
		"RepeatMode":    "RepeatNone",
		"PlaybackOrder": "Default",
	}
	if rep.RepeatMode != "" {
		ps["RepeatMode"] = rep.RepeatMode
	}
	if rep.PlaybackOrder != "" {
		ps["PlaybackOrder"] = rep.PlaybackOrder
	}
	if rep.PositionTicks != nil {
		ps["PositionTicks"] = *rep.PositionTicks
	}
	if rep.VolumeLevel != nil {
		ps["VolumeLevel"] = *rep.VolumeLevel
	}
	if rep.AudioStreamIndex != nil {
		ps["AudioStreamIndex"] = *rep.AudioStreamIndex
	}
	if rep.SubtitleStreamIndex != nil {
		ps["SubtitleStreamIndex"] = *rep.SubtitleStreamIndex
	}
	if rep.MediaSourceId != nil {
		ps["MediaSourceId"] = *rep.MediaSourceId
	}
	if rep.PlayMethod != nil {
		ps["PlayMethod"] = *rep.PlayMethod
	}
	if rep.LiveStreamId != nil {
		ps["LiveStreamId"] = *rep.LiveStreamId
	}
	se.PlayState = ps
}

// nowPlayingItem builds the BaseItemDto shown in SessionInfoDto.NowPlayingItem.
func (s *Server) nowPlayingItem(r *http.Request, itemID string) map[string]interface{} {
	libs := s.librariesForRequest(r)
	path, err := media.ItemPath(itemID)
	if err != nil || !media.IsPathAllowed(path, libs) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	item := s.buildJellyfinItem(path, info, findLibraryIndex(path, libs), libs)
	delete(item, "MediaSources") // keep the session listing light
	return item
}

// ------------------------------------------------------- remote control

// sendToSession forwards a socket message to the target session's device.
// Returns false (and answers 404) when the session is unknown or has no socket.
func (s *Server) sendToSession(w http.ResponseWriter, sessionID, msgType string, data interface{}) bool {
	se := s.sessions.find(sessionID)
	if se == nil {
		respondProblem(w, http.StatusNotFound, "Session not found")
		return false
	}
	s.sessions.mu.Lock()
	ws := se.ws
	s.sessions.mu.Unlock()
	if ws == nil {
		respondProblem(w, http.StatusNotFound, "Session has no active connection")
		return false
	}
	select {
	case ws.send <- encodeWS(msgType, data):
	default:
		ws.close()
		respondProblem(w, http.StatusNotFound, "Session connection is not responding")
		return false
	}
	return true
}

func controllingUserID(r *http.Request) string {
	if v := queryParam(r, "controllingUserId", "ControllingUserId"); v != "" {
		return v
	}
	return stableUserID(usernameFromContext(r))
}

// jfSessionPlay handles POST /Sessions/{sessionId}/Playing -> Play message.
func (s *Server) jfSessionPlay(w http.ResponseWriter, r *http.Request) {
	ids := splitList(queryParam(r, "itemIds", "ItemIds"))
	if ids == nil {
		ids = []string{}
	}
	req := map[string]interface{}{
		"ItemIds":           ids,
		"PlayCommand":       "PlayNow",
		"ControllingUserId": controllingUserID(r),
	}
	if v := queryParam(r, "playCommand", "PlayCommand"); v != "" {
		req["PlayCommand"] = v
	}
	if v, err := strconv.ParseInt(queryParam(r, "startPositionTicks", "StartPositionTicks"), 10, 64); err == nil {
		req["StartPositionTicks"] = v
	}
	if v := queryParam(r, "mediaSourceId", "MediaSourceId"); v != "" {
		req["MediaSourceId"] = v
	}
	for _, k := range []string{"audioStreamIndex", "subtitleStreamIndex", "startIndex"} {
		if v, err := strconv.Atoi(queryParam(r, k, strings.ToUpper(k[:1])+k[1:])); err == nil {
			req[strings.ToUpper(k[:1])+k[1:]] = v
		}
	}
	if s.sendToSession(w, mux.Vars(r)["sessionId"], "Play", req) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// jfSessionPlaystate handles POST /Sessions/{sessionId}/Playing/{command} -> Playstate message.
func (s *Server) jfSessionPlaystate(w http.ResponseWriter, r *http.Request) {
	req := map[string]interface{}{
		"Command":           mux.Vars(r)["command"],
		"ControllingUserId": controllingUserID(r),
	}
	if v, err := strconv.ParseInt(queryParam(r, "seekPositionTicks", "SeekPositionTicks"), 10, 64); err == nil {
		req["SeekPositionTicks"] = v
	}
	if s.sendToSession(w, mux.Vars(r)["sessionId"], "Playstate", req) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// jfSessionCommand handles POST /Sessions/{sessionId}/Command (GeneralCommand
// body), /Command/{command} and /System/{command} -> GeneralCommand message.
func (s *Server) jfSessionCommand(w http.ResponseWriter, r *http.Request) {
	cmd := map[string]interface{}{
		"Name":              mux.Vars(r)["command"],
		"ControllingUserId": controllingUserID(r),
		"Arguments":         map[string]interface{}{},
	}
	if cmd["Name"] == "" {
		var body struct {
			Name      string                 `json:"Name"`
			Arguments map[string]interface{} `json:"Arguments"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name == "" {
			respondProblem(w, http.StatusBadRequest, "GeneralCommand body with Name is required")
			return
		}
		cmd["Name"] = body.Name
		if body.Arguments != nil {
			cmd["Arguments"] = body.Arguments
		}
	}
	if s.sendToSession(w, mux.Vars(r)["sessionId"], "GeneralCommand", cmd) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// jfSessionMessage handles POST /Sessions/{sessionId}/Message -> DisplayMessage command.
func (s *Server) jfSessionMessage(w http.ResponseWriter, r *http.Request) {
	var msg struct {
		Header    string `json:"Header"`
		Text      string `json:"Text"`
		TimeoutMs *int64 `json:"TimeoutMs"`
	}
	json.NewDecoder(r.Body).Decode(&msg)
	if msg.Text == "" {
		msg.Text = queryParam(r, "text", "Text")
		msg.Header = queryParam(r, "header", "Header")
	}
	args := map[string]interface{}{"Header": msg.Header, "Text": msg.Text}
	if msg.TimeoutMs != nil {
		args["TimeoutMs"] = strconv.FormatInt(*msg.TimeoutMs, 10)
	}
	cmd := map[string]interface{}{
		"Name":              "DisplayMessage",
		"ControllingUserId": controllingUserID(r),
		"Arguments":         args,
	}
	if s.sendToSession(w, mux.Vars(r)["sessionId"], "GeneralCommand", cmd) {
		w.WriteHeader(http.StatusNoContent)
	}
}
