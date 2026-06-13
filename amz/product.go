package amz

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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

var rankRe = regexp.MustCompile(`#([\d,]+)\s+in\s+([^(#\n]+)`)

// stripParen drops a trailing "(See Top 100 ...)" clause from a rank category.
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

func cleanRankCategory(s string) string {
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	return collapseSpace(s)
}

func (c *Client) parseProduct(asin, url string, body []byte) (Product, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Product{}, err
	}
	p := Product{
		ASIN:        asin,
		Currency:    c.mkt.Currency,
		Marketplace: c.mkt.Slug,
		URL:         url,
		FetchedAt:   time.Now().UTC(),
		Specs:       map[string]string{},
	}

	// Pass 1: JSON-LD.
	applyProductJSONLD(doc, &p)

	// Pass 2: HTML selectors (fill what JSON-LD missed).
	if p.Title == "" {
		p.Title = collapseSpace(text(doc, "#productTitle"))
	}
	if p.Brand == "" {
		b := collapseSpace(text(doc, "#bylineInfo"))
		b = strings.TrimPrefix(b, "Visit the ")
		b = strings.TrimPrefix(b, "Brand: ")
		b = strings.TrimSuffix(b, " Store")
		p.Brand = b
	}
	if href := attr(doc, "#bylineInfo", "href"); href != "" {
		if m := regexp.MustCompile(`node=(\d+)`).FindStringSubmatch(href); m != nil {
			p.BrandID = m[1]
		}
	}
	if p.Price == 0 {
		ps := firstNonEmptyText(doc, "#corePrice_feature_div .a-offscreen", ".a-price .a-offscreen", "#priceblock_ourprice", "#priceblock_dealprice")
		p.Price, _ = ParsePrice(ps)
	}
	if p.ListPrice == 0 {
		lp := firstNonEmptyText(doc, ".basisPrice .a-offscreen", "span.a-price.a-text-price .a-offscreen", "#listPrice")
		p.ListPrice, _ = ParsePrice(lp)
	}
	// Savings derive from list vs current price.
	if p.ListPrice > p.Price && p.Price > 0 {
		p.Savings = round2(p.ListPrice - p.Price)
		p.SavingsPct = int((1 - p.Price/p.ListPrice) * 100)
	}
	// Coupon (clip/percent/amount), shown next to the price.
	p.Coupon = collapseSpace(firstNonEmptyText(doc,
		"#promoPriceBlockMessage_feature_div .a-color-success",
		".couponLabelText", "#couponText", "#vpcButton .a-color-success",
		"label[id^='couponText']"))
	if p.Rating == 0 {
		p.Rating = parseRating(attr(doc, "#acrPopover", "title"))
		if p.Rating == 0 {
			p.Rating = parseRating(text(doc, "#averageCustomerReviews .a-icon-alt"))
		}
	}
	if p.RatingsCount == 0 {
		p.RatingsCount = parseInt(text(doc, "#acrCustomerReviewText"))
	}
	p.AnsweredQs = int(parseInt(text(doc, "#askATFLink")))
	p.BoughtPastMonth = collapseSpace(firstNonEmptyText(doc,
		"#social-proofing-faceout-title-tk_bought .a-text-bold",
		"#socialProofingAsinFaceout_feature_div", "[data-csa-c-content-id='social-proofing-faceout']"))
	p.Availability = collapseSpace(firstNonEmptyText(doc, "#availability span", "#availability"))
	if p.Availability != "" {
		p.InStock = !availabilityOutOfStock(p.Availability)
	}
	if p.Description == "" {
		p.Description = collapseSpace(firstNonEmptyText(doc, "#productDescription p", "#productDescription", "#bookDescription_feature_div"))
	}

	doc.Find("#feature-bullets ul li span.a-list-item").Each(func(_ int, s *goquery.Selection) {
		if t := collapseSpace(s.Text()); t != "" {
			p.BulletPoints = append(p.BulletPoints, t)
		}
	})

	// Tech-spec tables.
	doc.Find("#productDetails_techSpec_section_1 tr, #productDetails_detailBullets_sections1 tr").Each(func(_ int, s *goquery.Selection) {
		k := collapseSpace(s.Find("th").Text())
		v := collapseSpace(s.Find("td").Text())
		if k != "" && v != "" {
			p.Specs[k] = v
		}
	})
	// Detail bullets.
	doc.Find("#detailBullets_feature_div li").Each(func(_ int, s *goquery.Selection) {
		spans := s.Find("span.a-list-item span")
		if spans.Length() >= 2 {
			k := collapseSpace(strings.Trim(spans.Eq(0).Text(), " :\u200e\u200f"))
			v := collapseSpace(spans.Eq(1).Text())
			if k != "" && v != "" {
				p.Specs[k] = v
			}
		}
	})

	// Images: the hero's dynamic-image map plus the alt-image thumbnail rail.
	// Every URL is canonicalized to full resolution and deduped, so the many
	// size variants of one photo collapse to a single master image.
	doc.Find("#imgBlkFront, #landingImage, #main-image-container img").Each(func(_ int, s *goquery.Selection) {
		if dyn, ok := s.Attr("data-a-dynamic-image"); ok && dyn != "" {
			var m map[string][]int
			if json.Unmarshal([]byte(dyn), &m) == nil {
				for k := range m {
					p.Images = append(p.Images, k)
				}
			}
		}
		if src, ok := s.Attr("data-old-hires"); ok {
			p.Images = append(p.Images, src)
		}
	})
	doc.Find("#altImages img, #imageBlockThumbs img").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			p.Images = append(p.Images, src)
		}
	})
	p.Images = normImages(p.Images)

	// Inline product videos (slate thumbnails carry the mp4 url).
	doc.Find("[data-video-url], #vse-related-videos-container [data-video-url]").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("data-video-url"); ok && strings.TrimSpace(src) != "" {
			p.Videos = append(p.Videos, strings.TrimSpace(src))
		}
	})
	p.Videos = dedup(p.Videos)

	// Breadcrumb category path.
	doc.Find("#wayfinding-breadcrumbs_feature_div li a").Each(func(_ int, s *goquery.Selection) {
		if t := collapseSpace(s.Text()); t != "" {
			p.CategoryPath = append(p.CategoryPath, t)
		}
		if href, ok := s.Attr("href"); ok {
			if m := regexp.MustCompile(`node=(\d+)`).FindStringSubmatch(href); m != nil {
				p.BrowseNodeIDs = append(p.BrowseNodeIDs, m[1])
			}
		}
	})
	p.BrowseNodeIDs = dedup(p.BrowseNodeIDs)

	// Seller / fulfillment.
	p.SellerName = collapseSpace(text(doc, "#sellerProfileTriggerId"))
	if href := attr(doc, "#sellerProfileTriggerId", "href"); href != "" {
		if m := regexp.MustCompile(`seller=([A-Z0-9]+)`).FindStringSubmatch(href); m != nil {
			p.SellerID = m[1]
		}
	}
	doc.Find("#tabular-buybox .tabular-buybox-text").Each(func(i int, s *goquery.Selection) {
		t := collapseSpace(s.Text())
		switch i {
		case 1:
			if p.SoldBy == "" {
				p.SoldBy = t
			}
		case 0:
			if p.FulfilledBy == "" {
				p.FulfilledBy = t
			}
		}
	})
	// "Ships from" is labelled in the tabular buybox; fall back to the free-text
	// merchant-info line ("Ships from Amazon.com Sold by ...").
	doc.Find("#tabular-buybox .tabular-buybox-text-message, #tabular-buybox tr").Each(func(_ int, s *goquery.Selection) {
		label := collapseSpace(s.Find(".tabular-buybox-text:first-child, td:first-child").Text())
		if strings.EqualFold(label, "Ships from") {
			if v := collapseSpace(s.Find(".tabular-buybox-text").Last().Text()); v != "" && !strings.EqualFold(v, label) {
				p.ShipsFrom = v
			}
		}
	})
	if p.ShipsFrom == "" {
		if mi := collapseSpace(text(doc, "#merchant-info")); mi != "" {
			if m := regexp.MustCompile(`(?i)ships?\s+from\s+(.+?)(?:\s+sold by\b|$)`).FindStringSubmatch(mi); m != nil {
				p.ShipsFrom = collapseSpace(m[1])
			}
		}
	}

	// Variants.
	doc.Find("li[data-asin], #variation_color_name a, #variation_size_name a").Each(func(_ int, s *goquery.Selection) {
		if a, ok := s.Attr("data-asin"); ok && bareASIN.MatchString(a) {
			p.VariantASINs = append(p.VariantASINs, a)
		}
		if href, ok := s.Attr("href"); ok {
			if a := ExtractASIN(href); a != "" {
				p.VariantASINs = append(p.VariantASINs, a)
			}
		}
	})
	p.VariantASINs = dedup(p.VariantASINs)
	if pa := attr(doc, "#landingAsin", "value"); bareASIN.MatchString(pa) {
		p.ParentASIN = pa
	} else if pa := attr(doc, "#ppd", "data-asin"); bareASIN.MatchString(pa) {
		p.ParentASIN = pa
	}

	// Similar / related ASINs.
	doc.Find("#similarities_feature_div [data-asin], .p13n-desktop-grid [data-asin]").Each(func(_ int, s *goquery.Selection) {
		if a, ok := s.Attr("data-asin"); ok && bareASIN.MatchString(a) && a != asin {
			p.SimilarASINs = append(p.SimilarASINs, a)
		}
	})
	p.SimilarASINs = dedup(p.SimilarASINs)

	// Best-sellers rank. A product carries one overall rank plus a rank in each
	// subcategory; capture them all, with the overall (first) also kept flat.
	doc.Find("#detailBulletsWrapper_feature_div li, #productDetails_detailBullets_sections1 tr, #detailBullets_feature_div li").Each(func(_ int, s *goquery.Selection) {
		t := s.Text()
		if !strings.Contains(t, "Best Sellers Rank") && !strings.Contains(t, "Amazon Best Sellers Rank") {
			return
		}
		for _, m := range rankRe.FindAllStringSubmatch(t, -1) {
			n, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
			cat := cleanRankCategory(m[2])
			if n == 0 || cat == "" {
				continue
			}
			p.Ranks = append(p.Ranks, ProductRank{Rank: n, Category: cat})
		}
	})
	if len(p.Ranks) > 0 {
		p.Rank = p.Ranks[0].Rank
		p.RankCategory = p.Ranks[0].Category
	}

	if len(p.Specs) == 0 {
		p.Specs = nil
	}
	if p.Title == "" && p.Price == 0 && p.Rating == 0 {
		return p, ErrNotFound
	}
	return p, nil
}

