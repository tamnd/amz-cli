// Package amz is a read-only client library for amazon.com: it fetches public
// pages, detects the bot wall, and normalizes each surface into a rich record.
package amz

import (
	"encoding/json"
	"strings"
	"time"
)

// Entity kinds used by the crawl queue and the seed command.
const (
	EntityProduct    = "product"
	EntityReviews    = "reviews"
	EntityQA         = "qa"
	EntityOffers     = "offers"
	EntityBrand      = "brand"
	EntityAuthor     = "author"
	EntityCategory   = "category"
	EntitySeller     = "seller"
	EntitySearch     = "search"
	EntityBestseller = "bestseller"
)

// ProductRank is one Best Sellers Rank line: a position within a named category.
// A product is usually ranked once overall and again in one or more subcategories.
//
// Kept because the field rules return it and because --flat still emits it. New
// code should read Product.Ranks, which is []Rank and carries the browse node
// the rank line links to.
type ProductRank struct {
	Rank     int    `json:"rank"`
	Category string `json:"category"`
}

// Product is a normalized amazon.com product detail page.
//
// Read the pointers. Rating, RatingsCount, ReviewsCount and BoughtPastMonthN are
// pointers because a product with no ratings and a product whose rating region
// was not read are different things, and a float64 zero cannot say which one it
// is. That distinction is what the whole envelope exists to serve and it would
// be undone here by four convenient value types.
type Product struct {
	// identity
	ASIN        string `json:"asin"`
	ParentASIN  string `json:"parent_asin,omitempty"`
	Marketplace string `json:"marketplace"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	ISBN10      string `json:"isbn10,omitempty"`
	ISBN13      string `json:"isbn13,omitempty"`
	ModelNumber string `json:"model_number,omitempty"`
	UPC         string `json:"upc,omitempty"`
	EAN         string `json:"ean,omitempty"`

	// who makes it
	Brand        *Ref   `json:"brand,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	// Byline is the bylineInfo text as written, before the Visit the / Store
	// wrapper is stripped off it.
	Byline  string `json:"byline,omitempty"`
	Authors []Ref  `json:"authors,omitempty"`

	// what it costs
	Offer       *Offer `json:"offer,omitempty"`
	OtherOffers *Conn  `json:"other_offers,omitempty"`

	// what people think
	Rating       *float64      `json:"rating,omitempty"`
	RatingsCount *int64        `json:"ratings_count,omitempty"`
	ReviewsCount *int64        `json:"reviews_count,omitempty"`
	Distribution *Distribution `json:"distribution,omitempty"`
	Reviews      *Conn         `json:"reviews,omitempty"`
	Questions    *Conn         `json:"questions,omitempty"`
	// ReviewSample is the handful of full reviews the detail page carries in
	// its medley. It is named a sample and not a list because that is what it
	// is: the corpus behind it is at /product-reviews/, which redirects to a
	// sign-in, so these are the only reviews amz can read and there is no
	// paging that gets more. Reviews above says how many exist.
	ReviewSample []Review `json:"review_sample,omitempty"`
	// QASample is the question and answer pairs the ask region carries inline,
	// which on most products is none. Questions above carries the count Amazon
	// states, and the two disagreeing is the normal case rather than a fault.
	QASample []QA `json:"qa_sample,omitempty"`

	// where it sits
	Breadcrumb []Ref  `json:"breadcrumb,omitempty"`
	Ranks      []Rank `json:"ranks,omitempty"`

	// what it is
	Bullets     []string          `json:"bullets,omitempty"`
	Description string            `json:"description,omitempty"`
	Details     map[string]string `json:"details,omitempty"`

	// variants
	Variation *Variation `json:"variation,omitempty"`

	// media
	Images    []Image  `json:"images,omitempty"`
	ImageURLs []string `json:"image_urls,omitempty"`
	Videos    []Video  `json:"videos,omitempty"`

	// social proof
	BoughtPastMonth string `json:"bought_past_month,omitempty"`
	// BoughtPastMonthN is the parse of the line above. Both are kept, because
	// Amazon writes "2K+", the parse is 2000, and the plus sign means the real
	// number is higher. Keeping the string means nobody has to trust the parse.
	BoughtPastMonthN *int64 `json:"bought_past_month_n,omitempty"`

	// rails
	Rails []Rail `json:"rails,omitempty"`

	// related
	SimilarASINs []string  `json:"similar_asins,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`

	// Extra carries every a-state blob the page shipped, verbatim. Three are
	// useful today and the rest are interface state, but the cost of keeping
	// them is a map entry and the cost of dropping one is a redesign that
	// silently removes a field nobody knew was there.
	Extra map[string]json.RawMessage `json:"extra,omitempty"`
	// Envelope says where every field above came from, what was looked for and
	// missed, and which named regions on the page nothing read.
	Envelope Envelope `json:"envelope,omitzero"`
}

