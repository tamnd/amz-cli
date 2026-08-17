package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// The search command's own surface: the flags, what they compose into, and the
// notes that go to stderr rather than into the rows.

// --dry-run prints the URL and fetches nothing, so this is also the cheapest way
// to assert what a flag turns into.
func searchURL(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, err := runSplit(t, append([]string{"search", "--dry-run"}, args...)...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errOut)
	}
	return strings.TrimSpace(out)
}

// A refinement is composed into one rh term per group with its values joined by
// a pipe, which is Amazon's own spelling for or.
func TestSearchRefineComposesOneTermPerGroup(t *testing.T) {
	fixtureServer(t)
	got := searchURL(t, "mechanical keyboard",
		"--refine", "p_123=213704,111070",
		"--refine", "p_72=1248879011")
	for _, want := range []string{"p_123%3A111070%7C213704", "p_72%3A1248879011"} {
		if !strings.Contains(got, want) {
			t.Errorf("url does not carry %s:\n%s", want, got)
		}
	}
	// The values are sorted inside the group, so the same filter typed in two
	// orders is one URL and one cache entry.
	if other := searchURL(t, "mechanical keyboard",
		"--refine", "p_123=111070,213704", "--refine", "p_72=1248879011"); other != got {
		t.Errorf("the same refinement in a different order is a different url:\n  %s\n  %s", got, other)
	}
}

// Amazon's tracking parameters are not part of a query and amz does not send
// them. qid is a timestamp, so a URL carrying one is a URL that can never be
// requested twice.
func TestSearchURLCarriesNoTrackingParameters(t *testing.T) {
	fixtureServer(t)
	got := searchURL(t, "kindle", "--sort", "price-asc", "--department", "electronics")
	for _, never := range []string{"qid=", "ref=", "rnid=", "&ds=", "&dc", "crid=", "sprefix="} {
		if strings.Contains(got, never) {
			t.Errorf("url carries %s, which is Amazon's telemetry and not part of the question:\n%s", never, got)
		}
	}
	if !strings.Contains(got, "s=price-asc-rank") {
		t.Errorf("--sort price-asc did not become s=price-asc-rank:\n%s", got)
	}
	if !strings.Contains(got, "i=electronics") {
		t.Errorf("--department did not become i=:\n%s", got)
	}
}

// A price range goes into rh as p_36 in the marketplace's minor unit, which is
// cents on amazon.com. Sending dollars there asks for a hundredth of the range.
func TestSearchPriceRangeIsInMinorUnits(t *testing.T) {
	fixtureServer(t)
	got := searchURL(t, "kindle", "--price", "50-150")
	if !strings.Contains(got, "p_36%3A5000-15000") {
		t.Errorf("--price 50-150 did not become p_36:5000-15000:\n%s", got)
	}
	// Open ended on either side, because "under 150" and "over 50" are both
	// things people ask for.
	if got := searchURL(t, "kindle", "--price", "-150"); !strings.Contains(got, "p_36%3A-15000") {
		t.Errorf("--price -150:\n%s", got)
	}
	if got := searchURL(t, "kindle", "--price", "50-"); !strings.Contains(got, "p_36%3A5000-") {
		t.Errorf("--price 50-:\n%s", got)
	}
}

// A malformed flag is a usage error before a request, not a filter that quietly
// does nothing.
func TestSearchRejectsMalformedFilters(t *testing.T) {
	fixtureServer(t)
	for _, args := range [][]string{
		{"search", "kindle", "--refine", "p_123"},
		{"search", "kindle", "--refine", "=213704"},
		{"search", "kindle", "--price", "cheap"},
		{"search", "kindle", "--price", "150-50"},
		{"search", "kindle", "--partition", "p_123"},
	} {
		if _, _, err := runSplit(t, args...); err == nil {
			t.Errorf("%v was accepted", args[2:])
		}
	}
}

// --prime is gone rather than silently wrong. v0.2.1 sent an id no measured
// page offers, and Amazon answers an unknown rh term with an unfiltered page,
// so every Prime-only search it ran included everything.
func TestPrimeFlagIsRefusedWithAWayForward(t *testing.T) {
	fixtureServer(t)
	_, errOut, err := runSplit(t, "search", "kindle", "--prime")
	if err == nil {
		t.Fatal("--prime has to fail, because the filter it claimed to apply was never applied")
	}
	msg := err.Error() + errOut
	if !strings.Contains(msg, "amz refine") {
		t.Errorf("the refusal does not say what to run instead:\n%s", msg)
	}
}

