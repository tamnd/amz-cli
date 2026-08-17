package amz

import (
	"encoding/json"
	"math/big"
	"testing"
)

func mkt(t *testing.T, slug string) Marketplace {
	t.Helper()
	m, ok := LookupMarketplace(slug)
	if !ok {
		t.Fatalf("unknown marketplace %q", slug)
	}
	return m
}

func TestMoneyReadsEachMarketplaceInItsOwnConvention(t *testing.T) {
	tests := []struct {
		slug string
		in   string
		want string // exact, as a fraction of the minor unit
		cur  string
	}{
		{"us", "$1,299.00", "1299", "USD"},
		{"us", "$12.99", "1299/100", "USD"},
		{"us", "$0.42", "21/50", "USD"},
		{"de", "1.299,00 €", "1299", "EUR"},
		{"de", "12,99 €", "1299/100", "EUR"},
		{"fr", "1 299,00 €", "1299", "EUR"},
		{"uk", "£1,299.00", "1299", "GBP"},
		{"jp", "￥1,480", "1480", "JPY"},
		{"in", "₹1,23,456.78", "6172839/50", "INR"},
		{"br", "R$ 1.299,90", "12999/10", "BRL"},
		// The bug the marketplace table exists to prevent. Three digits after a
		// dot is a decimal in the US and a thousands group in Germany, and the
		// string alone cannot say which.
		{"us", "$1.299", "1299/1000", "USD"},
		{"de", "1.299 €", "1299", "EUR"},
	}
	for _, tc := range tests {
		got := ParseMoney(tc.in, mkt(t, tc.slug), "test")
		if got == nil {
			t.Errorf("%s %q parsed to nothing", tc.slug, tc.in)
			continue
		}
		want, _ := new(big.Rat).SetString(tc.want)
		if got.Amount.Cmp(want) != 0 {
			t.Errorf("%s %q = %s, want %s", tc.slug, tc.in, got.Amount, want)
		}
		if got.Currency != tc.cur {
			t.Errorf("%s %q currency = %q, want %q", tc.slug, tc.in, got.Currency, tc.cur)
		}
		if got.Display != collapseSpace(tc.in) {
			t.Errorf("%s %q lost its display string: %q", tc.slug, tc.in, got.Display)
		}
	}
}

func TestMoneyIsAbsentRatherThanZeroWhenThereIsNoPrice(t *testing.T) {
	for _, in := range []string{"", "  ", "Currently unavailable", "Auf Lager.", "$"} {
		if got := ParseMoney(in, mkt(t, "us"), "test"); got != nil {
			t.Errorf("%q parsed to %v, want nil so a caller can tell free from missing", in, got)
		}
	}
	// Free is a real price and has to survive.
	free := ParseMoney("$0.00", mkt(t, "us"), "test")
	if free == nil || free.Amount.Sign() != 0 {
		t.Errorf("$0.00 = %v, want an exact zero", free)
	}
}

func TestMoneyStaysExactOverTenThousandRows(t *testing.T) {
	// The reason this type is not a float64. Ten thousand copies of $12.99 is
	// $129,900.00 and nothing else, and the float sum is not that number.
	price := ParseMoney("$12.99", mkt(t, "us"), "test")
	sum := new(big.Rat)
	var lossy float64
	for range 10000 {
		sum.Add(sum, price.Amount)
		lossy += price.Value
	}
	want, _ := new(big.Rat).SetString("129900")
	if sum.Cmp(want) != 0 {
		t.Errorf("exact sum = %s, want %s", sum.FloatString(4), want)
	}
	if lossy == 129900 {
		t.Skip("this platform's float64 sum happens to be exact, which does not make it exact in general")
	}
}

func TestMoneyRoundTripsThroughJSONWithoutBecomingAFloat(t *testing.T) {
	in := ParseMoney("1.299,00 €", mkt(t, "de"), "corePrice")
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if bytes := string(b); !contains([]string{bytes}, bytes) || len(bytes) == 0 {
		t.Fatal("empty encoding")
	}
	var out Money
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Amount == nil {
		t.Fatal("Amount was not rebuilt, so a stored price is a float again")
	}
	if out.Amount.Cmp(in.Amount) != 0 {
		t.Errorf("round trip = %s, want %s", out.Amount, in.Amount)
	}
	// The fraction is deliberately not in the JSON. A consumer reading
	// "1299/1" where it expected a price would be right to complain.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["amount"]; ok {
		t.Errorf("the rational leaked into the JSON: %s", b)
	}
	for _, k := range []string{"display", "value", "currency"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("%q missing from the JSON: %s", k, b)
		}
	}
}

func TestMoneySubRefusesToMixCurrencies(t *testing.T) {
	usd := ParseMoney("$20.00", mkt(t, "us"), "list")
	eur := ParseMoney("15,00 €", mkt(t, "de"), "price")
	if got := usd.Sub(eur); got != nil {
		t.Errorf("subtracted euros from dollars and got %v", got)
	}
	other := ParseMoney("$15.00", mkt(t, "us"), "price")
	saving := usd.Sub(other)
	if saving == nil || saving.Display != "5.00" {
		t.Errorf("saving = %v, want 5.00", saving)
	}
}

func TestMoneyRangeStopsAtTheFirstNumber(t *testing.T) {
	// "$10.00 - $20.00" is a range, and the old parser's regular expression read
	// the hyphen as a sign on the second half. The first complete number is the
	// only honest answer from a field declared to hold one price.
	got := ParseMoney("$10.00 - $20.00", mkt(t, "us"), "test")
	if got == nil || got.Value != 10 {
		t.Errorf("range = %v, want 10", got)
	}
}
