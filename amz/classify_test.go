package amz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The bodies below are cut down from captures taken on 2026-08-17. The sizes in
// the comments are the real ones, and they are the reason the classifier has a
// size rule at all: every stop page amazon.com serves is under 4 KB and every
// real page is over 400 KB.

const (
	captchaBody = `<html><head><title>Amazon.com</title></head><body>` +
		`<form action="/errors/validateCaptcha" method="get">` +
		`<h4>Enter the characters you see below</h4>` +
		`<p>Sorry, we just need to make sure you're not a robot.</p>` +
		`<p>To discuss automated access to Amazon data please contact api-services-support@amazon.com.</p>` +
		`</form></body></html>`

	interstitialBody = `<html><head><meta name="bm-verify" content="x"></head><body>` +
		`<p>Sorry, we just need to make sure you're not a robot. For best results, please make sure your browser is accepting cookies.</p>` +
		`</body></html>`

	notFoundBody = `<html><body><img src="/dogsofamazon/rosco._TTD_.jpg">` +
		`<h1>Page Not Found</h1>` +
		`<p>Sorry! We couldn't find that page. Try searching or go to Amazon's home page.</p>` +
		`<p>Conditions of Use  Privacy Policy  api-services-support@amazon.com</p>` +
		`</body></html>`

	serverErrorBody = `<html><body><h1>Sorry! Something went wrong!</h1>` +
		`<img src="/dogsofamazon/gromit._TTD_.jpg"></body></html>`
)

// servedBody is a page the size of a real one that happens to print the
// automated access address in its footer.
//
// This is the case the size rule exists for. A substring search for that address
// on its own would call a 500 KB product page a bot wall, and the parser would
// never see a page it was perfectly able to read.
func servedBody() []byte {
	var b strings.Builder
	b.WriteString(`<html><body><div id="dp-container" data-feature-name="title"><h1>Anker 737 Power Bank</h1></div>`)
	for b.Len() < 400<<10 {
		b.WriteString(`<div data-feature-name="filler"><span class="a-size-base">Compatible with USB-C laptops and phones.</span></div>`)
	}
	b.WriteString(`<footer>To discuss automated access to Amazon data please contact api-services-support@amazon.com.</footer></body></html>`)
	return []byte(b.String())
}

