package amz

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ProductURL builds the canonical detail URL for an ASIN in this marketplace.
func (c *Client) ProductURL(asin string) string {
	return c.BaseURL() + "/dp/" + asin
}

// ResolveProductURL turns an ASIN or any amazon URL into a canonical detail URL.
func (c *Client) ResolveProductURL(asinOrURL string) (asin, url string) {
	asin = ExtractASIN(asinOrURL)
	if IsURL(asinOrURL) {
		if asin == "" {
			return "", asinOrURL
		}
		return asin, asinOrURL
	}
	if asin == "" {
		return "", ""
	}
	return asin, c.ProductURL(asin)
}

// FetchProduct fetches and normalizes one product detail page.
func (c *Client) FetchProduct(ctx context.Context, asinOrURL string) (Product, error) {
	asin, url := c.ResolveProductURL(asinOrURL)
	if url == "" {
		return Product{}, ErrNotFound
	}
	body, err := c.Get(ctx, url, 6*time.Hour)
	if err != nil {
		return Product{}, err
	}
	return c.parseProduct(asin, url, body)
}

// availabilityOutOfStock reports whether an availability line means "can't buy".
func availabilityOutOfStock(s string) bool {
	l := strings.ToLower(s)
	for _, neg := range []string{"unavailable", "out of stock", "not in stock", "sold out", "no longer available"} {
		if strings.Contains(l, neg) {
			return true
		}
	}
	return false
}

// cleanRankCategory drops a trailing "(See Top 100 ...)" clause.
func cleanRankCategory(s string) string {
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	return collapseSpace(s)
}

// interfaceState names the a-state blobs that are known to be interface state
// rather than data. They are still carried, per notes/Spec/3007/02_extraction.md
// section 4: a blob that is interface state today is a data payload after the
// next redesign, and the cost of keeping it is a map entry.
var interfaceState = map[string]bool{
	"a-wlab-states": true, "popoverState": true, "SpViewabilityConfigState": true,
	"acState": true, "atc-page-state": true, "cr-weblab-state": true,
	"detail-page-device-type": true, "dp-twister-csm": true,
}

// InterfaceState reports whether an a-state blob is known interface state rather
// than page data.
func InterfaceState(key string) bool { return interfaceState[key] }