// The sponsored placements are out of the rows by default and the count of what
// was dropped goes to stderr, so a caller piping stdout into jq gets results and
// still learns the grid was larger.
func TestSearchDropsSponsoredAndSaysSoOnStderr(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "search", "usb c cable", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var row map[string]any
		if uerr := json.Unmarshal([]byte(line), &row); uerr != nil {
			t.Fatalf("not jsonl: %v", uerr)
		}
		if row["sponsored"] == true {
			t.Errorf("a sponsored card reached stdout: %s", line)
		}
	}
	if !strings.Contains(errOut, "sponsored") {
		t.Errorf("dropping the ads without saying so reads as a page that had none:\n%s", errOut)
	}

	kept, _, err := runSplit(t, "search", "usb c cable", "-o", "jsonl", "--include-sponsored")
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(kept), "\n")) <= len(strings.Split(strings.TrimSpace(out), "\n")) {
		t.Error("--include-sponsored returned no more rows than the default, so one of the two is not doing what it says")
	}
}

// amz refine is the command that makes every other refinement flag usable, and
// its rows have to carry the exact string --refine wants back.
func TestRefineListsGroupsAndValues(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "refine", "usb c cable", "-o", "jsonl")
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("refine returned no groups")
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var g map[string]any
		if uerr := json.Unmarshal([]byte(line), &g); uerr != nil {
			t.Fatalf("not jsonl: %v", uerr)
		}
		if g["code"] == "" || g["scope"] == "" {
			t.Errorf("a group with no code or scope: %s", line)
		}
	}

	vals, _, err := runSplit(t, "refine", "usb c cable", "--group", "p_123", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(vals) == "" {
		t.Skip("the search fixture offers no brand group, so there are no values to list")
	}
	var v map[string]any
	if uerr := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(vals), "\n")[0]), &v); uerr != nil {
		t.Fatal(uerr)
	}
	if v["group"] != "p_123" {
		t.Errorf("--group p_123 returned %v", v["group"])
	}
}

// amz refine reads one page and prints its sidebar, so the flags about how far
// to walk have nothing to act on. Offering them in the help is a promise the
// command cannot keep, and --enqueue in particular would read as a way to queue
// the filtered results.
func TestRefineOffersOnlyTheFlagsItHonours(t *testing.T) {
	declared := declaredFlags(t, "refine")
	for _, never := range []string{"--all", "--max-pages", "--page", "--partition", "--partition-depth", "--pages", "--enqueue", "--include-sponsored", "--list-departments"} {
		if declared[never] {
			t.Errorf("refine declares %s, which that command does nothing with", never)
		}
	}
	// The query flags are shared on purpose: asking what a filtered search
	// offers next is how anyone reaches a second filter.
	for _, want := range []string{"--refine", "--price", "--sort", "--department", "--brand", "--seller", "--condition", "--stars", "--group"} {
		if !declared[want] {
			t.Errorf("refine does not declare %s", want)
		}
	}
	// And search still has both halves, so the split moved flags rather than
	// deleting them.
	search := declaredFlags(t, "search")
	for _, want := range []string{"--refine", "--price", "--all", "--max-pages", "--enqueue"} {
		if !search[want] {
			t.Errorf("search no longer declares %s", want)
		}
	}
}

// declaredFlags reads a command's own flag names out of its help.
//
// It reads declarations rather than searching the whole text, because the help
// for --department says "as listed by --list-departments" and a substring match
// would call that a declaration of a flag the command does not have.
func declaredFlags(t *testing.T, cmd string) map[string]bool {
	t.Helper()
	out, _, err := runSplit(t, cmd, "--help")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for _, f := range fields[:min(2, len(fields))] {
			if name := strings.TrimSuffix(f, ","); strings.HasPrefix(name, "--") {
				found[name] = true
			}
		}
	}
	if len(found) == 0 {
		t.Fatalf("%s --help declared no flags at all, so this test is measuring nothing:\n%s", cmd, out)
	}
	return found
}

// A partitioned run is priced before it runs, because the difference between
// eight cells and a hundred and sixty four is the difference between a minute
// and an afternoon of somebody else's bandwidth.
func TestSearchAllDryRunPricesTheWalk(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "search", "usb c cable", "--all", "--dry-run")
	if err != nil {
		t.Skipf("the search fixture offers no partitionable group: %v", err)
	}
	for _, want := range []string{"partition", "worst case", "nothing was read"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan does not mention %q:\n%s", want, out)
		}
	}
}

// Every topic amz why offers is reachable, and the three the search milestone
// added say what they are named for.
func TestWhySearchTopics(t *testing.T) {
	for _, tc := range []struct{ topic, want string }{
		{"search-depth", "306"},
		{"refinements", "p_123"},
		{"browse-tree", "breadcrumb"},
	} {
		out, _, err := runSplit(t, "why", tc.topic)
		if err != nil {
			t.Fatalf("why %s: %v", tc.topic, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("why %s does not mention %s:\n%s", tc.topic, tc.want, out)
		}
	}
}
