package cli

import (
	"strconv"
	"strings"

	"github.com/tamnd/amz-cli/amz"
)

func f2(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func i64(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

func itoa(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func productRow(p amz.Product) Row {
	return Row{
		Cols:  []string{"asin", "title", "brand", "price", "currency", "list_price", "rating", "ratings_count", "reviews_count", "availability", "seller_name", "rank", "url"},
		Vals:  []string{p.ASIN, p.Title, p.Brand, f2(p.Price), p.Currency, f2(p.ListPrice), f2(p.Rating), i64(p.RatingsCount), i64(p.ReviewsCount), p.Availability, p.SellerName, itoa(p.Rank), p.URL},
		Value: p, URL: p.URL,
	}
}

func priceRow(p amz.Product) Row {
	return Row{
		Cols: []string{"asin", "price", "currency", "list_price", "availability", "fetched_at"},
		Vals: []string{p.ASIN, f2(p.Price), p.Currency, f2(p.ListPrice), p.Availability, p.FetchedAt.Format("2006-01-02T15:04:05Z")},
		Value: map[string]any{
			"asin": p.ASIN, "price": p.Price, "currency": p.Currency,
			"list_price": p.ListPrice, "availability": p.Availability, "fetched_at": p.FetchedAt,
		},
		URL: p.URL,
	}
}

func cardRow(c amz.Card) Row {
	return Row{
		Cols:  []string{"position", "rank", "asin", "title", "price", "currency", "rating", "ratings_count", "sponsored", "kind", "url"},
		Vals:  []string{itoa(c.Position), itoa(c.Rank), c.ASIN, c.Title, f2(c.Price), c.Currency, f2(c.Rating), i64(c.RatingsCount), boolStr(c.Sponsored), c.Kind, c.URL},
		Value: c, URL: c.URL,
	}
}

func reviewRow(r amz.Review) Row {
	return Row{
		Cols:  []string{"review_id", "asin", "reviewer_name", "rating", "title", "verified_purchase", "helpful_votes", "country", "date", "url"},
		Vals:  []string{r.ReviewID, r.ASIN, r.ReviewerName, itoa(r.Rating), r.Title, boolStr(r.VerifiedPurchase), itoa(r.HelpfulVotes), r.Country, r.Date, r.URL},
		Value: r, URL: r.URL,
	}
}

func qaRow(q amz.QA) Row {
	return Row{
		Cols:  []string{"qa_id", "asin", "question", "answer", "url"},
		Vals:  []string{q.QAID, q.ASIN, q.Question, q.Answer, q.URL},
		Value: q, URL: q.URL,
	}
}

func offerRow(o amz.Offer) Row {
	return Row{
		Cols:  []string{"asin", "price", "currency", "condition", "seller_name", "seller_id", "fulfilled_by", "delivery", "is_buybox", "url"},
		Vals:  []string{o.ASIN, f2(o.Price), o.Currency, o.Condition, o.SellerName, o.SellerID, o.FulfilledBy, o.Delivery, boolStr(o.IsBuyBox), o.URL},
		Value: o, URL: o.URL,
	}
}

func chartRow(e amz.BestsellerEntry) Row {
	return Row{
		Cols:  []string{"rank", "asin", "title", "price", "currency", "rating", "ratings_count", "list_type", "url"},
		Vals:  []string{itoa(e.Rank), e.ASIN, e.Title, f2(e.Price), e.Currency, f2(e.Rating), i64(e.RatingsCount), e.ListType, e.URL},
		Value: e, URL: e.URL,
	}
}

func categoryRow(c amz.Category) Row {
	return Row{
		Cols:  []string{"node_id", "name", "breadcrumb", "children", "top_asins", "url"},
		Vals:  []string{c.NodeID, c.Name, strings.Join(c.Breadcrumb, " > "), itoa(len(c.ChildNodeIDs)), itoa(len(c.TopASINs)), c.URL},
		Value: c, URL: c.URL,
	}
}

func brandRow(b amz.Brand) Row {
	return Row{
		Cols:  []string{"slug", "name", "followers", "featured", "url"},
		Vals:  []string{b.Slug, b.Name, itoa(b.FollowerCount), itoa(len(b.FeaturedASINs)), b.URL},
		Value: b, URL: b.URL,
	}
}

func sellerRow(s amz.Seller) Row {
	return Row{
		Cols:  []string{"seller_id", "name", "rating", "rating_count", "positive_pct", "negative_pct", "url"},
		Vals:  []string{s.SellerID, s.Name, s.Rating, itoa(s.RatingCount), f2(s.PositivePct), f2(s.NegativePct), s.URL},
		Value: s, URL: s.URL,
	}
}

func authorRow(a amz.Author) Row {
	return Row{
		Cols:  []string{"slug", "name", "followers", "books", "website", "url"},
		Vals:  []string{a.Slug, a.Name, itoa(a.FollowerCount), itoa(len(a.BookASINs)), a.Website, a.URL},
		Value: a, URL: a.URL,
	}
}

func dealRow(d amz.Deal) Row {
	return Row{
		Cols:  []string{"asin", "title", "deal_price", "list_price", "discount_pct", "badge", "currency", "url"},
		Vals:  []string{d.ASIN, d.Title, f2(d.DealPrice), f2(d.ListPrice), itoa(d.DiscountPct), d.Badge, d.Currency, d.URL},
		Value: d, URL: d.URL,
	}
}

func stringRow(col, val string) Row {
	return Row{Cols: []string{col}, Vals: []string{val}, Value: map[string]string{col: val}, URL: val}
}