// parseProduct reads a detail page through the ladder.
//
// Three passes in rung order, and the order is the point: a value from a named
// region is not overwritten by a value from a payload or a selector, because set
// keeps the first source and records the second as a disagreement.
func (c *Client) parseProduct(asin, url string, body []byte) (Product, error) {
	doc, err := ParseDoc(FamilyProduct, body)
	if err != nil {
		return Product{}, err
	}
	e := NewExtractor(doc)

	// Rung 1 and the declared rung 3 and 4 fields.
	e.Run(productFields(c.BaseURL(), asin))

	// Rung 2: the payloads, read from the raw body by byte offset.
	ib := c.readImageBlock(e, body)
	tw := c.readTwister(e, body)
	states := ReadAStates(body)

	p := Product{
		ASIN:        asin,
		Marketplace: c.mkt.Slug,
		URL:         url,
		FetchedAt:   time.Now().UTC(),
	}
	if p.ASIN == "" && ib != nil {
		p.ASIN = ib.ASIN
	}

	p.Title = e.Str("title")
	p.Brand = e.Str("brand")
	p.BrandURL = e.Str("brand_url")
	if m := nodeIDRe.FindStringSubmatch(p.BrandURL); m != nil {
		p.BrandID = m[1]
	}
	p.Price = e.Float("price")
	p.Currency = e.Str("currency")
	if p.Currency == "" {
		p.Currency = c.mkt.Currency
	}
	p.ListPrice = e.Float("list_price")
	if p.ListPrice > p.Price && p.Price > 0 {
		p.Savings = round2(p.ListPrice - p.Price)
		p.SavingsPct = int((1 - p.Price/p.ListPrice) * 100)
	}
	p.Coupon = e.Str("coupon")
	p.Rating = e.Float("rating")
	p.RatingsCount = e.Int("ratings_count")
	p.AnsweredQs = int(e.Int("answered_qs"))
	p.BoughtPastMonth = e.Str("bought_past_month")
	p.Availability = e.Str("availability")
	p.InStock = p.Availability != "" && !availabilityOutOfStock(p.Availability)
	p.Description = e.Str("description")
	p.BulletPoints = e.Strings("bullet_points")
	p.CategoryPath = e.Strings("category_path")
	p.BrowseNodeIDs = e.Strings("browse_node_ids")
	p.SoldBy = e.Str("sold_by")
	p.SellerName = p.SoldBy
	p.SellerID = e.Str("seller_id")
	p.ShipsFrom = e.Str("ships_from")
	p.FulfilledBy = p.ShipsFrom
	p.Returns = e.Str("returns")
	p.Condition = e.Str("condition")
	p.SimilarASINs = e.Strings("similar_asins")
	p.ParentASIN = e.Str("parent_asin")

	if v, ok := e.Value("specs"); ok {
		p.Specs, _ = v.(map[string]string)
	}
	if v, ok := e.Value("ranks"); ok {
		p.Ranks, _ = v.([]ProductRank)
		if len(p.Ranks) > 0 {
			p.Rank = p.Ranks[0].Rank
			p.RankCategory = p.Ranks[0].Category
		}
	}

	if ib != nil {
		p.ImageSet = ib.Images
		p.Videos = ib.Videos
		p.ColorToASIN = ib.ColorToASIN
		for _, img := range ib.Images {
			if u := img.URL(); u != "" {
				p.Images = append(p.Images, normalizeImageURL(u))
			}
		}
		p.Images = dedup(p.Images)
	}
	if tw != nil {
		p.Variants = tw
		p.VariantASINs = tw.ASINs()
		if tw.ParentASIN != "" {
			p.ParentASIN = tw.ParentASIN
		}
		if tw.Total > 0 && tw.Shipped() < tw.Total {
			// The page ships the variants near the current selection, not all of
			// them. Recording both numbers makes an incomplete record visible
			// rather than implied.
			e.miss("variant_asins", "the page shipped "+strconv.Itoa(tw.Shipped())+
				" of the "+strconv.Itoa(tw.Total)+" variations num_total_variations claims")
		}
	}

	if len(states) > 0 {
		p.Extra = map[string]json.RawMessage{}
		data := 0
		for _, st := range states {
			if st.Data == nil {
				continue
			}
			p.Extra[st.Key] = st.Data
			if !InterfaceState(st.Key) {
				data++
			}
		}
		if data > 0 {
			e.set("extra", data, LevelPayload, "a-state")
		}
	}

	p.Envelope = e.Envelope()
	p.Envelope.RetrievedAt = p.FetchedAt.Format(time.RFC3339)
	p.Envelope.AgentMap = doc.AgentMap()

	if p.Title == "" && p.Price == 0 && p.Rating == 0 {
		return p, ErrNotFound
	}
	return p, nil
}

func (c *Client) readImageBlock(e *Extractor, body []byte) *ImageBlock {
	if !HasPayload(body, PayloadImageBlock) {
		e.miss("images", "payload "+PayloadImageBlock+" not present on this page")
		return nil
	}
	ib, err := ReadImageBlock(body)
	if err != nil {
		e.miss("images", "payload "+PayloadImageBlock+" did not parse: "+err.Error())
		return nil
	}
	if len(ib.Images) > 0 {
		e.set("images", len(ib.Images), LevelPayload, PayloadImageBlock)
	}
	if len(ib.Videos) > 0 {
		e.set("videos", len(ib.Videos), LevelPayload, PayloadImageBlock)
	}
	return ib
}

func (c *Client) readTwister(e *Extractor, body []byte) *Twister {
	if !HasPayload(body, PayloadTwister) {
		// Not every product has variants. A single-variant product genuinely has
		// no twister, so this is a miss and not an error.
		e.miss("variant_asins", "payload "+PayloadTwister+" not present, so this product has no variations")
		return nil
	}
	tw, err := ReadTwister(body)
	if err != nil {
		e.miss("variant_asins", "payload "+PayloadTwister+" did not parse: "+err.Error())
		return nil
	}
	if n := tw.Shipped(); n > 0 {
		e.set("variant_asins", n, LevelPayload, PayloadTwister)
	}
	if tw.Total > 0 {
		e.set("variant_total", tw.Total, LevelPayload, PayloadTwister)
	}
	return tw
}
