package amz

import (
	"context"
	"encoding/json"
	"math"
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
	body, src, err := c.GetSource(ctx, url, 6*time.Hour)
	if err != nil {
		return Product{}, err
	}
	p, err := c.parseProduct(asin, url, body)
	if err != nil {
		return p, err
	}
	c.record(ctx, &p.Envelope, src)
	return p, nil
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
	//
	// The unread pass is deferred to the end of this function rather than run
	// inside Run, because the recommendation rails are discovered by pattern
	// while parsing and are not in the registry. Marking unread first would put
	// every rail that was read into the worklist of regions nobody reads.
	fields := productFields(c.BaseURL(), asin)
	e.RunFields(fields)

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
	p.Brand = NewRef(RefBrand, c.mkt.Slug, brandIDIn(e.Str("brand_url")), e.Str("brand"), e.Str("brand_url"))
	p.Byline = e.Str("brand")
	p.Rating = f64OrNil(e.Float("rating"))
	p.RatingsCount = i64OrNil(e.Int("ratings_count"))
	p.BoughtPastMonth = e.Str("bought_past_month")
	p.BoughtPastMonthN = i64OrNil(parseCount(p.BoughtPastMonth))
	p.Description = e.Str("description")
	p.Bullets = e.Strings("bullet_points")
	p.SimilarASINs = e.Strings("similar_asins")
	p.ParentASIN = e.Str("parent_asin")

	if v, ok := e.Value("distribution"); ok {
		if pct, ok := v.([5]int); ok {
			p.Distribution = NewDistribution(pct, p.RatingsCount, provOf(e, "distribution"))
		}
	}

	if p.Rating != nil || p.RatingsCount != nil {
		// The page states how many people rated the product and shows the
		// histogram, and it holds none of the reviews themselves. Both surfaces
		// that do redirect to a sign-in, so this is a wall and not a gap in the
		// parser, and the record says which of the two it is.
		e.missSurface("reviews",
			"amazon requires a sign-in for the review corpus, and the detail page carries the rating and the histogram only",
			[]string{"/product-reviews/", "/portal/customer-reviews/"},
			"")
	}

	if n := e.Int("answered_qs"); n > 0 {
		// The question count is all /ask gives without a login, so the
		// connection is honest about having loaded none of them.
		p.Questions = NewConn(0, &n, c.BaseURL()+"/ask/questions/asin/"+asin)
	}

	p.Offer = c.buildOffer(e)

	if v, ok := e.Value("specs"); ok {
		p.Details, _ = v.(map[string]string)
		p.ISBN10 = detailOf(p.Details, "ISBN-10")
		p.ISBN13 = detailOf(p.Details, "ISBN-13")
		p.ModelNumber = detailOf(p.Details, "Item model number", "Part Number")
		p.UPC = detailOf(p.Details, "UPC")
		p.EAN = detailOf(p.Details, "EAN")
		p.Manufacturer = detailOf(p.Details, "Manufacturer")
	}
	if v, ok := e.Value("ranks"); ok {
		p.Ranks, _ = v.([]Rank)
		resolveRankNodes(p.Ranks, c.mkt.Slug, c.BaseURL())
	}
	p.Rails = readRails(e, c.BaseURL())
	p.Breadcrumb = breadcrumbRefs(c.mkt.Slug, c.BaseURL(), e.Strings("category_path"), e.Strings("browse_node_ids"))

	if ib != nil {
		p.Images = ib.Images
		p.Videos = ib.Videos
		for _, img := range ib.Images {
			if u := img.URL(); u != "" {
				p.ImageURLs = append(p.ImageURLs, normalizeImageURL(u))
			}
		}
		p.ImageURLs = dedup(p.ImageURLs)
	}
	if tw != nil {
		p.Variation = tw.Variation()
		if tw.ParentASIN != "" {
			p.ParentASIN = tw.ParentASIN
		}
		if tw.Total > 0 && tw.Shipped() < tw.Total {
			// The page ships the variants near the current selection, not all of
			// them. Recording both numbers makes an incomplete record visible
			// rather than implied.
			e.missPartial("variant_asins", tw.Shipped(), int64(tw.Total),
				"the page ships only the variations near the current selection",
				"amz product on each sibling asin")
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

	claimed := claimedRegions(fields)
	for _, r := range p.Rails {
		claimed[r.Region] = true
	}
	e.MarkUnread(claimed)

	p.Envelope = e.Envelope()
	p.Envelope.RetrievedAt = p.FetchedAt
	p.Envelope.AgentMap = doc.AgentMap()

	if p.Title == "" && p.Offer == nil && p.Rating == nil {
		return p, ErrNotFound
	}
	return p, nil
}

// buildOffer assembles the buy box out of the fields the ladder already read.
//
// It returns nil rather than an empty struct when the page had no buy box at
// all, which happens on an unavailable listing and on the soft 404. A record
// with no offer and a record with an offer that quotes no price are different
// readings of a page and this is the line between them.
func (c *Client) buildOffer(e *Extractor) *Offer {
	o := Offer{
		Availability: e.Str("availability"),
		Returns:      e.Str("returns"),
		Condition:    e.Str("condition"),
	}
	if p, ok := e.Prov("price"); ok {
		o.Via = p.Via
	}
	o.Price = money(e, "price", c.mkt)
	o.ListPrice = money(e, "list_price", c.mkt)
	if s := o.ListPrice.Sub(o.Price); s != nil && s.Amount.Sign() > 0 {
		o.Savings = s
		pct := int(math.Round((1 - o.Price.Float()/o.ListPrice.Float()) * 100))
		o.SavingsPct = &pct
	}
	if d := e.Str("coupon"); d != "" {
		o.Coupon = &Coupon{Display: d, Via: provOf(e, "coupon")}
		if n := parseInt(d); n > 0 && strings.Contains(d, "%") {
			pct := int(n)
			o.Coupon.Percent = &pct
		}
	}
	if o.Availability != "" {
		in := !availabilityOutOfStock(o.Availability)
		o.InStock = &in
	}
	o.SoldBy = NewRef(RefSeller, c.mkt.Slug, e.Str("seller_id"), e.Str("sold_by"), "")
	if o.SoldBy != nil && o.SoldBy.ID != "" {
		o.SoldBy.URL = c.SellerURL(o.SoldBy.ID)
	}
	o.ShipsFrom = NamedRef(RefSeller, e.Str("ships_from"))
	if o.Price == nil && o.Availability == "" && o.SoldBy == nil {
		return nil
	}
	return &o
}

// brandIDIn reads the browse node out of a brand storefront URL, which is the
// only identifier a byline carries.
func brandIDIn(url string) string {
	if m := nodeIDRe.FindStringSubmatch(url); m != nil {
		return m[1]
	}
	return ""
}

// detailOf reads the first of several spellings out of the detail table. Amazon
// labels the same fact "Item model number" in one category and "Part Number" in
// the next, and neither spelling is more correct than the other.
func detailOf(details map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := details[k]; v != "" {
			return v
		}
	}
	return ""
}

// breadcrumbRefs pairs the breadcrumb names with the node ids behind them.
//
// The two lists come from the same region and normally have the same length,
// but a breadcrumb whose last crumb is plain text rather than a link produces
// one more name than id. Pairing by index and leaving the surplus names as
// unresolved refs keeps the trail intact rather than truncating it.
func breadcrumbRefs(mkt, base string, names, ids []string) []Ref {
	if len(names) == 0 {
		return nil
	}
	out := make([]Ref, 0, len(names))
	for i, n := range names {
		if i < len(ids) && ids[i] != "" {
			if r := NewRef(RefNode, mkt, ids[i], n, base+"/b?node="+ids[i]); r != nil {
				out = append(out, *r)
				continue
			}
		}
		if r := NamedRef(RefNode, n); r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func provOf(e *Extractor, field string) string {
	if p, ok := e.Prov(field); ok {
		return p.Via
	}
	return ""
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
