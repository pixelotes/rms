package server

// Contract test: starts RMS against a synthetic library for each supported
// jellyfin_version and validates every implemented Jellyfin response (status,
// keys, types, enums, required and nullable fields) against the matching
// OpenAPI spec in specs/. Needs no ffmpeg: media files are empty and the
// probe fallback path is exercised.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/media"
)

// ---------------------------------------------------------------- spec model

type specResponse struct {
	Content map[string]struct {
		Schema map[string]interface{} `json:"schema"`
	} `json:"content"`
}

type specOp struct {
	Responses map[string]specResponse `json:"responses"`
}

type specDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]map[string]interface{} `json:"schemas"`
	} `json:"components"`
	pathRegex map[string]*regexp.Regexp
}

func loadSpec(t *testing.T, version string) *specDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "specs", "jellyfin-openapi-"+version+".json"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc specDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	doc.pathRegex = map[string]*regexp.Regexp{}
	placeholder := regexp.MustCompile(`\{[^}]+\}`)
	for p := range doc.Paths {
		// Quote the literal segments and turn each {param} into one path segment.
		var pat strings.Builder
		pat.WriteString("^")
		last := 0
		for _, m := range placeholder.FindAllStringIndex(p, -1) {
			pat.WriteString(regexp.QuoteMeta(p[last:m[0]]))
			pat.WriteString("[^/]+")
			last = m[1]
		}
		pat.WriteString(regexp.QuoteMeta(p[last:]))
		pat.WriteString("$")
		doc.pathRegex[p] = regexp.MustCompile(pat.String())
	}
	return &doc
}

// op finds the spec operation for a request, preferring literal paths over
// templated ones. nil when the route is not part of this spec version.
func (d *specDoc) op(method, urlPath string) *specOp {
	var bestPath string
	bestScore := 1 << 30
	for p, re := range d.pathRegex {
		if !re.MatchString(urlPath) {
			continue
		}
		if _, ok := d.Paths[p][strings.ToLower(method)]; !ok {
			continue
		}
		if score := strings.Count(p, "{"); score < bestScore {
			bestScore, bestPath = score, p
		}
	}
	if bestPath == "" {
		return nil
	}
	var op specOp
	if err := json.Unmarshal(d.Paths[bestPath][strings.ToLower(method)], &op); err != nil {
		return nil
	}
	return &op
}

func (d *specDoc) deref(s map[string]interface{}) map[string]interface{} {
	for {
		ref, ok := s["$ref"].(string)
		if !ok {
			break
		}
		s = d.Components.Schemas[ref[strings.LastIndex(ref, "/")+1:]]
	}
	if all, ok := s["allOf"].([]interface{}); ok && len(all) == 1 {
		onlyWrapper := true
		for k := range s {
			if k != "allOf" && k != "nullable" && k != "description" {
				onlyWrapper = false
			}
		}
		if onlyWrapper {
			inner := d.deref(all[0].(map[string]interface{}))
			merged := map[string]interface{}{}
			for k, v := range inner {
				merged[k] = v
			}
			if n, ok := s["nullable"]; ok {
				merged["nullable"] = n
			}
			return merged
		}
	}
	return s
}

