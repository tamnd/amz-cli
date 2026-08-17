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

// State three, absent and not read. The field is nowhere in the record, the
// entry names the surface that holds it, and a reader can go and check rather
// than take the tool's word for it.
//
// The review corpus was this state's capture backed example until v0.3.0, when
// the parser started reading the medley on the detail page. It is a partial now,
// eight or thirteen of several thousand, and the test for that is below. What is
// left of state three is the same page with an empty medley, which happens on a
// product whose reviews are all ratings with no text, and none of the six
// captures is one. So this is a unit test, for the same reason the partial test
// is: the state is real and the path is live, and this is what says the entry is
// right on the day a capture finally exercises it.
func TestMissingStateAbsentNotRead(t *testing.T) {
	doc, err := ParseDoc(FamilyProduct, []byte(`<html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	e := NewExtractor(doc)
	e.missPartialSurface("reviews", 0, 4812,
		"amazon requires a sign-in for the review corpus",
		[]string{"/product-reviews/", "/portal/customer-reviews/"},
		"amz why reviews")

	got := missFor(e.Envelope(), "reviews")
	if len(got) != 1 {
		t.Fatalf("want exactly one missed entry for reviews, got %d", len(got))
	}
	m := got[0]
	if m.Have != 0 {
		t.Errorf("nothing was read, so have must be zero, got %d", m.Have)
	}
	if !strings.Contains(m.Why, "sign-in") {
		t.Errorf("the entry must say what stopped the read, got %q", m.Why)
	}
	for _, want := range []string{"/product-reviews/", "/portal/customer-reviews/"} {
		if !containsString(m.Surfaces, want) {
			t.Errorf("missing surface %s in %v", want, m.Surfaces)
		}
	}
}

// The reviews on a detail page are a partial, and every number in the entry has
// to be one the page itself printed.
//
// This is the assertion that keeps the record honest about a hard case. Thirteen
// reviews were read out of a total that is the ratings count, and ratings and
// reviews are not the same act, so the denominator is an upper bound. A reader
// who takes 13 of 6,793 at face value concludes 6,780 reviews are behind the
// wall, when most of that number is people who tapped a star and wrote nothing.
// The entry says which number it is, and this test says it must.
func TestReviewsAreAPartialThatNamesItsSurfaces(t *testing.T) {
	p := parseCapture(t, "product_simple")
	if p.Reviews == nil {
		t.Fatal("the medley carries reviews, so the connection must exist")
	}
	if p.Reviews.Complete {
		t.Fatal("a handful of reviews out of thousands is not a complete read")
	}
	if len(p.ReviewSample) == 0 || p.Reviews.Loaded != len(p.ReviewSample) {
		t.Fatalf("loaded=%d with %d reviews in the record", p.Reviews.Loaded, len(p.ReviewSample))
	}
	// Every review has to be worth having. A medley read with the corpus page
	// hooks alone yields the ids and the ratings and nothing else, which passes
	// a count check and fails a reader.
	for _, r := range p.ReviewSample {
		if r.ReviewID == "" || r.Title == "" || r.Text == "" || r.Rating == 0 {
			t.Errorf("thin review %+v", r)
		}
		if strings.HasPrefix(r.ReviewID, "customer_review-") {
			t.Errorf("the slot prefix is not part of the id: %q", r.ReviewID)
		}
	}
	got := missFor(p.Envelope, "reviews")
	if len(got) != 1 {
		t.Fatalf("want exactly one missed entry for reviews, got %d", len(got))
	}
	m := got[0]
	if m.Have != p.Reviews.Loaded || m.Total < int64(m.Have) {
		t.Errorf("a partial carries both numbers, got have=%d total=%d", m.Have, m.Total)
	}
	if !strings.Contains(m.Why, "ratings count") {
		t.Errorf("the entry has to say the total is the ratings count, got %q", m.Why)
	}
	for _, want := range []string{"/product-reviews/", "/portal/customer-reviews/"} {
		if !containsString(m.Surfaces, want) {
			t.Errorf("missing surface %s in %v", want, m.Surfaces)
		}
	}
}

// The medley holds two strips and both are read.
//
// B075F5X8BR carries thirteen review blocks and eight of them are ids under
// customer_review-. The other five are customer_review_foreign-, which is
// Amazon's name for reviews translated in from its other storefronts, and they
// are ordinary reviews with a country other than this one. Reading only the
// first strip would drop five real reviews and, worse, would make the record
// disagree with the histogram for a reason nothing in it explains.
func TestReviewsIncludeTheInternationalStrip(t *testing.T) {
	p := parseCapture(t, "product_simple")
	seen := map[string]bool{}
	countries := map[string]int{}
	for _, r := range p.ReviewSample {
		if seen[r.ReviewID] {
			t.Errorf("duplicate review id %s", r.ReviewID)
		}
		seen[r.ReviewID] = true
		if strings.Contains(r.ReviewID, "customer_review") {
			t.Errorf("the slot prefix is not part of the id: %q", r.ReviewID)
		}
		countries[r.Country]++
	}
	if len(p.ReviewSample) != 13 {
		t.Errorf("the capture carries thirteen review blocks, got %d", len(p.ReviewSample))
	}
	if countries["United States"] != 8 {
		t.Errorf("eight of the thirteen were written here, got %d", countries["United States"])
	}
	if len(countries) < 2 {
		t.Errorf("the other five came from other storefronts and must keep their country: %v", countries)
	}
}

// The all-offers ingress is one line of text with three numbers in it, and only
// the bracketed one is a count. Reading the price instead gives a product that
// reports nine other offers where the page says twenty two, which is wrong in a
// way that looks entirely plausible, so it is worth a test of its own.
func TestOtherOffersReadsTheBracketedCount(t *testing.T) {
	p := parseCapture(t, "product_book")
	if p.OtherOffers == nil {
		t.Fatal("product_book carries an all-offers ingress, measured 2026-08-17")
	}
	if p.OtherOffers.TotalCount == nil || *p.OtherOffers.TotalCount != 22 {
		t.Fatalf("the ingress reads New & Used (22) from $9.21, got %v", p.OtherOffers.TotalCount)
	}
	// The buy box is one of the twenty two and the caller has it, so the
	// connection says one loaded rather than none.
	if p.OtherOffers.Loaded != 1 {
		t.Errorf("the buy box counts as one offer read, got %d", p.OtherOffers.Loaded)
	}
	if got := missFor(p.Envelope, "other_offers"); len(got) != 1 {
		t.Fatalf("an incomplete offers connection must say so, got %d entries", len(got))
	}
	// A product only Amazon sells has no ingress and no other offers, which is a
	// complete answer. Recording a miss there would put an entry on the majority
	// of records to say nothing at all.
	q := parseCapture(t, "product_apparel")
	if q.OtherOffers != nil {
		t.Errorf("product_apparel has no ingress, so there is nothing to report: %+v", q.OtherOffers)
	}
	if got := missFor(q.Envelope, "other_offers"); len(got) != 0 {
		t.Errorf("no ingress means nothing was missed, got %+v", got)
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
