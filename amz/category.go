package amz

import (
	"bytes"
	"context"
	"regexp"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// CategoryURL builds the browse-node URL.
func (c *Client) CategoryURL(node string) string {
	return c.BaseURL() + "/b?node=" + node
}

var nodeRe = regexp.MustCompile(`node=(\d+)`)

// FetchCategory fetches and normalizes a browse-node page.
func (c *Client) FetchCategory(ctx context.Context, nodeOrURL string) (Category, error) {
	node := nodeOrURL
	url := nodeOrURL
	if IsURL(nodeOrURL) {
		if m := nodeRe.FindStringSubmatch(nodeOrURL); m != nil {
			node = m[1]
		}
	} else {
		url = c.CategoryURL(node)
	}
	body, err := c.Get(ctx, url, 24*time.Hour)
	if err != nil {
		return Category{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Category{}, err
	}
	cat := Category{NodeID: node, URL: url, FetchedAt: time.Now().UTC()}
	cat.Name = collapseSpace(firstNonEmptyText(doc, "h1.a-spacing-none", "#departments h1", ".a-carousel-heading", "title"))
	doc.Find("#wayfinding-breadcrumbs_feature_div li a, .a-breadcrumb li a, #nav-subnav a").Each(func(_ int, s *goquery.Selection) {
		if t := collapseSpace(s.Text()); t != "" {
			cat.Breadcrumb = append(cat.Breadcrumb, t)
		}
	})
	cat.Breadcrumb = dedup(cat.Breadcrumb)
	doc.Find("a[href*='node=']").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if m := nodeRe.FindStringSubmatch(href); m != nil && m[1] != node {
				cat.ChildNodeIDs = append(cat.ChildNodeIDs, m[1])
			}
		}
	})
	cat.ChildNodeIDs = dedup(cat.ChildNodeIDs)
	if len(cat.ChildNodeIDs) > 50 {
		cat.ChildNodeIDs = cat.ChildNodeIDs[:50]
	}
	doc.Find("a[href*='/dp/']").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if a := ExtractASIN(href); a != "" {
				cat.TopASINs = append(cat.TopASINs, a)
			}
		}
	})
	cat.TopASINs = dedup(cat.TopASINs)
	if len(cat.TopASINs) > 50 {
		cat.TopASINs = cat.TopASINs[:50]
	}
	if cat.Name == "" && len(cat.TopASINs) == 0 {
		return cat, ErrNotFound
	}
	return cat, nil
}