// Card is a lightweight hit from a search page, chart, or recommendation rail.
type Card struct {
	Position     int      `json:"position,omitempty"`
	Rank         int      `json:"rank,omitempty"`
	ASIN         string   `json:"asin"`
	Title        string   `json:"title,omitempty"`
	Price        *Money   `json:"price,omitempty"`
	ListPrice    *Money   `json:"list_price,omitempty"`
	Rating       *float64 `json:"rating,omitempty"`
	RatingsCount *int64   `json:"ratings_count,omitempty"`
	Image        string   `json:"image,omitempty"`
	Badge        string   `json:"badge,omitempty"`
	Prime        *bool    `json:"prime,omitempty"`
	// Change is the movers-and-shakers delta, "+412%".
	Change          string `json:"change,omitempty"`
	BoughtPastMonth string `json:"bought_past_month,omitempty"`
	Delivery        string `json:"delivery,omitempty"`
	// Sponsored is never omitempty. An ad and an organic result are different
	// data, and a consumer that cannot tell them apart is holding a corrupted
	// dataset without knowing it.
	Sponsored bool   `json:"sponsored"`
	Kind      string `json:"kind,omitempty"`
	URL       string `json:"url"`
	// Cells are the partition cells this ASIN was found in, on a --all run. A
	// result that appears under three brands is one record naming all three
	// rather than three records that look like three products.
	Cells []string `json:"cells,omitempty"`
	// Envelope says which of the card's slots answered and which were absent.
	// A card that shipped no price and a card whose price slot moved are
	// different facts and this is where they are told apart.
	Envelope Envelope `json:"envelope,omitzero"`
}

// SearchPage is one page of /s results and what the page said about itself.
//
// The counts are kept separately from the cards because they disagree. Measured
// on 2026-08-17, page 3 of "mechanical keyboard" printed a range of 33 to 48 and
// carried 22 cards, because the grid holds sponsored placements and editorial
// rows that the range does not count. Reporting one number would mean choosing
// which of Amazon's two answers to suppress.
type SearchPage struct {
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
	// Page is the page number requested.
	Page int `json:"page,omitempty"`
	// QueryEcho is the query the result bar printed back.
	QueryEcho string `json:"query_echo,omitempty"`
	// From and To are the range of results this page covers.
	From int `json:"result_from,omitempty"`
	To   int `json:"result_to,omitempty"`
	// Total is how many results Amazon claims, and Approx records that it said
	// "over" first. The number moves between pages of one query, so it is an
	// estimate that happens to be printed without an error bar.
	Total  int64 `json:"result_total,omitempty"`
	Approx bool  `json:"result_total_approx,omitempty"`
	// CurrentPage, NextPage and LastPage come from the pagination strip.
	// LastPage is the highest page Amazon will navigate to rather than the
	// number of pages the result total implies.
	CurrentPage int `json:"page_current,omitempty"`
	NextPage    int `json:"page_next,omitempty"`
	LastPage    int `json:"page_last,omitempty"`
	// PageSize is this page's own size, read from its range. It is not a
	// constant: the same query serves sixteen per page unrefined and twenty-four
	// once a department narrows it, so a crawl that computed an offset from the
	// page number would be eight results out from page two onward.
	PageSize int `json:"page_size,omitempty"`
	// Refinements is every group the sidebar offered, on every page, never
	// behind a flag. It is free, it is the vocabulary this package refuses to
	// hardcode, and it is the input to --partition.
	Refinements []RefineGroup `json:"available_refinements,omitempty"`
	// Applied is the refinements the page says are on, read from the sidebar
	// rather than echoed back from the request.
	Applied []Refinement `json:"refinements,omitempty"`
	// Sorts is the sort dropdown as offered, and Sort is the option it marked
	// selected, which is the page's own answer rather than the flag that was
	// sent.
	Sorts []SortOption `json:"sorts,omitempty"`
	Sort  string       `json:"sort,omitempty"`
	// Departments is the search scope dropdown, whose aliases differ by location
	// and are therefore read per marketplace and never compiled in.
	Departments []Department `json:"departments,omitempty"`
	// SponsoredCount is how many of the cards below are advertising.
	SponsoredCount int      `json:"sponsored_count"`
	Cards          []Card   `json:"cards,omitempty"`
	Envelope       Envelope `json:"envelope,omitzero"`
}

