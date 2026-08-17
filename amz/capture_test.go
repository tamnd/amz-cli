package amz

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The golden captures and the ledger that watches them.
//
// A parser test asserts that a field this package knows about still reads. It
// cannot assert anything about a field the page has and the package does not,
// and it cannot tell a parser that broke from a page that changed. The ledger
// is the second signal: every capture records what it yielded on the day it was
// taken, and any movement in those numbers is a fact about amazon.com that
// somebody has to look at and either accept or fix.
//
// The numbers are deliberately coarse. Fields set, fields missed, regions
// nothing read, records found. A ledger of exact values would fail on a price
// change and teach everyone to run -update without reading the diff, which is
// how a drift detector becomes a rubber stamp.

var updateLedger = flag.Bool("update", false, "rewrite capture.txt from the captures on disk")

const capturesDir = "testdata/captures"

// capture is the sidecar written beside every capture. It records the fetch, so
// a reader can tell what a page was without unzipping half a megabyte of HTML,
// and so a capture that is quietly edited stops matching its own hash.
type capture struct {
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	Marketplace string            `json:"marketplace"`
	Fetched     string            `json:"fetched"`
	UserAgent   string            `json:"user_agent"`
	Bytes       int               `json:"bytes"`
	SHA256      string            `json:"sha256"`
	Scrubbed    int               `json:"scrubbed_session_ids,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Why         string            `json:"why"`
}

// ledgerRow is one line of capture.txt.
type ledgerRow struct {
	Name   string
	Op     string
	Family string
	Bytes  int
	Set    int
	Missed int
	Unread int
	Recs   int
	// Stop is set instead of the counts when the right outcome for a capture is
	// a refusal. soft_404 is a page Amazon serves with a 200 and no product on
	// it, and "this parser returns not_found" is exactly what it is here to
	// assert.
	Stop string
}

func TestCaptureLedger(t *testing.T) {
	c := NewClient(DefaultConfig())

	files, err := filepath.Glob(filepath.Join(capturesDir, "*.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no captures in %s", capturesDir)
	}
	sort.Strings(files)

	want, err := readLedger(filepath.Join(capturesDir, "capture.txt"))
	if err != nil && !*updateLedger {
		t.Fatal(err)
	}

	var got []ledgerRow
	seen := map[string]bool{}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".html.gz")
		seen[name] = true

		meta, err := readSidecar(filepath.Join(capturesDir, name+".json"))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		body, err := readCapture(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		// The hash is what makes a capture golden. Without it a capture is a
		// file anybody can edit into agreement with a parser, and a ledger over
		// editable inputs measures nothing.
		if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != meta.SHA256 {
			t.Errorf("%s: body does not match the sha256 in its sidecar, so the capture or the sidecar was edited", name)
			continue
		}
		if len(body) != meta.Bytes {
			t.Errorf("%s: body is %d bytes, sidecar says %d", name, len(body), meta.Bytes)
		}
		if meta.URL == "" || meta.Fetched == "" || meta.Why == "" {
			t.Errorf("%s: sidecar needs a url, a date and a reason the capture exists", name)
		}

		row := ledgerRow{Name: name, Bytes: len(body)}
		x, err := c.ExtractBody(meta.URL, body)
		row.Op, row.Family = x.Op, string(x.Family)
		switch {
		case errors.Is(err, ErrNotFound):
			row.Stop = "not_found"
		case err != nil:
			t.Errorf("%s: %v", name, err)
			continue
		default:
			row.Set, row.Missed, row.Unread, row.Recs = x.Set, x.Missed, x.Unread, x.Records
		}
		got = append(got, row)
	}

	// A capture with no sidecar is caught above. A sidecar with no capture is
	// caught here, because a sidecar left behind after a capture was deleted
	// describes a page that is no longer being watched.
	metas, err := filepath.Glob(filepath.Join(capturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range metas {
		if name := strings.TrimSuffix(filepath.Base(path), ".json"); !seen[name] {
			t.Errorf("%s.json has no capture beside it", name)
		}
	}

	if *updateLedger {
		if err := writeLedger(filepath.Join(capturesDir, "capture.txt"), got); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote capture.txt with %d captures", len(got))
		return
	}

	for _, g := range got {
		w, ok := want[g.Name]
		if !ok {
			t.Errorf("%s is not in capture.txt: add it with -update and say in the commit what it is for", g.Name)
			continue
		}
		delete(want, g.Name)
		if g != w {
			t.Errorf("%s drifted:\n  ledger %+v\n  now    %+v\n"+
				"A capture cannot change, so this is the parser or the ledger. Decide which, then run:\n"+
				"  go test ./amz/ -run TestCaptureLedger -update", g.Name, w, g)
		}
	}
	for name := range want {
		t.Errorf("capture.txt has a line for %s and there is no such capture", name)
	}
}

// TestEveryFamilyHasACapture is the coverage floor.
//
// A family with no capture is a family whose drift nobody would notice, and the
// families that go unwatched are the ones nobody is looking at, which is exactly
// where a page changes quietly.
func TestEveryFamilyHasACapture(t *testing.T) {
	rows, err := readLedger(filepath.Join(capturesDir, "capture.txt"))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]int{}
	for _, r := range rows {
		have[r.Family]++
	}
	for _, fam := range Families() {
		if have[string(fam)] == 0 {
			t.Errorf("family %s has no golden capture", fam)
		}
	}
}

func readCapture(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	// Bounded, because a gzip bomb in testdata would otherwise be a hang rather
	// than a failure, and 32 MB is ten times the largest page measured.
	return io.ReadAll(io.LimitReader(zr, 32<<20))
}

func readSidecar(path string) (capture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return capture{}, err
	}
	var m capture
	err = json.Unmarshal(b, &m)
	return m, err
}

// ledgerHeader is written above the table by -update. It is here rather than in
// a README because the file it explains is the file people will open.
const ledgerHeader = `# The golden capture ledger.
#
# One line per capture in this directory, recording what the parser made of it on
# the day it was taken: fields set, fields the registry declared and did not
# fill, regions Amazon named that no field reads, and repeated records found.
#
# These numbers move when amazon.com moves. That is the point. A drop in "set" or
# a rise in "unread" is a page that changed under a parser that did not, and it
# is meant to be read and understood rather than regenerated. When the change is
# real and accepted, rewrite this file with:
#
#     go test ./amz/ -run TestCaptureLedger -update
#
# Every capture was taken on 2026-08-17 with no cookie jar and the User-Agent amz
# sends, so none of them carries an account, a cart or a personalized layout. The
# per request identifiers Amazon prints into its own ref tags were replaced with
# zeros before the files were stored, and the sidecars keep the response headers
# minus anything that names one request.
#
# The captcha and interstitial bodies are not here. They are 3,781 and 2,264
# bytes, they are the only thing on the page, and they live as constants in
# classify_test.go where the classifier tests read them.
#
`

func writeLedger(path string, rows []ledgerRow) error {
	var b strings.Builder
	b.WriteString(ledgerHeader)
	fmt.Fprintf(&b, "%-22s %-14s %-8s %9s %5s %6s %6s %7s  %s\n",
		"name", "op", "family", "bytes", "set", "missed", "unread", "records", "stop")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-22s %-14s %-8s %9d %5d %6d %6d %7d  %s\n",
			r.Name, r.Op, r.Family, r.Bytes, r.Set, r.Missed, r.Unread, r.Recs, r.Stop)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// readLedger reads capture.txt through the same parser the binary ships with,
// so the test cannot pass against a format the command cannot read.
func readLedger(path string) (map[string]ledgerRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := parseLedger(string(b))
	if err != nil {
		return nil, err
	}
	out := make(map[string]ledgerRow, len(entries))
	for _, e := range entries {
		out[e.Name] = ledgerRow{
			Name: e.Name, Op: e.Op, Family: string(e.Family), Bytes: e.Bytes,
			Set: e.Set, Missed: e.Missed, Unread: e.Unread, Recs: e.Records, Stop: e.Stop,
		}
	}
	return out, nil
}
