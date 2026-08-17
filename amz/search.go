package amz

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SearchQuery holds the refinements for a catalog search.
type SearchQuery struct {
	Sort       string // relevance|price-asc|price-desc|review|newest
	MinPrice   int
	MaxPrice   int
	MinRating  int
	Prime      bool
	Brand      string
	Department string
	StartPage  int
	Limit      int
}

var sortMap = map[string]string{
	"relevance":  "relevanceblender",
	"price-asc":  "price-asc-rank",
	"price-desc": "price-desc-rank",
	"review":     "review-rank",
	"newest":     "date-desc-rank",
}

// SearchURL builds the /s URL for a query and page.
func (c *Client) SearchURL(query string, q SearchQuery, page int) string {
	v := url.Values{}
	v.Set("k", query)
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if s, ok := sortMap[q.Sort]; ok {
		v.Set("s", s)
	}
	var rh []string
	if q.MinPrice > 0 || q.MaxPrice > 0 {
		rh = append(rh, "p_36:"+strconv.Itoa(q.MinPrice*100)+"-"+priceHi(q.MaxPrice))
	}
	if q.MinRating > 0 && q.MinRating <= 4 {
		// p_72 review-rating refinements: 1247-... ; use the documented "4 stars & up" style id.
		ids := map[int]string{4: "1248882011", 3: "1248883011", 2: "1248884011", 1: "1248885011"}
		if id, ok := ids[q.MinRating]; ok {
			rh = append(rh, "p_72:"+id)
		}
	}
	if q.Prime {
		rh = append(rh, "p_85:2470955011")
	}
	if len(rh) > 0 {
		v.Set("rh", strings.Join(rh, ","))
	}
	if q.Department != "" {
		v.Set("i", q.Department)
	}
	return c.BaseURL() + "/s?" + v.Encode()
}

func priceHi(hi int) string {
	if hi <= 0 {
		return ""
	}
	return strconv.Itoa(hi * 100)
}

// searchPageCap is how far Search will page when nothing on the page says to
// stop. Amazon caps navigation at 20 pages on an unrefined query, and the strip
// says so, so this is a backstop rather than the rule.
const searchPageCap = 20

// Search streams result cards for a query, paging until Limit is reached.
//
// Paging stops when the page says it has run out rather than when a counter
// says so. The strip stops offering a next page, the result bar starts printing
// a backwards range, or the grid comes back empty, and any of the three ends the
// walk. See SearchPage.Exhausted.
func (c *Client) Search(ctx context.Context, query string, q SearchQuery, emit func(Card) error) error {
	page := q.StartPage
	if page < 1 {
		page = 1
	}
	count := 0
	for {
		u := c.SearchURL(query, q, page)
		body, err := c.Get(ctx, u, time.Hour)
		if err != nil {
			return err
		}
		sp, err := c.parseSearchPage(query, u, page, body)
		if err != nil {
			return err
		}
		if sp.Exhausted() {
			return nil
		}
		for _, card := range sp.Cards {
			count++
			if card.Position == 0 {
				card.Position = count
			}
			if err := emit(card); err != nil {
				return err
			}
			if q.Limit > 0 && count >= q.Limit {
				return nil
			}
		}
		switch {
		case sp.NextPage > page:
			page = sp.NextPage
		case sp.LastPage > 0 || sp.CurrentPage > 0:
			// The strip is on the page and it offers nothing after this one,
			// which is Amazon saying the walk is over.
			return nil
		default:
			// No strip at all. Keep going on the grid alone and let an empty
			// page or a backwards range end it, under the cap.
			page++
		}
		if page-q.StartPage > searchPageCap {
			return nil
		}
	}
}

// parseSearchPage reads one /s page: the page's own counts, then every card.
//
// The page and the cards get separate extractors. A card that is missing its
// price slot is a fact about that card, and folding sixteen cards' misses into
// one envelope would say only that some card somewhere was short a price.
func (c *Client) parseSearchPage(query, url string, page int, body []byte) (SearchPage, error) {
	d, err := ParseDoc(FamilySearch, body)
	if err != nil {
		return SearchPage{}, err
	}
	pageFields, card := searchPageFields(), cardFields(c.BaseURL())

	e := NewExtractor(d)
	e.RunFields(pageFields)

	sp := SearchPage{
		Query:       query,
		URL:         url,
		Page:        page,
		QueryEcho:   e.Str("query_echo"),
		From:        int(e.Int("result_from")),
		To:          int(e.Int("result_to")),
		Total:       e.Int("result_total"),
		Approx:      e.Bool("result_total_approx"),
		CurrentPage: int(e.Int("page_current")),
		NextPage:    int(e.Int("page_next")),
		LastPage:    int(e.Int("page_last")),
	}

	d.Each("s-search-result", func(_ int, r Region) {
		if cd, ok := c.readCard(d, r, card); ok {
			sp.Cards = append(sp.Cards, cd)
		}
	})

	e.MarkUnread(claimedRegions(append(pageFields, card...)))
	sp.Envelope = e.Envelope()
	sp.Envelope.AgentMap = d.AgentMap()
	return sp, nil
}

// readCard reads one result card. The second result reports whether the node was
// a product at all: the grid carries placeholder slots whose data-asin is a
// widget name rather than an ASIN.
func (c *Client) readCard(d *Doc, r Region, fields []Field) (Card, bool) {
	asin := r.Attr("data-asin")
	if !bareASIN.MatchString(asin) {
		return Card{}, false
	}
	e := NewExtractor(d)
	e.RunIn(r, fields)

	card := Card{
		ASIN:            asin,
		Kind:            "search",
		URL:             c.ProductURL(asin),
		Position:        int(e.Int("position")),
		Title:           e.Str("title"),
		Price:           e.Float("price"),
		ListPrice:       e.Float("list_price"),
		Currency:        e.Str("currency"),
		Rating:          e.Float("rating"),
		RatingsCount:    e.Int("ratings_count"),
		Image:           upgradeImage(e.Str("image")),
		Badge:           e.Str("badge"),
		Prime:           e.Bool("prime"),
		Sponsored:       e.Bool("sponsored"),
		BoughtPastMonth: e.Str("bought_past_month"),
		Delivery:        e.Str("delivery"),
	}
	if card.Currency == "" {
		card.Currency = c.mkt.Currency
	}
	card.Envelope = e.Envelope()
	return card, true
}