func TestClassifierTellsTheStopPagesApart(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		final  string
		want   PageKind
	}{
		{"captcha", captchaBody, 200, "https://www.amazon.com/dp/B075F5X8BR", PageCaptcha},
		{"interstitial", interstitialBody, 200, "https://www.amazon.com/dp/B075F5X8BR", PageInterstitial},
		{"not found", notFoundBody, 404, "https://www.amazon.com/dp/B000000000", PageNotFound},
		{"server error", serverErrorBody, 200, "https://www.amazon.com/dp/B075F5X8BR", PageServerError},
		// A soft 404 answers 200 and looks like a page. It is still an answer,
		// and the answer is that the thing is not there.
		{"soft 404", notFoundBody, 200, "https://www.amazon.com/dp/B000000000", PageNotFound},
		{"sign-in", "<html><body>Sign in</body></html>", 200, "https://www.amazon.com/ap/signin?openid.return_to=x", PageSignIn},
		// The other half of the sign-in wall. /ap/cvf is the identity check
		// Amazon sends a reader to instead of /ap/signin often enough that
		// missing it means a redirect gets parsed as a page.
		{"identity check", "<html><body>Authentication required</body></html>", 200, "https://www.amazon.com/ap/cvf/request", PageSignIn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify([]byte(tt.body), tt.status, tt.final); got != tt.want {
				t.Errorf("Classify = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServedPageWithSupportEmailIsNotBlocked is the test the size rule is for.
//
// api-services-support@amazon.com is the address on every stop page Amazon
// serves, and it is also an address a real page is free to print. A body over
// 400 KB that carries it is a page, and a parser that refuses to read it because
// of one substring is worse than no classifier at all.
func TestServedPageWithSupportEmailIsNotBlocked(t *testing.T) {
	body := servedBody()
	if got := Classify(body, 200, "https://www.amazon.com/dp/B075F5X8BR"); got != PageServed {
		t.Fatalf("Classify = %v on a %d byte page, want served", got, len(body))
	}
	if DetectBlocked(body) {
		t.Error("DetectBlocked said a served page was a bot wall")
	}
}

// TestSmallBodyWithTheSupportAddressIsTreatedAsAStop is the same rule read from
// the other side.
//
// A short body carrying the automated access address and none of the specific
// markers is a stop page this classifier has not seen before. Guessing it is a
// page and parsing it would put whatever Amazon's next challenge says into a
// product record.
func TestSmallBodyWithTheSupportAddressIsTreatedAsAStop(t *testing.T) {
	body := []byte(`<html><body><p>To discuss automated access to Amazon data please contact api-services-support@amazon.com.</p></body></html>`)
	if got := Classify(body, 200, "https://www.amazon.com/dp/B075F5X8BR"); got != PageCaptcha {
		t.Fatalf("Classify = %v on an unrecognized %d byte stop page, want captcha", got, len(body))
	}
}

// TestSignInIsReadFromTheRedirectNotTheBody covers the one stop that is
// invisible in the HTML.
//
// /product-reviews/<asin> answers 302 to /ap/signin, and once the client has
// followed it the body is an ordinary looking Amazon page. Nothing in it says
// the reader was turned away, so the final URL is the only evidence there is.
func TestSignInIsReadFromTheRedirectNotTheBody(t *testing.T) {
	body := servedBody()
	if got := Classify(body, 200, "https://www.amazon.com/ap/signin?openid.return_to=x"); got != PageSignIn {
		t.Fatalf("Classify = %v, want sign-in from the redirect target", got)
	}
	// And the same body without the redirect is a page, which is what makes the
	// URL the load-bearing part.
	if got := Classify(body, 200, "https://www.amazon.com/product-reviews/B075F5X8BR"); got != PageServed {
		t.Fatalf("Classify = %v, want served", got)
	}
}

// TestSignInStopsTheFetchAndNamesWhereItWentSent is the exit 9 path end to end.
func TestSignInStopsTheFetchAndNamesWhereItWentSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/robots.txt"):
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
		case strings.HasPrefix(r.URL.Path, "/ap/signin"):
			_, _ = w.Write([]byte("<html><body>Sign in</body></html>"))
		default:
			http.Redirect(w, r, "/ap/signin?openid.return_to="+r.URL.Path, http.StatusFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, false)
	_, err := c.Get(context.Background(), srv.URL+"/product-reviews/B075F5X8BR", 0)
	if !errors.Is(err, ErrSignIn) {
		t.Fatalf("err = %v, want ErrSignIn", err)
	}
	var se *SignInError
	if !errors.As(err, &se) {
		t.Fatal("the error should carry where the reader was sent")
	}
	if !strings.Contains(se.Redirect, "/ap/signin") {
		t.Errorf("redirect = %q, want the sign-in target named", se.Redirect)
	}
	// The message says what amz will not do, because "needs a signed-in session"
	// on its own reads like a missing feature rather than a decision.
	if !strings.Contains(err.Error(), "will not borrow") {
		t.Errorf("message = %q", err.Error())
	}
}

// TestCaptchaIsNotRetried is the exit 5 path.
//
// A CAPTCHA is a decision rather than a rate, so asking again is not persistence,
// it is a polite client turning into an impolite one without anybody choosing to.
func TestCaptchaIsNotRetried(t *testing.T) {
	var pageHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
			return
		}
		pageHits++
		_, _ = w.Write([]byte(captchaBody))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, false)
	_, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if pageHits != 1 {
		t.Errorf("%d requests, want exactly one: a CAPTCHA is not retried", pageHits)
	}
	if !strings.Contains(err.Error(), "will not disguise itself") {
		t.Errorf("message = %q, want it to say what amz refuses to do", err.Error())
	}
}

// TestInterstitialBacksOffThenGivesUp is the exit 6 path, and the schedule is
// the assertion.
//
// The challenge is transient, so the right answer is to wait, and the waits are
// 60s, 120s and 240s. What makes this worth a test is that the wait must not
// consume a transient retry: if it did, the ladder would be cut short by the
// retry budget and amz would give up after one minute while claiming it waited
// seven. The wait is replaced rather than shortened so the schedule itself is
// what gets checked.
func TestInterstitialBacksOffThenGivesUp(t *testing.T) {
	var pageHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
			return
		}
		pageHits++
		_, _ = w.Write([]byte(interstitialBody))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, false)
	var waits []time.Duration
	setWaitFn(c, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	})
	var notes strings.Builder
	c.SetNotes(&notes)

	_, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0)
	if !errors.Is(err, ErrInterstitial) {
		t.Fatalf("err = %v, want ErrInterstitial", err)
	}
	want := []time.Duration{60 * time.Second, 120 * time.Second, 240 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("wait %d = %s, want %s", i, waits[i], want[i])
		}
	}
	// Four attempts: the first challenge and one after each of the three waits.
	if pageHits != len(want)+1 {
		t.Errorf("%d requests, want %d: the waits must not be spent from the retry budget", pageHits, len(want)+1)
	}
	// The message says how long it waited, because "gave up" without a number is
	// advice a reader cannot act on.
	if !strings.Contains(err.Error(), "after waiting 7m0s") {
		t.Errorf("message = %q, want the total wait named", err.Error())
	}
	// And it said so while it was waiting, rather than going quiet for seven
	// minutes and then reporting a failure.
	if n := strings.Count(notes.String(), "interstitial challenge"); n != len(want) {
		t.Errorf("%d notes, want one per wait: a silent seven minute pause looks like a hang", n)
	}
}

// TestServerErrorIsRetried separates the one small body that is worth asking
// again for from the ones that are not.
func TestServerErrorIsRetried(t *testing.T) {
	var pageHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/robots.txt") {
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
			return
		}
		pageHits++
		if pageHits == 1 {
			_, _ = w.Write([]byte(serverErrorBody))
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Anker 737 Power Bank</h1></body></html>`))
	}))
	defer srv.Close()

	// Retries are set here rather than taken from newTestClient, because a
	// client with no retry budget cannot demonstrate the difference this test is
	// about.
	c := NewClient(Config{Delay: MinDelay, Timeout: 5 * time.Second, CacheDir: t.TempDir(), NoCache: true, Retries: 2})
	c.SetBaseURL(srv.URL)
	forceRobotsCheck(c)
	setWaitFn(c, func(context.Context, time.Duration) error { return nil })
	body, err := c.Get(context.Background(), srv.URL+"/dp/B075F5X8BR", 0)
	if err != nil {
		t.Fatalf("a transient error page should be retried: %v", err)
	}
	if !strings.Contains(string(body), "Anker 737") {
		t.Errorf("body = %q, want the page the retry got", body)
	}
}
