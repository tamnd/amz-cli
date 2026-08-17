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
	DefaultRetries = 3

	// MinDelay is the floor on request spacing. It is not overridable by flag,
	// by env or by config, because a pace flag that can be set to zero is not a
	// pace flag. --rate can raise it and nothing can lower it.
	MinDelay = 1 * time.Second
)

// ClampDelay applies the floor. A zero or negative delay means "unset", which
// resolves to the default rather than to no delay at all.
func ClampDelay(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultDelay
	}
	if d < MinDelay {
		return MinDelay
	}
	return d
}

// Config carries the resolved settings for a run.
type Config struct {
	Marketplace string
	UseAPI      bool
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
