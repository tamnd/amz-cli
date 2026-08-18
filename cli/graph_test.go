package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/amz-cli/amz"
	"github.com/tamnd/amz-cli/pkg/graph"
)

// seedGraph writes edges straight into a store and returns the data dir.
//
// Straight into the store rather than through a crawl, because these tests are
// about the traversal and the export and not about the parser. A fixture crawl
// would make every assertion here depend on how many rail cards happen to be in
// a captured page.
func seedGraph(t *testing.T, edges []amz.Edge) string {
	t.Helper()
	dir := t.TempDir()
	s, err := amz.OpenStore(filepath.Join(dir, "amz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutEdges(context.Background(), edges); err != nil {
		t.Fatal(err)
	}
	return dir
}

var seeded = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func edge(src, predicate, dst string) amz.Edge {
	return amz.Edge{Src: src, Predicate: predicate, Dst: dst, Via: "s1", ObservedAt: seeded}
}

func prod(asin string) string { return "amz:us/product/" + asin }

// A small graph with one of each thing the command has to get right: an organic
// neighbour, a paid one, a second hop, and a seller.
func sampleEdges() []amz.Edge {
	paid := edge(prod("B0START001"), graph.RelatedTo, prod("B0SPONSOR1"))
	paid.Sponsored = true
	return []amz.Edge{
		edge(prod("B0START001"), graph.RelatedTo, prod("B0ORGANIC1")),
		paid,
		edge(prod("B0START001"), graph.SoldBy, "amz:seller/A1SELLER"),
		edge(prod("B0ORGANIC1"), graph.RelatedTo, prod("B0SECOND01")),
	}
}

func TestGraphWalksOneHopByDefault(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "graph", "B0START001", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B0ORGANIC1") {
		t.Fatalf("the first hop is missing:\n%s", out)
	}
	if strings.Contains(out, "B0SECOND01") {
		t.Fatalf("--depth defaults to 1 and the walk went two hops:\n%s", out)
	}
	if strings.Contains(out, "B0SPONSOR1") {
		t.Fatalf("a paid placement was followed by default:\n%s", out)
	}
}

func TestGraphDepthAndSponsored(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "graph", "B0START001", "--depth", "2", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B0SECOND01") {
		t.Fatalf("--depth 2 did not reach the second hop:\n%s", out)
	}

	out, err = run(t, "--data-dir", dir, "graph", "B0START001", "--include-sponsored", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B0SPONSOR1") {
		t.Fatalf("--include-sponsored did not put the ad back:\n%s", out)
	}
}

func TestGraphPredicateFilter(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "graph", "B0START001", "--predicate", "sold_by", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "amz:seller/A1SELLER") {
		t.Fatalf("the seller is missing:\n%s", out)
	}
	if strings.Contains(out, "B0ORGANIC1") {
		t.Fatalf("--predicate sold_by returned a related_to neighbour:\n%s", out)
	}
}

