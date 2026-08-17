package amz

import (
	"context"
	"time"
)

// forceRobotsCheck makes a client treat its fixture server as a marketplace, so
// the robots gate runs against it. Production code reaches the gate by pointing
// at a real amazon.com host; the tests need a way in that does not require one.
func forceRobotsCheck(c *Client) { c.forceRobots = true }

// cachePathFor is where a client stores one URL, so a test can age the entry on
// disk and check what a cached read says about itself.
func cachePathFor(c *Client, rawURL string) string { return c.cache.path(rawURL) }

// setWaitFn replaces the sleep the interstitial ladder uses.
//
// The ladder waits 60s, 120s and 240s before it gives up, which is the right
// behaviour against a real challenge and seven minutes of nothing in a test. The
// override records what was asked for instead of sleeping, so the test can
// assert the schedule rather than live through it.
func setWaitFn(c *Client, fn func(context.Context, time.Duration) error) { c.waitFn = fn }

// deref reads through an optional value in a test assertion. The records keep
// the difference between absent and zero on purpose, and an assertion that a
// number equals 4.8 is not the place to relitigate it.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
