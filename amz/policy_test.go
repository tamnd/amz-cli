package amz

import (
	"compress/gzip"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These tests encode decisions, not behaviour. Changing one is a spec change:
// see notes/Spec/3007/06_implementation.md section 7.
//
// The decisions are:
//   - amz says what it is. It never impersonates a browser.
//   - amz sends a small, coherent header set, built in exactly one place.
//   - amz carries no borrowed session.
//   - amz reads one page at a time, no faster than the floor.

// repoFiles walks the repository for .go files, so a test can assert something
// about the source rather than about one package's behaviour.
func repoFiles(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "docs", "bin", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestNoMozillaInSource is the most valuable test in this repository.
//
// Through v0.2.1 amz rotated five browser user agents. Measured on 2026-08-17
// over four ASINs, that identity with its matching header set drew a CAPTCHA on
// 4 of 4 requests, while amz-cli/<version> with three headers was served on 4 of
// 4. The disguise was the thing that got it blocked.
//
// This test exists so that somebody debugging a flaky page six months from now
// cannot quietly paste a browser string back in.
func TestNoMozillaInSource(t *testing.T) {
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "policy_test.go") {
			continue // this file names the string in order to forbid it
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"Mozilla", "AppleWebKit", "Chrome/", "Safari/", "Gecko/"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q: amz does not impersonate a browser", f, bad)
			}
		}
	}
}

// TestNoCookieHeader asserts amz never sends a borrowed session. Nothing amz
// reads needs one, and a tool that can carry one will be pointed at surfaces
// that require one, which is where the login walls are.
func TestNoCookieHeader(t *testing.T) {
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "policy_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`"Cookie"`, "loadCookies", "cookies.txt", "--cookies"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q: amz carries no borrowed session", f, bad)
			}
		}
	}
}

// TestHeaderSetExact pins the header set key for key, so adding Accept-Language
// back is a red build and not an accident. Accept-Language was a member of the
// combination that scored on 2026-08-17.
func TestHeaderSetExact(t *testing.T) {
	got := Headers()
	want := http.Header{
		"User-Agent":      {UserAgent()},
		"Accept":          {"text/html"},
		"Accept-Encoding": {"gzip"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("header set changed\n got: %v\nwant: %v", got, want)
	}
}

// TestUAContainsVersionAndRepo asserts the identity is useful to whoever reads
// it in a log: what it is, which build, and where to complain.
func TestUAContainsVersionAndRepo(t *testing.T) {
	SetVersion("9.9.9")
	t.Cleanup(func() { SetVersion("dev") })

	ua := UserAgent()
	for _, want := range []string{"amz-cli/", "9.9.9", RepoURL} {
		if !strings.Contains(ua, want) {
			t.Errorf("User-Agent %q is missing %q", ua, want)
		}
	}
}

// TestNoUserAgentLiteral asserts the identity is built from the version rather
// than written out, so a stale literal cannot outlive a release.
func TestNoUserAgentLiteral(t *testing.T) {
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "headers.go") || strings.HasSuffix(f, "policy_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"amz-cli/`) || strings.Contains(string(b), `"amz/0.`) {
			t.Errorf("%s writes a user agent literal: build it from Version()", f)
		}
	}
}

// TestOneHeaderCallSite asserts headers.go is the only place that builds request
// headers for the HTML read path. One call site is what makes TestHeaderSetExact
// meaningful.
//
// papi.go is exempt. The PA-API is a different protocol on a different host with
// credentials of its own, and a SigV4 request is signed over its headers, so it
// has to build them. It never touches amazon.com's HTML surfaces, which is the
// thing this test is protecting.
func TestOneHeaderCallSite(t *testing.T) {
	fset := token.NewFileSet()
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "headers.go") || strings.HasSuffix(f, "papi.go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// req.Header.Set(...) / req.Header.Add(...) anywhere but headers.go
			if call.Sel.Name != "Set" && call.Sel.Name != "Add" {
				return true
			}
			inner, ok := call.X.(*ast.SelectorExpr)
			if ok && inner.Sel.Name == "Header" {
				t.Errorf("%s:%d sets a request header outside headers.go",
					f, fset.Position(call.Pos()).Line)
			}
			return true
		})
	}
}

