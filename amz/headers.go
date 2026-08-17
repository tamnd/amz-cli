package amz

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// RepoURL is where a site operator who sees this client in their logs can find out
// what it is and how to reach the people responsible for it.
const RepoURL = "https://github.com/tamnd/amz-cli"

// version is stamped at build time via cli.Version and handed here by SetVersion.
var version atomic.Value

func init() { version.Store("dev") }

// SetVersion records the build version used in the User-Agent.
func SetVersion(v string) {
	if v != "" {
		version.Store(v)
	}
}

// Version returns the build version.
func Version() string { return version.Load().(string) }

// UserAgent is what amz calls itself. It names the tool, its version and its repo,
// and it is never a browser string.
//
// This is not politeness for its own sake. Amazon scores the whole request, and the
// browser user agents amz used through v0.2.1, together with the header set that
// went with them, are what earned it a CAPTCHA on every page. Measured 2026-08-17
// over four ASINs: the browser identity plus the full header set failed 4 of 4, and
// this identity with the header set below succeeded 4 of 4. See notes/Spec/3007.
func UserAgent() string {
	return fmt.Sprintf("amz-cli/%s (+%s)", Version(), RepoURL)
}

// Headers returns the complete header set for every request amz makes.
//
// Three headers. Adding a fourth is a spec change, not a bug fix: an incoherent
// request is worse than an honest one, and every header amz sends is one more
// thing that has to be true. TestHeaderSetExact asserts these contents key for key
// and TestOneHeaderCallSite asserts no other file builds request headers.
func Headers() http.Header {
	return http.Header{
		"User-Agent":      {UserAgent()},
		"Accept":          {"text/html"},
		"Accept-Encoding": {"gzip"},
	}
}
