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

	// MinDelayNoRobots is the floor once --no-robots is set. Reading what a site
	// asked you not to read is a thing to do slowly, or not at all.
	MinDelayNoRobots = 5 * time.Second
)

// ClampDelayWith applies the floor that fits the run. Under --no-robots the
// floor rises, and --rate can still only raise it further.
func ClampDelayWith(d time.Duration, noRobots bool) time.Duration {
	d = ClampDelay(d)
	if noRobots && d < MinDelayNoRobots {
		return MinDelayNoRobots
	}
	return d
}

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

	// NoRobots is the --no-robots override. It is set from the flag and from
	// nowhere else: not from config, not from the environment, not from the MCP
	// server. A stop signal you can turn off in a file you forgot about is not a
	// stop signal.
	NoRobots bool

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
		DBPath:          filepath.Join(dataDir(), "amz.db"),
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
