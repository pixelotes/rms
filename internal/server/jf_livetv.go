package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"raspberry-media-server/internal/config"
	"raspberry-media-server/internal/tv"
)

// visibleTVLibraries returns the content_type: "tv" libraries the requesting
// user can access AND that parsed at least one channel (ADR-015 visibility).
func (s *Server) visibleTVLibraries(r *http.Request) []config.Library {
	var out []config.Library
	for _, lib := range s.librariesForRequest(r) {
		if lib.ContentType == "tv" && tv.ChannelCount(lib.Path) > 0 {
			out = append(out, lib)
		}
	}
	return out
}

// jfLiveTvChannels handles GET /LiveTv/Channels. It returns the channels of
// every visible TV library as a BaseItemDtoQueryResult, honoring StartIndex
// and Limit. No EPG: AddCurrentProgram and sorting params are ignored.
func (s *Server) jfLiveTvChannels(w http.ResponseWriter, r *http.Request) {
	var channels []*tv.Channel
	for _, lib := range s.visibleTVLibraries(r) {
		channels = append(channels, tv.ChannelsForLibrary(lib.Path)...)
	}

	total := len(channels)

	start, _ := strconv.Atoi(queryParam(r, "startIndex", "StartIndex"))
	if start > 0 && start < len(channels) {
		channels = channels[start:]
	} else if start >= len(channels) {
		channels = nil
	}
	if limit, _ := strconv.Atoi(queryParam(r, "limit", "Limit")); limit > 0 && limit < len(channels) {
		channels = channels[:limit]
	}

	items := make([]map[string]interface{}, 0, len(channels))
	for _, ch := range channels {
		items = append(items, channelItem(ch))
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"Items":            items,
		"StartIndex":       start,
		"TotalRecordCount": total,
	})
}

// jfLiveTvChannel handles GET /LiveTv/Channels/{channelId}.
func (s *Server) jfLiveTvChannel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["channelId"]
	ch, ok := tv.LookupChannel(id)
	if !ok || !s.tvLibraryVisible(ch.LibKey, r) {
		respondError(w, http.StatusNotFound, "Channel not found")
		return
	}
	item := channelItem(ch)
	item["MediaSources"] = []map[string]interface{}{channelMediaSource(ch)}
	respondJSON(w, http.StatusOK, item)
}

// jfOpenLiveStream handles POST /LiveStreams/Open. The request identifies the
// channel via OpenToken or ItemId; the response carries its HLS MediaSource.
func (s *Server) jfOpenLiveStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OpenToken string `json:"OpenToken"`
		ItemID    string `json:"ItemId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	id := body.ItemID
	if id == "" {
		id = body.OpenToken
	}
	ch, ok := tv.LookupChannel(id)
	if !ok || !s.tvLibraryVisible(ch.LibKey, r) {
		respondError(w, http.StatusNotFound, "Channel not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"MediaSource": channelMediaSource(ch),
	})
}

// jfCloseLiveStream handles POST /LiveStreams/Close. RMS keeps no per-stream
// state in redirect mode, so there is nothing to tear down.
func (s *Server) jfCloseLiveStream(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// channelItem builds the BaseItemDto for a TV channel (Type "TvChannel").
func channelItem(ch *tv.Channel) map[string]interface{} {
	number := strconv.Itoa(ch.Number)
	item := map[string]interface{}{
		"Id":            ch.ID,
		"ServerId":      serverID,
		"Name":          ch.Name,
		"Type":          "TvChannel",
		"MediaType":     "Video",
		"ChannelType":   "TV",
		"IsFolder":      false,
		"Number":        number,
		"ChannelNumber": number,
		"LocationType":  "Remote",
		"UserData":      defaultUserData(ch.ID),
	}
	if ch.Logo != "" {
		item["ImageTags"] = map[string]string{"Primary": ch.ID}
	} else {
		item["ImageTags"] = map[string]string{}
	}
	return item
}

// channelMediaSource builds the HLS MediaSource for a TV channel. It is an
// infinite, non-seekable remote stream delivered through RMS's own /Videos
// endpoint (which redirects/proxies upstream).
func channelMediaSource(ch *tv.Channel) map[string]interface{} {
	streamURL := "/Videos/" + ch.ID + "/stream"
	session := "ps-" + ch.ID[:8]
	return map[string]interface{}{
		"Id":                    ch.ID,
		"Path":                  streamURL,
		"Protocol":              "Http",
		"Type":                  "Default",
		"Container":             "hls",
		"Name":                  ch.Name,
		"IsRemote":              true,
		"IsInfiniteStream":      true,
		"ETag":                  ch.ID,
		"SupportsDirectPlay":    true,
		"SupportsDirectStream":  true,
		"SupportsTranscoding":   false,
		"SupportsProbing":       false,
		"RequiresOpening":       false,
		"RequiresClosing":       false,
		"RequiresLooping":       false,
		"ReadAtNativeFramerate": false,
		"MediaStreams": []map[string]interface{}{
			{"Type": "Video", "Index": 0, "Codec": "h264", "IsDefault": true, "IsExternal": false, "VideoRange": "SDR"},
			{"Type": "Audio", "Index": 1, "Codec": "aac", "IsDefault": true, "IsExternal": false, "Language": "und"},
		},
		"DirectStreamUrl":        streamURL + "?static=true&mediaSourceId=" + ch.ID,
		"TranscodingUrl":         nil,
		"TranscodingSubProtocol": nil,
		"PlaySessionId":          session,
	}
}
