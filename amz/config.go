package amz

import (
	"os"
	"path/filepath"
	"time"
)

// Defaults for the polite read path.
const (
	DefaultDelay   = 3 * time.Second
	DefaultTimeout = 30 * time.Second
	DefaultWorkers = 2
	DefaultRetries = 3
	UserAgent      = "amz/0.1 (+https://github.com/tamnd/amz-cli)"
)

// Config carries the resolved settings for a run.
type Config struct {
	Marketplace string
	Cookies     string
	UseAPI      bool
	Workers     int
	Delay       time.Duration
	Retries     int
	Timeout     time.Duration
	DataDir     string
	CacheDir    string
	DBPath      string
	NoCache     bool
	Refresh     bool

	// PA-API credentials (opt-in path).
	PAAPIAccessKey  string
	PAAPISecretKey  string
	PAAPIPartnerTag string
	PAAPIHost       string
	PAAPIRegion     string
}

// DefaultConfig returns the built-in defaults with XDG-resolved paths.
func DefaultConfig() Config {
	return Config{
		Marketplace:     "us",
		Workers:         DefaultWorkers,
		Delay:           DefaultDelay,
		Retries:         DefaultRetries,
		Timeout:         DefaultTimeout,
		DataDir:         dataDir(),
		CacheDir:        cacheDir(),
		DBPath:          filepath.Join(dataDir(), "amz.duckdb"),
		PAAPIHost:       "webservices.amazon.com",
		PAAPIRegion:     "us-east-1",
		PAAPIAccessKey:  os.Getenv("AMZ_PAAPI_ACCESS_KEY"),
		PAAPISecretKey:  os.Getenv("AMZ_PAAPI_SECRET_KEY"),
		PAAPIPartnerTag: os.Getenv("AMZ_PAAPI_PARTNER_TAG"),
	}
}

func dataDir() string {
	if d := os.Getenv("AMZ_DATA_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "amz")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "amz")
}

func cacheDir() string {
	if d := os.Getenv("AMZ_CACHE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "amz")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "amz")
}

// ConfigDir returns the XDG config directory for amz.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "amz")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "amz")
}
