package amz

import (
	"path/filepath"
	"strings"
	"testing"
)

// The four states of a missing field, one test per state, each against the
// capture that exercises it.
//
// This is the distinction the envelope exists to make, and it is worth this much
// test surface because it is the one thing a consumer cannot check for
// themselves. A field is either in the record or it is not, and when it is not
// the record has to say whether the tool looked. The rule the tests below hold
// the code to is one sentence: if a field is absent and no missed entry names
// it, the tool looked and there was nothing there.

// missFor returns the missed entries naming a field.
func missFor(env Envelope, field string) []Miss {
	var out []Miss
	for _, m := range env.Missed {
		if m.Field == field {
			out = append(out, m)
		}
	}
	return out
}

// parseCapture reads one golden capture through the product parser.
func parseCapture(t *testing.T, name string) Product {
	t.Helper()
	body, err := readCapture(filepath.Join(capturesDir, name+".html.gz"))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	p, err := NewClient(Config{}).parseProduct("", "https://www.amazon.com/dp/TEST000000", body)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return p
}

// State one, present. The field is in the record and nothing is said about it,
// because there is nothing to say.
func TestMissingStatePresent(t *testing.T) {
	p := parseCapture(t, "product_simple")
	if p.Offer == nil || p.Offer.Price == nil {
		t.Fatal("product_simple has a price in corePrice, so the record must carry one")
	}
	if got := missFor(p.Envelope, "price"); len(got) != 0 {
		t.Errorf("price is set and also in missed: %+v", got)
	}
	if p.Envelope.Via["price"] == "" {
		t.Error("a field that is set must say which region set it")
	}
}

