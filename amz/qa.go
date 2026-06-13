package amz

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// QAURL builds the Q&A page URL for an ASIN.
func (c *Client) QAURL(asin string) string {
	return c.BaseURL() + "/ask/questions/asin/" + asin
}

// HasQA reports whether the body has a classic Q&A section.
func hasQASection(doc *goquery.Document) bool {
	return doc.Find(`.askTeaserQuestions, div[id^="question-"], .a-section.askInlineWidget`).Length() > 0
}

// FetchQA streams Q&A pairs for an ASIN. It returns ErrNoQA when the product has
// no classic Q&A section (amazon is deprecating it across many categories).
var ErrNoQA = errNoQA{}

type errNoQA struct{}

func (errNoQA) Error() string { return "no Q&A section on this product page" }

func (c *Client) FetchQA(ctx context.Context, asin string, emit func(QA) error) error {
	u := c.QAURL(asin)
	body, err := c.Get(ctx, u, 24*time.Hour)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if !hasQASection(doc) {
		return ErrNoQA
	}
	var perr error
	doc.Find(`.askTeaserQuestions > div, div[id^="question-"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		spans := s.Find(".a-fixed-left-grid-col.a-col-right span")
		q := collapseSpace(s.Find(`a[href*="/ask/questions/"], .askQuestionText`).First().Text())
		if q == "" && spans.Length() > 0 {
			q = collapseSpace(spans.First().Text())
		}
		ans := ""
		if spans.Length() > 1 {
			ans = collapseSpace(spans.Eq(1).Text())
		}
		if q == "" {
			return true
		}
		sum := md5.Sum([]byte(asin + "|" + q + "|" + ans))
		qa := QA{
			QAID:      hex.EncodeToString(sum[:]),
			ASIN:      asin,
			Question:  strings.TrimSpace(q),
			Answer:    strings.TrimSpace(ans),
			URL:       u,
			FetchedAt: time.Now().UTC(),
		}
		if err := emit(qa); err != nil {
			perr = err
			return false
		}
		return true
	})
	return perr
}
