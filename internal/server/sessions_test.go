package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// deviceClient issues requests as one client device (Authorization header with
// its own DeviceId) and validates responses against the spec.
type deviceClient struct {
	t      *testing.T
	spec   *specDoc
	base   string
	token  string
	device string
}

func (d *deviceClient) header() string {
	return `MediaBrowser Client="` + d.device + `-app", Device="` + d.device + `", DeviceId="` + d.device + `", Version="1.0", Token="` + d.token + `"`
}

func (d *deviceClient) do(method, path string, body interface{}) (int, []byte) {
	d.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, d.base+path, rd)
	req.Header.Set("Authorization", d.header())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (d *deviceClient) expect(method, path string, body interface{}, status int) []byte {
	d.t.Helper()
	got, raw := d.do(method, path, body)
	if got != status {
		d.t.Fatalf("%s %s: want %d, got %d (%s)", method, path, status, got, truncate(raw))
	}
	return raw
}

// sessionsOf fetches GET /Sessions (validated as SessionInfoDto[]) and decodes it.
func (d *deviceClient) sessionsOf(path string) []map[string]interface{} {
	d.t.Helper()
	raw := d.expect("GET", path, nil, 200)
	var list []interface{}
	if err := json.Unmarshal(raw, &list); err != nil {
		d.t.Fatalf("%s: not a JSON array: %s", path, truncate(raw))
	}
	var problems []string
	d.spec.check(list, map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/SessionInfoDto"}}, "$", &problems)
	if len(problems) > 0 {
		d.t.Errorf("%s violates SessionInfoDto:\n  %s", path, strings.Join(problems, "\n  "))
	}
	return items(map[string]interface{}{"_array": list})
}

func findSession(list []map[string]interface{}, deviceID string) map[string]interface{} {
	for _, se := range list {
		if se["DeviceId"] == deviceID {
			return se
		}
	}
	return nil
}

func TestJellyfinSessions(t *testing.T) {
	ts, _ := newContractServer(t, "12.0")
	spec := loadSpec(t, "12.0")
	login := &contractClient{t: t, spec: spec, base: ts.URL}
	auth := login.call("POST", "/Users/AuthenticateByName", map[string]string{"Username": "rms", "Pw": "test"}, 200)
	token, _ := auth["AccessToken"].(string)
	uid, _ := auth["User"].(map[string]interface{})["Id"].(string)

	remote := &deviceClient{t: t, spec: spec, base: ts.URL, token: token, device: "phone"} // the controller
	tv := &deviceClient{t: t, spec: spec, base: ts.URL, token: token, device: "livingroom-tv"}

	// The TV announces it can be controlled and starts playing a movie.
	tv.expect("POST", "/Sessions/Capabilities/Full", map[string]interface{}{
		"PlayableMediaTypes": []string{"Video"}, "SupportedCommands": []string{"DisplayMessage", "GoHome"},
		"SupportsMediaControl": true, "SupportsPersistentIdentifier": true,
	}, 204)
	login.token = token
	movies := items(login.call("GET", "/Items?recursive=true&includeItemTypes=Movie&userId="+uid, nil, 200))
	movieID, _ := movies[0]["Id"].(string)
	tv.expect("POST", "/Sessions/Playing", map[string]interface{}{
		"ItemId": movieID, "PositionTicks": 50000000, "IsPaused": false, "CanSeek": true, "PlayMethod": "DirectPlay", "MediaSourceId": movieID,
	}, 204)

	// The phone sees the TV session with NowPlayingItem and PlayState.
	all := remote.sessionsOf("/Sessions")
	tvSession := findSession(all, "livingroom-tv")
	if tvSession == nil {
		t.Fatalf("TV session missing from /Sessions: %s", mustJSON(all))
	}
	if np, _ := tvSession["NowPlayingItem"].(map[string]interface{}); np["Id"] != movieID {
		t.Errorf("NowPlayingItem: want %s, got %s", movieID, mustJSON(tvSession["NowPlayingItem"]))
	}
	if ps, _ := tvSession["PlayState"].(map[string]interface{}); ps["PositionTicks"] != float64(50000000) || ps["PlayMethod"] != "DirectPlay" || ps["CanSeek"] != true {
		t.Errorf("PlayState not mirrored: %s", mustJSON(tvSession["PlayState"]))
	}
	if tvSession["SupportsMediaControl"] != true || tvSession["SupportsRemoteControl"] != false {
		t.Errorf("control flags before socket: %s", mustJSON(tvSession))
	}
	if findSession(all, "phone") == nil {
		t.Errorf("the caller's own session should be listed by plain GET /Sessions")
	}

	// controllableByUserId lists controllable sessions except the caller's own.
	controllable := remote.sessionsOf("/Sessions?controllableByUserId=" + uid)
	if len(controllable) != 1 || controllable[0]["DeviceId"] != "livingroom-tv" {
		t.Errorf("controllableByUserId: want only the TV, got %s", mustJSON(controllable))
	}
	if byDevice := remote.sessionsOf("/Sessions?deviceId=livingroom-tv"); len(byDevice) != 1 {
		t.Errorf("deviceId filter: want 1, got %d", len(byDevice))
	}
	if recent := remote.sessionsOf("/Sessions?activeWithinSeconds=600"); len(recent) < 2 {
		t.Errorf("activeWithinSeconds=600: want both sessions, got %d", len(recent))
	}

	// Without a socket the TV cannot be controlled.
	tvID, _ := tvSession["Id"].(string)
	remote.expect("POST", "/Sessions/"+tvID+"/Playing/Pause", nil, 404)
	remote.expect("POST", "/Sessions/00000000-0000-0000-0000-000000000000/Playing/Pause", nil, 404)

	// The TV connects its WebSocket; commands from the phone arrive there.
	hdr := http.Header{"Authorization": {tv.header()}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/socket", hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	readWS(t, spec, conn, "ForceKeepAliveMessage")
	if se := findSession(remote.sessionsOf("/Sessions?deviceId=livingroom-tv"), "livingroom-tv"); se["SupportsRemoteControl"] != true {
		t.Errorf("SupportsRemoteControl should be true once the socket is up: %s", mustJSON(se))
	}

	remote.expect("POST", "/Sessions/"+tvID+"/Playing/Pause", nil, 204)
	ps := readWS(t, spec, conn, "PlaystateMessage")
	if d, _ := ps["Data"].(map[string]interface{}); ps["MessageType"] != "Playstate" || d["Command"] != "Pause" || !sameGUID(d["ControllingUserId"].(string), uid) {
		t.Errorf("Playstate: %s", mustJSON(ps))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Playing/Seek?seekPositionTicks=1234", nil, 204)
	if d, _ := readWS(t, spec, conn, "PlaystateMessage")["Data"].(map[string]interface{}); d["Command"] != "Seek" || d["SeekPositionTicks"] != float64(1234) {
		t.Errorf("Seek: %s", mustJSON(d))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Playing?itemIds="+movieID+"&playCommand=PlayNow&startPositionTicks=99", nil, 204)
	play := readWS(t, spec, conn, "PlayMessage")
	if d, _ := play["Data"].(map[string]interface{}); play["MessageType"] != "Play" || d["PlayCommand"] != "PlayNow" || d["StartPositionTicks"] != float64(99) || mustJSON(d["ItemIds"]) != `["`+movieID+`"]` {
		t.Errorf("Play: %s", mustJSON(play))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Message", map[string]interface{}{"Header": "RMS", "Text": "hello", "TimeoutMs": 5000}, 204)
	gc := readWS(t, spec, conn, "GeneralCommandMessage")
	if d, _ := gc["Data"].(map[string]interface{}); d["Name"] != "DisplayMessage" || d["Arguments"].(map[string]interface{})["Text"] != "hello" {
		t.Errorf("DisplayMessage: %s", mustJSON(gc))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Command/GoHome", nil, 204)
	if d, _ := readWS(t, spec, conn, "GeneralCommandMessage")["Data"].(map[string]interface{}); d["Name"] != "GoHome" {
		t.Errorf("GoHome: %s", mustJSON(d))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Command", map[string]interface{}{"Name": "SetVolume", "Arguments": map[string]string{"Volume": "40"}}, 204)
	if d, _ := readWS(t, spec, conn, "GeneralCommandMessage")["Data"].(map[string]interface{}); d["Name"] != "SetVolume" || d["Arguments"].(map[string]interface{})["Volume"] != "40" {
		t.Errorf("SetVolume: %s", mustJSON(d))
	}
	remote.expect("POST", "/Sessions/"+tvID+"/Command", map[string]interface{}{}, 400)

	// Stopping playback clears NowPlayingItem; Logout drops the session.
	tv.expect("POST", "/Sessions/Playing/Stopped", map[string]interface{}{"ItemId": movieID, "PositionTicks": 60000000}, 204)
	if se := findSession(remote.sessionsOf("/Sessions"), "livingroom-tv"); se == nil || se["NowPlayingItem"] != nil {
		t.Errorf("NowPlayingItem should be gone after Stopped: %s", mustJSON(se))
	}
	tv.expect("POST", "/Sessions/Logout", nil, 204)
	deadline := time.Now().Add(time.Second)
	for findSession(remote.sessionsOf("/Sessions"), "livingroom-tv") != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if findSession(remote.sessionsOf("/Sessions"), "livingroom-tv") != nil {
		t.Errorf("TV session should be gone after Logout")
	}
}