// The vocabulary is closed, so a predicate outside it is a usage error that
// names the flag that prints the sixteen rather than an empty result.
func TestGraphRejectsAPredicateOutsideTheVocabulary(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	_, _, err := runSplit(t, "--data-dir", dir, "graph", "B0START001", "--predicate", "recommends")
	if err == nil {
		t.Fatal("an invented predicate was accepted")
	}
	if code := codeOf(err); code != CodeUsage {
		t.Errorf("exit code %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(err.Error(), "--predicates") {
		t.Errorf("the error does not point at the vocabulary: %v", err)
	}
}

func TestGraphPredicatesPrintsTheVocabulary(t *testing.T) {
	out, err := run(t, "graph", "--predicates")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range graph.Predicates() {
		if !strings.Contains(out, p.Name) {
			t.Errorf("%s is not listed:\n%s", p.Name, out)
		}
	}
	if !strings.Contains(out, "inferred") {
		t.Errorf("the listing does not say which edge is inferred:\n%s", out)
	}
}

// Past this point the output is not something a person reads, so the command
// says how big the answer is and waits.
func TestGraphAsksAboveFiveHundredNodes(t *testing.T) {
	var edges []amz.Edge
	for i := range nodeWarnThreshold + 10 {
		edges = append(edges, edge(prod("B0START001"), graph.RelatedTo, prod("B0BULK"+pad(i))))
	}
	dir := seedGraph(t, edges)

	_, _, err := runSplit(t, "--data-dir", dir, "graph", "B0START001")
	if err == nil {
		t.Fatal("a traversal wider than the threshold ran without --yes")
	}
	if code := codeOf(err); code != CodeUsage {
		t.Errorf("exit code %d, want %d", code, CodeUsage)
	}
	for _, want := range []string{"--yes", "--depth"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}

	out, err := run(t, "--data-dir", dir, "--yes", "graph", "B0START001", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n < nodeWarnThreshold {
		t.Fatalf("--yes returned %d lines, want the whole walk", n)
	}
}

func pad(i int) string {
	s := strconv.Itoa(i)
	for len(s) < 5 {
		s = "0" + s
	}
	return s
}

// A node with no edges is exit 3 and a sentence naming the crawl that would fill
// it, rather than an empty result the caller has to interpret.
func TestGraphOnANodeNobodyCrawled(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	_, _, err := runSplit(t, "--data-dir", dir, "graph", "B0UNKNOWN1")
	if err == nil {
		t.Fatal("a node with no edges produced no error")
	}
	if code := codeOf(err); code != CodeNoData {
		t.Errorf("exit code %d, want %d", code, CodeNoData)
	}
	if !strings.Contains(err.Error(), "amz crawl") {
		t.Errorf("the error does not name the command that would fetch it: %v", err)
	}
}

// A full amz: URI beats --marketplace, because the same ASIN in two storefronts
// is two listings and picking one silently is how they get confused.
func TestGraphAcceptsAFullURI(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "--marketplace", "uk",
		"graph", "amz:us/product/B0START001", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B0ORGANIC1") {
		t.Fatalf("the URI did not override --marketplace:\n%s", out)
	}
}

func TestGraphEdgesFlagPrintsTheClaims(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "graph", "B0START001", "--edges", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var e amz.Edge
	if err := json.Unmarshal([]byte(firstLine(out)), &e); err != nil {
		t.Fatalf("the first line is not an edge: %v\n%s", err, out)
	}
	if e.Predicate == "" || e.ObservedAt.IsZero() {
		t.Fatalf("an edge went out without its predicate or its clock: %+v", e)
	}
	if e.Via == "" {
		t.Fatalf("an edge went out without the surface that asserted it: %+v", e)
	}
}

// The header is a line in the file and not a sidecar, because an export that
// gets piped, split and reassembled should carry its own provenance.
func TestExportJSONLLeadsWithAHeader(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "export", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var head map[string]any
	if err := json.Unmarshal([]byte(firstLine(out)), &head); err != nil {
		t.Fatalf("the first line is not JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"type", "tool", "version", "marketplace"} {
		if head[k] == nil || head[k] == "" {
			t.Errorf("the header has no %s: %v", k, head)
		}
	}
	if head["type"] != "header" {
		t.Errorf("the first line is a %v, want a header", head["type"])
	}
	if !strings.Contains(out, `"type":"edge"`) {
		t.Errorf("the export carries no edges:\n%s", out)
	}
}

func TestExportJSONLExcludesSponsoredByDefault(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "export", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "B0SPONSOR1") {
		t.Fatalf("a paid placement was exported by default:\n%s", out)
	}
	out, err = run(t, "--data-dir", dir, "export", "--include-sponsored", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B0SPONSOR1") {
		t.Fatalf("--include-sponsored did not put the ad back:\n%s", out)
	}
}

func TestExportTurtleAndNTriples(t *testing.T) {
	dir := seedGraph(t, sampleEdges())

	ttl, err := run(t, "--data-dir", dir, "export", "--format", "turtle")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ttl, "@prefix schema:") {
		t.Fatalf("turtle does not start with its prefix header:\n%s", ttl)
	}
	if !strings.Contains(ttl, "schema:isRelatedTo") {
		t.Fatalf("turtle carries no edges:\n%s", ttl)
	}

	nts, err := run(t, "--data-dir", dir, "export", "--format", "ntriples")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nts, "@prefix") {
		t.Fatalf("n-triples wrote a prefix header:\n%s", nts)
	}
	if !strings.Contains(nts, "<https://schema.org/isRelatedTo>") {
		t.Fatalf("n-triples did not expand the predicate:\n%s", nts)
	}
	for _, line := range strings.Split(strings.TrimSpace(nts), "\n") {
		if !strings.HasSuffix(line, " .") {
			t.Fatalf("an n-triples line is not terminated: %q", line)
		}
	}
}

// Two exports of the same store are byte identical, which is what makes a diff
// between two crawls readable.
func TestExportIsReproducible(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	a, err := run(t, "--data-dir", dir, "export", "--format", "turtle")
	if err != nil {
		t.Fatal(err)
	}
	b, err := run(t, "--data-dir", dir, "export", "--format", "turtle")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("two exports of one store differ:\n%s\n---\n%s", a, b)
	}
}

func TestExportRejectsAnUnknownFormat(t *testing.T) {
	dir := seedGraph(t, sampleEdges())
	_, _, err := runSplit(t, "--data-dir", dir, "export", "--format", "rdfxml")
	if err == nil {
		t.Fatal("an unknown format was accepted")
	}
	if code := codeOf(err); code != CodeUsage {
		t.Errorf("exit code %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(err.Error(), "turtle") {
		t.Errorf("the error does not list the formats it does take: %v", err)
	}
}

// An empty store exporting RDF is exit 3 and a sentence, because a zero byte
// turtle file looks like a successful export of nothing.
func TestExportOnAnEmptyStore(t *testing.T) {
	_, _, err := runSplit(t, "--data-dir", t.TempDir(), "export", "--format", "turtle")
	if err == nil {
		t.Fatal("an empty store exported successfully")
	}
	if code := codeOf(err); code != CodeNoData {
		t.Errorf("exit code %d, want %d", code, CodeNoData)
	}
}

// Neither command fetches. The fixture server would answer if anything asked it,
// so a traversal that reached for the network would find data the store does not
// have.
func TestGraphAndExportNeverFetch(t *testing.T) {
	fixtureServer(t)
	dir := seedGraph(t, sampleEdges())
	out, err := run(t, "--data-dir", dir, "graph", "B0START001", "--depth", "3", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "B084DWG2VQ") {
		t.Fatalf("the traversal fetched a page and walked into it:\n%s", out)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
