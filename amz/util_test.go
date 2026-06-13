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