// Inverted reports the range running backwards, which is Amazon's own tell that
// the request went past the end of what it will serve.
//
// Page 21 of a query capped at twenty came back reading "321-306 of over 30,000
// results" with no pagination strip and six filler cards. The start is computed
// from the page number, the end is frozen at the ceiling, and the crossover is
// the clearest statement on the page.
func (p SearchPage) Inverted() bool { return p.From > 0 && p.To > 0 && p.To < p.From }

// Exhausted reports whether this page ran past the end of the result set.
//
// Two signals, either of which is enough. The strip stops offering a next page,
// and the result bar starts printing a range that runs backwards: page 21 of a
// query capped at 20 came back reading "321-306 of over 30,000 results" with no
// strip at all. A crawl that only counted pages would have kept going.
func (p SearchPage) Exhausted() bool {
	if p.From > 0 && p.To > 0 && p.To < p.From {
		return true
	}
	return len(p.Cards) == 0
}

// Review is a single product review.
type Review struct {
	ReviewID string `json:"review_id"`
	// Marketplace is the storefront this review was read in, and it is on the
	// row rather than left to be inferred from the ASIN. The same ASIN is a
	// different listing in every storefront, so a review row without it cannot
	// be joined back to the product it is about.
	Marketplace string `json:"marketplace"`
	ASIN        string `json:"asin"`
	// Author is the reviewer. It is almost always unresolved: the profile link
	// goes to /gp/profile/, which robots.txt disallows, so amz has a display
	// name and no identifier to go with it and says exactly that rather than
	// dropping the name or inventing an id.
	Author       *Ref   `json:"author,omitempty"`
	ReviewerID   string `json:"reviewer_id,omitempty"`
	ReviewerName string `json:"reviewer_name"`
	Rating       int    `json:"rating"`
	Title        string `json:"title"`
	Text         string `json:"text"`
	// Date is the line as Amazon wrote it plus the parse when one succeeded. A
	// failed parse must not become a zero time, because a zero time is a real
	// date in January of year 1 and everything downstream will treat it as one.
	Date             *Date             `json:"date,omitempty"`
	Country          string            `json:"country,omitempty"`
	VerifiedPurchase bool              `json:"verified_purchase"`
	HelpfulVotes     int               `json:"helpful_votes"`
	Images           []string          `json:"images,omitempty"`
	VariantAttrs     map[string]string `json:"variant_attrs,omitempty"`
	URL              string            `json:"url"`
	FetchedAt        time.Time         `json:"fetched_at"`
}

