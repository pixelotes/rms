package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// readWS reads the next socket message and validates it against the named
// spec schema (e.g. "ForceKeepAliveMessage").
func readWS(t *testing.T, spec *specDoc, conn *websocket.Conn, schema string) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read %s: %v", schema, err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("%s is not JSON: %s", schema, raw)
	}
	var problems []string
	spec.check(msg, map[string]interface{}{"$ref": "#/components/schemas/" + schema}, "$", &problems)
	if len(problems) > 0 {
		t.Errorf("%s violates the spec:\n  %s\n  %s", schema, strings.Join(problems, "\n  "), raw)
	}
	return msg
}

func TestJellyfinWebSocket(t *testing.T) {
	ts, srv := newContractServer(t, "12.0")
	spec := loadSpec(t, "12.0")
	c := &contractClient{t: t, spec: spec, base: ts.URL}
	auth := c.call("POST", "/Users/AuthenticateByName", map[string]string{"Username": "rms", "Pw": "test"}, 200)
	c.token, _ = auth["AccessToken"].(string)
	uid, _ := auth["User"].(map[string]interface{})["Id"].(string)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/socket"

	// Unauthenticated upgrades are refused with 401.
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /socket: want 401 handshake failure, got err=%v resp=%v", err, resp)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?api_key="+c.token+"&deviceId=test", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Server greets with ForceKeepAlive carrying the timeout in seconds.
	fka := readWS(t, spec, conn, "ForceKeepAliveMessage")
	if fka["MessageType"] != "ForceKeepAlive" || fka["Data"] != float64(60) {
		t.Errorf("unexpected greeting: %v", fka)
	}

	// KeepAlive is answered with KeepAlive.
	if err := conn.WriteJSON(map[string]interface{}{"MessageType": "KeepAlive"}); err != nil {
		t.Fatal(err)
	}
	if ka := readWS(t, spec, conn, "OutboundKeepAliveMessage"); ka["MessageType"] != "KeepAlive" {
		t.Errorf("want KeepAlive reply, got %v", ka)
	}

	// Subscriptions are accepted silently.
	if err := conn.WriteJSON(map[string]interface{}{"MessageType": "SessionsStart", "Data": "0,1500"}); err != nil {
		t.Fatal(err)
	}

	// A user-data mutation over HTTP is pushed as UserDataChanged.
	all := c.call("GET", "/Items?recursive=true&includeItemTypes=Movie&userId="+uid, nil, 200)
	movieID, _ := items(all)[0]["Id"].(string)
	c.call("POST", "/UserFavoriteItems/"+movieID+"?userId="+uid, nil, 200)
	udc := readWS(t, spec, conn, "UserDataChangedMessage")
	data, _ := udc["Data"].(map[string]interface{})
	list, _ := data["UserDataList"].([]interface{})
	if udc["MessageType"] != "UserDataChanged" || !sameGUID(data["UserId"].(string), uid) || len(list) != 1 {
		t.Fatalf("unexpected UserDataChanged: %s", mustJSON(udc))
	}
	if ud := list[0].(map[string]interface{}); ud["ItemId"] != movieID || ud["IsFavorite"] != true {
		t.Errorf("UserDataChanged payload does not describe the favourite: %s", mustJSON(ud))
	}

	// A library rescan is pushed as LibraryChanged to every connection.
	srv.notifyLibraryChanged([]string{movieID}, nil)
	lc := readWS(t, spec, conn, "LibraryChangedMessage")
	ldata, _ := lc["Data"].(map[string]interface{})
	if added, _ := ldata["ItemsAdded"].([]interface{}); lc["MessageType"] != "LibraryChanged" || len(added) != 1 || added[0] != movieID {
		t.Errorf("unexpected LibraryChanged: %s", mustJSON(lc))
	}

	// Closing the client removes it from the hub.
	conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for srv.wsHub.count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.wsHub.count(); n != 0 {
		t.Errorf("hub still holds %d connection(s) after close", n)
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
