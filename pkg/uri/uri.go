// Package uri builds and reads the amz: identifier space.
//
// Every URI this tool emits is built here and nowhere else. That is a stronger
// rule than it sounds, and it exists to make one specific bug impossible: a
// product URI without a marketplace on it. B075F5X8BR on amazon.com and
// B075F5X8BR on amazon.co.uk are different listings with different prices,
// different sellers and occasionally different products, so "amz:/product/
// B075F5X8BR" is an identifier that silently merges two things. With one
// constructor there is one place to check, and TestNoUnscopedProductURI has
// something to point at.
//
// The space, from notes/Spec/3007/04_graph.md section 3:
//
//	amz:<marketplace>/product/<asin>
//	amz:<marketplace>/node/<id>
//	amz:<marketplace>/chart/<kind>/<node>
//	amz:<marketplace>/search/<sha1 of the normalised query>
//	amz:seller/<merchant>
//	amz:brand/<store-uuid>
//	amz:author/<author-asin>
//	amz:review/<review-id>
//	amz:person/<sha1 of the display name>
//	amz:deal/<deal-id>
//
// A URI is built from an id and never from a display name. The one exception is
// amz:person, which is a review author, and review authors have no id this tool
// can read because the profile pages are disallowed. That node exists so a
// review keeps its author as a node rather than as a loose string, and it can
// never be resolved. See section 5 of the same document.
package uri

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Scheme is the prefix on every identifier in this space.
const Scheme = "amz"

// The kinds of thing that get an identifier.
const (
	KindProduct = "product"
	KindNode    = "node"
	KindChart   = "chart"
	KindSearch  = "search"
	KindSeller  = "seller"
	KindBrand   = "brand"
	KindAuthor  = "author"
	KindReview  = "review"
	KindPerson  = "person"
	KindDeal    = "deal"
)

// scoped says, for each kind, whether its identifier is meaningful only inside
// one storefront.
//
// Products and browse nodes are scoped because their id spaces are per
// marketplace: 172282 is Electronics on .com and something else on .de. Charts
// and searches are scoped because they are defined over those nodes.
//
// Merchant ids are the exception worth stating out loud, because it looks like
// an inconsistency until you know why. A merchant id is global across Amazon's
// storefronts: A2L77EE7U53NWQ is the same company everywhere. What is per
// marketplace is the seller's feedback and storefront, and those are properties
// of the edge that carries the marketplace it was measured in, not of the node.
// Brands, authors, reviews and deals are global for the same reason: the id is
// issued once and means one thing.
var scoped = map[string]bool{
	KindProduct: true,
	KindNode:    true,
	KindChart:   true,
	KindSearch:  true,
	KindSeller:  false,
	KindBrand:   false,
	KindAuthor:  false,
	KindReview:  false,
	KindPerson:  false,
	KindDeal:    false,
}

// Errors returned when an identifier cannot be built or read.
var (
	// ErrUnknownKind is a kind that is not in the space.
	ErrUnknownKind = errors.New("not a kind of thing amz identifies")
	// ErrNoMarketplace is a scoped kind built without a marketplace. This is
	// the error the whole package exists to raise.
	ErrNoMarketplace = errors.New("this kind of id is only meaningful inside one marketplace")
	// ErrNoID is a kind that needs an id and was given none.
	ErrNoID = errors.New("no id")
	// ErrNotAURI is a string that is not in the amz: space at all.
	ErrNotAURI = errors.New("not an amz: URI")
)

// Known reports whether kind is part of the identifier space.
func Known(kind string) bool { _, ok := scoped[kind]; return ok }

// Scoped reports whether this kind of identifier carries a marketplace.
func Scoped(kind string) bool { return scoped[kind] }

// Kinds returns every kind in the space, in the order the spec lists them.
func Kinds() []string {
	return []string{
		KindProduct, KindNode, KindChart, KindSearch, KindSeller,
		KindBrand, KindAuthor, KindReview, KindPerson, KindDeal,
	}
}

// New builds the identifier for one thing.
//
// The marketplace argument is ignored for an unscoped kind rather than rejected,
// because callers hold a client that always knows its marketplace and making
// each one remember which kinds care would spread this rule back out over the
// codebase, which is exactly what this package is here to prevent.
func New(kind, marketplace, id string) (string, error) {
	if !Known(kind) {
		return "", fmt.Errorf("%q: %w", kind, ErrUnknownKind)
	}
	if id == "" {
		return "", fmt.Errorf("%s: %w", kind, ErrNoID)
	}
	if !scoped[kind] {
		return Scheme + ":" + kind + "/" + id, nil
	}
	if marketplace == "" {
		return "", fmt.Errorf("%s/%s: %w", kind, id, ErrNoMarketplace)
	}
	return Scheme + ":" + marketplace + "/" + kind + "/" + id, nil
}

// Product is the identifier for one listing in one storefront.
func Product(marketplace, asin string) (string, error) {
	return New(KindProduct, marketplace, asin)
}

// Node is the identifier for one browse node in one storefront.
func Node(marketplace, id string) (string, error) { return New(KindNode, marketplace, id) }

