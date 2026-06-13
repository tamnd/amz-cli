package amz

import (
	"bytes"
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
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var out []Review
	doc.Find(`div[data-hook="review"]`).Each(func(_ int, s *goquery.Selection) {
		r := Review{ASIN: asin, URL: pageURL, FetchedAt: time.Now().UTC()}
		r.ReviewID, _ = s.Attr("id")
		r.ReviewerName = collapseSpace(s.Find(`span.a-profile-name`).First().Text())
		if href, ok := s.Find(`a.a-profile`).First().Attr("href"); ok {
			if m := reviewerIDRe.FindStringSubmatch(href); m != nil {
				r.ReviewerID = m[1]
			}
		}
		r.Rating = int(parseRating(s.Find(`[data-hook="review-star-rating"] span, [data-hook="cmps-review-star-rating"] span`).First().Text()))
		r.Title = collapseSpace(s.Find(`[data-hook="review-title"] span:last-child, [data-hook="review-title"]`).Last().Text())
		r.Text = collapseSpace(s.Find(`[data-hook="review-body"] span`).First().Text())
		dateLine := s.Find(`[data-hook="review-date"]`).First().Text()
		r.Country, r.Date = splitReviewDate(dateLine)
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
			sum := md5.Sum([]byte(asin + r.ReviewerName + r.Title + r.Text))
			r.ReviewID = hex.EncodeToString(sum[:])
		}
		out = append(out, r)
	})
	return out
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
