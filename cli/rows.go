package cli

import (
	"strconv"

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

// deref reads through an optional value, giving the zero when there is none.
// The record keeps the difference between absent and zero and a table cell
// cannot, so this is where that distinction is deliberately given up.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// dateStr prints the parsed date when there is one and the line Amazon wrote
// when there is not. A table that printed an empty cell for a date the page
// plainly carried would be reporting a parser gap as missing data.
func dateStr(d *amz.Date) string {
	switch {
	case d == nil:
		return ""
	case d.Parsed != nil:
		return d.Parsed.Format("2006-01-02")
	default:
		return d.Raw
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// productRow builds the table row off the flat projection.
//
// A table is a flat view by definition, so the columns come from Flat rather
// than from a dozen nil checks written out longhand here. The JSON value stays
// the nested record: a caller who asked for a table wants columns, and a caller
// who asked for JSON wants everything.
func productRow(app *App, p amz.Product) Row {
	f := p.Flat()
	row := Row{
		Cols:  []string{"asin", "title", "brand", "price", "currency", "list_price", "savings_pct", "rating", "ratings_count", "reviews_count", "availability", "bought_past_month", "seller_name", "rank", "url"},
		Vals:  []string{f.ASIN, f.Title, f.Brand, f2(f.Price), f.Currency, f2(f.ListPrice), itoa(f.SavingsPct), f2(f.Rating), i64(f.RatingsCount), i64(f.ReviewsCount), f.Availability, f.BoughtPastMonth, f.SellerName, itoa(f.Rank), f.URL},
		Value: p, URL: p.URL,
	}
	if app != nil && app.Flat {
		row.Value = f
	}
	return row
}

// priceRow is the narrow record `amz price` emits.
//
// Its JSON value is a hand built map rather than the product, because a price
// watcher that stores the whole detail record every fifteen minutes is storing
// two megabytes to notice a two dollar change. The values are the Money pointers
// themselves, so a product with no price serialises price as null and not as
// zero, which is the difference between "not for sale" and "free".
func priceRow(p amz.Product) Row {
	f := p.Flat()
	// The marketplace is in the record because a price without one is not a
	// price. The same ASIN costs different amounts in every storefront Amazon
	// runs, so a price log that pools two of them is pooling two facts.
	v := map[string]any{
		"asin": p.ASIN, "marketplace": p.Marketplace, "currency": f.Currency,
		"availability": f.Availability, "fetched_at": p.FetchedAt,
	}
	if p.Offer != nil {
		v["price"] = p.Offer.Price
		v["list_price"] = p.Offer.ListPrice
	}
	return Row{
		Cols:  []string{"asin", "price", "currency", "list_price", "availability", "fetched_at"},
		Vals:  []string{f.ASIN, f2(f.Price), f.Currency, f2(f.ListPrice), f.Availability, f.FetchedAt.Format("2006-01-02T15:04:05Z")},
		Value: v,
		URL:   p.URL,
	}
}

func cardRow(c amz.Card) Row {
	return Row{
		Cols:  []string{"position", "rank", "asin", "title", "price", "list_price", "currency", "rating", "ratings_count", "badge", "prime", "sponsored", "kind", "url"},
		Vals:  []string{itoa(c.Position), itoa(c.Rank), c.ASIN, c.Title, f2(c.Price.Float()), f2(c.ListPrice.Float()), c.Price.Cur(), f2(deref(c.Rating)), i64(deref(c.RatingsCount)), c.Badge, boolStr(deref(c.Prime)), boolStr(c.Sponsored), c.Kind, c.URL},
		Value: c, URL: c.URL,
	}
}

func reviewRow(r amz.Review) Row {
	return Row{
		Cols:  []string{"review_id", "asin", "reviewer_name", "rating", "title", "verified_purchase", "helpful_votes", "country", "date", "url"},
		Vals:  []string{r.ReviewID, r.ASIN, r.ReviewerName, itoa(r.Rating), r.Title, boolStr(r.VerifiedPurchase), itoa(r.HelpfulVotes), r.Country, dateStr(r.Date), r.URL},
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

func offerRow(o amz.OfferListing) Row {
	return Row{
		Cols:  []string{"asin", "price", "currency", "condition", "seller_name", "seller_id", "fulfilled_by", "delivery", "is_buybox", "url"},
		Vals:  []string{o.ASIN, f2(o.Price), o.Currency, o.Condition, o.SellerName, o.SellerID, o.FulfilledBy, o.Delivery, boolStr(o.IsBuyBox), o.URL},
		Value: o, URL: o.URL,
	}
}

func chartRow(e amz.BestsellerEntry) Row {
	return Row{
		Cols:  []string{"rank", "asin", "title", "price", "currency", "rating", "ratings_count", "list_type", "url"},
		Vals:  []string{itoa(e.Rank), e.ASIN, e.Title, f2(e.Price.Float()), e.Price.Cur(), f2(deref(e.Rating)), i64(deref(e.RatingsCount)), e.ListType, e.URL},
		Value: e, URL: e.URL,
	}
}

// categoryRow drops the breadcrumb column the table used to carry. A /b page
// publishes no breadcrumb at all, so the column was an empty string on every row
// amz has ever printed, and the related nodes it does publish now come with the
// names Amazon gave them.
func categoryRow(c amz.Category) Row {
	return Row{
		Cols:  []string{"node_id", "name", "related", "top_asins", "url"},
		Vals:  []string{c.NodeID, c.Name, itoa(len(c.Related)), itoa(len(c.TopASINs)), c.URL},
		Value: c, URL: c.URL,
	}
}

// brandRow leads with the page id and the nav size, because those are what a
// caller does something with next. The followers column that used to sit here
// was always zero: Amazon renders a follow button with no figure on it, so the
// old table printed a 0 that read as a brand nobody follows.
func brandRow(b amz.Brand) Row {
	return Row{
		Cols:  []string{"slug", "page_id", "name", "nav", "featured", "url"},
		Vals:  []string{b.Slug, b.PageID, b.Name, itoa(len(b.Nav)), itoa(len(b.FeaturedASINs)), b.URL},
		Value: b, URL: b.URL,
	}
}

// sellerRow drops the negative percentage column, which was derived from a
// positive percentage that was itself always zero, and prints the rating count
// beside the rating. A 5.0 tells a reader nothing without the count under it.
func sellerRow(s amz.Seller) Row {
	return Row{
		Cols:  []string{"seller_id", "name", "rating", "rating_count", "positive_pct", "reviews", "url"},
		Vals:  []string{s.SellerID, s.Name, f2(s.Rating), i64(s.RatingCount), f2(s.PositivePct), itoa(len(s.Reviews)), s.URL},
		Value: s, URL: s.URL,
	}
}

// authorRow prints books against total_books so a truncated first page is
// visible in the table rather than only in the JSON. On the measured page those
// two read 70 and 135.
func authorRow(a amz.Author) Row {
	return Row{
		Cols:  []string{"slug", "page_id", "name", "books", "total_books", "url"},
		Vals:  []string{a.Slug, a.PageID, a.Name, itoa(len(a.Books)), itoa(a.TotalBooks), a.URL},
		Value: a, URL: a.URL,
	}
}

func dealRow(d amz.Deal) Row {
	return Row{
		Cols:  []string{"asin", "title", "deal_price", "list_price", "discount_pct", "badge", "currency", "url"},
		Vals:  []string{d.ASIN, d.Title, f2(d.DealPrice.Float()), f2(d.ListPrice.Float()), itoa(d.DiscountPct), d.Badge, d.DealPrice.Cur(), d.URL},
		Value: d, URL: d.URL,
	}
}

// refRow prints a reference to another entity with the name attached. A column
// of bare node ids made the reader spend a request each to find out that 565108
// is Laptops.
func refRow(r amz.Ref) Row {
	return Row{
		Cols:  []string{"kind", "id", "name", "url"},
		Vals:  []string{r.Kind, r.ID, r.Name, r.URL},
		Value: r, URL: r.URL,
	}
}

func stringRow(col, val string) Row {
	return Row{Cols: []string{col}, Vals: []string{val}, Value: map[string]string{col: val}, URL: val}
}
