package asin

import "testing"

// The check digit is the whole point of IsISBN10, so it is tested against real
// numbers and against numbers that differ from a real one by a single digit.
// Every B0 ASIN is a ten character alphanumeric string, so a shape test alone
// would call most of Amazon's catalogue an ISBN.
func TestIsISBN10(t *testing.T) {
	for _, s := range []string{
		"0439023483",    // The Hunger Games
		"0-439-02348-3", // the same, as a person writes it
		"043902348 3",
		"0306406152", // the Wikipedia example
		"080442957X", // the check digit that is an X
	} {
		if !IsISBN10(s) {
			t.Errorf("IsISBN10(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"0439023484",  // the last digit off by one
		"0439023843",  // two digits transposed
		"B075F5X8BR",  // an ASIN, which is the shape but not the number
		"X439023483",  // X outside the check position
		"043902348",   // nine characters
		"04390234833", // eleven
		"",
	} {
		if IsISBN10(s) {
			t.Errorf("IsISBN10(%q) = true, want false", s)
		}
	}
}

// ISBN13From10 recomputes the check digit rather than moving the old one, and
// refuses to convert anything that did not verify. A plausible looking ISBN-13
// in an export is worse than an absent one, because nothing downstream can tell
// it is wrong.
func TestISBN13From10(t *testing.T) {
	for in, want := range map[string]string{
		"0439023483": "9780439023481",
		"0306406152": "9780306406157",
		"080442957X": "9780804429573",
	} {
		if got := ISBN13From10(in); got != want {
			t.Errorf("ISBN13From10(%q) = %q, want %q", in, got, want)
		}
		if !IsISBN13(want) {
			t.Errorf("%q does not verify as an ISBN-13", want)
		}
	}
	for _, in := range []string{"B075F5X8BR", "0439023484", "", "not an isbn"} {
		if got := ISBN13From10(in); got != "" {
			t.Errorf("ISBN13From10(%q) = %q, want it to refuse", in, got)
		}
	}
}

// IsASIN is strict about case because its caller is checking a token pulled out
// of markup, where a lowercase ten character string is a slug the selector
// caught rather than an id.
func TestIsASIN(t *testing.T) {
	for _, s := range []string{"B075F5X8BR", "0439023483", "B08N5WRWNW"} {
		if !IsASIN(s) {
			t.Errorf("IsASIN(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"b075f5x8br", "B075F5X8B", "B075F5X8BRR", "B075-F5X8B", ""} {
		if IsASIN(s) {
			t.Errorf("IsASIN(%q) = true, want false", s)
		}
	}
}

// Every form a person can plausibly paste, from notes/Spec/3007/04_graph.md
// section 2. A tool that accepted eight of these nine would answer "not found"
// to a question it understood.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in    string
		value string
		mkt   string
		kind  Kind
	}{
		{"B075F5X8BR", "B075F5X8BR", "", KindASIN},
		{"b075f5x8br", "B075F5X8BR", "", KindASIN},
		{"0439023483", "0439023483", "", KindISBN10},
		{"0-439-02348-3", "0439023483", "", KindISBN10},
		{"https://www.amazon.com/dp/B075F5X8BR", "B075F5X8BR", "us", KindASIN},
		{"https://www.amazon.com/dp/B075F5X8BR/ref=sr_1_3?keywords=x&qid=1", "B075F5X8BR", "us", KindASIN},
		{"https://www.amazon.com/Skullcandy-Jib-Wired-Earbuds/dp/B075F5X8BR/", "B075F5X8BR", "us", KindASIN},
		{"https://www.amazon.com/gp/product/B075F5X8BR", "B075F5X8BR", "us", KindASIN},
		{"https://www.amazon.co.uk/dp/B075F5X8BR", "B075F5X8BR", "uk", KindASIN},
		{"https://amazon.de/gp/aw/d/B075F5X8BR", "B075F5X8BR", "de", KindASIN},
		{"https://www.amazon.com/product-reviews/B075F5X8BR?pageNumber=2", "B075F5X8BR", "us", KindASIN},
		{"amz:us/product/B075F5X8BR", "B075F5X8BR", "us", KindASIN},
		{"amz:uk/product/B075F5X8BR", "B075F5X8BR", "uk", KindASIN},
		{"/dp/B075F5X8BR", "B075F5X8BR", "", KindASIN},
	} {
		id, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if id.Value != tc.value || id.Marketplace != tc.mkt || id.Kind != tc.kind {
			t.Errorf("Parse(%q) = {%q %q %q}, want {%q %q %q}",
				tc.in, id.Value, id.Marketplace, id.Kind, tc.value, tc.mkt, tc.kind)
		}
		if id.Raw != tc.in {
			t.Errorf("Parse(%q) lost the input: raw = %q", tc.in, id.Raw)
		}
	}
}

// A bare id names no storefront and Parse says so by leaving the marketplace
// empty. Returning "us" would invent the one fact the input did not carry, and
// the caller could no longer tell "the user said us" from "the user said
// nothing", which is the difference the stderr note in the CLI depends on.
func TestParseDoesNotInventAMarketplace(t *testing.T) {
	id, err := Parse("B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if id.Marketplace != "" {
		t.Errorf("a bare id came back scoped to %q", id.Marketplace)
	}
}

// A host amz does not know returns no marketplace rather than a guess. The guess
// would pick a currency and a number format, and a price read as dollars when it
// was written as euros is wrong in a way nothing downstream can detect.
func TestUnknownHostIsNotGuessed(t *testing.T) {
	id, err := Parse("https://amazon.example.com/dp/B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if id.Marketplace != "" {
		t.Errorf("an unknown host resolved to %q", id.Marketplace)
	}
	if id.Host != "amazon.example.com" {
		t.Errorf("host = %q, want it kept so an error can quote it", id.Host)
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"product",
		"usb c cable",
		"https://www.amazon.com/",
		"https://www.amazon.com/s?k=kindle",
		"amz:us/seller/A2L77EE7U53NWQ",
		"amz:product/B075F5X8BR",
	} {
		if id, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", s, id)
		}
	}
}

// A URL to a host amz does not know still yields the id, because the id is in
// the path and the path is the same everywhere.
func TestParseUnknownHostStillYieldsTheID(t *testing.T) {
	id, err := Parse("https://example.com/dp/B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if id.Value != "B075F5X8BR" {
		t.Errorf("value = %q", id.Value)
	}
}

func TestMarketplaceForHost(t *testing.T) {
	for host, want := range map[string]string{
		"www.amazon.com":     "us",
		"amazon.com":         "us",
		"AMAZON.COM":         "us",
		"smile.amazon.com":   "us",
		"www.amazon.com:443": "us",
		"www.amazon.co.uk":   "uk",
		"www.amazon.co.jp":   "jp",
		"www.amazon.com.br":  "br",
		"example.com":        "",
		"":                   "",
	} {
		if got := MarketplaceForHost(host); got != want {
			t.Errorf("MarketplaceForHost(%q) = %q, want %q", host, got, want)
		}
	}
}
