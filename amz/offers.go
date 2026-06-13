package amz

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// OfferQuery filters the offer-listing.
type OfferQuery struct {
	Condition string // new|used|...
	Prime     bool
}

// OffersURL builds the offer-listing URL for an ASIN.
func (c *Client) OffersURL(asin string) string {
	return c.BaseURL() + "/gp/offer-listing/" + asin
}

var sellerIDRe = regexp.MustCompile(`seller=([A-Z0-9]+)`)

// FetchOffers streams buying options for an ASIN.
func (c *Client) FetchOffers(ctx context.Context, asin string, q OfferQuery, emit func(Offer) error) error {
	u := c.OffersURL(asin)
	body, err := c.Get(ctx, u, 6*time.Hour)
	if err != nil {
		return err
	}
	doc, err := newDocument(body)
	if err != nil {
		return err
	}
	var perr error
	doc.Find("#aod-offer-list #aod-offer, #olpOfferList .olpOffer, .aod-offer").EachWithBreak(func(i int, s *goquery.Selection) bool {
		o := Offer{ASIN: asin, Currency: c.mkt.Currency, URL: u, FetchedAt: time.Now().UTC()}
		o.Price, _ = ParsePrice(s.Find(".a-price .a-offscreen, .olpOfferPrice").First().Text())
		o.Shipping = collapseSpace(s.Find("#aod-bottlecap-deliveryMessage, .olpShippingInfo").First().Text())
		o.Condition = collapseSpace(s.Find("#aod-offer-heading, .olpCondition").First().Text())
		o.SellerName = collapseSpace(s.Find("#aod-offer-soldBy a, .olpSellerName a, #aod-offer-soldBy .a-col-right").First().Text())
		if href, ok := s.Find("#aod-offer-soldBy a, .olpSellerName a").First().Attr("href"); ok {
			if m := sellerIDRe.FindStringSubmatch(href); m != nil {
				o.SellerID = m[1]
			}
		}
		o.SellerRating = collapseSpace(s.Find("#aod-offer-seller-rating, .olpSellerColumn .a-icon-alt").First().Text())
		o.Delivery = collapseSpace(s.Find("#aod-offer-shipsFrom .a-col-right, .olpDeliveryColumn").First().Text())
		if strings.Contains(strings.ToLower(s.Text()), "ships from amazon") || s.Find(".supersaver, #aod-offer-shipsFrom").Length() > 0 {
			o.FulfilledBy = "Amazon"
		}
		o.IsBuyBox = i == 0
		if o.Price == 0 && o.SellerName == "" {
			return true
		}
		if q.Condition != "" && !strings.Contains(strings.ToLower(o.Condition), strings.ToLower(q.Condition)) {
			return true
		}
		if q.Prime && o.FulfilledBy != "Amazon" {
			return true
		}
		if err := emit(o); err != nil {
			perr = err
			return false
		}
		return true
	})
	return perr
}
