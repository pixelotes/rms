package server

import (
	"encoding/json"
	"testing"
)

// The typed UserDataDTO must serialise exactly like the map it replaced:
// defaults always present, LastPlayedDate and Likes only once they are set.
func TestUserDataDTO_JSONShape(t *testing.T) {
	store := NewUserDataStore("", 0)
	liked := true

	decode := func(dto UserDataDTO) map[string]interface{} {
		t.Helper()
		raw, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// No entry: only the defaults, keyed by the item.
	empty := decode(store.toDTO("item1", nil))
	for _, k := range []string{"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "Key", "ItemId", "UnplayedItemCount"} {
		if _, ok := empty[k]; !ok {
			t.Errorf("empty: missing %s", k)
		}
	}
	for _, k := range []string{"LastPlayedDate", "Likes"} {
		if _, ok := empty[k]; ok {
			t.Errorf("empty: %s must be omitted", k)
		}
	}
	if empty["Key"] != "item1" || empty["ItemId"] != "item1" {
		t.Errorf("empty: Key/ItemId = %v/%v", empty["Key"], empty["ItemId"])
	}

	// Entry without a date or rating: still no LastPlayedDate/Likes.
	plain := decode(store.toDTO("item2", &UserDataEntry{PlayCount: 2, Played: true}))
	if plain["PlayCount"] != float64(2) || plain["Played"] != true {
		t.Errorf("plain: got %v", plain)
	}
	if _, ok := plain["LastPlayedDate"]; ok {
		t.Error("plain: LastPlayedDate must be omitted when empty")
	}
	if _, ok := plain["Likes"]; ok {
		t.Error("plain: Likes must be omitted when unset")
	}

	// Fully populated entry: everything present with its value.
	full := decode(store.toDTO("item3", &UserDataEntry{
		PlaybackPositionTicks: 42,
		IsFavorite:            true,
		LastPlayedDate:        "2026-11-05T21:00:00.0000000Z",
		Likes:                 &liked,
	}))
	if full["PlaybackPositionTicks"] != float64(42) || full["IsFavorite"] != true {
		t.Errorf("full: got %v", full)
	}
	if full["LastPlayedDate"] != "2026-11-05T21:00:00.0000000Z" {
		t.Errorf("full: LastPlayedDate = %v", full["LastPlayedDate"])
	}
	if full["Likes"] != true {
		t.Errorf("full: Likes = %v", full["Likes"])
	}
}
