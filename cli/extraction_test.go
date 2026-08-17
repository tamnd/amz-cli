package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tamnd/amz-cli/amz"
)

func TestExtractionLadderCountsEveryFamily(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "extraction")
	if err != nil {
		t.Fatal(err)
	}
	for _, fam := range amz.Families() {
		if !strings.Contains(out, string(fam)) {
			t.Errorf("family %s missing from the ladder:\n%s", fam, out)
		}
	}
	// The bottom rung is the point of the report. A run that prints counts and
	// hides which fields are guesses is the report nobody needed.
	if !strings.Contains(out, "bare CSS selector") {
		t.Errorf("no rung 4 listing:\n%s", out)
	}
	if !strings.Contains(out, "2026-08-17") {
		t.Errorf("rung 4 fields printed without the date they were added:\n%s", out)
	}
}

func TestExtractionLadderRejectsAFamilyThatIsNotOne(t *testing.T) {
	fixtureServer(t)
	_, err := run(t, "extraction", "--family", "widgets")
	if err == nil {
		t.Fatal("an unknown family was accepted")
	}
	if got := codeFor(err); got != CodeUsage {
		t.Errorf("exit code = %d, want %d for a bad flag value", got, CodeUsage)
	}
}

func TestExtractionOnAPageReportsSetMissedAndUnread(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "extraction", "B084DWG2VQ", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var x amz.Extraction
	if err := json.Unmarshal([]byte(out), &x); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if x.Op != "product" || x.Family != amz.FamilyProduct {
		t.Errorf("op/family = %s/%s, want product/product", x.Op, x.Family)
	}
	if x.Set == 0 {
		t.Errorf("the fixture filled no fields, which means the report is not reading the page")
	}
	if x.Bytes == 0 {
		t.Errorf("bytes = 0")
	}
	// Fields() is what the text report walks, so a name in via and not in
	// Fields() is a field the human sees a count of and never a name for.
	if len(x.Fields()) != len(x.Envelope.Via) {
		t.Errorf("Fields() has %d names for %d recorded sources", len(x.Fields()), len(x.Envelope.Via))
	}
}

func TestExtractionUnreadListsTheWorklist(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "extraction", "B084DWG2VQ", "--unread")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "regions") {
		t.Errorf("--unread said nothing about regions:\n%s", out)
	}
}

func TestExtractionDryRunPrintsTheURLAndStops(t *testing.T) {
	srv := fixtureServer(t)
	out, err := run(t, "extraction", "B084DWG2VQ", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/dp/B084DWG2VQ"; strings.TrimSpace(out) != want {
		t.Errorf("out = %q, want %q", strings.TrimSpace(out), want)
	}
}

func TestVerifyReadsOnlyTheCacheByDefault(t *testing.T) {
	fixtureServer(t)
	// The cache is a fresh temp dir, so nothing in the ledger is on disk. The
	// right behaviour is to say so rather than to quietly fetch twenty one pages
	// from a site nobody asked this command to touch.
	out, err := run(t, "verify")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0 checked") {
		t.Errorf("something was checked out of an empty cache:\n%s", out)
	}
	if !strings.Contains(out, "--live") {
		t.Errorf("an empty run did not say how to get a real one:\n%s", out)
	}
}

func TestVerifyDryRunNamesEveryLedgerPage(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "verify", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := amz.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) != len(entries) {
		t.Fatalf("printed %d URLs for %d ledger entries", len(lines), len(entries))
	}
}

func TestVerifyStrictFailsWhenAPageYieldsLess(t *testing.T) {
	// Compare is the policy and this pins it, because the whole value of
	// --strict is that it fires on a regression and stays quiet for a marketing
	// widget. A test that only ran the command would pass either way.
	e := amz.LedgerEntry{Name: "x", Set: 20, Missed: 1, Unread: 100, Records: 30}

	same := amz.Compare(e, amz.Extraction{Set: 20, Missed: 1, Unread: 100, Records: 30})
	if len(same.Notes) != 0 || same.Worse {
		t.Errorf("an identical read reported drift: %v", same.Notes)
	}

	grew := amz.Compare(e, amz.Extraction{Set: 20, Missed: 1, Unread: 140, Records: 30})
	if grew.Worse {
		t.Errorf("Amazon adding 40 sections was called a regression")
	}
	if len(grew.Notes) == 0 {
		t.Errorf("Amazon adding 40 sections went unreported")
	}

	shrank := amz.Compare(e, amz.Extraction{Set: 14, Missed: 7, Unread: 100, Records: 30})
	if !shrank.Worse {
		t.Errorf("six fields stopped filling and nothing was called worse")
	}

	thin := amz.Compare(e, amz.Extraction{Set: 20, Missed: 1, Unread: 100, Records: 4})
	if !thin.Worse {
		t.Errorf("a 30 record page came back with 4 and nothing was called worse")
	}
}

func TestAgentMapSaysSoWhenThePageHasNone(t *testing.T) {
	fixtureServer(t)
	// The product fixture carries no AgentInterfaceMap, and the honest answer to
	// "print what the page says about itself" when the page says nothing is a
	// no-data exit rather than an empty object.
	_, err := run(t, "agent-map", "B084DWG2VQ")
	if err == nil {
		t.Fatal("a page with no agent map returned success")
	}
	if got := codeFor(err); got != CodeNoData {
		t.Errorf("exit code = %d, want %d", got, CodeNoData)
	}
}

func TestAgentMapDryRunDoesNotFetch(t *testing.T) {
	srv := fixtureServer(t)
	out, err := run(t, "agent-map", "B084DWG2VQ", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/dp/B084DWG2VQ"; strings.TrimSpace(out) != want {
		t.Errorf("out = %q, want %q", strings.TrimSpace(out), want)
	}
}
