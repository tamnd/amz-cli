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
	p.Availability = collapseSpace(firstNonEmptyText(doc, "#availability span", "#availability"))
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

	// Images from the dynamic-image JSON map.
	if dyn := attr(doc, "#imgBlkFront, #landingImage", "data-a-dynamic-image"); dyn != "" {
		var m map[string][]int
		if json.Unmarshal([]byte(dyn), &m) == nil {
			for k := range m {
				p.Images = append(p.Images, k)
			}
		}
	}
	doc.Find("#altImages img").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			p.Images = append(p.Images, src)
		}
	})
	p.Images = dedup(p.Images)

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

	// Best-sellers rank.
	doc.Find("#detailBulletsWrapper_feature_div li, #productDetails_detailBullets_sections1 tr").Each(func(_ int, s *goquery.Selection) {
		t := s.Text()
		if !strings.Contains(t, "Best Sellers Rank") && !strings.Contains(t, "Amazon Best Sellers Rank") {
			return
		}
		if m := rankRe.FindStringSubmatch(t); m != nil && p.Rank == 0 {
			p.Rank, _ = strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
			p.RankCategory = collapseSpace(m[2])
		}
	})

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
