package amz

import (
	"encoding/json"
	"time"
)

// The v0.2.1 product record, kept for one version behind --flat.
//
// The nested record in types.go is a better description of a detail page and it
// is also a breaking change for everything that reads a price out of amz product
// with jq. Shipping both shapes for one release costs this file and buys every
// script a version to move in, which is a trade worth making once.
//
// This is deprecated on arrival. It goes in v0.4.0, the flag prints a notice
// saying so, and nothing new is added here.

// FlatProduct is the product record as v0.2.1 emitted it.
//
// Deprecated: use Product. This shape loses the distinction between a field that
// is absent and a field that is zero, which is most of the point of the nested
// record, and it cannot carry the buy box delivery promises, the rating
// histogram, the rails or the variation dimensions at all.
type FlatProduct struct {
	ASIN            string            `json:"asin"`
	Title           string            `json:"title"`
	Brand           string            `json:"brand"`
	BrandID         string            `json:"brand_id,omitempty"`
	BrandURL        string            `json:"brand_url,omitempty"`
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
	ImageSet        []Image           `json:"image_set,omitempty"`
	Videos          []Video           `json:"videos,omitempty"`
	CategoryPath    []string          `json:"category_path,omitempty"`
	BrowseNodeIDs   []string          `json:"browse_node_ids,omitempty"`
	SellerID        string            `json:"seller_id,omitempty"`
	SellerName      string            `json:"seller_name,omitempty"`
	SoldBy          string            `json:"sold_by,omitempty"`
	ShipsFrom       string            `json:"ships_from,omitempty"`
	FulfilledBy     string            `json:"fulfilled_by,omitempty"`
	Returns         string            `json:"returns,omitempty"`
	Condition       string            `json:"condition,omitempty"`
	VariantASINs    []string          `json:"variant_asins,omitempty"`
	ColorToASIN     map[string]string `json:"color_to_asin,omitempty"`
	ParentASIN      string            `json:"parent_asin,omitempty"`
	SimilarASINs    []string          `json:"similar_asins,omitempty"`
	Rank            int               `json:"rank,omitempty"`
	RankCategory    string            `json:"rank_category,omitempty"`
	Ranks           []ProductRank     `json:"ranks,omitempty"`
	Marketplace     string            `json:"marketplace"`
	URL             string            `json:"url"`
	FetchedAt       time.Time         `json:"fetched_at"`

	Extra    map[string]json.RawMessage `json:"extra,omitempty"`
	Envelope Envelope                   `json:"envelope,omitzero"`
}

// Flat projects the nested record down onto the v0.2.1 shape.
//
// Everything the flat shape cannot express is dropped rather than approximated.
// The rails, the delivery promises, the rating histogram, the variation
// dimensions and the per unit price have no slot here, so a caller on --flat is
// getting a smaller record and should know it. What is not dropped is the
// envelope, which travels unchanged, so even a flat record can still say where
// each of these fields came from.
func (p Product) Flat() FlatProduct {
	f := FlatProduct{
		ASIN:            p.ASIN,
		Title:           p.Title,
		BoughtPastMonth: p.BoughtPastMonth,
		Description:     p.Description,
		BulletPoints:    p.Bullets,
		Specs:           p.Details,
		Images:          p.ImageURLs,
		ImageSet:        p.Images,
		Videos:          p.Videos,
		ParentASIN:      p.ParentASIN,
		SimilarASINs:    p.SimilarASINs,
		Marketplace:     p.Marketplace,
		URL:             p.URL,
		FetchedAt:       p.FetchedAt,
		Extra:           p.Extra,
		Envelope:        p.Envelope,
	}
	if m, ok := LookupMarketplace(p.Marketplace); ok {
		f.Currency = m.Currency
	}

	if p.Brand != nil {
		f.Brand = p.Brand.Name
		f.BrandID = p.Brand.ID
		f.BrandURL = p.Brand.URL
	}
	if p.Rating != nil {
		f.Rating = *p.Rating
	}
	if p.RatingsCount != nil {
		f.RatingsCount = *p.RatingsCount
	}
	if p.ReviewsCount != nil {
		f.ReviewsCount = *p.ReviewsCount
	}
	if p.Questions != nil {
		f.AnsweredQs = p.Questions.Loaded
		if p.Questions.TotalCount != nil {
			f.AnsweredQs = int(*p.Questions.TotalCount)
		}
	}
	if o := p.Offer; o != nil {
		f.Price = o.Price.Float()
		f.ListPrice = o.ListPrice.Float()
		f.Savings = o.Savings.Float()
		if o.SavingsPct != nil {
			f.SavingsPct = *o.SavingsPct
		}
		if c := o.Price.Cur(); c != "" {
			f.Currency = c
		}
		if o.Coupon != nil {
			f.Coupon = o.Coupon.Display
		}
		f.Availability = o.Availability
		if o.InStock != nil {
			f.InStock = *o.InStock
		}
		f.Returns = o.Returns
		f.Condition = o.Condition
		if o.SoldBy != nil {
			f.SoldBy = o.SoldBy.Name
			f.SellerName = o.SoldBy.Name
			f.SellerID = o.SoldBy.ID
		}
		if o.ShipsFrom != nil {
			f.ShipsFrom = o.ShipsFrom.Name
			f.FulfilledBy = o.ShipsFrom.Name
		}
	}
	for _, b := range p.Breadcrumb {
		f.CategoryPath = append(f.CategoryPath, b.Name)
		if b.ID != "" {
			f.BrowseNodeIDs = append(f.BrowseNodeIDs, b.ID)
		}
	}
	for _, r := range p.Ranks {
		f.Ranks = append(f.Ranks, ProductRank{Rank: r.Rank, Category: r.Category})
	}
	if len(f.Ranks) > 0 {
		f.Rank = f.Ranks[0].Rank
		f.RankCategory = f.Ranks[0].Category
	}
	if v := p.Variation; v != nil {
		f.ColorToASIN = map[string]string{}
		for _, s := range v.Siblings {
			f.VariantASINs = append(f.VariantASINs, s.ASIN)
			if c := s.Values["color_name"]; c != "" {
				f.ColorToASIN[c] = s.ASIN
			}
		}
		if len(f.ColorToASIN) == 0 {
			f.ColorToASIN = nil
		}
	}
	return f
}