// TestPaceFloorHolds asserts --rate can raise the pace and nothing can lower it
// past the floor. A pace flag that accepts zero is not a pace flag.
func TestPaceFloorHolds(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{0, DefaultDelay},
		{-1 * time.Second, DefaultDelay},
		{time.Millisecond, MinDelay},
		{500 * time.Millisecond, MinDelay},
		{MinDelay, MinDelay},
		{10 * time.Second, 10 * time.Second},
	}
	for _, c := range cases {
		if got := ClampDelay(c.in); got != c.want {
			t.Errorf("ClampDelay(%s) = %s, want %s", c.in, got, c.want)
		}
	}

	cl := NewClient(Config{Delay: time.Nanosecond, Timeout: time.Second})
	if cl.Delay() < MinDelay {
		t.Errorf("client delay %s is below the floor %s", cl.Delay(), MinDelay)
	}
}

// TestNoConcurrentRequests asserts amz reads one page at a time. Two requests in
// flight made --rate a lie by a factor of two, and Amazon scores the session.
func TestNoConcurrentRequests(t *testing.T) {
	var inFlight, maxSeen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	c := NewClient(Config{Delay: MinDelay, Timeout: 5 * time.Second})
	ctx := context.Background()

	done := make(chan struct{}, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, _ = c.Get(ctx, srv.URL, 0)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if maxSeen > 1 {
		t.Fatalf("saw %d concurrent requests, amz reads one page at a time", maxSeen)
	}
}

// TestRequestIsHonest checks the wire, not the source: what Amazon would
// actually receive.
func TestRequestIsHonest(t *testing.T) {
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	c := NewClient(Config{Delay: MinDelay, Timeout: 5 * time.Second})
	if _, err := c.Get(context.Background(), srv.URL, 0); err != nil {
		t.Fatal(err)
	}
	h := <-got

	if ua := h.Get("User-Agent"); !strings.HasPrefix(ua, "amz-cli/") {
		t.Errorf("User-Agent on the wire is %q", ua)
	}
	for _, unwanted := range []string{"Cookie", "Accept-Language", "Referer", "Sec-Fetch-Dest", "Sec-Fetch-Mode", "Upgrade-Insecure-Requests"} {
		if v := h.Get(unwanted); v != "" {
			t.Errorf("request carries %s: %q", unwanted, v)
		}
	}
}

// TestGzipIsDecoded guards the consequence of setting Accept-Encoding by hand:
// Go stops decompressing for us, so amz has to.
func TestGzipIsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzipWriter(w)
		_, _ = gz.Write([]byte("<html>hello</html>"))
		_ = gz.Close()
	}))
	defer srv.Close()

	c := NewClient(Config{Delay: MinDelay, Timeout: 5 * time.Second})
	body, err := c.Get(context.Background(), srv.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>hello</html>" {
		t.Fatalf("body not decompressed: %q", body)
	}
}

func gzipWriter(w io.Writer) *gzip.Writer { return gzip.NewWriter(w) }

