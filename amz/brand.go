package amz

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// BrandURL builds a brand storefront URL from a slug or page id.
func (c *Client) BrandURL(slug string) string {
	slug = strings.TrimPrefix(slug, "/")
	if strings.HasPrefix(slug, "stores/") {
		return c.BaseURL() + "/" + slug
	}
	return c.BaseURL() + "/stores/" + slug
}

// FetchBrand fetches and normalizes a brand storefront.
func (c *Client) FetchBrand(ctx context.Context, slugOrURL string) (Brand, error) {
	url := slugOrURL
	slug := slugOrURL
	if IsURL(slugOrURL) {
		slug = brandSlug(slugOrURL)
	} else {
		url = c.BrandURL(slugOrURL)
	}
	body, err := c.Get(ctx, url, 24*time.Hour)
	if err != nil {
		return Brand{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Brand{}, err
	}
	b := Brand{Slug: slug, URL: url, FetchedAt: time.Now().UTC()}
	b.Name = collapseSpace(firstNonEmptyText(doc,
		"meta[property='og:title']", "#brandLogoImage", "h1", "title"))
	if v, ok := doc.Find("meta[property='og:title']").Attr("content"); ok && v != "" {
		b.Name = collapseSpace(v)
	}
	b.Description = collapseSpace(attr(doc, "meta[name='description']", "content"))
	b.LogoURL = attr(doc, "#brandLogoImage img, img#brandLogo", "src")
	b.BannerURL = attr(doc, "meta[property='og:image']", "content")
	doc.Find("a[href*='/dp/']").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if a := ExtractASIN(href); a != "" {
				b.FeaturedASINs = append(b.FeaturedASINs, a)
			}
		}
	})
	b.FeaturedASINs = dedup(b.FeaturedASINs)
	if len(b.FeaturedASINs) > 100 {
		b.FeaturedASINs = b.FeaturedASINs[:100]
	}
	if b.Name == "" && len(b.FeaturedASINs) == 0 {
		return b, ErrNotFound
	}
	return b, nil
}

func brandSlug(u string) string {
	u = strings.TrimSuffix(u, "/")
	if _, rest, ok := strings.Cut(u, "/stores/"); ok {
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return u
}
