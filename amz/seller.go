package amz

import (
	"context"
	"time"
)

// SellerURL builds a third-party seller profile URL.
func (c *Client) SellerURL(id string) string {
	return c.BaseURL() + "/sp?seller=" + id
}

// sellerClaimed is the sections this parser reads or deliberately ignores, so
// the unread worklist reports only sections nothing has looked at yet.
//
// The policy sections are claimed because they are read. The rest are Amazon's
// own boilerplate, identical on every seller profile on the site, and are named
// here so they stop appearing on a worklist that is meant to list work.
var sellerClaimed = map[string]bool{
	"seller-profile-container":   true,
	"seller-info-card":           true,
	"seller-logo-column":         true,
	"seller-desc-column":         true,
	"seller-contact-text":        true,
	"seller-contact-button":      true,
	"page-section-seller-header": true,
	// Boilerplate. The A-to-z guarantee, the privacy notice and the help block
	// are Amazon's text about Amazon, not this seller's, and recording them
	// would put the same three paragraphs on every seller record in the store.
	"page-section-atoz":           true,
	"page-section-other-policies": true,
	"page-section-help":           true,
	// The feedback furniture. The time periods block is the dropdown that
	// switches between the four windows this parser reads all of, and the rating
	// explanation is Amazon's paragraph about how it computes a star average.
	"seller-rating-time-periods":                           true,
	"seller-feedback-summary-histogram-rating-explanation": true,
	// Read as data, not as a section: page-section-products is a single link to
	// the seller's catalogue, which storefront_url already carries from the
	// header where Amazon states it first.
	"page-section-products": true,
}

// FetchSeller fetches and normalizes a seller profile.
//
// Measured on 2026-08-17 against /sp?seller=A2L77EE7U53NWQ. A seller profile is
// not a stores page: it carries no var config widget, no data-testid and no
// cel_widget_id past the navbar. It is server rendered and it names its eight
// sections with id="page-section-*", which is why FamilySeller exists.
//
// Of the parser this replaces, one selector out of five worked. #seller-name
// matched. h1#sellerName, #sellerName, the two rating selectors and both
// feedback table selectors matched nothing, so name was the only field a seller
// record ever carried. The feedback percentages were always zero and the rating
// count came from a regexp run over the whole document text, which on this
// capture finds nothing because the word "ratings" does not appear on the page.
//
// That last point is worth stating plainly, because it is the difference between
// the old record and this one. This seller publishes no feedback rating at all.
// The old parser reported 0 ratings and 0 percent positive, which reads as a
// seller with a perfect absence of goodwill rather than a seller Amazon does not
// rate. Here those fields are declared, they miss, and the miss is in the
// envelope with the reason.
//
// What the page does carry, and the old parser did not take: the legal business
// name and registered address, the seller's own description, the shipping and
// return policies, the logo, and the link to the seller's catalogue.
//
// The feedback fields were written against a second capture, taken the same day
// from /sp?seller=AKI54NNZ6PH23, a third party seller Amazon does rate. That was
// necessary rather than thorough. A2L77EE7U53NWQ is Amazon owned and carries no
// feedback section at all, not a hidden one, so any region name for a rating
// taken off it would have been invented, and this package already has one
// registry it had to throw away for exactly that. The two captures together are
// also what the fields are for: one seller publishes feedback and one does not,
// and the difference between "not rated" and "rated zero" only survives into the
// record because both fields are declared and the absent one reports a miss.
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
	body, src, err := c.GetSource(ctx, url, 24*time.Hour)
	if err != nil {
		return Seller{}, err
	}
	s, err := c.parseSellerPage(id, url, body)
	c.record(ctx, &s.Envelope, src)
	return s, err
}

func (c *Client) parseSellerPage(id, url string, body []byte) (Seller, error) {
	d, err := ParseDoc(FamilySeller, body)
	if err != nil {
		return Seller{}, err
	}
	e := NewExtractor(d)
	e.RunFields(sellerFields())

	s := Seller{
		SellerID:        id,
		URL:             url,
		Name:            e.Str("name"),
		LogoURL:         upgradeImage(e.Str("logo")),
		StorefrontURL:   absoluteURL(c.BaseURL(), e.Str("storefront_url")),
		About:           e.Str("about"),
		BusinessName:    e.Str("business_name"),
		BusinessAddress: e.Str("business_address"),
		ShippingPolicy:  e.Str("shipping_policy"),
		ReturnPolicy:    e.Str("return_policy"),
		Rating:          e.Float("rating"),
		RatingCount:     e.Int("ratings_count"),
		PositivePct:     e.Float("positive_pct"),
		FetchedAt:       time.Now().UTC(),
	}
	if v, ok := e.Value("feedback"); ok {
		s.Feedback, _ = v.([]SellerFeedback)
	}
	if v, ok := e.Value("rating_histogram"); ok {
		s.RatingHistogram, _ = v.([]SellerStarShare)
	}
	if v, ok := e.Value("reviews"); ok {
		s.Reviews, _ = v.([]SellerReview)
	}
	e.MarkUnread(claimedWith(sellerFields(), sellerClaimed))
	s.Envelope = e.Envelope()
	s.Envelope.AgentMap = d.AgentMap()
	if s.Name == "" {
		return s, ErrNotFound
	}
	return s, nil
}