// check validates a decoded JSON value against a schema, appending problems.
func (d *specDoc) check(val interface{}, schema map[string]interface{}, path string, problems *[]string) {
	s := d.deref(schema)
	add := func(format string, a ...interface{}) {
		*problems = append(*problems, path+": "+fmt.Sprintf(format, a...))
	}
	if val == nil {
		if s["nullable"] != true {
			add("null in non-nullable %v", s["type"])
		}
		return
	}
	if enum, ok := s["enum"].([]interface{}); ok {
		for _, e := range enum {
			if e == val {
				return
			}
		}
		add("%v not in enum %v", val, enum)
		return
	}
	typ, _ := s["type"].(string)
	props, hasProps := s["properties"].(map[string]interface{})
	switch {
	case typ == "object" || hasProps:
		obj, ok := val.(map[string]interface{})
		if !ok {
			add("expected object, got %T", val)
			return
		}
		addl := s["additionalProperties"]
		for k, v := range obj {
			if ps, ok := props[k]; ok {
				d.check(v, ps.(map[string]interface{}), path+"."+k, problems)
			} else if am, ok := addl.(map[string]interface{}); ok {
				d.check(v, am, path+"."+k, problems)
			} else if addl == nil && hasProps {
				add("key %q is not a property of the schema", k)
			}
		}
		if req, ok := s["required"].([]interface{}); ok {
			for _, r := range req {
				if _, ok := obj[r.(string)]; !ok {
					add("missing required %v", r)
				}
			}
		}
	case typ == "array":
		arr, ok := val.([]interface{})
		if !ok {
			add("expected array, got %T", val)
			return
		}
		items, _ := s["items"].(map[string]interface{})
		for i, it := range arr {
			if i >= 50 {
				break
			}
			d.check(it, items, fmt.Sprintf("%s[%d]", path, i), problems)
		}
	case typ == "string":
		if _, ok := val.(string); !ok {
			add("expected string, got %T (%v)", val, val)
		}
	case typ == "integer":
		f, ok := val.(float64)
		if !ok || f != math.Trunc(f) {
			add("expected integer, got %T (%v)", val, val)
		}
	case typ == "number":
		if _, ok := val.(float64); !ok {
			add("expected number, got %T (%v)", val, val)
		}
	case typ == "boolean":
		if _, ok := val.(bool); !ok {
			add("expected boolean, got %T (%v)", val, val)
		}
	}
}

// ------------------------------------------------------------- test harness

type contractClient struct {
	t        *testing.T
	spec     *specDoc
	base     string
	token    string
	problems []string
	checked  int // responses validated against a spec schema
}

