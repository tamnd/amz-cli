package amz

import "testing"

func TestExtractASIN(t *testing.T) {
	cases := map[string]string{
		"https://www.amazon.com/dp/B08N5WRWNW":                          "B08N5WRWNW",
		"https://www.amazon.com/Some-Title/dp/B08N5WRWNW/ref=sr_1_1":    "B08N5WRWNW",
		"https://www.amazon.co.uk/gp/product/B07PGL2N7J":                "B07PGL2N7J",
		"https://www.amazon.de/product-reviews/B09B8V1LZ3?pageNumber=2": "B09B8V1LZ3",
		"B084DWG2VQ":                "B084DWG2VQ",
		"not-an-asin":               "",
		"https://example.com/x/y/z": "",
	}
	for in, want := range cases {
		if got := ExtractASIN(in); got != want {
			t.Errorf("ExtractASIN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePrice(t *testing.T) {
	cases := []struct {
		in    string
		price float64
		cur   string
	}{
		{"$1,299.00", 1299.00, "USD"},
		{"£49.99", 49.99, "GBP"},
		{"1.299,00 €", 1299.00, "EUR"},
		{"24.99", 24.99, ""},
		{"", 0, ""},
		{"Currently unavailable", 0, ""},
	}
	for _, c := range cases {
		p, cur := ParsePrice(c.in)
		if p != c.price || cur != c.cur {
			t.Errorf("ParsePrice(%q) = %v %q, want %v %q", c.in, p, cur, c.price, c.cur)
		}
	}
}

func TestUpgradeImage(t *testing.T) {
	cases := map[string]string{
		"https://m.media-amazon.com/images/I/71abcID._AC_SX466_.jpg":                                                  "https://m.media-amazon.com/images/I/71abcID.jpg",
		"https://images-na.ssl-images-amazon.com/images/I/71abcID._SL1000_.jpg":                                       "https://m.media-amazon.com/images/I/71abcID.jpg",
		"https://m.media-amazon.com/images/I/71abcID._SX38_SY50_CR,0,0,38,50_.jpg":                                    "https://m.media-amazon.com/images/I/71abcID.jpg",
		"https://m.media-amazon.com/images/I/71abcID.jpg":                                                             "https://m.media-amazon.com/images/I/71abcID.jpg",
		"//images-na.ssl-images-amazon.com/images/I/71abcID._SL500_.jpg":                                              "https://m.media-amazon.com/images/I/71abcID.jpg",
		"https://m.media-amazon.com/images/I/71abcID._AC_.jpg?x=1":                                                    "https://m.media-amazon.com/images/I/71abcID.jpg",
		"https://example.com/logo.png":                                                                                "https://example.com/logo.png",
		"data:image/gif;base64,R0lGODlh":                                                                              "",
		"https://m.media-amazon.com/images/G/01/x-locale/sprites/foo._CB1_.png":                                       "",
		"https://images-na.ssl-images-amazon.com/images/I/transparent-pixel.gif":                                      "",
		"https://m.media-amazon.com/images/I/410cDFU7CXL._CR0,0,35,46_BG85,85,85_BR-120_PKdp-play-icon-overlay__.jpg": "",
	}
	for in, want := range cases {
		if got := upgradeImage(in); got != want {
			t.Errorf("upgradeImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormImages(t *testing.T) {
	in := []string{
		"https://m.media-amazon.com/images/I/aaa._SL500_.jpg",
		"https://images-na.ssl-images-amazon.com/images/I/aaa._SL1000_.jpg", // same photo, other CDN
		"https://m.media-amazon.com/images/I/bbb._AC_SX466_.jpg",
		"data:image/gif;base64,zz", // junk, dropped
		"",                         // empty, dropped
	}
	out := normImages(in)
	if len(out) != 2 {
		t.Fatalf("normImages = %v", out)
	}
	if out[0] != "https://m.media-amazon.com/images/I/aaa.jpg" || out[1] != "https://m.media-amazon.com/images/I/bbb.jpg" {
		t.Errorf("normImages = %v", out)
	}
}

func TestDetectBlocked(t *testing.T) {
	if !DetectBlocked([]byte(`<html><title>Robot Check</title><form action="/errors/validateCaptcha"></form></html>`)) {
		t.Error("captcha page should be detected as blocked")
	}
	if DetectBlocked([]byte(`<html><span id="productTitle">A real product</span></html>`)) {
		t.Error("real page should not be detected as blocked")
	}
}

func TestMarketplaces(t *testing.T) {
	uk, ok := LookupMarketplace("uk")
	if !ok || uk.Host != "www.amazon.co.uk" || uk.Currency != "GBP" {
		t.Errorf("uk = %+v ok=%v", uk, ok)
	}
	if _, ok := LookupMarketplace("zz"); ok {
		t.Error("zz should be unknown")
	}
	if len(Marketplaces()) < 10 {
		t.Errorf("expected >=10 marketplaces, got %d", len(Marketplaces()))
	}
}

// TestParseIntStartsOnADigit pins the fix for a number reader that matched runs
// of digits and separators without requiring a digit in them.
//
// The strings below are all Amazon's own wording. Each has punctuation in front
// of the number that a run of "[\d,]" happily matched on its own, so the reader
// returned zero for a page that plainly stated a figure.
func TestParseIntStartsOnADigit(t *testing.T) {
	cases := map[string]int64{
		"Go to next page, page 2":      2,
		"Go to previous page, page 7":  7,
		"1,739 ratings":                1739,
		"33-48 of over 20,000 results": 33,
		"Previous":                     0,
		"":                             0,
		",":                            0,
	}
	for in, want := range cases {
		if got := parseInt(in); got != want {
			t.Errorf("parseInt(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestParseCountExpandsAbbreviations pins the other half of the same problem.
//
// A search card prints "(1.7K)" beside a link it labels "1,739 ratings". Reading
// the visible text with parseInt gives 1, and a product with 1 rating and a 4.6
// average is not a rounding error, it is a different product.
func TestParseCountExpandsAbbreviations(t *testing.T) {
	cases := map[string]int64{
		"(1.7K)":        1700,
		"3K+":           3000,
		"(12K)":         12000,
		"2.3M":          2300000,
		"1,739 ratings": 1739,
		"284,512":       284512,
		"":              0,
	}
	for in, want := range cases {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestCountRefusesARating pins the reason Count scans every match rather than
// the first: on a search card the label above the rating count reads "4.6 out of
// 5 stars, rating details", and reading it as a count gives four ratings.
func TestCountRefusesARating(t *testing.T) {
	const card = `<div data-cy="reviews-block">
	  <a aria-label="4.6 out of 5 stars, rating details"><span class="a-icon-alt">4.6 out of 5 stars</span></a>
	  <a aria-label="1,739 ratings"><span>(1.7K)</span></a>
	</div>`
	d, err := ParseDoc(FamilySearch, []byte(card))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := Count("a[aria-label]", "[aria-label]", "span")(nil, d.Region("reviews-block"))
	if !ok || v != int64(1739) {
		t.Errorf("Count = %v %v, want 1739", v, ok)
	}
}

// TestReadResultBar covers the line above a search grid, including the shape it
// takes past the last page.
func TestReadResultBar(t *testing.T) {
	cases := []struct {
		in   string
		want resultBar
	}{
		{`1-16 of over 20,000 results for "mechanical keyboard"`,
			resultBar{From: 1, To: 16, Total: 20000, Approx: true, Query: "mechanical keyboard"}},
		{`33-48 of over 20,000 results for "mechanical keyboard"`,
			resultBar{From: 33, To: 48, Total: 20000, Approx: true, Query: "mechanical keyboard"}},
		// Past the cap Amazon prints a range that runs backwards. It is recorded
		// as it was printed, and SearchPage.Exhausted is what reads it as an end.
		{`321-306 of over 30,000 results for "mechanical keyboard"`,
			resultBar{From: 321, To: 306, Total: 30000, Approx: true, Query: "mechanical keyboard"}},
		{`1,048 results for "usb c cable"`,
			resultBar{Total: 1048, Query: "usb c cable"}},
	}
	for _, c := range cases {
		got, ok := readResultBar(c.in)
		if !ok || got != c.want {
			t.Errorf("readResultBar(%q) = %+v %v, want %+v", c.in, got, ok, c.want)
		}
	}
	if _, ok := readResultBar("Sort by: Featured"); ok {
		t.Errorf("readResultBar read a bar out of a sort menu")
	}
}

// TestSearchPageExhausted covers the two ways a walk over /s ends.
func TestSearchPageExhausted(t *testing.T) {
	if (SearchPage{From: 1, To: 16, Cards: []Card{{}}}).Exhausted() {
		t.Errorf("a full page reports exhausted")
	}
	if !(SearchPage{From: 321, To: 306, Cards: []Card{{}}}).Exhausted() {
		t.Errorf("a backwards range does not report exhausted")
	}
	if !(SearchPage{From: 1, To: 16}).Exhausted() {
		t.Errorf("an empty grid does not report exhausted")
	}
}