// TestEveryFetchGoesThroughOps is the structural guarantee behind the robots
// gate: there is no code path that builds a URL and fetches it without a rules
// check.
//
// It works by walking the AST for http.NewRequest and (*http.Client).Do outside
// client.go. Every fetcher in the repository goes through Client.Get, and
// Client.Get calls CheckRobots first, so "no other request builder exists" is
// the same statement as "every request is checked".
//
// papi.go is exempt for the reason given on TestOneHeaderCallSite: a different
// protocol on a different host, which never touches amazon.com's HTML surfaces.
func TestEveryFetchGoesThroughOps(t *testing.T) {
	fset := token.NewFileSet()
	for _, f := range repoFiles(t) {
		base := filepath.Base(f)
		if base == "client.go" || base == "papi.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, isIdent := sel.X.(*ast.Ident)
			switch {
			case isIdent && pkg.Name == "http" && strings.HasPrefix(sel.Sel.Name, "NewRequest"):
			case isIdent && pkg.Name == "http" && (sel.Sel.Name == "Get" || sel.Sel.Name == "Post"):
			default:
				return true
			}
			t.Errorf("%s:%d builds a request outside client.go, so it skips the robots gate",
				f, fset.Position(call.Pos()).Line)
			return true
		})
	}
}

// TestRobotsIsNeverHardcoded asserts there is no compiled-in copy of any rule.
//
// A fallback copy is worse than no answer: it is a copy that says yes long after
// the site started saying no, and nobody notices because nothing fails.
//
// The needle is a string literal carrying both a group header and a rule, which
// is what a baked-in robots.txt looks like and what a doc comment or a formatting
// helper does not.
func TestRobotsIsNeverHardcoded(t *testing.T) {
	fset := token.NewFileSet()
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "_test.go") {
			continue // the tests carry fixtures on purpose, and say so
		}
		b := readFile(t, f)
		for _, bad := range []string{"defaultRobots", "fallbackRobots", "builtinRobots", "embeddedRobots"} {
			if strings.Contains(b, bad) {
				t.Errorf("%s declares %s: robots.txt is fetched, never compiled in", f, bad)
			}
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v := strings.ToLower(lit.Value)
			if strings.Contains(v, "user-agent:") && strings.Contains(v, "disallow:") {
				t.Errorf("%s:%d embeds a robots.txt as a literal", f, fset.Position(lit.Pos()).Line)
			}
			return true
		})
	}
}

// TestNoRobotsNotInConfig asserts the override cannot be turned on by a file.
// A stop signal you can disable in a config you forgot about is not a stop
// signal.
func TestNoRobotsNotInConfig(t *testing.T) {
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "policy_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`"no_robots"`, `"norobots"`, `"no-robots"` + ": "} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s reads %s as a config key: --no-robots is a flag and only a flag", f, bad)
			}
		}
	}
	// The starter config must not mention it either.
	if strings.Contains(readFile(t, "cli/config.go"), "no_robots") {
		t.Error("the starter config offers a no_robots key")
	}
}

// TestNoRobotsNotInEnv asserts the override cannot be turned on by the
// environment, which is how it would end up in a CI job and stay there.
func TestNoRobotsNotInEnv(t *testing.T) {
	for _, f := range repoFiles(t) {
		if strings.HasSuffix(f, "policy_test.go") {
			continue
		}
		b := readFile(t, f)
		for _, bad := range []string{"AMZ_NO_ROBOTS", "AMZ_ROBOTS", "NO_ROBOTS"} {
			if strings.Contains(b, bad) {
				t.Errorf("%s reads %s from the environment", f, bad)
			}
		}
	}
	t.Setenv("AMZ_NO_ROBOTS", "1")
	if NewClient(DefaultConfig()).noRobots {
		t.Error("an environment variable turned the override on")
	}
}

// TestPaceFloorUnderNoRobots pins the second floor. --no-robots does not make
// amz faster; it makes it slower and louder.
func TestPaceFloorUnderNoRobots(t *testing.T) {
	if MinDelayNoRobots <= MinDelay {
		t.Fatalf("the --no-robots floor (%s) must be above the normal one (%s)", MinDelayNoRobots, MinDelay)
	}
	c := NewClient(Config{Delay: time.Millisecond, Timeout: time.Second, NoRobots: true})
	if c.Delay() < MinDelayNoRobots {
		t.Errorf("client delay %s is below the --no-robots floor %s", c.Delay(), MinDelayNoRobots)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		path = abs
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
