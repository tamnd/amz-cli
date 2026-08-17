package amz

import (
	"context"
	"time"
)

// forceRobotsCheck makes a client treat its fixture server as a marketplace, so
// the robots gate runs against it. Production code reaches the gate by pointing
// at a real amazon.com host; the tests need a way in that does not require one.
func forceRobotsCheck(c *Client) { c.forceRobots = true }

// setWaitFn replaces the sleep the interstitial ladder uses.
//
// The ladder waits 60s, 120s and 240s before it gives up, which is the right
// behaviour against a real challenge and seven minutes of nothing in a test. The
// override records what was asked for instead of sleeping, so the test can
// assert the schedule rather than live through it.
func setWaitFn(c *Client, fn func(context.Context, time.Duration) error) { c.waitFn = fn }