// Seller is the identifier for one merchant, which is global.
func Seller(merchant string) (string, error) { return New(KindSeller, "", merchant) }

// Chart is the identifier for one bestseller style ranking of one node.
//
// The kind is part of the path rather than folded into the id because a node
// carries several charts at once: bestsellers, new releases, movers and shakers
// and most wished for are four rankings of the same set of products, and a URI
// that could not tell them apart would put four different orderings on one node.
func Chart(marketplace, kind, node string) (string, error) {
	if marketplace == "" {
		return "", fmt.Errorf("chart/%s/%s: %w", kind, node, ErrNoMarketplace)
	}
	if kind == "" || node == "" {
		return "", fmt.Errorf("chart: %w", ErrNoID)
	}
	return Scheme + ":" + marketplace + "/" + KindChart + "/" + kind + "/" + node, nil
}

// Search is the identifier for one query, hashed so that two spellings of the
// same search collapse to one node.
//
// The hash is over the already normalised query and this function does not
// normalise anything. Normalisation is lowercase, collapsed whitespace, sorted
// refinements and dropped tracking parameters, it is specified once in
// 07_search.md section 3, and it is a breaking change to alter it. Doing it in
// two places would let those two places drift, and the symptom would be a store
// that has the same search twice under different ids.
func Search(marketplace, normalizedQuery string) (string, error) {
	if normalizedQuery == "" {
		return "", fmt.Errorf("search: %w", ErrNoID)
	}
	return New(KindSearch, marketplace, Hash(normalizedQuery))
}

// Person is the identifier for a review author, hashed from the display name.
//
// This is the one node built from a name, because a review author has no id this
// tool can read: the profile pages are disallowed and are not fetched. The hash
// is not an attempt to identify a human being. Two reviewers who both call
// themselves "Amazon Customer" collide into one node and there is nothing to be
// done about that, which is why this node is never reported as resolved.
func Person(displayName string) (string, error) {
	name := strings.Join(strings.Fields(displayName), " ")
	if name == "" {
		return "", fmt.Errorf("person: %w", ErrNoID)
	}
	return New(KindPerson, "", Hash(name))
}

// Hash is the digest the search and person URIs are built from.
//
// SHA-1 is used because these are identifiers and not signatures. Nothing here
// resists an adversary, the input is a search phrase or a display name, and the
// only property needed is that the same input gives the same id every run.
func Hash(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec // an identifier, not a signature
	return hex.EncodeToString(sum[:])
}

// Ref is a parsed identifier.
type Ref struct {
	// Kind is what the identifier names.
	Kind string
	// Marketplace is the storefront, and is empty for an unscoped kind.
	Marketplace string
	// ID is the identifier within the kind. For a chart it is the node, with
	// the chart kind in ChartKind.
	ID string
	// ChartKind is set only for a chart.
	ChartKind string
}

// String rebuilds the identifier, and returns "" for a zero Ref.
func (r Ref) String() string {
	if r.Kind == KindChart {
		s, err := Chart(r.Marketplace, r.ChartKind, r.ID)
		if err != nil {
			return ""
		}
		return s
	}
	s, err := New(r.Kind, r.Marketplace, r.ID)
	if err != nil {
		return ""
	}
	return s
}

// Parse reads an identifier back into its parts.
//
// This is the half of the package that keeps the other half honest: a URI that
// does not parse back to what it was built from is a URI the store cannot look
// up, and round tripping is what the tests assert.
func Parse(s string) (Ref, error) {
	rest, ok := strings.CutPrefix(s, Scheme+":")
	if !ok {
		return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
	}

	// An unscoped kind puts the kind first, a scoped one puts the marketplace
	// first. There is no ambiguity to resolve because no marketplace slug is
	// also a kind, and a test asserts that stays true.
	if Known(parts[0]) && !scoped[parts[0]] {
		if len(parts) != 2 || parts[1] == "" {
			return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
		}
		return Ref{Kind: parts[0], ID: parts[1]}, nil
	}

	if len(parts) < 3 || parts[0] == "" {
		return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
	}
	mkt, kind := parts[0], parts[1]
	if !Known(kind) {
		return Ref{}, fmt.Errorf("%q: %q: %w", s, kind, ErrUnknownKind)
	}
	if !scoped[kind] {
		// An unscoped kind written with a marketplace in front of it is not a
		// harmless variant, it is a second identifier for a node that already
		// has one, and accepting it would put the same seller in the store twice.
		return Ref{}, fmt.Errorf("%q: %s ids are not scoped to a marketplace: %w", s, kind, ErrNotAURI)
	}
	if kind == KindChart {
		if len(parts) != 4 || parts[2] == "" || parts[3] == "" {
			return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
		}
		return Ref{Kind: kind, Marketplace: mkt, ChartKind: parts[2], ID: parts[3]}, nil
	}
	if len(parts) != 3 || parts[2] == "" {
		return Ref{}, fmt.Errorf("%q: %w", s, ErrNotAURI)
	}
	return Ref{Kind: kind, Marketplace: mkt, ID: parts[2]}, nil
}
