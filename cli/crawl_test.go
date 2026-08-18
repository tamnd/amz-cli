package cli

import (
	"errors"
	"strings"
	"testing"
)

// codeOf is the process exit code a command failed with, which for these tests
// is as much of the answer as the message is: exit 2 and exit 3 send a caller
// down different paths.
func codeOf(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return CodeRuntime
}

// A crawl says what it will cost before it spends it, and --dry-run reads
// nothing at all. The fixture server would answer if anything asked it, so a
// plan that took a request would show up as a plan that is slower than the
// numbers it printed.
func TestCrawlDryRunPricesTheWorkAndReadsNothing(t *testing.T) {
	fixtureServer(t)
	dir := t.TempDir()
	out, _, err := runSplit(t, "--data-dir", dir, "--dry-run",
		"crawl", "--chart", "bestsellers", "--category", "electronics", "--depth", "quick")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"plan",
		"chart pages",
		"detail pages",
		"light, /gp/aw/d/",
		"requests",
		"at the",
		"pace",
		"frontier",
		"rails off",
		"--with-text",
		"nothing was read",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan does not mention %q:\n%s", want, out)
		}
	}
}

// The frontier line has to change when the frontier does, or it is decoration.
func TestCrawlDryRunDescribesTheRailBudget(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "--data-dir", t.TempDir(), "--dry-run",
		"crawl", "--asin", "B084DWG2VQ", "--follow-rails", "--rail-depth", "2", "--rail-budget", "50")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rails on", "depth 2", "budget 50", "sponsored cards excluded", "via rails"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan does not mention %q:\n%s", want, out)
		}
	}
}

// Somebody will try --from-sitemap, because every other crawler in the world has
// one. There is no sitemap, and a flag that silently did nothing would send them
// looking for the bug in their own shell quoting.
func TestCrawlFromSitemapExplainsItself(t *testing.T) {
	_, _, err := runSplit(t, "--data-dir", t.TempDir(), "crawl", "--from-sitemap")
	if err == nil {
		t.Fatal("--from-sitemap was accepted")
	}
	if code := codeOf(err); code != CodeUsage {
		t.Errorf("exit code %d, want %d for a usage error", code, CodeUsage)
	}
	for _, want := range []string{"no sitemap", "chart", "search", "rail", "seed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// --no-robots on a single read is one request somebody chose to make. On a crawl
// it is thousands, unattended, so it needs the second hand on the switch.
func TestCrawlUnderNoRobotsNeedsYes(t *testing.T) {
	_, _, err := runSplit(t, "--data-dir", t.TempDir(), "--no-robots", "crawl", "--asin", "B084DWG2VQ")
	if err == nil {
		t.Fatal("a --no-robots crawl started without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// A crawl with nothing to crawl is a usage error naming the flags, rather than a
// command that runs for no time and reports success.
func TestCrawlWithNoSeedsSaysWhatIsMissing(t *testing.T) {
	_, _, err := runSplit(t, "--data-dir", t.TempDir(), "crawl", "--resume=false")
	if err == nil {
		t.Fatal("a crawl with no seeds was accepted")
	}
	for _, want := range []string{"--seed", "--chart", "--search"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

// The local commands never fetch. A store with nothing in it answers "not here"
// and names the command that would go and get it, rather than going and getting
// it, because the whole point of paying for a crawl is that afterwards the
// answers are local and instant.
func TestLookupNeverFetches(t *testing.T) {
	fixtureServer(t)
	_, _, err := runSplit(t, "--data-dir", t.TempDir(), "lookup", "B084DWG2VQ")
	if err == nil {
		t.Fatal("lookup found a record in an empty store")
	}
	if code := codeOf(err); code != CodeNoData {
		t.Errorf("exit code %d, want %d", code, CodeNoData)
	}
	for _, want := range []string{"never fetches", "amz product", "amz crawl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// The store holds hours of somebody's bandwidth and Amazon's. A mistyped
// statement at a prompt must not be able to empty it, and that is enforced
// rather than documented.
func TestQueryRefusesToWrite(t *testing.T) {
	dir := t.TempDir()
	// Create the store first, so the refusal is about the statement and not
	// about a missing file.
	if _, _, err := runSplit(t, "--data-dir", dir, "db", "stats"); err != nil {
		t.Fatal(err)
	}
	_, _, err := runSplit(t, "--data-dir", dir, "query", "DELETE FROM product")
	if err == nil {
		t.Fatal("a DELETE was run")
	}
	if code := codeOf(err); code != CodeUsage {
		t.Errorf("exit code %d, want %d", code, CodeUsage)
	}
}

// The engine line exists because the v0.2 store shelled out to a duckdb binary,
// which made the README's first line false. Saying which engine is in use, and
// that it needs nothing installed, is the answer to that finding.
func TestDoctorNamesTheStoreEngine(t *testing.T) {
	out, _, err := runSplit(t, "--data-dir", t.TempDir(), "doctor", "--offline")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sqlite") || !strings.Contains(out, "no external binary") {
		t.Errorf("doctor does not say what the store runs on:\n%s", out)
	}
}
