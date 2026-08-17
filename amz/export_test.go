package amz

// forceRobotsCheck makes a client treat its fixture server as a marketplace, so
// the robots gate runs against it. Production code reaches the gate by pointing
// at a real amazon.com host; the tests need a way in that does not require one.
func forceRobotsCheck(c *Client) { c.forceRobots = true }
