package amz

import (
	"context"
	"time"
)

// DealsURL builds the deals grid URL.
func (c *Client) DealsURL() string {
	return c.BaseURL() + "/deals"
}

// FetchDeals streams entries from the deals grid.
//
// The parser this replaced looked for [data-testid='deal-card'], .DealCard-module
// and div[data-asin]. Measured against a live capture on 2026-08-17, /deals
// carries none of those: zero data-testid attributes, zero data-asin attributes,
// and no DealCard string anywhere in 768 KB of HTML. So the old selector matched
// nothing and FetchDeals returned an empty list and no error, which is the worst
// of the two ways to be wrong. The tiles are there, eleven of them, anchored on
// data-csa-c-item-id like every other browse tile.
//
// One more thing that capture said about itself: /deals answered with
// rel=canonical pointing at /events/wintersale. The deals page is a seasonal
// event page wearing a stable URL, so what a crawl finds there changes with the
// calendar and the canonical is where Amazon admits it.
func (c *Client) FetchDeals(ctx context.Context, limit int, emit func(Deal) error) error {
	u := c.DealsURL()
	body, err := c.Get(ctx, u, time.Hour)
	if err != nil {
		return err
	}
	bp, err := c.parseBrowsePage("", u, body)
	if err != nil {
		return err
	}
	count := 0
	for _, sh := range bp.Shelves {
		for _, it := range sh.Items {
			d := Deal{
				ASIN:        it.ASIN,
				DealID:      it.DealID,
				Title:       it.Title,
				DealPrice:   it.Price,
				ListPrice:   it.WasPrice,
				ListLabel:   it.WasPriceLabel,
				DiscountPct: it.DiscountPct,
				Badge:       it.DealType,
				EndsSoon:    it.EndsSoon,
				Currency:    it.Currency,
				URL:         it.URL,
				Image:       it.Image,
				Shelf:       it.Shelf,
				Position:    it.Position,
				FetchedAt:   time.Now().UTC(),
				Envelope:    it.Envelope,
			}
			count++
			if err := emit(d); err != nil {
				return err
			}
			if limit > 0 && count >= limit {
				return nil
			}
		}
	}
	return nil
}
