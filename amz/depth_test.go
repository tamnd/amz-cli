package amz

import (
	"context"
	"strings"
	"testing"
)

func TestParseDepth(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Depth
		err  bool
	}{
		{"", DepthMeta, false},
		{"quick", DepthQuick, false},
		{"META", DepthMeta, false},
		{" full ", DepthFull, false},
		{"deep", DepthDeep, false},
		{"everything", "", true},
	} {
		got, err := ParseDepth(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("ParseDepth(%q) accepted an unknown level", tc.in)
				continue
			}
			// The error has to name the alternatives, because a caller who typed
			// the wrong word needs the right one and not a scolding.
			for _, want := range []string{"quick", "meta", "full", "deep"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ParseDepth(%q) error does not name %s: %v", tc.in, want, err)
				}
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDepth(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseDepth(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The empty string is meta and not an error, because a caller that never set the
// flag has not made a mistake.
func TestDepthCosts(t *testing.T) {
	for _, d := range []Depth{DepthQuick, DepthMeta, DepthFull} {
		if got := d.Cost(88); got != 1 {
			t.Errorf("%s costs %d requests for 88 siblings, want 1", d, got)
		}
	}
	if got := DepthDeep.Cost(0); got != 2 {
		t.Errorf("deep on a product with no siblings is the page and the seller, got %d", got)
	}
	if got := DepthDeep.Cost(88); got != 90 {
		t.Errorf("deep on the apparel capture is 90 requests, got %d", got)
	}
	if DepthDeep.Cost(88) <= DeepConfirmAt {
		t.Error("the apparel capture is the case the confirmation exists for")
	}
	if DepthDeep.Cost(4) > DeepConfirmAt {
		t.Error("a product with four colours must not need a confirmation")
	}
}

func TestDepthReadsTheRightSurface(t *testing.T) {
	if DepthQuick.Surface() != "s2" {
		t.Error("quick reads the mobile page")
	}
	for _, d := range []Depth{DepthMeta, DepthFull, DepthDeep} {
		if d.Surface() != "s1" {
			t.Errorf("%s reads the desktop detail page", d)
		}
	}
	if DepthMeta.Rails() || DepthQuick.Rails() {
		t.Error("the rails are a full read")
	}
	if !DepthFull.Rails() || !DepthDeep.Rails() {
		t.Error("full and deep keep the rails")
	}
	if DepthFull.Siblings() {
		t.Error("full spends no extra request, which is the whole reason it is free")
	}
	if !DepthDeep.Siblings() {
		t.Error("deep is the level that fetches siblings")
	}
}

// A quick read asks for the mobile URL. This is the one thing about quick that a
// caller cannot verify from the record alone, so the envelope has to say it.
func TestFetchQuickReadsTheLightPage(t *testing.T) {
	c, done := fixtureServer(t)
	defer done()

	p, err := c.FetchProductDepth(context.Background(), "B075F5X8BR", DepthQuick)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.URL, "/gp/aw/d/") {
		t.Errorf("quick must read the mobile page, got %s", p.URL)
	}
	if p.Envelope.Depth != "quick" {
		t.Errorf("the record has to say which depth produced it, got %q", p.Envelope.Depth)
	}
	if len(p.Envelope.Sources) != 1 {
		t.Fatalf("quick is one request, got %d sources", len(p.Envelope.Sources))
	}
	if got := p.Envelope.Sources[0].Surface; got != "s2" {
		t.Errorf("the source names the surface it read, got %q", got)
	}
	if !containsString(p.Envelope.Surfaces, "s2") {
		t.Errorf("surfaces = %v, want s2", p.Envelope.Surfaces)
	}
	if p.Envelope.Sources[0].Bytes == 0 {
		t.Error("a source that records no size cannot be used to explain a crawl's bandwidth")
	}
}

// meta and full are the same request and differ only in what is kept, and the
// record says which of the two it is rather than leaving a reader to infer it
// from an empty list.
func TestFetchMetaDropsTheRailsAndSaysSo(t *testing.T) {
	c, done := fixtureServer(t)
	defer done()

	full, err := c.FetchProductDepth(context.Background(), "B075F5X8BR", DepthFull)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Rails) == 0 {
		t.Skip("the product fixture carries no recommendation strip, so there is nothing to drop")
	}

	meta, err := c.FetchProductDepth(context.Background(), "B075F5X8BR", DepthMeta)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Rails) != 0 {
		t.Errorf("meta keeps no rails, got %d", len(meta.Rails))
	}
	var said bool
	for _, m := range meta.Envelope.Missed {
		if m.Field == "rails" {
			said = true
			if m.Total != int64(len(full.Rails)) {
				t.Errorf("the entry counts the strips that were dropped, got %d want %d", m.Total, len(full.Rails))
			}
			if m.Fix == "" {
				t.Error("a field dropped by a flag has to name the flag that keeps it")
			}
		}
	}
	if !said {
		t.Error("dropping the rails silently is how a consumer concludes the product has none")
	}
	if meta.Envelope.Depth != "meta" || full.Envelope.Depth != "full" {
		t.Errorf("depth = %q and %q", meta.Envelope.Depth, full.Envelope.Depth)
	}
}

func TestFetchDepthRejectsAnUnknownLevel(t *testing.T) {
	c, done := fixtureServer(t)
	defer done()

	if _, err := c.FetchProductDepth(context.Background(), "B075F5X8BR", Depth("everything")); err == nil {
		t.Fatal("an unknown depth is a usage error and not a silent default")
	}
}
