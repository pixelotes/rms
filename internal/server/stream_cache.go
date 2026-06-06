package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// streamCache stores remuxed MP4s on disk so subsequent plays can be served
// via http.ServeFile (which supports Range/seek). nil means caching is
// disabled.
type streamCache struct {
	root     string
	maxBytes int64
	inflight sync.Map // map[string]chan struct{}
	evictMu  sync.Mutex
}

func newStreamCache(root string, maxGB int) *streamCache {
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		log.Printf("stream cache: cannot create %s: %v", root, err)
		return nil
	}
	log.Printf("stream cache: enabled at %s (max %d GB)", root, maxGB)
	return &streamCache{
		root:     root,
		maxBytes: int64(maxGB) * 1024 * 1024 * 1024,
	}
}

// key derives a stable cache identifier from the source path, size, mtime,
// and strategy. A source file that changes (different size or mtime) gets a
// different key, so the old cache entry is naturally orphaned and evicted.
func (c *streamCache) key(filePath, strategy string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s", filePath, info.Size(), info.ModTime().UnixNano(), strategy)))
	return hex.EncodeToString(h[:])[:32], nil
}

func (c *streamCache) finalPath(key string) string   { return filepath.Join(c.root, key+".mp4") }
func (c *streamCache) partialPath(key string) string { return filepath.Join(c.root, key+".partial") }

// claim is the entry point for a streaming request.
// Returns the final cached path if it already exists.
// Otherwise registers this caller as the "leader" responsible for building
// the cache (returns isLeader=true). Concurrent callers for the same key
// receive isLeader=false and can proceed independently (they'll stream live
// while the leader populates the cache).
func (c *streamCache) claim(key string) (cachedPath string, isLeader bool) {
	if _, err := os.Stat(c.finalPath(key)); err == nil {
		return c.finalPath(key), false
	}
	ch := make(chan struct{})
	_, loaded := c.inflight.LoadOrStore(key, ch)
	return "", !loaded
}

// complete signals that a cache build has finished. On success the .partial
// file is renamed to .mp4 and eviction runs in the background.
func (c *streamCache) complete(key string, success bool) {
	if val, ok := c.inflight.LoadAndDelete(key); ok {
		close(val.(chan struct{}))
	}
	if success {
		if err := os.Rename(c.partialPath(key), c.finalPath(key)); err != nil {
			log.Printf("stream cache: failed to finalize %s: %v", key, err)
			os.Remove(c.partialPath(key))
			return
		}
		go c.evict()
	} else {
		os.Remove(c.partialPath(key))
	}
}

// evict deletes oldest cache entries (by mtime) until total size is under
// the configured limit. Safe to call from a goroutine; mutex serializes
// concurrent invocations.
func (c *streamCache) evict() {
	c.evictMu.Lock()
	defer c.evictMu.Unlock()

	entries, err := os.ReadDir(c.root)
	if err != nil {
		return
	}

	type fi struct {
		name  string
		size  int64
		mtime int64
	}
	var files []fi
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp4") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fi{name: e.Name(), size: info.Size(), mtime: info.ModTime().UnixNano()})
		total += info.Size()
	}

	if total <= c.maxBytes {
		return
	}

	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(filepath.Join(c.root, f.name)); err != nil {
			continue
		}
		total -= f.size
		log.Printf("stream cache: evicted %s (%.1f MB)", f.name, float64(f.size)/1024/1024)
	}
}
