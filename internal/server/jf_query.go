package server

import (
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// itemQuery holds the filtering/sorting parameters of an /Items request that
// RMS applies in memory over the collected BaseItemDto maps.
type itemQuery struct {
	excludeTypes  []string
	genres        []string
	years         map[int]bool
	nameStarts    string
	nameStartsGE  string
	nameLessThan  string
	isFavorite    *bool
	isPlayed      *bool
	filters       []string // Jellyfin ItemFilter values: IsResumable, IsFavorite, IsPlayed, IsUnplayed, IsFolder, IsNotFolder
	sortBy        []string
	sortOrder     []string
	minCommRating float64
}

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '|' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseBoolParam(r *http.Request, names ...string) *bool {
	v := queryParam(r, names...)
	if v == "" {
		return nil
	}
	b := strings.EqualFold(v, "true")
	return &b
}

func parseItemQuery(r *http.Request) itemQuery {
	q := itemQuery{
		excludeTypes: splitList(queryParam(r, "excludeItemTypes", "ExcludeItemTypes")),
		genres:       splitList(queryParam(r, "genres", "Genres")),
		nameStarts:   queryParam(r, "nameStartsWith", "NameStartsWith"),
		nameStartsGE: queryParam(r, "nameStartsWithOrGreater", "NameStartsWithOrGreater"),
		nameLessThan: queryParam(r, "nameLessThan", "NameLessThan"),
		isFavorite:   parseBoolParam(r, "isFavorite", "IsFavorite"),
		isPlayed:     parseBoolParam(r, "isPlayed", "IsPlayed"),
		filters:      splitList(queryParam(r, "filters", "Filters")),
		sortBy:       splitList(queryParam(r, "sortBy", "SortBy")),
		sortOrder:    splitList(queryParam(r, "sortOrder", "SortOrder")),
	}
	if ys := splitList(queryParam(r, "years", "Years")); len(ys) > 0 {
		q.years = map[int]bool{}
		for _, y := range ys {
			if n, err := strconv.Atoi(y); err == nil {
				q.years[n] = true
			}
		}
	}
	q.minCommRating, _ = strconv.ParseFloat(queryParam(r, "minCommunityRating", "MinCommunityRating"), 64)
	return q
}

func itemString(item map[string]interface{}, key string) string {
	v, _ := item[key].(string)
	return v
}

func itemInt(item map[string]interface{}, key string) int {
	switch v := item[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func itemFloat(item map[string]interface{}, key string) float64 {
	switch v := item[key].(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	}
	return 0
}

func itemGenres(item map[string]interface{}) []string {
	switch g := item["Genres"].(type) {
	case []string:
		return g
	case []interface{}:
		out := make([]string, 0, len(g))
		for _, x := range g {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// filterItems applies the query's filters. userID is needed for the
// favorite/played/resumable predicates, which live in the user data store.
func (s *Server) filterItems(items []map[string]interface{}, q itemQuery, userID string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if !s.itemMatches(item, q, userID) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *Server) itemMatches(item map[string]interface{}, q itemQuery, userID string) bool {
	typ := itemString(item, "Type")
	for _, t := range q.excludeTypes {
		if strings.EqualFold(t, typ) {
			return false
		}
	}
	name := strings.ToLower(itemString(item, "SortName"))
	if name == "" {
		name = strings.ToLower(itemString(item, "Name"))
	}
	if q.nameStarts != "" && !strings.HasPrefix(name, strings.ToLower(q.nameStarts)) {
		return false
	}
	if q.nameStartsGE != "" && name < strings.ToLower(q.nameStartsGE) {
		return false
	}
	if q.nameLessThan != "" && name >= strings.ToLower(q.nameLessThan) {
		return false
	}
	if len(q.genres) > 0 {
		match := false
		for _, want := range q.genres {
			for _, have := range itemGenres(item) {
				if strings.EqualFold(want, have) {
					match = true
				}
			}
		}
		if !match {
			return false
		}
	}
	if q.years != nil && !q.years[itemInt(item, "ProductionYear")] {
		return false
	}
	if q.minCommRating > 0 && itemFloat(item, "CommunityRating") < q.minCommRating {
		return false
	}

	id := itemString(item, "Id")
	isFolder, _ := item["IsFolder"].(bool)
	needUserData := q.isFavorite != nil || q.isPlayed != nil
	for _, f := range q.filters {
		switch f {
		case "IsFavorite", "IsPlayed", "IsUnplayed", "IsResumable":
			needUserData = true
		}
	}
	var ud map[string]interface{}
	if needUserData {
		ud = s.userData.Get(userID, id)
	}
	udBool := func(k string) bool { b, _ := ud[k].(bool); return b }
	if q.isFavorite != nil && udBool("IsFavorite") != *q.isFavorite {
		return false
	}
	if q.isPlayed != nil && udBool("Played") != *q.isPlayed {
		return false
	}
	for _, f := range q.filters {
		switch f {
		case "IsFavorite":
			if !udBool("IsFavorite") {
				return false
			}
		case "IsPlayed":
			if !udBool("Played") {
				return false
			}
		case "IsUnplayed":
			if udBool("Played") {
				return false
			}
		case "IsResumable":
			if !s.userData.IsResumable(userID, id) {
				return false
			}
		case "IsFolder":
			if !isFolder {
				return false
			}
		case "IsNotFolder":
			if isFolder {
				return false
			}
		}
	}
	return true
}

// sortItems orders items by the query's sortBy fields (ItemSortBy values),
// each with its own SortOrder when given. Unknown fields fall back to
// SortName; "Random" shuffles.
func (s *Server) sortItems(items []map[string]interface{}, q itemQuery, userID string) {
	fields := q.sortBy
	if len(fields) == 0 {
		fields = []string{"SortName"}
	}
	if len(fields) == 1 && fields[0] == "Random" {
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		return
	}
	desc := func(i int) bool {
		if i < len(q.sortOrder) {
			return strings.EqualFold(q.sortOrder[i], "Descending")
		}
		if len(q.sortOrder) > 0 {
			return strings.EqualFold(q.sortOrder[len(q.sortOrder)-1], "Descending")
		}
		return false
	}
	// compare returns <0, 0, >0 for one field.
	compare := func(a, b map[string]interface{}, field string) int {
		switch field {
		case "ProductionYear", "IndexNumber", "ParentIndexNumber", "ChildCount":
			return itemInt(a, field) - itemInt(b, field)
		case "CommunityRating", "CriticRating":
			return cmpFloat(itemFloat(a, field), itemFloat(b, field))
		case "Runtime":
			return cmpFloat(itemFloat(a, "RunTimeTicks"), itemFloat(b, "RunTimeTicks"))
		case "PremiereDate", "DateCreated", "DateLastContentAdded":
			return strings.Compare(itemString(a, field), itemString(b, field))
		case "DatePlayed", "DateLastPlayed":
			return strings.Compare(itemString(s.userData.Get(userID, itemString(a, "Id")), "LastPlayedDate"),
				itemString(s.userData.Get(userID, itemString(b, "Id")), "LastPlayedDate"))
		case "PlayCount":
			return itemInt(s.userData.Get(userID, itemString(a, "Id")), "PlayCount") -
				itemInt(s.userData.Get(userID, itemString(b, "Id")), "PlayCount")
		case "IsFavoriteOrLiked":
			fa, _ := s.userData.Get(userID, itemString(a, "Id"))["IsFavorite"].(bool)
			fb, _ := s.userData.Get(userID, itemString(b, "Id"))["IsFavorite"].(bool)
			return boolCmp(fa, fb)
		case "IsFolder":
			fa, _ := a["IsFolder"].(bool)
			fb, _ := b["IsFolder"].(bool)
			return boolCmp(fa, fb)
		default: // SortName, Name, Album, Artist, ...
			sa := strings.ToLower(itemString(a, "SortName"))
			sb := strings.ToLower(itemString(b, "SortName"))
			if sa == "" || sb == "" {
				sa, sb = strings.ToLower(itemString(a, "Name")), strings.ToLower(itemString(b, "Name"))
			}
			return strings.Compare(sa, sb)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		for k, f := range fields {
			c := compare(items[i], items[j], f)
			if c == 0 {
				continue
			}
			if desc(k) {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}

// paginateItems applies startIndex/limit and returns the page, the applied
// start index and the total before paging (QueryResult semantics).
func paginateItems(items []map[string]interface{}, r *http.Request) (page []map[string]interface{}, startIndex, total int) {
	total = len(items)
	startIndex, _ = strconv.Atoi(queryParam(r, "startIndex", "StartIndex"))
	limit, _ := strconv.Atoi(queryParam(r, "limit", "Limit"))
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= total {
		return items[:0], startIndex, total
	}
	page = items[startIndex:]
	if limit > 0 && limit < len(page) {
		page = page[:limit]
	}
	return page, startIndex, total
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}
