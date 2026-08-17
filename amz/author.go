package amz

import (
	"context"
	"strings"
	"time"
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
//
// Measured on 2026-08-17 against the Michael Lewis page. An author page is a
// stores page, so it keeps its layout in the DOM and its data in a payload, and
// the gap between the two is wider here than anywhere else on the site. The
// served HTML renders ProductGrid__no-matches where the bibliography should be,
// which is Amazon's own "We couldn't find anything matching these filters", and
// the page's grid payload carries seventy complete book records and states that
// the author has 135.
//
// So the parser this replaces returned three ASINs. It collected every
// a[href*='/dp/'] in the document, and the only such links outside the content
// are B07984JN3L and B0CHTVMXZJ, the Amazon Business Card and Reload Your
// Balance, which sit in the footer of every page on amazon.com. Two of Michael
// Lewis's three books were Amazon's credit card and a gift card top up. A wrong
// ASIN in a bibliography looks exactly like a right one, which is why that was
// worse than returning nothing.
//
// The books here are whole records rather than ASINs, because the payload
// publishes whole records: binding, price, contributors and their roles, series
// position, other editions of the same work, and the accolade badges. TotalBooks
// carries what Amazon says the shelf holds so a caller can tell this first page
// from a complete one.
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
	return c.parseAuthorPage(slug, url, body)
}

// parseAuthorPage reads an author page that has already been fetched. It stays a
// method because absoluteURL needs the marketplace host, which brand does not.
func (c *Client) parseAuthorPage(slug, url string, body []byte) (Author, error) {
	d, err := ParseDoc(FamilyStore, body)
	if err != nil {
		return Author{}, err
	}
	e := NewExtractor(d)
	e.RunFields(storeFields())

	a := Author{
		Slug:     slug,
		PageID:   e.Str("page_id"),
		Name:     e.Str("name"),
		Bio:      e.Str("bio"),
		PhotoURL: upgradeImage(e.Str("image")),
		AboutURL: absoluteURL(c.BaseURL(), e.Str("about_url")),
		URL:      url,

		FetchedAt: time.Now().UTC(),
	}

	// The grid is the record. Everything above is the page's header.
	if g := storeGrid(d.Widgets(), c.BaseURL()); g != nil {
		a.Books = g.Products
		a.TotalBooks = g.Total
		a.SortOptions = g.SortOptions
		a.Languages = g.Languages
		a.BookASINs = g.ASINList
		if len(a.BookASINs) == 0 {
			for _, p := range g.Products {
				a.BookASINs = append(a.BookASINs, p.ASIN)
			}
		}
	} else {
		// Named, because an author page with no grid payload is the one failure
		// this parser cannot work around, and it should say so rather than
		// return a name and an empty shelf.
		e.miss("books", "no ProductGrid payload on this page, so the bibliography is not in the served HTML at all")
	}
	a.BookASINs = dedup(a.BookASINs)

	e.MarkUnread(claimedWith(storeFields(), storeClaimed))
	a.Envelope = e.Envelope()
	a.Envelope.AgentMap = d.AgentMap()
	if a.Name == "" && len(a.Books) == 0 {
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
