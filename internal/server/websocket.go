package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Minimal Jellyfin WebSocket (/socket): enough of the session protocol for
// clients to stay connected and receive change notifications.
//
//   - On connect the server sends ForceKeepAlive with the timeout in seconds;
//     the client then sends KeepAlive periodically and gets KeepAlive back.
//   - UserDataChanged is pushed to the owning user's connections whenever RMS
//     changes played/favorite/position/rating state through the API.
//   - LibraryChanged is pushed to everyone after a library rescan with the
//     added/removed item ids.
//
// Subscriptions such as SessionsStart or ActivityLogEntryStart are accepted
// and ignored: RMS has no sessions or activity log to report.

const (
	wsKeepAliveTimeout = 60 * time.Second // what we announce in ForceKeepAlive
	wsWriteTimeout     = 10 * time.Second
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	// Clients connect from arbitrary origins (apps, other hosts); the token
	// in the query string is the credential, not the Origin header.
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsHub struct {
	mu    sync.Mutex
	conns map[*wsConn]struct{}
}

type wsConn struct {
	conn   *websocket.Conn
	userID string
	send   chan []byte
	once   sync.Once
	done   chan struct{}
}

func newWSHub() *wsHub { return &wsHub{conns: map[*wsConn]struct{}{}} }

func (h *wsHub) add(c *wsConn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(c *wsConn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

// count returns the number of open connections (for tests and logging).
func (h *wsHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// wsMessage is the outbound envelope every Jellyfin socket message uses.
type wsMessage struct {
	MessageType string      `json:"MessageType"`
	MessageId   string      `json:"MessageId"`
	Data        interface{} `json:"Data,omitempty"`
}

func newMessageID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func encodeWS(msgType string, data interface{}) []byte {
	b, _ := json.Marshal(wsMessage{MessageType: msgType, MessageId: newMessageID(), Data: data})
	return b
}

// broadcast queues a message for every connection, or only for userID's
// connections when userID is not empty. Slow consumers are dropped rather
// than allowed to block the sender.
func (h *wsHub) broadcast(userID, msgType string, data interface{}) {
	if h == nil {
		return
	}
	payload := encodeWS(msgType, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		if userID != "" && !sameGUID(c.userID, userID) {
			continue
		}
		select {
		case c.send <- payload:
		default:
			c.close()
		}
	}
}

func (c *wsConn) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

// handleSocket upgrades GET /socket. Authentication follows the HTTP rules
// (api_key query parameter or any of the token headers).
func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request) {
	tokenStr := tokenFromRequest(r)
	username := ""
	if tokenStr != "" {
		username = s.parseToken(tokenStr)
	}
	if username == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already replied with an HTTP error
	}
	c := &wsConn{conn: conn, userID: stableUserID(username), send: make(chan []byte, 32), done: make(chan struct{})}
	s.wsHub.add(c)
	s.logDebug("websocket: %s connected (%d open)", username, s.wsHub.count())

	go c.writeLoop()
	c.send <- encodeWS("ForceKeepAlive", int(wsKeepAliveTimeout.Seconds()))
	c.readLoop()

	s.wsHub.remove(c)
	c.close()
	s.logDebug("websocket: %s disconnected (%d open)", username, s.wsHub.count())
}

func (c *wsConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case payload := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				c.close()
				return
			}
		}
	}
}

// readLoop answers KeepAlive and ignores subscriptions. A client that stays
// silent for twice the announced timeout is considered gone.
func (c *wsConn) readLoop() {
	deadline := 2 * wsKeepAliveTimeout
	c.conn.SetReadLimit(64 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(deadline))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(deadline)) })
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.conn.SetReadDeadline(time.Now().Add(deadline))
		var in struct {
			MessageType string `json:"MessageType"`
		}
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		switch strings.ToLower(in.MessageType) {
		case "keepalive":
			select {
			case c.send <- encodeWS("KeepAlive", nil):
			case <-c.done:
				return
			}
		default:
			// SessionsStart/Stop, ActivityLogEntryStart/Stop,
			// ScheduledTasksInfoStart/Stop: nothing to report.
		}
	}
}

// notifyUserData pushes UserDataChanged for one item to the user's clients.
func (s *Server) notifyUserData(userID, itemID string) {
	if s.wsHub == nil || s.wsHub.count() == 0 {
		return
	}
	s.wsHub.broadcast(userID, "UserDataChanged", map[string]interface{}{
		"UserId":       userID,
		"UserDataList": []map[string]interface{}{s.userData.Get(userID, itemID)},
	})
}

// notifyLibraryChanged pushes LibraryChanged (LibraryUpdateInfo) to every client.
func (s *Server) notifyLibraryChanged(added, removed []string) {
	if s.wsHub == nil || s.wsHub.count() == 0 || (len(added) == 0 && len(removed) == 0) {
		return
	}
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	s.wsHub.broadcast("", "LibraryChanged", map[string]interface{}{
		"FoldersAddedTo":     []string{},
		"FoldersRemovedFrom": []string{},
		"ItemsAdded":         added,
		"ItemsRemoved":       removed,
		"ItemsUpdated":       []string{},
		"CollectionFolders":  []string{},
		"IsEmpty":            false,
	})
}
