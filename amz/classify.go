package amz

import (
	"bytes"
	"errors"
	"strings"
)

// What came back, before anything tries to read it.
//
// Every one of these arrives with HTTP 200 or 404 and an HTML body, so the only
// honest way to tell them apart is to look. Five of the six are named in
// notes/Spec/3007/02_extraction.md; the sixth turned up in the captures and is
// noted below.
//
// The distinction that matters is between "Amazon said no" and "Amazon said
// nothing useful". A CAPTCHA and a sign-in wall are access controls and amz
// stops. A soft 404 is an answer, and the answer is that the thing is not there.

// PageKind is what a response body turned out to be.
type PageKind int

const (
	// PageServed is a real page. It is the only kind worth parsing.
	PageServed PageKind = iota
	// PageNotFound is the 2,296 byte "Page Not Found" body. It arrives with
	// HTTP 404 and it carries api-services-support@amazon.com, which is why a
	// substring search for that address alone misreads it as a bot wall.
	PageNotFound
	// PageSignIn is a redirect to /ap/signin. Reviews, questions and the answers
	// portal all end here for a signed-out reader.
	PageSignIn
	// PageInterstitial is the 2,264 byte Akamai bm-verify challenge. It is
	// transient: the same URL serves normally after a pause.
	PageInterstitial
	// PageCaptcha is the 3,781 byte /errors/validateCaptcha body. It is not
	// transient and amz does not solve it.
	PageCaptcha
	// PageServerError is the 2,672 byte "Sorry! Something went wrong!" body.
	// The spec named five kinds; the captures found this one, and it matters
	// because it is retryable and the other small bodies are not.
	PageServerError
)

func (k PageKind) String() string {
	switch k {
	case PageNotFound:
		return "not-found"
	case PageSignIn:
		return "sign-in"
	case PageInterstitial:
		return "interstitial"
	case PageCaptcha:
		return "captcha"
	case PageServerError:
		return "server-error"
	default:
		return "served"
	}
}

// Served reports whether this kind of body is worth parsing.
func (k PageKind) Served() bool { return k == PageServed }

// smallBody is the ceiling under which a weak signal is allowed to count.
//
// api-services-support@amazon.com is the address Amazon prints on its stop
// pages, and it is also the address a served page could one day print in a
// footer. Every stop page measured is under 4 KB and every served page measured
// is over 400 KB, so the size check turns a substring that would fire on a real
// product page into one that cannot.
const smallBody = 5 << 10

var (
	captchaMarkers = [][]byte{
		[]byte("/errors/validateCaptcha"),
		[]byte("Type the characters you see in this image"),
		[]byte("Enter the characters you see below"),
		[]byte("opfcaptcha"),
	}
	interstitialMarkers = [][]byte{
		[]byte("bm-verify"),
		[]byte("Sorry, we just need to make sure you're not a robot"),
	}
	notFoundMarkers = [][]byte{
		[]byte("dogsofamazon"),
		[]byte("Page Not Found"),
	}
	serverErrorMarkers = [][]byte{
		[]byte("Sorry! Something went wrong!"),
	}
	// weakMarkers only count under smallBody.
	weakMarkers = [][]byte{
		[]byte("api-services-support@amazon.com"),
		[]byte("To discuss automated access to Amazon data"),
		[]byte("Robot Check"),
	}
)

// Classify decides what a response is.
//
// It takes the final URL because a sign-in wall is a redirect and the body it
// lands on is a normal looking page: /product-reviews/<asin> answers 302 to
// /ap/signin, and after the client follows it there is nothing in the body that
// says the reader was turned away.
func Classify(body []byte, status int, finalURL string) PageKind {
	if isSignInURL(finalURL) {
		return PageSignIn
	}

	// Only the head of a large body is scanned. The stop pages are all under
	// 4 KB, so a marker past the first 64 KB of a 2 MB page is a coincidence.
	scan := body
	if len(scan) > 64<<10 {
		scan = scan[:64<<10]
	}

	switch {
	case containsAny(scan, captchaMarkers):
		return PageCaptcha
	case containsAny(scan, interstitialMarkers):
		return PageInterstitial
	}

	if len(body) < smallBody {
		switch {
		case containsAny(scan, serverErrorMarkers):
			return PageServerError
		case containsAny(scan, notFoundMarkers):
			return PageNotFound
		case containsAny(scan, weakMarkers):
			// A small body carrying the automated-access address and none of the
			// specific markers is a stop page amz has not seen before. Treating
			// it as a bot wall is the safe reading.
			return PageCaptcha
		}
	}

	if status == 404 {
		return PageNotFound
	}
	if status >= 500 {
		return PageServerError
	}
	return PageServed
}

func containsAny(body []byte, markers [][]byte) bool {
	for _, m := range markers {
		if bytes.Contains(body, m) {
			return true
		}
	}
	return false
}

func isSignInURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	p := pathAndQuery(rawURL)
	return strings.HasPrefix(p, "/ap/signin") || strings.HasPrefix(p, "/ap/cvf")
}

// The errors each stop maps to. The exit codes are in cli/root.go and the
// mapping is one to one, so a reader who sees a number can look it up.

// ErrBlocked is a CAPTCHA. Exit 5, no retry.
//
// amz does not solve it and does not rotate anything to get around it. An
// honestly identified client that reads three pages a minute and still gets one
// is worth reporting rather than working around, which is what the message says.
var ErrBlocked = errors.New("amazon served a CAPTCHA. amz does not solve these and will not disguise itself to avoid one.\n" +
	"If an honest client at this pace is being challenged, that is worth reporting: " + RepoURL + "/issues")

// ErrInterstitial is the Akamai challenge, after the backoff gave up. Exit 6.
var ErrInterstitial = errors.New("amazon served a bot interstitial and kept serving it")

// ErrSignIn is a redirect to /ap/signin. Exit 9, no retry.
var ErrSignIn = errors.New("amazon requires a signed-in session for this surface")

// ErrNotFound is a hard or soft 404.
var ErrNotFound = errors.New("not found")

// SignInError names where the reader was sent, so the message can be acted on.
type SignInError struct {
	URL      string
	Redirect string
}

func (e *SignInError) Error() string {
	return e.URL + " redirects to " + e.Redirect + ": this surface needs a signed-in session, which amz does not have and will not borrow"
}

func (e *SignInError) Is(target error) bool { return target == ErrSignIn }

// DetectBlocked reports whether a body is a CAPTCHA or an interstitial.
//
// Kept because the crawl queue and the store both ask this question of a body
// they have already read, with no status code and no URL to hand.
func DetectBlocked(body []byte) bool {
	k := Classify(body, 200, "")
	return k == PageCaptcha || k == PageInterstitial
}
