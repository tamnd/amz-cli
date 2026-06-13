package amz

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// AuthorURL builds an Author Central page URL from a slug or author id.
func (c *Client) AuthorURL(slug string) string {
	slug = strings.TrimPrefix(slug, "/")
	if strings.HasPrefix(slug, "author/") || strings.HasPrefix(slug, "stores/author/") {
		return c.BaseURL() + "/" + slug
	}
	return c.BaseURL() + "/author/" + slug
}

// FetchAuthor fetches and normalizes an Author Central page.
func (c *Client) FetchAuthor(ctx context.Context, slugOrURL string) (Author, error) {
	url := slugOrURL
	slug := slugOrURL
	if IsURL(slugOrURL) {
		slug = authorSlug(slugOrURL)
	} else {
		url = c.AuthorURL(slugOrURL)
	}
	body, err := c.Get(ctx, url, 24*time.Hour)
	if err != nil {
		return Author{}, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Author{}, err
	}
	a := Author{Slug: slug, URL: url, FetchedAt: time.Now().UTC()}
	a.Name = collapseSpace(firstNonEmptyText(doc,
		"meta[property='og:title']", "#author-profile-name", ".author-name", "h1", "title"))
	if v, ok := doc.Find("meta[property='og:title']").Attr("content"); ok && v != "" {
		a.Name = collapseSpace(v)
	}
	a.Bio = collapseSpace(firstNonEmptyText(doc, "#author-profile-biography", ".author-biography", "meta[name='description']"))
	if a.Bio == "" {
		a.Bio = collapseSpace(attr(doc, "meta[name='description']", "content"))
	}
	a.PhotoURL = attr(doc, "#author-profile-photo img, .author-photo img, meta[property='og:image']", "src")
	if a.PhotoURL == "" {
		a.PhotoURL = attr(doc, "meta[property='og:image']", "content")
	}
	doc.Find("a[href*='/dp/']").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if x := ExtractASIN(href); x != "" {
				a.BookASINs = append(a.BookASINs, x)
			}
		}
	})
	a.BookASINs = dedup(a.BookASINs)
	if len(a.BookASINs) > 100 {
		a.BookASINs = a.BookASINs[:100]
	}
	if a.Name == "" && len(a.BookASINs) == 0 {
		return a, ErrNotFound
	}
	return a, nil
}

func authorSlug(u string) string {
	u = strings.TrimSuffix(u, "/")
	const marker = "/author/"
	if _, rest, ok := strings.Cut(u, marker); ok {
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return u
}
