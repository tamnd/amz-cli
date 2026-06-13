package amz

import (
	"bytes"
	"errors"
)

// ErrBlocked is returned when amazon serves a CAPTCHA or robot-check wall
// instead of the requested page. It maps to CLI exit code 5.
var ErrBlocked = errors.New("blocked by amazon (CAPTCHA / robot check); slow down with --rate, try --cookies, switch --marketplace, or use --api")

// ErrNotFound is returned when a page is a hard 404 (e.g. an unknown ASIN).
var ErrNotFound = errors.New("not found")

// blockMarkers are byte signatures of the bot wall. Amazon serves these with
// either 200 or 503, so detection is by content, not status.
var blockMarkers = [][]byte{
	[]byte("/errors/validateCaptcha"),
	[]byte("Type the characters you see in this image"),
	[]byte("Enter the characters you see below"),
	[]byte("Robot Check"),
	[]byte("To discuss automated access to Amazon data"),
	[]byte("api-services-support@amazon.com"),
	[]byte("Sorry, we just need to make sure you're not a robot"),
}

// DetectBlocked reports whether the response body is a CAPTCHA / robot wall.
func DetectBlocked(body []byte) bool {
	// The captcha page is small; only scan a prefix for speed.
	scan := body
	if len(scan) > 200_000 {
		scan = scan[:200_000]
	}
	for _, m := range blockMarkers {
		if bytes.Contains(scan, m) {
			return true
		}
	}
	return false
}
