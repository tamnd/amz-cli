package amz

// Marketplace is one regional amazon storefront.
type Marketplace struct {
	Slug     string
	Host     string
	Currency string
	Language string
}

// marketplaces is the registry of supported regional storefronts.
var marketplaces = map[string]Marketplace{
	"us": {"us", "www.amazon.com", "USD", "en-US,en;q=0.9"},
	"uk": {"uk", "www.amazon.co.uk", "GBP", "en-GB,en;q=0.9"},
	"de": {"de", "www.amazon.de", "EUR", "de-DE,de;q=0.9,en;q=0.8"},
	"fr": {"fr", "www.amazon.fr", "EUR", "fr-FR,fr;q=0.9,en;q=0.8"},
	"jp": {"jp", "www.amazon.co.jp", "JPY", "ja-JP,ja;q=0.9,en;q=0.8"},
	"ca": {"ca", "www.amazon.ca", "CAD", "en-CA,en;q=0.9,fr;q=0.8"},
	"in": {"in", "www.amazon.in", "INR", "en-IN,en;q=0.9"},
	"it": {"it", "www.amazon.it", "EUR", "it-IT,it;q=0.9,en;q=0.8"},
	"es": {"es", "www.amazon.es", "EUR", "es-ES,es;q=0.9,en;q=0.8"},
	"mx": {"mx", "www.amazon.com.mx", "MXN", "es-MX,es;q=0.9,en;q=0.8"},
	"br": {"br", "www.amazon.com.br", "BRL", "pt-BR,pt;q=0.9,en;q=0.8"},
	"au": {"au", "www.amazon.com.au", "AUD", "en-AU,en;q=0.9"},
	"nl": {"nl", "www.amazon.nl", "EUR", "nl-NL,nl;q=0.9,en;q=0.8"},
	"se": {"se", "www.amazon.se", "SEK", "sv-SE,sv;q=0.9,en;q=0.8"},
	"sg": {"sg", "www.amazon.sg", "SGD", "en-SG,en;q=0.9"},
	"ae": {"ae", "www.amazon.ae", "AED", "en-AE,en;q=0.9,ar;q=0.8"},
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
