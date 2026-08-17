package amz

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

// Cache is a tiny on-disk page cache keyed by a hash of the URL.
type Cache struct {
	dir string
}

// NewCache returns a cache rooted at dir (created on first write).
func NewCache(dir string) *Cache { return &Cache{dir: dir} }

func (c *Cache) path(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, "pages", h[:2], h+".html")
}

// Get returns the cached body if present and fresher than ttl.
func (c *Cache) Get(rawURL string, ttl time.Duration) ([]byte, bool) {
	b, _, ok := c.GetAt(rawURL, ttl)
	return b, ok
}

// GetAt is Get with the time the body was written.
//
// A record built from a cached page was not retrieved when the command ran, and
// a source that stamped it with the moment of the read would be dating a
// yesterday's price to today. The write time is what the entry means.
func (c *Cache) GetAt(rawURL string, ttl time.Duration) ([]byte, time.Time, bool) {
	p := c.path(rawURL)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	if ttl > 0 && time.Since(fi.ModTime()) > ttl {
		return nil, time.Time{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	return b, fi.ModTime().UTC(), true
}

// Put writes the body to the cache.
func (c *Cache) Put(rawURL string, body []byte) error {
	p := c.path(rawURL)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}

// Dir returns the cache root.
func (c *Cache) Dir() string { return c.dir }
