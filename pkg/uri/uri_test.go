package uri

import (
	"errors"
	"strings"
	"testing"
)

// The test the package exists for. A product URI without a marketplace on it
// merges amazon.com and amazon.co.uk into one node, and those are two listings
// with different prices and sometimes different products. It has to be
// impossible to build, not merely discouraged.
func TestNoUnscopedProductURI(t *testing.T) {
	for _, kind := range Kinds() {
		if !Scoped(kind) {
			continue
		}
		u, err := New(kind, "", "B075F5X8BR")
		if err == nil {
			t.Errorf("%s built %q with no marketplace", kind, u)
			continue
		}
		if !errors.Is(err, ErrNoMarketplace) {
			t.Errorf("%s: err = %v, want ErrNoMarketplace", kind, err)
		}
	}
	if _, err := Chart("", "bestsellers", "172282"); !errors.Is(err, ErrNoMarketplace) {
		t.Errorf("Chart with no marketplace: err = %v, want ErrNoMarketplace", err)
	}
	if _, err := Search("", "kindle"); !errors.Is(err, ErrNoMarketplace) {
		t.Errorf("Search with no marketplace: err = %v, want ErrNoMarketplace", err)
	}
}

// The same ASIN in two storefronts is two identifiers, which is the property the
// store's primary key depends on.
func TestTwoMarketplacesAreTwoIdentifiers(t *testing.T) {
	us, err := Product("us", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	uk, err := Product("uk", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if us == uk {
		t.Fatalf("both storefronts produced %q", us)
	}
	if us != "amz:us/product/B075F5X8BR" || uk != "amz:uk/product/B075F5X8BR" {
		t.Errorf("us = %q, uk = %q", us, uk)
	}
}

// A merchant id is global. Scoping it would give one company sixteen nodes and
// the graph would report Anker Direct as sixteen sellers.
func TestMerchantIDsAreNotScoped(t *testing.T) {
	for _, kind := range []string{KindSeller, KindBrand, KindAuthor, KindReview, KindDeal, KindPerson} {
		if Scoped(kind) {
			t.Errorf("%s is scoped, but its id space is global", kind)
		}
		u, err := New(kind, "us", "A2L77EE7U53NWQ")
		if err != nil {
			t.Errorf("%s: %v", kind, err)
			continue
		}
		if strings.HasPrefix(u, "amz:us/") {
			t.Errorf("%s = %q, want the marketplace left out", kind, u)
		}
	}
	got, err := Seller("A2L77EE7U53NWQ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "amz:seller/A2L77EE7U53NWQ" {
		t.Errorf("Seller = %q", got)
	}
}

// Every identifier this package writes has to read back as what it was built
// from, or the store cannot look up what it wrote.
func TestRoundTrip(t *testing.T) {
	for _, want := range []Ref{
		{Kind: KindProduct, Marketplace: "us", ID: "B075F5X8BR"},
		{Kind: KindProduct, Marketplace: "uk", ID: "0439023483"},
		{Kind: KindNode, Marketplace: "de", ID: "172282"},
		{Kind: KindChart, Marketplace: "us", ChartKind: "bestsellers", ID: "172282"},
		{Kind: KindSeller, ID: "A2L77EE7U53NWQ"},
		{Kind: KindBrand, ID: "anker"},
		{Kind: KindAuthor, ID: "B000APZ33E"},
		{Kind: KindReview, ID: "R1234567890ABC"},
		{Kind: KindDeal, ID: "d3a1b2c4"},
	} {
		s := want.String()
		if s == "" {
			t.Errorf("%+v did not render", want)
			continue
		}
		got, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", s, got, want)
		}
	}
}

// A search collapses two spellings of the same query onto one node, which is the
// reason it is hashed rather than embedded. The normalisation itself lives in
// the search package and is deliberately not repeated here.
func TestSearchHashesTheQuery(t *testing.T) {
	a, err := Search("us", "usb c cable")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Search("us", "usb c cable")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("the same query hashed twice: %q and %q", a, b)
	}
	c, _ := Search("us", "usb-c cable")
	if a == c {
		t.Error("two different queries collided")
	}
	if strings.Contains(a, "usb") {
		t.Errorf("the query leaked into the identifier: %q", a)
	}
	if u, _ := Search("uk", "usb c cable"); u == a {
		t.Error("the same query in two storefronts gave one identifier")
	}
}

// A review author is the one node built from a name, and two people who both
// call themselves Amazon Customer collapse into one. That is unavoidable and it
// is why the reference is never reported resolved.
func TestPersonHashesTheName(t *testing.T) {
	a, err := Person("Amazon Customer")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Person("  Amazon   Customer  ")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("whitespace changed the identity: %q and %q", a, b)
	}
	if !strings.HasPrefix(a, "amz:person/") {
		t.Errorf("Person = %q", a)
	}
	if _, err := Person("   "); err == nil {
		t.Error("a blank name produced an identifier")
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"B075F5X8BR",
		"https://www.amazon.com/dp/B075F5X8BR",
		"amz:",
		"amz:us",
		"amz:us/product",
		"amz:us/product/",
		"amz:us/widget/123",
		"amz:us/seller/A2L77EE7U53NWQ", // an unscoped kind written scoped
		"amz:product/B075F5X8BR",       // a scoped kind written unscoped
		"amz:us/chart/bestsellers",
		"amz:seller/A2L77EE7U53NWQ/extra",
	} {
		if r, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", s, r)
		}
	}
}

// The parser tells a scoped URI from an unscoped one by looking at whether the
// first segment is a kind, which only works while no marketplace slug is also a
// kind. Nothing stops somebody adding one, so this says so out loud.
func TestNoMarketplaceSlugIsAlsoAKind(t *testing.T) {
	// Every marketplace slug amz supports, kept here rather than imported so
	// that pkg/uri stays free of the client. The amz package asserts the two
	// lists agree.
	for _, slug := range []string{
		"us", "uk", "de", "fr", "jp", "ca", "in", "it", "es", "mx", "br", "au", "nl", "se", "sg", "ae",
	} {
		if Known(slug) {
			t.Errorf("%q is both a marketplace and a kind, so a URI starting with it is ambiguous", slug)
		}
	}
}

func TestNewRejectsWhatItCannotBuild(t *testing.T) {
	if _, err := New("widget", "us", "1"); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("err = %v, want ErrUnknownKind", err)
	}
	if _, err := New(KindProduct, "us", ""); !errors.Is(err, ErrNoID) {
		t.Errorf("err = %v, want ErrNoID", err)
	}
	if _, err := Seller(""); !errors.Is(err, ErrNoID) {
		t.Errorf("err = %v, want ErrNoID", err)
	}
}
