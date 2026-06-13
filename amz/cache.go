package amz

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
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
	p := c.path(rawURL)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	if ttl > 0 && time.Since(fi.ModTime()) > ttl {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
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

func requestURL(s string) (*url.URL, error) { return url.Parse(s) }
