package amz

import (
	"context"
	"regexp"
	"time"
)

// CategoryURL builds the browse-node URL.
func (c *Client) CategoryURL(node string) string {
	return c.BaseURL() + "/b?node=" + node
}

var nodeRe = regexp.MustCompile(`node=(\d+)`)

// FetchCategory fetches and normalizes a browse-node page.
//
// What a browse node is not, measured on 2026-08-17 across two captures: it is
// not a paginated catalogue. Neither page carried a single pagination control,
// and the items on them came to 59 and 91 spread across four and six carousels.
// So this returns what the page actually is, a merchandised landing page and its
// shelves, and anything wanting the whole of a category has to go to
// /s?rh=n:<node> or to the chart for that node. Reporting 59 items as if they
// were the category would be a quieter answer and a false one.
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
	body, src, err := c.GetSource(ctx, url, 24*time.Hour)
	if err != nil {
		return Category{}, err
	}
	bp, err := c.parseBrowsePage(node, url, body)
	if err != nil {
		return Category{}, err
	}
	c.record(ctx, &bp.Envelope, src)
	cat := Category{
		NodeID:        node,
		CanonicalNode: bp.CanonicalNode,
		Slug:          bp.Slug,
		Name:          bp.Name,
		URL:           url,
		CanonicalURL:  bp.CanonicalURL,
		Related:       bp.Related,
		ItemCount:     bp.Items,
		FetchedAt:     time.Now().UTC(),
		Envelope:      bp.Envelope,
	}
	if len(cat.Related) > 50 {
		// The cap is a report of what was kept, not a silent truncation. Without
		// the entry a consumer counting related nodes reads fifty as the answer.
		cat.Envelope.Missed = append(cat.Envelope.Missed, Miss{
			Field: "related",
			Why:   "the page links more browse nodes than one record carries",
			Have:  50,
			Total: int64(len(cat.Related)),
		})
		cat.Related = cat.Related[:50]
	}
	for _, sh := range bp.Shelves {
		s := CategoryShelf{Widget: sh.Widget, Title: sh.Title}
		for _, it := range sh.Items {
			s.ASINs = append(s.ASINs, it.ASIN)
			cat.TopASINs = append(cat.TopASINs, it.ASIN)
		}
		cat.Shelves = append(cat.Shelves, s)
	}
	cat.TopASINs = dedup(cat.TopASINs)
	if cat.Name == "" && len(cat.TopASINs) == 0 {
		return cat, ErrNotFound
	}
	return cat, nil
}
