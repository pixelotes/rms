package media

import (
	"fmt"
	"crypto/sha256"
	"os"
	"strings"
	"sync"

	"raspberry-media-server/internal/config"
)

// idStore maps deterministic UUID → filesystem path.
// sync.Map is used for lock-free concurrent reads during request handling.
var idStore sync.Map

// ItemID returns a deterministic UUID for the given filesystem path.
// The same path always yields the same ID — no store lookup needed.
func ItemID(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// ItemPath resolves a UUID back to its filesystem path.
// Returns an error if the ID is not registered in the store.
func ItemPath(id string) (string, error) {
	if v, ok := idStore.Load(id); ok {
		return v.(string), nil
	}
	return "", fmt.Errorf("item not found: %s", id)
}

// PopulateIDStore walks all library paths, registers every directory and video file
// in the ID store, and returns the IDs of newly added and removed items.
func PopulateIDStore(libs []config.Library) (added, removed []string) {
	// Walk all library directories and collect registrable paths.
	newPaths := map[string]bool{}
	for _, lib := range libs {
		if _, err := os.Stat(lib.Path); err != nil {
			continue
		}
		walkLibraryPaths(lib.Path, newPaths)
	}

	// Register new paths; collect added IDs.
	for path := range newPaths {
		id := ItemID(path)
		if _, loaded := idStore.LoadOrStore(id, path); !loaded {
			added = append(added, id)
		}
	}

	// Remove IDs whose paths are no longer on disk (single pass over the store).
	idStore.Range(func(k, v interface{}) bool {
		if !newPaths[v.(string)] {
			removed = append(removed, k.(string))
			idStore.Delete(k)
		}
		return true
	})

	return added, removed
}

// walkLibraryPaths recursively registers all directories and video files under root.
func walkLibraryPaths(root string, found map[string]bool) {
	found[root] = true

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fullPath := root + "/" + e.Name()
		if e.IsDir() {
			walkLibraryPaths(fullPath, found)
		} else if IsVideoFile(e.Name()) {
			found[fullPath] = true
		}
	}
}
