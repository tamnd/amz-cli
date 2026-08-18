package amz

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three brands the fixture sidebar offers. The middle one is the one Amazon
// takes and does not honour, which is the case this file exists for.
const (
	brandKept    = "111111"
	brandDropped = "222222"
	brandKept2   = "333333"
)

// partitionServer is a search that offers one refinement group and lies about
// one of its values.
//
// Amazon does not reject an rh term it will not apply. It drops the term, serves
// the unfiltered grid with a 200 and a full sidebar, and the only tell is that
// the sidebar comes back with nothing marked as removed. That is what this
// server does for brandDropped, and it is not a hypothetical: measured on
// 2026-08-18, "usb-c hub" partitioned into 50 cells and amazon ignored
// p_n_cpf_labels on one of the sub-splits.
func partitionServer(t *testing.T) (*Client, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nAllow: /\n")
			return
		}
		hits++
		rh := r.URL.Query().Get("rh")
		applied := ""
		switch {
		case strings.Contains(rh, brandKept):
			applied = brandKept
		case strings.Contains(rh, brandKept2):
			applied = brandKept2
		}
		// One page per cell, and a different ASIN block per applied value so the
		// union has something to be a union of.
		start := 1
		if applied == brandKept2 {
			start = 101
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, refinedSearchHTML(start, applied))
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig()
	cfg.CacheDir = t.TempDir()
	c := NewClient(cfg)
	c.SetBaseURL(srv.URL)
	// The pace floor is a promise to amazon.com, not to a httptest server.
	c.delay = 0
	return c, &hits
}

// refinedSearchHTML is one result page with a brand sidebar on it. applied names
// the value the page reports as switched on, and "" means the page came back
// unfiltered.
func refinedSearchHTML(start int, applied string) string {
	var b strings.Builder
	b.WriteString(`<html><body><span data-component-type="s-result-info-bar">`)
	fmt.Fprintf(&b, `<div><h2><span>1-3 of over 3 results</span></h2></div></span>`)
	b.WriteString(`<div data-component-type="s-search-results"><div class="s-main-slot s-result-list">`)
	for i := start; i < start+3; i++ {
		fmt.Fprintf(&b, `<div data-component-type="s-search-result" data-asin="B%09d" data-index="%d">`, i, i)
		fmt.Fprintf(&b, `<h2 aria-label="Item %d"><span>Item %d</span></h2>`, i, i)
		b.WriteString(`<a class="a-link-normal" href="/dp/B000000001"></a></div>`)
	}
	b.WriteString(`</div><div class="s-pagination-container"><span class="s-pagination-strip">`)
	b.WriteString(`<span class="s-pagination-item s-pagination-selected" aria-label="Page 1">1</span>`)
	b.WriteString(`<span class="s-pagination-item s-pagination-next s-pagination-disabled" aria-disabled="true">Next</span>`)
	b.WriteString(`</span></div></div>`)

	// The sidebar, written the way Amazon writes it: the group code on the list
	// id, the code and the value id on each item, and Apply or Remove in the
	// link's own aria-label.
	b.WriteString(`<div data-component-type="s-filters-panel-view">`)
	b.WriteString(`<span id="p_123-title">Brands</span><ul id="filter-p_123">`)
	for _, v := range []struct{ id, name string }{
		{brandKept, "Anker"}, {brandDropped, "Belkin"}, {brandKept2, "Ugreen"},
	} {
		verb := "Apply"
		if v.id == applied {
			verb = "Remove"
		}
		fmt.Fprintf(&b, `<li id="p_123/%s"><a aria-label="%s %s filter to narrow results" href="/s?k=hub">%s</a></li>`,
			v.id, verb, v.name, v.name)
	}
	b.WriteString(`</ul></div></body></html>`)
	return b.String()
}

// One cell Amazon refuses to filter by must not throw away the other cells.
//
// This is the whole point of the file. Before the fix, SearchWalk returned
// ErrRefinementIgnored, partition returned it, SearchAll returned before it had
// emitted anything, and a run that had read 263 cards printed none of them and
// reported "0 unique results" beside a duplicate count that could only have come
// from the cards it was in the middle of discarding.
func TestAnIgnoredCellDoesNotSinkTheUnion(t *testing.T) {
	c, _ := partitionServer(t)

	var got []string
	sum, err := c.SearchAll(context.Background(), "hub", SearchQuery{}, PartitionOptions{}, func(card Card) error {
		got = append(got, card.ASIN)
		return nil
	})
	if err != nil {
		t.Fatalf("one ignored cell out of three ended the run: %v", err)
	}
	if len(got) != 6 {
		t.Errorf("emitted %d results, want the 3 from each of the two cells amazon honoured", len(got))
	}
	if sum.Unique != len(got) {
		t.Errorf("summary says %d unique and %d were emitted, and a summary that does not match the rows is worse than no summary",
			sum.Unique, len(got))
	}
	if len(sum.Cells) != 3 {
		t.Errorf("ran %d cells, want one per sidebar value including the one that read nothing", len(sum.Cells))
	}
}

// The cell that read nothing has to be named, because it is a hole in the union.
func TestAnIgnoredCellIsNamedInTheSummary(t *testing.T) {
	c, _ := partitionServer(t)

	sum, err := c.SearchAll(context.Background(), "hub", SearchQuery{}, PartitionOptions{}, func(Card) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := "p_123:" + brandDropped
	if len(sum.Ignored) != 1 || sum.Ignored[0] != want {
		t.Fatalf("ignored = %v, want exactly [%s]", sum.Ignored, want)
	}
	var ignored int
	for _, cell := range sum.Cells {
		if cell.Ignored {
			ignored++
			if cell.Results != 0 {
				t.Errorf("cell %s is marked ignored and claims %d results, and it yielded none",
					cell.Cell, cell.Results)
			}
		}
	}
	if ignored != 1 {
		t.Errorf("%d cells marked ignored, want 1", ignored)
	}
}

// A refinement somebody typed is still an error. The leniency is scoped to the
// cells --all generates for itself, and if it leaked into a plain search then
// `amz search --brand Belkin` would hand back the unfiltered catalog labelled as
// Belkin, which is the failure ErrRefinementIgnored was written to stop.
func TestATypedRefinementIsStillAnError(t *testing.T) {
	c, _ := partitionServer(t)

	q := SearchQuery{Refine: []Refinement{{Group: "p_123", Values: []string{brandDropped}}}}
	_, err := c.SearchWalk(context.Background(), "hub", q, func(SearchPage) error { return nil })
	if err == nil {
		t.Fatal("a refinement amazon dropped came back as a clean search")
	}
	if !strings.Contains(err.Error(), "did not apply the refinement") {
		t.Fatalf("error = %v, want the ignored-refinement error", err)
	}
}
