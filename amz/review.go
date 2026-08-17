package amz

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ReviewQuery holds review-page refinements.
type ReviewQuery struct {
	Sort       string // recent|helpful
	Stars      int    // 1..5, 0 = all
	Verified   bool
	WithImages bool
	StartPage  int
	Limit      int
}

var reviewerIDRe = regexp.MustCompile(`amzn1\.account\.([A-Z0-9]+)`)

// ReviewURL builds the product-reviews URL.
func (c *Client) ReviewURL(asin string, q ReviewQuery, page int) string {
	v := url.Values{}
	if page > 1 {
		v.Set("pageNumber", strconv.Itoa(page))
	}
	switch q.Sort {
	case "helpful":
		v.Set("sortBy", "helpful")
	case "recent":
		v.Set("sortBy", "recent")
	}
	if q.Stars >= 1 && q.Stars <= 5 {
		names := map[int]string{1: "one", 2: "two", 3: "three", 4: "four", 5: "five"}
		v.Set("filterByStar", names[q.Stars]+"_star")
	}
	if q.Verified {
		v.Set("reviewerType", "avp_only_reviews")
	}
	u := c.BaseURL() + "/product-reviews/" + asin
	if e := v.Encode(); e != "" {
		u += "?" + e
	}
	return u
}

// FetchReviews streams reviews for an ASIN, paging until Limit.
func (c *Client) FetchReviews(ctx context.Context, asin string, q ReviewQuery, emit func(Review) error) error {
	page := q.StartPage
	if page < 1 {
		page = 1
	}
	count := 0
	for {
		u := c.ReviewURL(asin, q, page)
		body, err := c.Get(ctx, u, 6*time.Hour)
		if err != nil {
			return err
		}
		reviews := c.parseReviews(asin, u, body)
		if len(reviews) == 0 {
			break
		}
		for _, r := range reviews {
			count++
			if err := emit(r); err != nil {
				return err
			}
			if q.Limit > 0 && count >= q.Limit {
				return nil
			}
		}
		page++
		if page > 10 {
			break
		}
	}
	return nil
}

func (c *Client) parseReviews(asin, pageURL string, body []byte) []Review {
	doc, err := newDocument(body)
	if err != nil {
		return nil
	}
	return c.readReviews(asin, pageURL, doc.Selection)
}

// readReviews reads every review block under root.
//
// It is separate from parseReviews because the same markup appears on two
// pages and only one of them can still be fetched. The review corpus at
// /product-reviews/ redirects to a sign-in, and the detail page carries the
// most recent handful in the reviews medley, so this is the only path that
// returns anything today. See ReviewsOnPage.
//
// Amazon spells the hooks differently on the two pages. The corpus page writes
// review-title and review-body, the medley writes reviewTitle and reviewText,
// and both spellings are listed here rather than picked between, because a
// parser that guesses which page it is on is one redesign from reading nothing.
func (c *Client) readReviews(asin, pageURL string, root *goquery.Selection) []Review {
	var out []Review
	root.Find(`div[data-hook="review"]`).Each(func(_ int, s *goquery.Selection) {
		r := Review{Marketplace: c.mkt.Slug, ASIN: asin, URL: pageURL, FetchedAt: time.Now().UTC()}
		r.ReviewID = reviewIDOf(s)
		r.ReviewerName = collapseSpace(s.Find(`span.a-profile-name`).First().Text())
		if href, ok := s.Find(`a.a-profile`).First().Attr("href"); ok {
			if m := reviewerIDRe.FindStringSubmatch(href); m != nil {
				r.ReviewerID = m[1]
			}
		}
		r.Rating = int(parseRating(s.Find(`[data-hook="review-star-rating"] span, [data-hook="cmps-review-star-rating"] span`).First().Text()))
		r.Title = collapseSpace(s.Find(`[data-hook="review-title"] span:last-child, [data-hook="review-title"], [data-hook="reviewTitle"]`).Last().Text())
		r.Text = collapseSpace(s.Find(`[data-hook="review-body"] span, [data-hook="reviewRichContentContainer"], [data-hook="reviewText"]`).First().Text())
		dateLine := s.Find(`[data-hook="review-date"]`).First().Text()
		var raw string
		r.Country, raw = splitReviewDate(dateLine)
		r.Date = NewDate(raw)
		r.Author = PersonRef(r.ReviewerName)
		if s.Find(`[data-hook="avp-badge"]`).Length() > 0 {
			r.VerifiedPurchase = true
		}
		r.HelpfulVotes = int(parseInt(s.Find(`[data-hook="helpful-vote-statement"]`).First().Text()))
		s.Find(`.review-image-tile, img.review-image-tile`).Each(func(_ int, img *goquery.Selection) {
			if src, ok := img.Attr("src"); ok {
				r.Images = append(r.Images, src)
			}
		})
		r.Images = normImages(r.Images)
		if strip := strings.TrimSpace(s.Find(`[data-hook="format-strip"]`).First().Text()); strip != "" {
			r.VariantAttrs = parseVariantStrip(strip)
		}
		if r.ReviewID == "" {
			// A review Amazon did not stamp with an id gets one derived from its
			// content, and the marketplace is in the hash. Without it the same review
			// text under the same ASIN on two storefronts derives the same id, and the
			// reviews table, which is keyed on that id, would keep one of them.
			sum := md5.Sum([]byte(c.mkt.Slug + "|" + asin + "|" + r.ReviewerName + "|" + r.Title + "|" + r.Text))
			r.ReviewID = hex.EncodeToString(sum[:])
		}
		out = append(out, r)
	})
	return out
}

// reviewIDOf reads the review id off a review block.
//
// The id attribute carries it on both pages, but the medley also writes it into
// a slot id, and on some renderings the id attribute is the slot spelling rather
// than the bare one. Trimming the prefix means the same review gets the same id
// whichever page it was read from, which is what the reviews table is keyed on.
//
// There are two prefixes because a medley holds two strips. The reviews written
// in the marketplace being read are customer_review-R143X21KH8JWEO, and the ones
// translated in from other Amazon storefronts are customer_review_foreign-, five
// of the thirteen on B075F5X8BR. They are ordinary reviews with a country other
// than this one, the Country field carries which, and the wider prefix has to be
// tried first or the trim leaves _foreign- on the front of the id.
func reviewIDOf(s *goquery.Selection) string {
	id, _ := s.Attr("id")
	if id == "" {
		id, _ = s.Attr("data-csa-c-slot-id")
	}
	id = collapseSpace(id)
	for _, prefix := range []string{"customer_review_foreign-", "customer_review-"} {
		if strings.HasPrefix(id, prefix) {
			return strings.TrimPrefix(id, prefix)
		}
	}
	return id
}

func splitReviewDate(s string) (country, date string) {
	s = collapseSpace(s)
	const marker = "Reviewed in "
	if i := strings.Index(s, marker); i >= 0 {
		rest := s[i+len(marker):]
		if j := strings.Index(rest, " on "); j >= 0 {
			return strings.TrimPrefix(rest[:j], "the "), strings.TrimSpace(rest[j+4:])
		}
		return rest, ""
	}
	return "", s
}

var multiSpaceRe = regexp.MustCompile(`\s{2,}`)

func parseVariantStrip(s string) map[string]string {
	out := map[string]string{}
	for _, part := range multiSpaceRe.Split(s, -1) {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			k := collapseSpace(kv[0])
			v := collapseSpace(kv[1])
			if k != "" && v != "" {
				out[strings.ToLower(k)] = v
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
