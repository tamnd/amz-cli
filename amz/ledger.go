package amz

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The ledger, compiled in.
//
// capture.txt records what every golden capture yielded on the day it was taken.
// That is a test fixture, and it is also the only statement this project has
// about what amazon.com looked like at a known moment, which makes it the thing
// `amz verify` compares a live page against. Shipping it inside the binary is
// what lets a user run the drift check themselves rather than read about it in a
// CI log they cannot reproduce.
//
// The captures themselves are not embedded. They are 4.4 MB and a user who wants
// to compare against a stored page has the repository. What ships is the two
// kilobytes of numbers and the sidecars naming the URLs they came from.

//go:embed testdata/captures/capture.txt testdata/captures/*.json
var ledgerFS embed.FS

// LedgerEntry is one golden capture: where it came from and what it yielded.
type LedgerEntry struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Op     string `json:"op"`
	Family Family `json:"family"`
	// Fetched is the date the capture was taken, which is the date every number
	// below is a fact about.
	Fetched string `json:"fetched"`
	Bytes   int    `json:"bytes"`
	Set     int    `json:"set"`
	Missed  int    `json:"missed"`
	Unread  int    `json:"unread"`
	Records int    `json:"records"`
	// Stop is non-empty when the right outcome for this page is a refusal
	// rather than a record, "not_found" for the body Amazon serves with a 200
	// and no product on it.
	Stop string `json:"stop,omitempty"`
	Why  string `json:"why,omitempty"`
}

var (
	ledgerOnce sync.Once
	ledger     []LedgerEntry
	ledgerErr  error
)

// Ledger returns the golden capture ledger, in capture name order.
func Ledger() ([]LedgerEntry, error) {
	ledgerOnce.Do(func() { ledger, ledgerErr = loadLedger() })
	return ledger, ledgerErr
}

// LedgerFor returns the ledger entry for a URL, if there is one.
func LedgerFor(rawURL string) (LedgerEntry, bool) {
	entries, err := Ledger()
	if err != nil {
		return LedgerEntry{}, false
	}
	for _, e := range entries {
		if e.URL == rawURL {
			return e, true
		}
	}
	return LedgerEntry{}, false
}

func loadLedger() ([]LedgerEntry, error) {
	b, err := ledgerFS.ReadFile("testdata/captures/capture.txt")
	if err != nil {
		return nil, err
	}
	rows, err := parseLedger(string(b))
	if err != nil {
		return nil, err
	}

	// The sidecars carry the URL and the reason each capture exists. A row with
	// no sidecar is kept rather than dropped: the numbers are still the numbers,
	// and a verify run that silently skipped a page would be the wrong kind of
	// quiet.
	sides, err := fs.Glob(ledgerFS, "testdata/captures/*.json")
	if err != nil {
		return nil, err
	}
	meta := make(map[string]LedgerEntry, len(sides))
	for _, p := range sides {
		raw, err := ledgerFS.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var m struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Fetched string `json:"fetched"`
			Why     string `json:"why"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", path.Base(p), err)
		}
		meta[m.Name] = LedgerEntry{URL: m.URL, Fetched: m.Fetched, Why: m.Why}
	}

	out := make([]LedgerEntry, 0, len(rows))
	for _, r := range rows {
		if m, ok := meta[r.Name]; ok {
			r.URL, r.Fetched, r.Why = m.URL, m.Fetched, m.Why
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseLedger reads capture.txt. The format is a fixed-width table with a
// comment header, chosen so the file reads as a document and diffs one line per
// capture when a page moves.
func parseLedger(src string) ([]LedgerEntry, error) {
	var out []LedgerEntry
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 || f[0] == "name" {
			continue
		}
		e := LedgerEntry{Name: f[0], Op: f[1], Family: Family(f[2])}
		for i, p := range []*int{&e.Bytes, &e.Set, &e.Missed, &e.Unread, &e.Records} {
			n, err := strconv.Atoi(f[3+i])
			if err != nil {
				return nil, fmt.Errorf("capture.txt: %s: %w", e.Name, err)
			}
			*p = n
		}
		if len(f) > 8 {
			e.Stop = f[8]
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Drift is one page compared against what it yielded when it was captured.
type Drift struct {
	Entry LedgerEntry `json:"entry"`
	Now   Extraction  `json:"now"`
	// Notes are the differences, in the words a reader needs. Empty means the
	// page reads the same as it did on the capture date.
	Notes []string `json:"notes,omitempty"`
	// Worse is true when the page yielded less than it used to. A page that
	// yields more is drift too, and it is the kind nobody needs to be woken up
	// for, so the two are reported separately.
	Worse bool `json:"worse"`
}

// Compare measures a fresh read against the ledger.
//
// What counts as worse is deliberately narrow: fewer fields set, or a field that
// used to fill and now misses. More unread regions is reported and is not worse,
// because Amazon adding a section is Amazon adding a section, and a tool that
// cried failure every time a marketing widget appeared would be ignored inside a
// month.
func Compare(e LedgerEntry, x Extraction) Drift {
	d := Drift{Entry: e, Now: x}
	switch {
	case x.Set < e.Set:
		d.Notes = append(d.Notes, fmt.Sprintf("%d fields set, was %d", x.Set, e.Set))
		d.Worse = true
	case x.Set > e.Set:
		d.Notes = append(d.Notes, fmt.Sprintf("%d fields set, was %d", x.Set, e.Set))
	}
	if x.Missed > e.Missed {
		d.Notes = append(d.Notes, fmt.Sprintf("%d fields missed, was %d", x.Missed, e.Missed))
		d.Worse = true
	}
	if x.Unread != e.Unread {
		d.Notes = append(d.Notes, fmt.Sprintf("%d unread regions, was %d", x.Unread, e.Unread))
	}
	if e.Records > 0 && x.Records < e.Records {
		d.Notes = append(d.Notes, fmt.Sprintf("%d records, was %d", x.Records, e.Records))
		d.Worse = true
	}
	return d
}
