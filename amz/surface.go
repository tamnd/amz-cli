package amz

import (
	"net/url"
	"sort"
	"strings"
)

// The Ops registry: one table, three consumers.
//
// Consumer one is robots. Every request in amz goes through Client.Get, which
// asks the live robots.txt, and Ops carries what the last measurement said the
// answer would be. The two are compared and a disagreement is printed, because a
// stale expectation compiled into a binary is exactly the bug this design exists
// to prevent. The live file always wins.
//
// Consumer two is `amz surfaces` and `amz why`, which print this table rather
// than a hand-written list, so the documentation cannot drift from the code.
//
// Consumer three is the merge policy, which arrives with M4: Fields is what lets
// a light read avoid deleting a full read's data.
//
// See notes/Spec/3007/01_surfaces.md for the measurements behind every row.

// RobotsExpect is what the last measurement said robots.txt would answer.
type RobotsExpect int

const (
	// RobotsUnknown means nobody has measured this surface against the file.
	RobotsUnknown RobotsExpect = iota
	// RobotsAllowed means the measurement found no rule refusing it.
	RobotsAllowed
	// RobotsDisallowed means a rule refused it.
	RobotsDisallowed
	// RobotsNA is for surfaces robots.txt has no opinion about, like robots.txt.
	RobotsNA
)

func (r RobotsExpect) String() string {
	switch r {
	case RobotsAllowed:
		return "allowed"
	case RobotsDisallowed:
		return "disallowed"
	case RobotsNA:
		return "n/a"
	default:
		return "unknown"
	}
}

// Op is one surface amz knows how to read.
type Op struct {
	ID     string       // the spec's surface id, s1..s20
	Name   string       // "product", "search", "offers"
	Path   string       // the shape of the path, for display
	Robots RobotsExpect // what the last measurement said
	Login  bool         // known to redirect to /ap/signin
	Since  string       // when the Login or Robots fact was measured
	Fields []string     // what this surface can carry
	Why    string       // the `amz why` topic
	Note   string       // one line, printed by `amz surfaces`

	// match scores a URL against this surface: the length of the path it
	// recognised, or -1 for no match. The score is how /stores/author/<id> wins
	// over /stores/<name> for the same URL.
	match func(u *url.URL) int
}

// Matches reports whether a URL is a read of this surface.
func (o *Op) Matches(u *url.URL) bool { return o.score(u) >= 0 }

func (o *Op) score(u *url.URL) int {
	if o.match == nil {
		return -1
	}
	return o.match(u)
}

func pathPrefix(prefixes ...string) func(*url.URL) int {
	return func(u *url.URL) int {
		best := -1
		for _, p := range prefixes {
			if strings.HasPrefix(u.Path, p) && len(p) > best {
				best = len(p)
			}
		}
		return best
	}
}

func pathIs(paths ...string) func(*url.URL) int {
	return func(u *url.URL) int {
		got := strings.TrimSuffix(u.Path, "/")
		for _, p := range paths {
			if got == strings.TrimSuffix(p, "/") {
				// An exact path is more specific than any prefix of it.
				return len(p) + 1000
			}
		}
		return -1
	}
}