func newContractServer(t *testing.T, version string) (*httptest.Server, *Server) {
	t.Helper()
	root := t.TempDir()
	mk := func(rel string, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("Movies/Alpha Movie (2019)/Alpha Movie (2019).mkv", "")
	mk("Movies/Alpha Movie (2019)/movie.nfo", "<movie><title>Alpha Movie</title><year>2019</year><genre>Drama</genre><runtime>90</runtime></movie>")
	mk("Movies/Alpha Movie (2019)/Alpha Movie (2019).en.srt", "1\n00:00:01,000 --> 00:00:02,000\nhi\n")
	mk("Movies/Beta Movie (2021)/Beta Movie (2021).mkv", "")
	mk("Movies/Beta Movie (2021)/movie.nfo", "<movie><title>Beta Movie</title><year>2021</year><genre>Comedy</genre><runtime>100</runtime></movie>")
	mk("Shows/Test Show/tvshow.nfo", "<tvshow><title>Test Show</title><genre>Comedy</genre></tvshow>")
	mk("Shows/Test Show/Season 01/Test Show - S01E01 - Pilot.mkv", "")
	mk("Shows/Test Show/Season 01/Test Show - S01E02 - Second.mkv", "")
	mk("Shows/Test Show/Season 02/Test Show - S02E01 - Return.mkv", "")

	cfgYAML := fmt.Sprintf(`app:
  port: 0
  ui_enabled: false
  ui_password: "test"
  jwt_secret: "contract-test-secret"
  jellyfin_version: "%s"
libraries:
  - friendly_name: "Movies"
    path: %q
    content_type: movies
  - friendly_name: "Shows"
    path: %q
    content_type: tvseries
`, version, filepath.Join(root, "Movies"), filepath.Join(root, "Shows"))
	cfgPath := filepath.Join(root, "config.yml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv := New(cfg)
	media.PopulateIDStore(cfg.Libraries)
	ts := httptest.NewServer(srv.handler)
	t.Cleanup(ts.Close)
	return ts, srv
}

func (c *contractClient) do(method, path string, body interface{}) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	auth := `MediaBrowser Client="contract", Device="test", DeviceId="test", Version="1"`
	if c.token != "" {
		auth += `, Token="` + c.token + `"`
	}
	req.Header.Set("Authorization", auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// call performs the request and validates the response against the spec.
// Returns the decoded JSON body (nil when there is none). Routes absent from
// this spec version are skipped, not failed: RMS may serve extra routes.
func (c *contractClient) call(method, path string, body interface{}, expect int) map[string]interface{} {
	c.t.Helper()
	ctx := method + " " + path
	status, raw := c.do(method, path, body)
	op := c.spec.op(method, strings.SplitN(path, "?", 2)[0])
	if op == nil {
		return nil
	}
	c.checked++
	if expect != 0 && status != expect {
		c.problems = append(c.problems, fmt.Sprintf("%s: status %d, expected %d (%s)", ctx, status, expect, bytes.TrimSpace(raw)))
	}
	rs, ok := op.Responses[fmt.Sprint(status)]
	if !ok {
		keys := make([]string, 0, len(op.Responses))
		for k := range op.Responses {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		c.problems = append(c.problems, fmt.Sprintf("%s: status %d not documented (documented: %v)", ctx, status, keys))
		return nil
	}
	if len(rs.Content) == 0 {
		if len(bytes.TrimSpace(raw)) > 0 {
			c.problems = append(c.problems, fmt.Sprintf("%s: spec declares no body for %d, got %q", ctx, status, truncate(raw)))
		}
		return nil
	}
	ct, ok := rs.Content["application/json"]
	if !ok || len(raw) == 0 {
		return nil // binary/text responses are not schema-checked
	}
	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		c.problems = append(c.problems, fmt.Sprintf("%s: body is not JSON: %q", ctx, truncate(raw)))
		return nil
	}
	var probs []string
	c.spec.check(data, ct.Schema, "$", &probs)
	for _, p := range probs {
		c.problems = append(c.problems, ctx+" "+p)
	}
	obj, _ := data.(map[string]interface{})
	if arr, ok := data.([]interface{}); ok {
		return map[string]interface{}{"_array": arr}
	}
	return obj
}

func truncate(b []byte) string {
	if len(b) > 100 {
		return string(b[:100]) + "..."
	}
	return string(b)
}

func items(res map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	list, _ := res["Items"].([]interface{})
	if arr, ok := res["_array"].([]interface{}); ok {
		list = arr
	}
	for _, it := range list {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// --------------------------------------------------------------------- test

func TestJellyfinContract(t *testing.T) {
	for _, version := range []string{"12.0", "10.11.9", "10.10.7"} {
		t.Run(version, func(t *testing.T) {
			ts, _ := newContractServer(t, version)
			c := &contractClient{t: t, spec: loadSpec(t, version), base: ts.URL}

			// ---- login
			auth := c.call("POST", "/Users/AuthenticateByName", map[string]string{"Username": "rms", "Pw": "test"}, 200)
			if auth == nil {
				t.Fatalf("login failed: %v", c.problems)
			}
			c.token, _ = auth["AccessToken"].(string)
			user, _ := auth["User"].(map[string]interface{})
			uid, _ := user["Id"].(string)
			if c.token == "" || uid == "" {
				t.Fatalf("login returned no token/user id")
			}
			if status, _ := c.do("POST", "/Users/AuthenticateByName", map[string]string{"Username": "rms", "Pw": "wrong"}); status != 401 {
				t.Errorf("bad password: want 401, got %d", status)
			}

			// ---- system / auth / misc
			for _, e := range []struct {
				method, path string
				status       int
			}{
				{"GET", "/System/Info/Public", 200}, {"GET", "/System/Info", 200}, {"GET", "/System/Info/Storage", 200},
				{"GET", "/System/Configuration", 200}, {"GET", "/System/Endpoint", 200}, {"GET", "/Branding/Configuration", 200},
				{"GET", "/Users/Me", 200}, {"GET", "/Users/" + uid, 200}, {"GET", "/Users", 200}, {"GET", "/Users/Public", 200},
				{"GET", "/Sessions", 200}, {"GET", "/Devices", 200}, {"GET", "/ScheduledTasks", 200},
				{"GET", "/Auth/Providers", 200}, {"GET", "/Auth/PasswordResetProviders", 200},
				{"GET", "/Library/VirtualFolders", 200}, {"GET", "/Library/PhysicalPaths", 200}, {"GET", "/Items/Counts", 200},
				{"GET", "/LiveTv/Info", 200}, {"GET", "/LiveTv/Channels", 200}, {"GET", "/LiveTv/Programs", 200},
				{"GET", "/LiveTv/Timers", 200}, {"GET", "/LiveTv/SeriesTimers", 200}, {"GET", "/LiveTv/Recordings", 200},
				{"GET", "/LiveTv/GuideInfo", 200}, {"GET", "/LiveTv/Programs/Recommended", 200},
				{"GET", "/Localization/Options", 200}, {"GET", "/Localization/Countries", 200}, {"GET", "/Localization/Cultures", 200},
				{"GET", "/UserViews/GroupingOptions", 200}, {"GET", "/QuickConnect/Enabled", 200},
				{"GET", "/QuickConnect/Connect?secret=x", 404}, {"POST", "/QuickConnect/Authorize?code=1", 403},
				{"POST", "/QuickConnect/Initiate", 401}, {"POST", "/Users/AuthenticateWithQuickConnect", 400},
				{"GET", "/DisplayPreferences/usersettings?client=emby&userId=" + uid, 200},
				{"GET", "/UserViews?userId=" + uid, 200}, {"GET", "/Library/MediaFolders", 200},
				{"GET", "/Items?recursive=true&userId=" + uid, 200}, {"GET", "/Items?userId=" + uid, 200},
				{"GET", "/Items/Latest?userId=" + uid, 200}, {"GET", "/UserItems/Resume?userId=" + uid, 200},
				{"GET", "/Items/Filters?userId=" + uid, 200}, {"GET", "/Items/Filters2?userId=" + uid, 200},
				{"GET", "/Items/Suggestions?userId=" + uid, 200}, {"GET", "/Search/Hints?searchTerm=movie", 200},
				{"GET", "/Genres?userId=" + uid, 200}, {"GET", "/Movies/Recommendations?userId=" + uid, 200},
				{"GET", "/Shows/NextUp?userId=" + uid, 200}, {"GET", "/Shows/Upcoming?userId=" + uid, 200},
				{"GET", "/Persons", 200}, {"GET", "/Studios", 200}, {"GET", "/Artists", 200}, {"GET", "/Years", 200},
				{"GET", "/Startup/Configuration", 200}, {"GET", "/Plugins", 200}, {"GET", "/Packages", 200},
				{"GET", "/System/Logs", 200}, {"GET", "/GetUtcTime", 200}, {"GET", "/UserImage?userId=" + uid, 404},
				{"GET", "/FallbackFont/Fonts", 200},
				{"POST", "/Users", 204}, {"POST", "/Auth/Keys?app=x", 204}, {"DELETE", "/Branding/Splashscreen", 204},
				{"GET", "/Items/00000000000000000000000000000000/Ancestors", 404},
				{"GET", "/UserViews?userId=00000000-0000-0000-0000-000000000001", 403},
			} {
				var body interface{}
				if e.method == "POST" && strings.Contains(e.path, "QuickConnect") {
					body = map[string]string{"Secret": "x"}
				}
				c.call(e.method, e.path, body, e.status)
			}

			// ---- legacy Emby prefixes and X-Emby-Authorization header
			for _, p := range []string{"/emby/System/Info/Public", "/mediabrowser/System/Info/Public", "/EMBY/Users/Me"} {
				if status, _ := c.do("GET", p, nil); status != 200 {
					t.Errorf("legacy prefix %s: want 200, got %d", p, status)
				}
			}
			{
				req, _ := http.NewRequest("GET", c.base+"/Users/Me", nil)
				req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="legacy", Device="d", DeviceId="i", Version="1", Token="`+c.token+`"`)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Errorf("X-Emby-Authorization token: want 200, got %d", resp.StatusCode)
				}
			}

			// ---- case-insensitive routing (ASP.NET semantics)
			for _, p := range []string{"/users/me", "/system/info/public", "/SYSTEM/INFO", "/userviews?userId=" + uid, "/items/latest?userId=" + uid} {
				if status, _ := c.do("GET", p, nil); status != 200 {
					t.Errorf("case-insensitive route %s: want 200, got %d", p, status)
				}
			}
			if status, raw := c.do("GET", "/items/00000000000000000000000000000000/images/primary", nil); status != 404 || !bytes.Contains(raw, []byte(`"status":404`)) {
				t.Errorf("lowercase image route should reach the handler (ProblemDetails 404), got %d %q", status, truncate(raw))
			}

			// ---- item-level flows
			all := c.call("GET", "/Items?recursive=true&userId="+uid, nil, 200)
			byType := map[string]string{}
			for _, it := range items(all) {
				if typ, _ := it["Type"].(string); typ != "" {
					if _, seen := byType[typ]; !seen {
						byType[typ], _ = it["Id"].(string)
					}
				}
			}
			for _, want := range []string{"Movie", "Series", "Season", "Episode"} {
				if byType[want] == "" {
					t.Errorf("recursive /Items returned no %s", want)
				}
			}
			for typ, id := range byType {
				c.call("GET", "/Items/"+id+"?userId="+uid, nil, 200)
				c.call("GET", "/Items/"+id+"/Ancestors?userId="+uid, nil, 200)
				c.call("GET", "/Items/"+id+"/Similar?userId="+uid, nil, 200)
				c.call("GET", "/Items/"+id+"/ThemeMedia?userId="+uid, nil, 200)
				c.call("GET", "/MediaSegments/"+id, nil, 200)
				c.call("GET", "/Items/"+id+"/MetadataEditor", nil, 200)
				c.call("GET", "/Items/"+id+"/ExternalIdInfos", nil, 200)
				c.call("GET", "/UserItems/"+id+"/UserData?userId="+uid, nil, 200)
				c.call("GET", "/Items/"+id+"/Images/Primary", nil, 404)
				if typ != "Movie" && typ != "Episode" {
					continue
				}
				c.call("POST", "/Items/"+id+"/PlaybackInfo?userId="+uid, map[string]string{"UserId": uid}, 200)
				c.call("GET", "/Items/"+id+"/PlaybackInfo?userId="+uid, nil, 200)
				c.call("POST", "/UserItems/"+id+"/UserData?userId="+uid, map[string]int64{"PlaybackPositionTicks": 1234567}, 200)
				ud := c.call("POST", "/UserItems/"+id+"/UserData?userId="+uid, map[string]bool{"IsFavorite": true}, 200)
				if pos, _ := ud["PlaybackPositionTicks"].(float64); pos != 1234567 {
					t.Errorf("[%s] partial UserData update clobbered PlaybackPositionTicks: %v", typ, ud["PlaybackPositionTicks"])
				}
				c.call("POST", "/UserFavoriteItems/"+id+"?userId="+uid, nil, 200)
				c.call("DELETE", "/UserFavoriteItems/"+id+"?userId="+uid, nil, 200)
				c.call("POST", "/UserPlayedItems/"+id+"?userId="+uid, nil, 200)
				c.call("DELETE", "/UserPlayedItems/"+id+"?userId="+uid, nil, 200)
				rating := c.call("POST", "/UserItems/"+id+"/Rating?userId="+uid+"&likes=true", nil, 200)
				if rating["Likes"] != true {
					t.Errorf("[%s] Rating likes=true not reflected: %v", typ, rating["Likes"])
				}
				c.call("DELETE", "/UserItems/"+id+"/Rating?userId="+uid, nil, 200)
				c.call("POST", "/Sessions/Playing", map[string]interface{}{"ItemId": id, "PositionTicks": 0}, 204)
				c.call("POST", "/Sessions/Playing/Progress", map[string]interface{}{"ItemId": id, "PositionTicks": 100}, 204)
				c.call("POST", "/Sessions/Playing/Stopped", map[string]interface{}{"ItemId": id, "PositionTicks": 100}, 204)
			}

			// ---- shows: pagination and season filter
			if sid := byType["Series"]; sid != "" {
				c.call("GET", "/Shows/"+sid+"/Seasons?userId="+uid, nil, 200)
				eps := c.call("GET", "/Shows/"+sid+"/Episodes?userId="+uid, nil, 200)
				if got := len(items(eps)); got != 3 {
					t.Errorf("Episodes: want 3, got %d", got)
				}
				s2 := c.call("GET", "/Shows/"+sid+"/Episodes?userId="+uid+"&season=2", nil, 200)
				if got := len(items(s2)); got != 1 {
					t.Errorf("Episodes?season=2: want 1, got %d", got)
				}
				page := c.call("GET", "/Shows/"+sid+"/Episodes?userId="+uid+"&startIndex=1&limit=1", nil, 200)
				if got, total := len(items(page)), page["TotalRecordCount"]; got != 1 || total != float64(3) {
					t.Errorf("Episodes paging: got %d items, TotalRecordCount %v", got, total)
				}
			}

			// ---- /Items filters and sorting
			movies := c.call("GET", "/Items?recursive=true&includeItemTypes=Movie&sortBy=ProductionYear&sortOrder=Descending&userId="+uid, nil, 200)
			if list := items(movies); len(list) != 2 || list[0]["Name"] != "Beta Movie" {
				t.Errorf("sortBy=ProductionYear desc: unexpected order %v", names(list))
			}
			drama := c.call("GET", "/Items?recursive=true&includeItemTypes=Movie&genres=Drama&userId="+uid, nil, 200)
			if list := items(drama); len(list) != 1 || list[0]["Name"] != "Alpha Movie" {
				t.Errorf("genres=Drama: got %v", names(list))
			}
			excl := c.call("GET", "/Items?recursive=true&excludeItemTypes=Movie,Video,Series,Season&userId="+uid, nil, 200)
			for _, it := range items(excl) {
				if it["Type"] != "Episode" {
					t.Errorf("excludeItemTypes leaked %v", it["Type"])
				}
			}
			views := c.call("GET", "/Items?userId="+uid, nil, 200)
			if got := len(items(views)); got != 2 {
				t.Errorf("/Items without parentId should list the 2 library views, got %d", got)
			}

			// ---- external subtitle stream (sidecar index after container streams)
			if mid := byType["Movie"]; mid != "" {
				pi := c.call("POST", "/Items/"+mid+"/PlaybackInfo?userId="+uid, nil, 200)
				for _, src := range items(map[string]interface{}{"Items": pi["MediaSources"]}) {
					for _, st := range items(map[string]interface{}{"Items": src["MediaStreams"]}) {
						if url, _ := st["DeliveryUrl"].(string); url != "" {
							if status, _ := c.do("GET", url, nil); status != 200 {
								t.Errorf("subtitle DeliveryUrl %s -> %d", url, status)
							}
						}
					}
				}
			}

			// Guard against the harness silently skipping routes (e.g. a path
			// matcher regression): the table above covers well over 100 calls.
			if c.checked < 100 {
				t.Errorf("only %d responses were checked against the spec; expected > 100", c.checked)
			}
			t.Logf("%d responses validated against spec %s", c.checked, version)
			if len(c.problems) > 0 {
				sort.Strings(c.problems)
				t.Errorf("%d contract violation(s) against spec %s:\n  %s", len(c.problems), version, strings.Join(c.problems, "\n  "))
			}
		})
	}
}

func names(list []map[string]interface{}) []string {
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, fmt.Sprint(it["Name"]))
	}
	return out
}