// QA is a question-and-answer pair.
type QA struct {
	QAID         string    `json:"qa_id"`
	Marketplace  string    `json:"marketplace"`
	ASIN         string    `json:"asin"`
	Question     string    `json:"question"`
	QuestionBy   string    `json:"question_by,omitempty"`
	Answer       string    `json:"answer"`
	AnswerBy     string    `json:"answer_by,omitempty"`
	HelpfulVotes int       `json:"helpful_votes,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// OfferListing is one buying option from the offer-listing page.
//
// This is the row shape amz offers has emitted since v0.1.0 and it is kept as
// it is. The buy box has its own type, Offer, which is a different thing read
// off a different surface: one winning offer with delivery promises and a
// coupon attached, rather than a flat list of everybody selling the item.
type OfferListing struct {
	Marketplace  string    `json:"marketplace"`
	ASIN         string    `json:"asin"`
	Price        float64   `json:"price"`
	Currency     string    `json:"currency"`
	Shipping     string    `json:"shipping,omitempty"`
	Condition    string    `json:"condition"`
	SellerName   string    `json:"seller_name"`
	SellerID     string    `json:"seller_id,omitempty"`
	SellerRating string    `json:"seller_rating,omitempty"`
	FulfilledBy  string    `json:"fulfilled_by,omitempty"`
	Delivery     string    `json:"delivery,omitempty"`
	IsBuyBox     bool      `json:"is_buybox,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// BestsellerEntry is one ranked item in a chart.
type BestsellerEntry struct {
	Marketplace  string    `json:"marketplace"`
	ListType     string    `json:"list_type"`
	Category     string    `json:"category,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	Rank         int       `json:"rank"`
	ASIN         string    `json:"asin"`
	Title        string    `json:"title"`
	Price        *Money    `json:"price,omitempty"`
	Rating       *float64  `json:"rating,omitempty"`
	RatingsCount *int64    `json:"ratings_count,omitempty"`
	Image        string    `json:"image,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`

	// The movers-and-shakers numbers, which Amazon declares in its list payload
	// and left empty on every capture taken on 2026-08-17.
	SalesRank      int     `json:"sales_rank,omitempty"`
	PriorSalesRank int     `json:"prior_sales_rank,omitempty"`
	PercentChange  float64 `json:"percent_change,omitempty"`

	// RankOnly marks an entry Amazon named in its list but did not draw a tile
	// for. Its rank and ASIN are what the page states; the rest is unknown
	// rather than absent, and a crawl can fill it in from the detail page.
	RankOnly bool `json:"rank_only,omitempty"`

	Envelope Envelope `json:"envelope,omitzero"`
}

// Category is a browse node.
type Category struct {
	NodeID string `json:"node_id"`
	// CanonicalNode is the node Amazon says it served, which is not always the
	// one that was asked for.
	CanonicalNode string `json:"canonical_node,omitempty"`
	Name          string `json:"name"`
	// Slug is the readable segment of the canonical URL, "electronics-store". It
	// is the closest thing a browse page has to a stable name for itself.
	Slug string `json:"slug,omitempty"`
	// Related is every other browse node the page links to, as references with
	// the names Amazon wrote for them. Amazon gives no marker for which of those
	// are children and which are siblings, so these are related nodes and not a
	// tree, and the field is named for what it is.
	//
	// There is no Parent and no Breadcrumb here. A /b page carries neither: both
	// browse captures of 2026-08-17 have no wayfinding breadcrumb region, no
	// a-breadcrumb, and no subnav element, so a browse node states its children
	// and its siblings and never says what it hangs off. The edge upwards comes
	// from a product's rank line instead, which does link its node.
	Related []Ref `json:"related,omitempty"`
	// Shelves is what the page actually is: a stack of merchandised carousels,
	// each with its own heading. TopASINs is every ASIN across all of them, kept
	// flat for the callers that only want identifiers.
	Shelves  []CategoryShelf `json:"shelves,omitempty"`
	TopASINs []string        `json:"top_asins,omitempty"`
	// ItemCount is the tile count before deduplication, which differs from
	// len(TopASINs) when the same product is merchandised on two shelves.
	ItemCount    int       `json:"item_count,omitempty"`
	URL          string    `json:"url"`
	CanonicalURL string    `json:"canonical_url,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
	Envelope     Envelope  `json:"envelope,omitzero"`
}

// CategoryShelf is one carousel of a browse page, with the heading Amazon wrote
// above it. The heading is the page's only statement about why these products
// are together, so it is kept rather than flattened away.
type CategoryShelf struct {
	Widget string   `json:"widget,omitempty"`
	Title  string   `json:"title,omitempty"`
	ASINs  []string `json:"asins,omitempty"`
}

// Brand is a brand storefront.
//
// A storefront is a navigation page rather than a catalogue, so the field that
// matters most here is Nav. Measured on 2026-08-17, Skullcandy's landing page
// held two ASINs in 508 KB and named seven sub-pages, and the products are on
// those. FeaturedASINs is what the landing page itself put in front of a
// visitor, which is a small number by design and is not the brand's range.
type Brand struct {
	Slug string `json:"slug"`
	// PageID is the UUID the storefront URL names this page with. It is the id
	// the nav tree uses, so it is what says which of the seven pages this is.
	PageID       string `json:"page_id,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	LogoURL      string `json:"logo_url,omitempty"`
	BannerURL    string `json:"banner_url,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	// FollowerCount is declared and is not published. Amazon renders a follow
	// button carrying the word Follow and no figure, on every storefront
	// measured, so this stays zero and the envelope records why.
	FollowerCount int      `json:"follower_count,omitempty"`
	FeaturedASINs []string `json:"featured_asins,omitempty"`
	// Nav is the storefront's own page tree, which is how a crawl gets from a
	// landing page to the products.
	Nav []StoreNavPage `json:"nav,omitempty"`
	// Widgets is the type of each payload the page shipped, in document order.
	// It is the page's shape rather than its content, and it is here so a change
	// in what Amazon builds a storefront out of shows up in a diff.
	Widgets   []string  `json:"widgets,omitempty"`
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Envelope  Envelope  `json:"envelope,omitzero"`
}

// SellerFeedback is a seller's rating over one window of time.
//
// Amazon publishes four windows on every rated seller profile and they are not
// redundant. The seller measured on 2026-08-17 was 5.0 across 44 ratings over
// twelve months and 4.8 across 6,365 over its lifetime, so the headline figure
// says the seller is currently good and the lifetime figure says how much
// history that is drawn from. Reporting only one of those is how a seller with
// four ratings and a seller with six thousand end up looking identical.
type SellerFeedback struct {
	// Period is 30d, 90d, 12m or lifetime.
	Period string  `json:"period"`
	Rating float64 `json:"rating,omitempty"`
	Count  int64   `json:"count,omitempty"`
}

// SellerStarShare is one row of the star histogram, "5 star 87%".
type SellerStarShare struct {
	Stars int `json:"stars"`
	Pct   int `json:"pct"`
}

// SellerReview is one buyer's written feedback about a seller.
//
// This is not a product review. It is feedback on the transaction, which is why
// it has no title, no ASIN and no helpful vote count, and why the text on a real
// page is as short as "Excellent" or a row of asterisks where the buyer left
// stars and typed nothing.
type SellerReview struct {
	Rating float64 `json:"rating,omitempty"`
	Text   string  `json:"text,omitempty"`
	// Rater is the buyer's display name, which is "Amazon Customer" for most of
	// them and a company name when the buyer is a business.
	Rater string `json:"rater,omitempty"`
	// Date is left as Amazon printed it, "August 13, 2026", because parsing it
	// into a time would put a timezone on a date that does not have one.
	Date string `json:"date,omitempty"`
}

// Seller is a third-party seller profile.
//
// The feedback fields are the reason this struct is worth reading carefully. Not
// every seller has a rating: an Amazon owned seller publishes no feedback at
// all, and a zero here would mean the page carried no figure rather than a
// seller with no goodwill. Which of the two it is is in the envelope, where the
// fields are named and the miss is recorded with its reason.
type Seller struct {
	SellerID string `json:"seller_id"`
	Name     string `json:"name"`
	// BusinessName is the legal entity behind the display name, from the
	// detailed seller information block. The two are usually different, and the
	// legal one is what says who is actually selling: the storefront trading as
	// "SIKAI CASE" is Hangzhou Hang Kai Technology Co.,Ltd.
	BusinessName    string `json:"business_name,omitempty"`
	BusinessAddress string `json:"business_address,omitempty"`
	About           string `json:"about,omitempty"`
	LogoURL         string `json:"logo_url,omitempty"`
	// StorefrontURL is the link to this seller's own listings, which is how a
	// crawl gets from a seller to their catalogue.
	StorefrontURL  string `json:"storefront_url,omitempty"`
	ShippingPolicy string `json:"shipping_policy,omitempty"`
	ReturnPolicy   string `json:"return_policy,omitempty"`
	// Rating and RatingCount are the twelve month window, which is the one
	// Amazon puts in the header and the one a buyer is shown.
	Rating      float64 `json:"rating,omitempty"`
	RatingCount int64   `json:"rating_count,omitempty"`
	// PositivePct is the share of the twelve month ratings Amazon counts as
	// positive, which is a different statistic from the star average and is
	// stated separately on the page.
	PositivePct float64          `json:"positive_pct,omitempty"`
	Feedback    []SellerFeedback `json:"feedback,omitempty"`
	// RatingHistogram is the five star breakdown, highest first.
	RatingHistogram []SellerStarShare `json:"rating_histogram,omitempty"`
	// Reviews is the first page of written feedback, which is the five rows
	// Amazon serves in the HTML. The rest of the pages come from an AJAX call to
	// /sp/ajax/feedback and are not on this page at any depth, so a caller with
	// five reviews and a rating count of 6,365 is looking at a first page rather
	// than at a seller whose buyers rarely write anything.
	Reviews   []SellerReview `json:"reviews,omitempty"`
	URL       string         `json:"url"`
	FetchedAt time.Time      `json:"fetched_at"`
	Envelope  Envelope       `json:"envelope,omitzero"`
}

// Author is an Author Central page.
//
// Books carries whole product records rather than ASINs because the page's grid
// payload publishes whole records, with bindings, contributors, series position
// and other editions of the same work. TotalBooks is what Amazon says the
// bibliography holds, which on the measured page was 135 against 70 shipped, so
// a consumer can tell a first page from a complete one.
type Author struct {
	Slug     string `json:"slug"`
	PageID   string `json:"page_id,omitempty"`
	Name     string `json:"name"`
	Bio      string `json:"bio,omitempty"`
	PhotoURL string `json:"photo_url,omitempty"`
	Website  string `json:"website,omitempty"`
	// AboutURL is the author's full profile page, where the biography the tile
	// truncates is published whole.
	AboutURL  string         `json:"about_url,omitempty"`
	Books     []StoreProduct `json:"books,omitempty"`
	BookASINs []string       `json:"book_asins,omitempty"`
	// TotalBooks is the bibliography size Amazon states, which is larger than
	// len(Books) whenever the grid paginates.
	TotalBooks int `json:"total_books,omitempty"`
	// SortOptions and Languages are the grid's own filter vocabulary, and they
	// are the parameters a caller needs to page through the rest of Books.
	SortOptions   []string  `json:"sort_options,omitempty"`
	Languages     []string  `json:"languages,omitempty"`
	FollowerCount int       `json:"follower_count,omitempty"`
	URL           string    `json:"url"`
	FetchedAt     time.Time `json:"fetched_at"`
	Envelope      Envelope  `json:"envelope,omitzero"`
}

// Deal is one entry from the deals grid.
type Deal struct {
	ASIN string `json:"asin"`
	// DealID is the promotion, amzn1.deal.1e7a7bea. Amazon puts it in the same
	// attribute as the ASIN, and it is the identifier that will be gone when the
	// promotion ends while the ASIN stays.
	DealID    string `json:"deal_id,omitempty"`
	Title     string `json:"title"`
	DealPrice *Money `json:"deal_price,omitempty"`
	ListPrice *Money `json:"list_price,omitempty"`
	// ListLabel is what Amazon called the struck through price. It reads "List",
	// "List Price" or "Typical", and a typical price is a computed average
	// rather than a manufacturer's list price, so the label travels with the
	// number.
	ListLabel   string `json:"list_label,omitempty"`
	DiscountPct int    `json:"discount_pct,omitempty"`
	Badge       string `json:"badge,omitempty"`
	// EndsSoon reports that Amazon drew a countdown on the tile. The clock is
	// filled in by script, so the served HTML states that the deal is ending and
	// not when.
	EndsSoon  bool      `json:"ends_soon,omitempty"`
	URL       string    `json:"url"`
	Image     string    `json:"image,omitempty"`
	Shelf     string    `json:"shelf,omitempty"`
	Position  int       `json:"position,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	Envelope  Envelope  `json:"envelope,omitzero"`
}

// QueueItem is a row from the crawl queue.
type QueueItem struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	Entity   string `json:"entity"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
	// Depth is how many rail hops from a seed this item was reached at, which
	// is what --rail-depth bounds. A seed is 0.
	Depth int `json:"depth,omitempty"`
	// Attempts counts how many times this item has been claimed. A crawl that
	// is killed mid-item leaves it claimed, so the count is the difference
	// between an item that is hard and an item nobody has tried.
	Attempts int `json:"attempts,omitempty"`
}

func dedup(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