// ops is the registry. The order is the spec's surface order.
var ops = []*Op{
	{
		ID: "s1", Name: "product", Path: "/dp/<asin>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"asin", "title", "brand", "price", "rating", "ratings_count", "availability", "description", "bullet_points", "images", "category_path", "browse_node_ids", "variant_asins", "reviews", "rating_histogram", "buy_box"},
		Why:    "product",
		Note:   "1.7 to 2.2 MB. Carries reviews, the histogram and the buy box that the dead review surfaces used to serve.",
		match:  pathPrefix("/dp/", "/gp/product/"),
	},
	{
		ID: "s2", Name: "product-light", Path: "/gp/aw/d/<asin>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"asin", "title", "price", "rating", "ratings_count", "images"},
		Why:    "product",
		Note:   "374 KB, the mobile rendering. Fewer fields, one fifth the bytes.",
		match:  pathPrefix("/gp/aw/d/"),
	},
	{
		ID: "s3", Name: "search", Path: "/s?k=<query>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"position", "asin", "title", "price", "rating", "ratings_count", "image", "sponsored"},
		Why:    "search",
		Note:   "Stops at result 306 whatever the corpus size, and the total it reports is an estimate that changes between fetches.",
		match:  pathIs("/s"),
	},
	{
		ID: "s4", Name: "category", Path: "/b?node=<id>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"node", "name", "children", "products"},
		Why:    "robots",
		Note:   "Allowed except five nodes, which robots.txt refuses by a query-string pattern.",
		match:  pathIs("/b", "/gp/browse.html"),
	},
	{
		ID: "s5", Name: "bestsellers", Path: "/gp/bestsellers/<slug>/", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"rank", "asin", "title", "price", "rating", "ratings_count"},
		Why:    "charts",
		Note:   "50 items per page, two pages per list.",
		match:  pathPrefix("/gp/bestsellers"),
	},
	{
		ID: "s6", Name: "new-releases", Path: "/gp/new-releases/<slug>/", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"rank", "asin", "title", "price", "rating", "ratings_count"},
		Why:    "charts",
		match:  pathPrefix("/gp/new-releases"),
	},
	{
		ID: "s7", Name: "movers", Path: "/gp/movers-and-shakers/<slug>/", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"rank", "asin", "title", "price", "rank_change"},
		Why:    "charts",
		match:  pathPrefix("/gp/movers-and-shakers"),
	},
	{
		ID: "s8", Name: "most-wished-for", Path: "/gp/most-wished-for/<slug>/", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"rank", "asin", "title", "price", "rating"},
		Why:    "charts",
		match:  pathPrefix("/gp/most-wished-for"),
	},
	{
		ID: "s9", Name: "most-gifted", Path: "/gp/most-gifted/<slug>/", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"rank", "asin", "title", "price", "rating"},
		Why:    "charts",
		match:  pathPrefix("/gp/most-gifted"),
	},
	{
		ID: "s10", Name: "brand", Path: "/stores/<name>/page/<uuid>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"brand", "products"},
		Why:    "stores",
		Note:   "A storefront, not a catalogue. Anchored on data-testid, not data-feature-name.",
		match:  pathPrefix("/stores/"),
	},
	{
		ID: "s11", Name: "seller", Path: "/sp?seller=<id>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"seller_id", "name", "rating", "feedback_count", "business_name"},
		Why:    "seller",
		Note:   "amz reads /sp and not /gp/aag/main, because robots.txt disallows /gp/aag for every seller but one.",
		match:  pathIs("/sp"),
	},
	{
		ID: "s12", Name: "author", Path: "/stores/author/<id>", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"author", "books"},
		Why:    "stores",
		match:  pathPrefix("/stores/author/"),
	},
	{
		ID: "s13", Name: "deals", Path: "/deals", Robots: RobotsAllowed, Since: "2026-08-17",
		Fields: []string{"asin", "title", "price", "list_price", "discount", "deal_type", "ends_at"},
		Why:    "deals",
		match:  pathIs("/deals", "/gp/goldbox"),
	},
	{
		ID: "s14", Name: "robots", Path: "/robots.txt", Robots: RobotsNA, Since: "2026-08-17",
		Why:   "robots",
		Note:  "Fetched before anything else, cached 24 hours, never guessed at.",
		match: pathIs("/robots.txt"),
	},
	{
		ID: "s15", Name: "reviews-full", Path: "/product-reviews/<asin>", Robots: RobotsAllowed, Login: true, Since: "2026-08-17",
		Fields: []string{"review_id", "author", "rating", "title", "body", "date", "verified", "helpful_votes"},
		Why:    "reviews",
		Note:   "302 to /ap/signin. robots.txt allows it; Amazon does not serve it. That is a login wall, not a robots rule.",
		match:  pathPrefix("/product-reviews/"),
	},
	{
		ID: "s16", Name: "reviews-portal", Path: "/portal/customer-reviews/<asin>", Robots: RobotsAllowed, Login: true, Since: "2026-08-17",
		Why:   "reviews",
		Note:  "302 to /ap/signin, same as s15.",
		match: pathPrefix("/portal/customer-reviews/"),
	},
	{
		ID: "s17", Name: "qa", Path: "/ask/questions/asin/<asin>", Robots: RobotsAllowed, Login: true, Since: "2026-08-17",
		Fields: []string{"question", "answers", "votes", "asked_at"},
		Why:    "qa",
		Note:   "302 to /ap/signin. The product page still carries the answered-question count.",
		match:  pathPrefix("/ask/questions/"),
	},
	{
		ID: "s18", Name: "offers", Path: "/gp/offer-listing/<asin>", Robots: RobotsDisallowed, Since: "2026-08-17",
		Fields: []string{"seller", "price", "condition", "shipping", "prime"},
		Why:    "offers",
		Note:   "Disallowed by robots.txt for every ASIN except those beginning B000 or 9000, and it 301s to the detail page anyway.",
		match:  pathPrefix("/gp/offer-listing/"),
	},
	{
		ID: "s19", Name: "sitemap", Path: "/sitemap.xml", Robots: RobotsNA, Since: "2026-08-17",
		Why:   "discovery",
		Note:  "500. amazon.com publishes no sitemap and its robots.txt names none, so discovery starts from charts and browse nodes.",
		match: pathIs("/sitemap.xml"),
	},
}

// Ops returns the registry.
func Ops() []*Op { return ops }

// OpFor classifies a URL. It returns nil when no surface claims it, which is
// itself worth printing: an unclassified fetch is a fetch nobody has measured.
func OpFor(rawURL string) *Op {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	// The longest recognised path wins, so /stores/author/<id> beats /stores/<name>.
	var best *Op
	bestScore := -1
	for _, op := range ops {
		if s := op.score(u); s > bestScore {
			best, bestScore = op, s
		}
	}
	return best
}

// OpNamed returns the surface with this name.
func OpNamed(name string) *Op {
	for _, op := range ops {
		if op.Name == name {
			return op
		}
	}
	return nil
}

// OpNames returns every surface name, sorted.
func OpNames() []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Name)
	}
	sort.Strings(out)
	return out
}
