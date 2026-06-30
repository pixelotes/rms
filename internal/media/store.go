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
// A plain map guarded by RWMutex uses less memory than sync.Map for this
// read-heavy, write-rare workload. The write lock is held only for the
// pointer swap at the end of PopulateIDStore, not during the filesystem walk.
var (
	idMu    sync.RWMutex
	idStore = make(map[string]string)
)

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
	idMu.RLock()
	path, ok := idStore[id]
	idMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("item not found: %s", id)
	}
	return path, nil
}

// PopulateIDStore walks all library paths, registers every directory and video
// file in the ID store, and returns the IDs of newly added and removed items.
//
// The filesystem walk runs without holding any lock. Only the final map swap
// (a pointer assignment) acquires the write lock, keeping read contention
// near zero even for large libraries.
func PopulateIDStore(libs []config.Library) (added, removed []string) {
	// Walk filesystem — no lock held during I/O.
	newPaths := map[string]bool{}
	for _, lib := range libs {
		if _, err := os.Stat(lib.Path); err != nil {
			continue
		}
		walkLibraryPaths(lib.Path, newPaths)
	}

	// Build the replacement map entirely before acquiring any lock.
	newIDs := make(map[string]string, len(newPaths))
	for path := range newPaths {
		newIDs[ItemID(path)] = path
	}

	// Diff against the current store under a brief read lock.
	idMu.RLock()
	old := idStore
	idMu.RUnlock()

	for id := range newIDs {
		if _, exists := old[id]; !exists {
			added = append(added, id)
		}
	}
	for id := range old {
		if _, exists := newIDs[id]; !exists {
			removed = append(removed, id)
		}
	}

	// Swap in the new map. Write lock held for a pointer assignment only.
	idMu.Lock()
	idStore = newIDs
	idMu.Unlock()

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