// State two, absent and checked. The region the price lives in is on the page
// and empty, because Amazon would not quote a price for the delivery location
// the capture was taken from. The record has no price and says nothing further,
// which is the whole statement: we looked, there was nothing there.
func TestMissingStateAbsentChecked(t *testing.T) {
	p := parseCapture(t, "product_apparel")
	if p.Offer != nil && p.Offer.Price != nil {
		t.Fatalf("product_apparel quotes no price, got %v", p.Offer.Price)
	}
	if got := missFor(p.Envelope, "price"); len(got) != 0 {
		t.Errorf("the price region is present and empty, so nothing is missing:\n  %+v", got)
	}
	// The claim above is only true if the region really is on the page. A test
	// that asserted the absence of a missed entry without checking this would
	// pass just as happily against a parser that forgot to look.
	body, err := readCapture(filepath.Join(capturesDir, "product_apparel.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseDoc(FamilyProduct, body)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Region("corePrice_desktop").Exists() {
		t.Error("corePrice_desktop is not on product_apparel, so this capture no longer tests this state")
	}
}

// State three, absent and not read. The review corpus is on a surface this fetch
// did not touch and could not have touched, and the entry names both spellings
// of it so a reader can check rather than take the tool's word for it.
func TestMissingStateAbsentNotRead(t *testing.T) {
	p := parseCapture(t, "product_simple")
	if p.Reviews != nil {
		t.Fatal("the detail page carries no review corpus, so the connection must be nil")
	}
	got := missFor(p.Envelope, "reviews")
	if len(got) != 1 {
		t.Fatalf("want exactly one missed entry for reviews, got %d", len(got))
	}
	m := got[0]
	if !strings.Contains(m.Why, "sign-in") {
		t.Errorf("the entry must say what stopped the read, got %q", m.Why)
	}
	if len(m.Surfaces) == 0 {
		t.Error("an entry that blames a surface has to name it")
	}
	for _, want := range []string{"/product-reviews/", "/portal/customer-reviews/"} {
		if !containsString(m.Surfaces, want) {
			t.Errorf("missing surface %s in %v", want, m.Surfaces)
		}
	}
}

// State four, absent and drifted. Every region the model declares for this field
// is off the page, which is a statement about the model rather than about the
// product, and the entry names the regions so the fix is a one line change.
func TestMissingStateAbsentDrifted(t *testing.T) {
	p := parseCapture(t, "product_book")
	if len(p.Bullets) != 0 {
		t.Fatal("the book layout carries no feature bullets, so this capture no longer tests this state")
	}
	got := missFor(p.Envelope, "bullet_points")
	if len(got) != 1 {
		t.Fatalf("want exactly one missed entry for bullet_points, got %d", len(got))
	}
	if !strings.Contains(got[0].Why, "not present on this page") {
		t.Errorf("a drifted region must say so, got %q", got[0].Why)
	}
	for _, region := range []string{"featurebullets", "feature-bullets"} {
		if !strings.Contains(got[0].Why, region) {
			t.Errorf("the entry must name region %s, got %q", region, got[0].Why)
		}
	}
}

// A partial is not one of the four states and is the one most often mistaken for
// the first. The field is set, the value is short of the total the page itself
// printed, and both numbers are in the entry so nobody publishes the shipped
// count as a total.
//
// This is a unit test rather than a capture test because none of the six product
// captures is partial: all four that carry a twister ship every sibling they
// claim, ten of ten, eighty eight of eighty eight, five of five and one of one,
// measured on 2026-08-17. The state is real, the code path is live, and the day
// a capture does come back short this test is what says the entry is right.
func TestMissingPartialKeepsBothNumbers(t *testing.T) {
	doc, err := ParseDoc(FamilyProduct, []byte(`<html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	e := NewExtractor(doc)
	e.set("variant_asins", 12, LevelPayload, PayloadTwister)
	e.missPartial("variant_asins", 12, 88,
		"the page ships only the variations near the current selection",
		"amz product on each sibling asin")

	got := missFor(e.Envelope(), "variant_asins")
	if len(got) != 1 {
		t.Fatalf("a partial survives even though the field is set, got %d entries", len(got))
	}
	m := got[0]
	if m.Have != 12 || m.Total != 88 {
		t.Errorf("a partial carries what was read and what the page claims, got have=%d total=%d", m.Have, m.Total)
	}
	if m.Fix == "" {
		t.Error("a partial that can be closed has to say how")
	}
}

// The twister says complete only when it shipped everything it claims, and the
// captures are the evidence for both halves of that.
func TestVariationCompleteMatchesTheCounts(t *testing.T) {
	for _, name := range []string{"product_simple", "product_apparel", "product_out_of_stock", "product_no_variants"} {
		p := parseCapture(t, name)
		if p.Variation == nil {
			t.Errorf("%s carries a twister and must produce a variation", name)
			continue
		}
		if p.Variation.TotalCount == nil {
			t.Errorf("%s: the twister states num_total_variations, so the record must carry it", name)
			continue
		}
		want := len(p.Variation.Siblings) >= *p.Variation.TotalCount
		if p.Variation.Complete != want {
			t.Errorf("%s: complete=%v with %d siblings of %d",
				name, p.Variation.Complete, len(p.Variation.Siblings), *p.Variation.TotalCount)
		}
		if !p.Variation.Complete {
			if got := missFor(p.Envelope, "variant_asins"); len(got) == 0 {
				t.Errorf("%s: an incomplete variation must say so in missed", name)
			}
		}
	}
}

// The consumer facing rule, held against every product capture at once: a field
// that is set is never also in missed, unless the entry is a partial.
func TestASetFieldIsNeverAlsoMissed(t *testing.T) {
	for _, name := range []string{"product_simple", "product_apparel", "product_book", "product_coupon", "product_no_variants", "product_out_of_stock"} {
		p := parseCapture(t, name)
		for _, m := range p.Envelope.Missed {
			if m.Have > 0 || m.Total > 0 {
				continue
			}
			if via, ok := p.Envelope.Via[m.Field]; ok {
				t.Errorf("%s: %s was read from %s and is still listed as missed: %s", name, m.Field, via, m.Why)
			}
		}
	}
}
