package amz

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SellerURL builds a third-party seller profile URL.
func (c *Client) SellerURL(id string) string {
	return c.BaseURL() + "/sp?seller=" + id
}

var (
	pctRe       = regexp.MustCompile(`(\d+)%`)
	ratingCntRe = regexp.MustCompile(`([\d,]+)\s+ratings?`)
)

// FetchSeller fetches and normalizes a seller profile.
func (c *Client) FetchSeller(ctx context.Context, idOrURL string) (Seller, error) {
	id := idOrURL
	url := idOrURL
	if IsURL(idOrURL) {
		if m := sellerIDRe.FindStringSubmatch(idOrURL); m != nil {
			id = m[1]
		}
	} else {
		url = c.SellerURL(id)
	}
	body, err := c.Get(ctx, url, 24*time.Hour)
	if err != nil {
		return Seller{}, err
	}
	doc, err := newDocument(body)
	if err != nil {
		return Seller{}, err
	}
	s := Seller{SellerID: id, URL: url, FetchedAt: time.Now().UTC()}
	s.Name = collapseSpace(firstNonEmptyText(doc, "#seller-name", "h1#sellerName", "#sellerName", "h1"))
	s.Rating = collapseSpace(doc.Find("#effective-timeperiod-rating-year-description .a-icon-alt, #seller-rating-summary .a-icon-alt").First().Text())
	if m := ratingCntRe.FindStringSubmatch(doc.Text()); m != nil {
		s.RatingCount = int(parseInt(m[1]))
	}
	// Feedback breakdown table: positive/neutral/negative percentages.
	doc.Find("#feedback-summary-table tr, table.feedback-table tr").Each(func(_ int, tr *goquery.Selection) {
		label := strings.ToLower(collapseSpace(tr.Find("td").First().Text()))
		pct := 0.0
		if m := pctRe.FindStringSubmatch(tr.Text()); m != nil {
			pct = toFloat(m[1])
		}
		switch {
		case strings.Contains(label, "positive"):
			s.PositivePct = pct
		case strings.Contains(label, "neutral"):
			s.NeutralPct = pct
		case strings.Contains(label, "negative"):
			s.NegativePct = pct
		}
	})
	if s.Name == "" {
		return s, ErrNotFound
	}
	return s, nil
}
