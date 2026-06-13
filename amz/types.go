// Package amz is a read-only client library for amazon.com: it fetches public
// pages, detects the bot wall, and normalizes each surface into a rich record.
package amz

import (
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
type ProductRank struct {
	Rank     int    `json:"rank"`
	Category string `json:"category"`
}

// Product is a normalized amazon.com product detail page.
type Product struct {
	ASIN            string            `json:"asin"`
	Title           string            `json:"title"`
	Brand           string            `json:"brand"`
	BrandID         string            `json:"brand_id,omitempty"`
	Price           float64           `json:"price"`
	Currency        string            `json:"currency"`
	ListPrice       float64           `json:"list_price,omitempty"`
	Savings         float64           `json:"savings,omitempty"`
	SavingsPct      int               `json:"savings_pct,omitempty"`
	Coupon          string            `json:"coupon,omitempty"`
	Rating          float64           `json:"rating"`
	RatingsCount    int64             `json:"ratings_count"`
	ReviewsCount    int64             `json:"reviews_count,omitempty"`
	AnsweredQs      int               `json:"answered_qs,omitempty"`
	BoughtPastMonth string            `json:"bought_past_month,omitempty"`
	Availability    string            `json:"availability"`
	InStock         bool              `json:"in_stock"`
	Description     string            `json:"description,omitempty"`
	BulletPoints    []string          `json:"bullet_points,omitempty"`
	Specs           map[string]string `json:"specs,omitempty"`
	Images          []string          `json:"images,omitempty"`
	Videos          []string          `json:"videos,omitempty"`
	CategoryPath    []string          `json:"category_path,omitempty"`
	BrowseNodeIDs   []string          `json:"browse_node_ids,omitempty"`
	SellerID        string            `json:"seller_id,omitempty"`
	SellerName      string            `json:"seller_name,omitempty"`
	SoldBy          string            `json:"sold_by,omitempty"`
	ShipsFrom       string            `json:"ships_from,omitempty"`
	FulfilledBy     string            `json:"fulfilled_by,omitempty"`
	VariantASINs    []string          `json:"variant_asins,omitempty"`
	ParentASIN      string            `json:"parent_asin,omitempty"`
	SimilarASINs    []string          `json:"similar_asins,omitempty"`
	Rank            int               `json:"rank,omitempty"`
	RankCategory    string            `json:"rank_category,omitempty"`
	Ranks           []ProductRank     `json:"ranks,omitempty"`
	Marketplace     string            `json:"marketplace"`
	URL             string            `json:"url"`
	FetchedAt       time.Time         `json:"fetched_at"`
}

// Card is a lightweight hit from a search page, chart, or recommendation rail.
type Card struct {
	Position        int     `json:"position,omitempty"`
	Rank            int     `json:"rank,omitempty"`
	ASIN            string  `json:"asin"`
	Title           string  `json:"title"`
	Price           float64 `json:"price"`
	ListPrice       float64 `json:"list_price,omitempty"`
	Currency        string  `json:"currency,omitempty"`
	Rating          float64 `json:"rating,omitempty"`
	RatingsCount    int64   `json:"ratings_count,omitempty"`
	Image           string  `json:"image,omitempty"`
	Badge           string  `json:"badge,omitempty"`
	Prime           bool    `json:"prime,omitempty"`
	BoughtPastMonth string  `json:"bought_past_month,omitempty"`
	Sponsored       bool    `json:"sponsored,omitempty"`
	Kind            string  `json:"kind,omitempty"`
	URL             string  `json:"url"`
}

// Review is a single product review.
type Review struct {
	ReviewID         string            `json:"review_id"`
	ASIN             string            `json:"asin"`
	ReviewerID       string            `json:"reviewer_id,omitempty"`
	ReviewerName     string            `json:"reviewer_name"`
	Rating           int               `json:"rating"`
	Title            string            `json:"title"`
	Text             string            `json:"text"`
	Date             string            `json:"date,omitempty"`
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
	ASIN         string    `json:"asin"`
	Question     string    `json:"question"`
	QuestionBy   string    `json:"question_by,omitempty"`
	Answer       string    `json:"answer"`
	AnswerBy     string    `json:"answer_by,omitempty"`
	HelpfulVotes int       `json:"helpful_votes,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// Offer is one buying option from the offer-listing page.
type Offer struct {
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
	ListType     string    `json:"list_type"`
	Category     string    `json:"category,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	Rank         int       `json:"rank"`
	ASIN         string    `json:"asin"`
	Title        string    `json:"title"`
	Price        float64   `json:"price"`
	Currency     string    `json:"currency,omitempty"`
	Rating       float64   `json:"rating,omitempty"`
	RatingsCount int64     `json:"ratings_count,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// Category is a browse node.
type Category struct {
	NodeID       string    `json:"node_id"`
	Name         string    `json:"name"`
	ParentNodeID string    `json:"parent_node_id,omitempty"`
	Breadcrumb   []string  `json:"breadcrumb,omitempty"`
	ChildNodeIDs []string  `json:"child_node_ids,omitempty"`
	TopASINs     []string  `json:"top_asins,omitempty"`
	URL          string    `json:"url"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// Brand is a brand storefront.
type Brand struct {
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	LogoURL       string    `json:"logo_url,omitempty"`
	BannerURL     string    `json:"banner_url,omitempty"`
	FollowerCount int       `json:"follower_count,omitempty"`
	FeaturedASINs []string  `json:"featured_asins,omitempty"`
	URL           string    `json:"url"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// Seller is a third-party seller profile.
type Seller struct {
	SellerID    string    `json:"seller_id"`
	Name        string    `json:"name"`
	Rating      string    `json:"rating,omitempty"`
	RatingCount int       `json:"rating_count,omitempty"`
	PositivePct float64   `json:"positive_pct,omitempty"`
	NeutralPct  float64   `json:"neutral_pct,omitempty"`
	NegativePct float64   `json:"negative_pct,omitempty"`
	URL         string    `json:"url"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// Author is an Author Central page.
type Author struct {
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Bio           string    `json:"bio,omitempty"`
	PhotoURL      string    `json:"photo_url,omitempty"`
	Website       string    `json:"website,omitempty"`
	BookASINs     []string  `json:"book_asins,omitempty"`
	FollowerCount int       `json:"follower_count,omitempty"`
	URL           string    `json:"url"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// Deal is one entry from the deals grid.
type Deal struct {
	ASIN        string    `json:"asin"`
	Title       string    `json:"title"`
	DealPrice   float64   `json:"deal_price"`
	ListPrice   float64   `json:"list_price,omitempty"`
	DiscountPct int       `json:"discount_pct,omitempty"`
	Badge       string    `json:"badge,omitempty"`
	Currency    string    `json:"currency,omitempty"`
	URL         string    `json:"url"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// QueueItem is a row from the crawl queue.
type QueueItem struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	Entity   string `json:"entity"`
	Priority int    `json:"priority"`
	Status   string `json:"status"`
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