type jsonLD struct {
	Type        any    `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Brand       any    `json:"brand"`
	Image       any    `json:"image"`
	Offers      any    `json:"offers"`
	AggrRating  *struct {
		RatingValue any `json:"ratingValue"`
		ReviewCount any `json:"reviewCount"`
	} `json:"aggregateRating"`
}

func applyProductJSONLD(doc *goquery.Document, p *Product) {
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var ld jsonLD
		if json.Unmarshal([]byte(s.Text()), &ld) != nil {
			return true
		}
		if !typeContains(ld.Type, "Product") {
			return true
		}
		if p.Title == "" {
			p.Title = collapseSpace(ld.Name)
		}
		if p.Description == "" {
			p.Description = collapseSpace(ld.Description)
		}
		if p.Brand == "" {
			p.Brand = brandName(ld.Brand)
		}
		if ld.AggrRating != nil {
			if p.Rating == 0 {
				p.Rating = toFloat(ld.AggrRating.RatingValue)
			}
			if p.RatingsCount == 0 {
				p.RatingsCount = int64(toFloat(ld.AggrRating.ReviewCount))
			}
		}
		if price, cur := offerPrice(ld.Offers); price > 0 {
			if p.Price == 0 {
				p.Price = price
			}
			if cur != "" {
				p.Currency = cur
			}
		}
		if p.Availability == "" {
			if a := offerAvailability(ld.Offers); a != "" {
				p.Availability = a
			}
		}
		p.Images = append(p.Images, jsonLDImages(ld.Image)...)
		return false
	})
}

func typeContains(t any, want string) bool {
	switch v := t.(type) {
	case string:
		return v == want
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func brandName(b any) string {
	switch v := b.(type) {
	case string:
		return v
	case map[string]any:
		if n, ok := v["name"].(string); ok {
			return n
		}
	}
	return ""
}

func offerPrice(o any) (float64, string) {
	get := func(m map[string]any) (float64, string) {
		price := toFloat(m["price"])
		cur, _ := m["priceCurrency"].(string)
		return price, cur
	}
	switch v := o.(type) {
	case map[string]any:
		return get(v)
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				if pr, cur := get(m); pr > 0 {
					return pr, cur
				}
			}
		}
	}
	return 0, ""
}

// offerAvailability pulls a schema.org availability term ("InStock") out of an
// offer node and renders it human-readable.
func offerAvailability(o any) string {
	avail := func(m map[string]any) string {
		s, _ := m["availability"].(string)
		if i := strings.LastIndexAny(s, "/#"); i >= 0 {
			s = s[i+1:]
		}
		return s
	}
	switch v := o.(type) {
	case map[string]any:
		return avail(v)
	case []any:
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				if a := avail(m); a != "" {
					return a
				}
			}
		}
	}
	return ""
}

// jsonLDImages flattens the JSON-LD image field (string, list, or ImageObject).
func jsonLDImages(img any) []string {
	switch v := img.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			out = append(out, jsonLDImages(e)...)
		}
		return out
	case map[string]any:
		if u, ok := v["url"].(string); ok {
			return []string{u}
		}
	}
	return nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, _ := ParsePrice(n)
		return f
	}
	return 0
}
