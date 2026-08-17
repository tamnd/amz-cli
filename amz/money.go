package amz

import (
	"encoding/json"
	"math/big"
	"strings"
)

// Money is a price, kept exactly.
//
// Three reasons this is not a float64, which is what v0.2.1 used on eight
// different structs.
//
// A price rendered as $12.99, stored as a float and printed back is fine. The
// same price summed over ten thousand rows in the store and compared against a
// total is not, because 12.99 has no exact binary representation and the error
// compounds. Amount is a rational built from the digits Amazon printed, so the
// sum of ten thousand prices is the number a calculator gives.
//
// Amazon writes prices in the marketplace's own convention, and "1.299" is one
// thousand two hundred and ninety nine euros on amazon.de. The parse is per
// marketplace, from the marketplace table, and never from a regular expression
// that counts digits and hopes.
//
// A price of zero and no price are different, so Money is used as *Money
// everywhere and absent stays absent.
type Money struct {
	// Amount is the exact value. Use this for arithmetic.
	Amount *big.Rat `json:"-"`
	// Display is the string exactly as the page wrote it, "$1,299.00". It is
	// kept so a wrong parse is recoverable rather than lost.
	Display string `json:"display"`
	// Value is Amount as a float, for the consumers that want a number. It is
	// lossy by construction and documented as such. Do not sum it.
	Value float64 `json:"value"`
	// Currency is ISO 4217, resolved from an explicit code in the string when
	// there is one and from the marketplace when there is not.
	Currency string `json:"currency"`
	// Via names the region, payload or selector the display string came from.
	Via string `json:"via,omitempty"`
}

// ParseMoney reads a price string in one marketplace's convention.
//
// It returns nil when the string holds no number at all, because a caller that
// gets a zero Money back cannot tell "free" from "there was no price here".
// money reads a price field off the extractor and parses it in the
// marketplace's own convention, carrying the field's provenance onto the Money
// so a price in a record can still say which region produced it.
func money(e *Extractor, field string, m Marketplace) *Money {
	via := ""
	if p, ok := e.Prov(field); ok {
		via = p.Via
	}
	return ParseMoney(e.Str(field), m, via)
}

func ParseMoney(s string, m Marketplace, via string) *Money {
	display := collapseSpace(s)
	if display == "" {
		return nil
	}
	digits := numberIn(display, m)
	if digits == "" {
		return nil
	}
	amount, ok := new(big.Rat).SetString(digits)
	if !ok {
		return nil
	}
	// An explicit three letter code in the string wins, because it is the one
	// thing on the page that cannot be misread. Everything else defers to the
	// marketplace: "$" is USD, CAD, AUD, SGD, MXN and BRL depending only on
	// which host served the page, and "R$ 1.299,90" on amazon.com.br is reais
	// however confidently a glyph table says otherwise.
	cur := m.Currency
	if iso := isoIn(display); iso != "" {
		cur = iso
	}
	v, _ := amount.Float64()
	return &Money{Amount: amount, Display: display, Value: v, Currency: cur, Via: via}
}

// numberIn pulls the numeric part out of a price string and rewrites it in the
// one format big.Rat reads: an optional sign, digits, a dot, digits.
//
// Group separators are deleted and the marketplace's decimal separator becomes a
// dot. Nothing here inspects how many digits follow a separator, which is the
// guess the old parser made and the reason a German price ending in three digits
// came back a thousand times too large.
func numberIn(s string, m Marketplace) string {
	var b strings.Builder
	seenDigit, seenDecimal := false, false
	neg := false
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			seenDigit = true
		case r == '-' && !seenDigit && b.Len() == 0:
			// A leading minus is a real sign. A hyphen between two numbers is a
			// range, "$10 - $20", and stopping at the first complete number is
			// what keeps that from becoming negative ten.
			neg = true
		case r == m.Decimal && seenDigit && !seenDecimal:
			b.WriteByte('.')
			seenDecimal = true
		case strings.ContainsRune(m.Group, r) && seenDigit:
			// A group separator only groups when a digit follows it. Trailing
			// punctuation ends the number: "Auf Lager." is not 12,99 with a
			// stray dot on the end.
			if !digitFollows(s, i, r) {
				return finish(&b, neg, seenDigit)
			}
		default:
			if seenDigit {
				return finish(&b, neg, seenDigit)
			}
		}
	}
	return finish(&b, neg, seenDigit)
}

func digitFollows(s string, i int, sep rune) bool {
	rest := s[i+len(string(sep)):]
	return rest != "" && rest[0] >= '0' && rest[0] <= '9'
}

func finish(b *strings.Builder, neg, seenDigit bool) string {
	if !seenDigit {
		return ""
	}
	out := strings.TrimSuffix(b.String(), ".")
	if neg {
		return "-" + out
	}
	return out
}

// MarshalJSON emits Display, Value and Currency. Amount is skipped because a
// rational serializes as a fraction, and a consumer reading "1299/1" where it
// expected a price would be right to be annoyed.
func (m Money) MarshalJSON() ([]byte, error) {
	type alias Money
	return json.Marshal(alias(m))
}

// UnmarshalJSON rebuilds Amount from Display so a Money that has been through
// the store is still exact. Falling back to Value would quietly reintroduce the
// float this type exists to avoid.
func (m *Money) UnmarshalJSON(b []byte) error {
	type alias Money
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = Money(a)
	if m.Amount == nil && m.Display != "" {
		// The store round trip has no marketplace to hand, and Display was
		// written by ParseMoney, so the separators in it are the ones that
		// marketplace uses. Both conventions are tried and the one that reads
		// the whole numeric run wins.
		for _, mk := range []Marketplace{
			{Decimal: dotDecimal, Group: commaGroup},
			{Decimal: commaDecimal, Group: dotGroup},
		} {
			if d := numberIn(m.Display, mk); d != "" {
				if r, ok := new(big.Rat).SetString(d); ok {
					f, _ := r.Float64()
					// The right convention is the one whose float agrees with
					// the Value that was serialized alongside it.
					if m.Value == 0 || nearly(f, m.Value) {
						m.Amount = r
						break
					}
				}
			}
		}
	}
	return nil
}

func nearly(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.0000001
}

// Float is Value with a nil check, for the callers that hold a *Money.
func (m *Money) Float() float64 {
	if m == nil {
		return 0
	}
	return m.Value
}

// Cur is Currency with a nil check.
func (m *Money) Cur() string {
	if m == nil {
		return ""
	}
	return m.Currency
}

// Sub returns m minus other, exactly, or nil if either is absent or they are in
// different currencies. Subtracting dollars from euros is not an arithmetic
// problem to be rounded away, it is a question with no answer.
func (m *Money) Sub(other *Money) *Money {
	if m == nil || other == nil || m.Currency != other.Currency {
		return nil
	}
	amount := new(big.Rat).Sub(m.Amount, other.Amount)
	v, _ := amount.Float64()
	return &Money{Amount: amount, Display: formatMoney(amount, m.Currency), Value: v, Currency: m.Currency}
}

// formatMoney renders a rational back to a plain decimal string. It is used only
// for values this package computed, never to restate what a page printed.
func formatMoney(r *big.Rat, currency string) string {
	minor := 2
	if currency == "JPY" {
		minor = 0
	}
	return r.FloatString(minor)
}
