package amz

// Marketplace is one regional amazon storefront.
type Marketplace struct {
	Slug     string
	Host     string
	Currency string
	Language string

	// Decimal is the character this marketplace writes between the units and
	// the fraction, and Group holds every character it writes between thousands.
	//
	// These are here rather than guessed from the string because "1.299" is one
	// thousand two hundred and ninety nine euros on amazon.de and one dollar
	// twenty nine and nine tenths nowhere at all, and no amount of looking at
	// that string alone tells the two apart. The old parser guessed by counting
	// digits after the separator, which is right until a German price happens to
	// end in three digits.
	Decimal rune
	Group   string
	// Minor is how many decimal places the currency has. Yen has none, so a
	// price parser that assumes two turns 1,480 yen into fourteen.
	Minor int
	// InStock is what an in-stock buy box says here, lowercased. Availability is
	// kept as the page wrote it and this is what derives the boolean from it.
	InStock []string
}

// The two number formats Amazon writes, named so the table below reads as a
// statement about conventions rather than a column of punctuation. The group
// sets include U+00A0 and U+202F because Amazon emits both, and a plain space
// because the HTML sometimes arrives with the non-breaking space collapsed.
const (
	dotDecimal   = '.'
	commaDecimal = ','
	commaGroup   = ",\u00a0\u202f "
	dotGroup     = ".\u00a0\u202f "
)

// marketplaces is the registry of supported regional storefronts.
var marketplaces = map[string]Marketplace{
	"us": {Slug: "us", Host: "www.amazon.com", Currency: "USD", Language: "en-US,en;q=0.9",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually ships", "available"}},
	"uk": {Slug: "uk", Host: "www.amazon.co.uk", Currency: "GBP", Language: "en-GB,en;q=0.9",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually dispatched", "available"}},
	"de": {Slug: "de", Host: "www.amazon.de", Currency: "EUR", Language: "de-DE,de;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"auf lager", "auf lager.", "verfügbar"}},
	"fr": {Slug: "fr", Host: "www.amazon.fr", Currency: "EUR", Language: "fr-FR,fr;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"en stock", "disponible"}},
	"jp": {Slug: "jp", Host: "www.amazon.co.jp", Currency: "JPY", Language: "ja-JP,ja;q=0.9,en;q=0.8",
		Decimal: dotDecimal, Group: commaGroup, Minor: 0, InStock: []string{"在庫あり", "in stock"}},
	"ca": {Slug: "ca", Host: "www.amazon.ca", Currency: "CAD", Language: "en-CA,en;q=0.9,fr;q=0.8",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually ships", "available"}},
	"in": {Slug: "in", Host: "www.amazon.in", Currency: "INR", Language: "en-IN,en;q=0.9",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually dispatched"}},
	"it": {Slug: "it", Host: "www.amazon.it", Currency: "EUR", Language: "it-IT,it;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"disponibilità", "disponibile"}},
	"es": {Slug: "es", Host: "www.amazon.es", Currency: "EUR", Language: "es-ES,es;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"en stock", "disponible"}},
	"mx": {Slug: "mx", Host: "www.amazon.com.mx", Currency: "MXN", Language: "es-MX,es;q=0.9,en;q=0.8",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"en stock", "disponible"}},
	"br": {Slug: "br", Host: "www.amazon.com.br", Currency: "BRL", Language: "pt-BR,pt;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"em estoque", "disponível"}},
	"au": {Slug: "au", Host: "www.amazon.com.au", Currency: "AUD", Language: "en-AU,en;q=0.9",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually ships", "available"}},
	"nl": {Slug: "nl", Host: "www.amazon.nl", Currency: "EUR", Language: "nl-NL,nl;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"op voorraad", "beschikbaar"}},
	"se": {Slug: "se", Host: "www.amazon.se", Currency: "SEK", Language: "sv-SE,sv;q=0.9,en;q=0.8",
		Decimal: commaDecimal, Group: dotGroup, Minor: 2, InStock: []string{"i lager", "tillgänglig"}},
	"sg": {Slug: "sg", Host: "www.amazon.sg", Currency: "SGD", Language: "en-SG,en;q=0.9",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually ships"}},
	"ae": {Slug: "ae", Host: "www.amazon.ae", Currency: "AED", Language: "en-AE,en;q=0.9,ar;q=0.8",
		Decimal: dotDecimal, Group: commaGroup, Minor: 2, InStock: []string{"in stock", "usually ships"}},
}

// LookupMarketplace returns the marketplace for a slug, defaulting to US for
// an unknown or empty slug. The second return reports whether the slug was known.
func LookupMarketplace(slug string) (Marketplace, bool) {
	if slug == "" {
		return marketplaces["us"], true
	}
	m, ok := marketplaces[slug]
	if !ok {
		return marketplaces["us"], false
	}
	return m, true
}

// Marketplaces returns every registered marketplace slug in a stable-ish order.
func Marketplaces() []Marketplace {
	out := make([]Marketplace, 0, len(marketplaces))
	for _, m := range marketplaces {
		out = append(out, m)
	}
	return out
}

// BaseURL is the https origin for the marketplace.
func (m Marketplace) BaseURL() string { return "https://" + m.Host }
