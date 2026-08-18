package amz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/amz-cli/pkg/uri"
)

// searchPageCap is the highest page this tool will request, ever.
//
// Amazon serves 306 results per search and twenty pages to hold them, and it
// does not move: measured on 2026-08-17 the same query reported "over 40,000
// results" and stopped at 305-306 on page 20, and a two-brand query whose own
// total was 510 stopped in exactly the same place. Past page 20 the response is
// not an error and is not empty. It is six filler cards, no pagination strip,
// and a range whose start is computed from the page number while its end stays
// frozen at 306, so "321-306 of over 30,000 results" is what page 21 says about
// itself.
//
// A paginator that loops until the page comes back empty never stops on this
// site. --max-pages can lower this and nothing can raise it.
const searchPageCap = 20

// searchResultCeiling is how many results those twenty pages hold. It is printed
// rather than computed from: the range on the last page is the measurement, and
// this is here so the note can say the number without deriving it.
const searchResultCeiling = 306

// Ceiling is what stopped the walk, and whether anything was left behind.
type Ceiling struct {
	// Hit is true when the walk stopped because Amazon stopped, rather than
	// because the caller's limit or the result set ran out. It is the field a
	// consumer checks before believing they have everything.
	Hit bool `json:"hit"`
	// MaxPages is the highest page this walk was willing to request.
	MaxPages int `json:"max_pages,omitempty"`
	// Advertised is the last page the pagination strip offered, which is a claim
	// and not a promise: a department-filtered query advertised 176 pages on
	// 2026-08-17 and served its last result on page 20 like every other search.
	Advertised int `json:"advertised_last_page,omitempty"`
	// Reason is why the walk stopped, in the words of whatever stopped it.
	Reason string `json:"reason,omitempty"`
}

// SearchSummary is what one walk did, as opposed to what it found.
type SearchSummary struct {
	Query string `json:"query"`
	URI   string `json:"uri,omitempty"`
	// Pages is how many pages were fetched, Cards how many were read, and
	// Organic how many of those were not advertising.
	Pages   int `json:"pages"`
	Cards   int `json:"cards"`
	Organic int `json:"organic"`
	// Sponsored is the count of advertising placements skipped over. It is never
	// omitempty for the same reason Card.Sponsored is not.
	Sponsored int     `json:"sponsored"`
	Ceiling   Ceiling `json:"ceiling"`
	// Refinements is the vocabulary the first page offered, which is the input
	// to --partition and the reason this package hardcodes six codes and no
	// more.
	Refinements []RefineGroup `json:"available_refinements,omitempty"`
	Sorts       []SortOption  `json:"sorts,omitempty"`
	// Applied is the refinements Amazon confirmed it had applied.
	Applied []Refinement `json:"refinements,omitempty"`
	// Gaps records ranges the walk did not see, when the pages did not join up.
	Gaps []string `json:"gaps,omitempty"`
}

// Search streams result cards for a query, paging until the page says to stop.
func (c *Client) Search(ctx context.Context, query string, q SearchQuery, emit func(Card) error) error {
	_, err := c.SearchWalk(ctx, query, q, func(p SearchPage) error {
		for _, card := range p.Cards {
			if err := emit(card); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// SearchWalk pages a query and hands each page to onPage.
//
// The walk stops on whichever of four things comes first: the caller's limit,
// the last page the strip advertised, a range that stops advancing or starts
// running backwards, and the page cap. Only the middle two are Amazon's answer
// and all four are recorded, because "we read everything there was" and "we read
// everything we were allowed to" are different statements and a consumer needs
// to know which one they are holding.
func (c *Client) SearchWalk(ctx context.Context, query string, q SearchQuery, onPage func(SearchPage) error) (SearchSummary, error) {
	sum := SearchSummary{Query: query}
	if q.NeedsResolve() {
		resolved, err := c.ResolveRefinements(ctx, query, q)
		if err != nil {
			return sum, err
		}
		q = resolved
	}
	if uri, err := c.SearchURI(query, q); err == nil {
		sum.URI = uri
	}

	limit := searchPageCap
	if q.MaxPages > 0 && q.MaxPages < limit {
		limit = q.MaxPages
	}
	sum.Ceiling.MaxPages = limit

	start := q.StartPage
	if start < 1 {
		start = 1
	}
	page, prevEnd, count := start, 0, 0
	for {
		sp, err := c.fetchSearchPage(ctx, query, q, page)
		if err != nil {
			return sum, err
		}
		if page == start {
			sum.Refinements, sum.Sorts = sp.Refinements, sp.Sorts
			sum.Applied = sp.Applied
			if err := c.confirmRefinements(q, sp); err != nil {
				return sum, err
			}
		}
		sum.Ceiling.Advertised = max(sum.Ceiling.Advertised, sp.LastPage)

		// A range that runs backwards is Amazon's own tell that the walk ran
		// past the end, and it arrives with a grid full of near-duplicates of
		// page 20. Nothing on this page is new, so it is not reported.
		if sp.Inverted() {
			sum.Ceiling.Hit, sum.Ceiling.Reason = true, fmt.Sprintf(
				"page %d printed %d-%d, a range that runs backwards, which is how amazon says the walk is past the end",
				page, sp.From, sp.To)
			return sum, nil
		}
		if len(sp.Cards) == 0 {
			sum.Ceiling.Reason = fmt.Sprintf("page %d carried no result cards", page)
			return sum, nil
		}
		if prevEnd > 0 && sp.To > 0 && sp.To <= prevEnd {
			sum.Ceiling.Hit, sum.Ceiling.Reason = true, fmt.Sprintf(
				"the result range stopped advancing at %d, so page %d covers ground page %d already did",
				prevEnd, page, page-1)
			return sum, nil
		}
		if prevEnd > 0 && sp.From > prevEnd+1 {
			sum.Gaps = append(sum.Gaps, fmt.Sprintf("%d-%d", prevEnd+1, sp.From-1))
		}

		sum.Pages++
		sum.Cards += len(sp.Cards)
		sum.Sponsored += sp.SponsoredCount
		sum.Organic += len(sp.Cards) - sp.SponsoredCount
		if err := onPage(sp); err != nil {
			return sum, err
		}
		count += len(sp.Cards)
		if q.Limit > 0 && count >= q.Limit {
			sum.Ceiling.Reason = fmt.Sprintf("--limit %d reached", q.Limit)
			return sum, nil
		}
		if sp.To > 0 {
			prevEnd = sp.To
		}

		switch {
		case sp.NextPage > page:
			page = sp.NextPage
		case sp.LastPage > page:
			// The strip is on the page and offers a higher last page without
			// naming the next one, which happens on the sorted grid. Step by one
			// rather than guessing at the strip's arithmetic.
			page++
		case sp.LastPage > 0 || sp.CurrentPage > 0:
			sum.Reason(fmt.Sprintf("page %d is the last page the paginator offers", page))
			return sum, nil
		default:
			page++
		}
		if page > limit {
			// Hit means Amazon stopped the walk, and --max-pages is the caller
			// stopping it. Both end the run at the same statement and only one of
			// them means there was more to be had, so they are not reported as
			// the same thing.
			if limit < searchPageCap {
				sum.Reason(fmt.Sprintf("--max-pages %d reached, and amazon would have served up to %d pages",
					limit, searchPageCap))
				return sum, nil
			}
			sum.Ceiling.Hit = true
			sum.Ceiling.Reason = fmt.Sprintf(
				"amazon serves at most %d results per search over %d pages, whatever the corpus says",
				searchResultCeiling, searchPageCap)
			return sum, nil
		}
	}
}

// Reason records why the walk ended without claiming Amazon cut it short.
func (s *SearchSummary) Reason(why string) { s.Ceiling.Reason = why }

// fetchSearchPage gets one page and parses it.
func (c *Client) fetchSearchPage(ctx context.Context, query string, q SearchQuery, page int) (SearchPage, error) {
	u := c.SearchURL(query, q, page)
	body, src, err := c.GetSource(ctx, u, time.Hour)
	if err != nil {
		return SearchPage{}, err
	}
	sp, err := c.parseSearchPage(query, u, page, body)
	if err != nil {
		return SearchPage{}, err
	}
	c.record(ctx, &sp.Envelope, src)
	for i := range sp.Cards {
		sp.Cards[i].Envelope.Inherit(sp.Envelope)
	}
	return sp, nil
}

// SearchURI is the amz: identifier for a query and its refinements.
//
// It hashes SearchKey rather than the URL, so the same search asked for twice
// with the flags in a different order is one node, and a search with a
// refinement on it is a different node from the same words unrefined.
func (c *Client) SearchURI(query string, q SearchQuery) (string, error) {
	return uri.Search(c.mkt.Slug, SearchKey(query, q, c.mkt))
}

// confirmRefinements checks that Amazon applied what was asked for.
//
// Amazon does not reject an rh group it does not understand. It drops the term
// and serves the unrefined result set with a 200 and a full grid, so the failure
// mode this guards against is a filtered search that quietly is not one. The
// check is against the sidebar of the page that came back rather than against a
// list of codes, because what matters is not whether the code exists somewhere
// on Amazon but whether this search honoured it.
func (c *Client) confirmRefinements(q SearchQuery, sp SearchPage) error {
	byCode := map[string]RefineGroup{}
	for _, g := range sp.Refinements {
		byCode[g.Code] = g
	}
	for _, want := range q.refinements(c.mkt) {
		// p_36 is a range rather than a value, and a range is never listed in
		// the sidebar as a thing that is switched on. There is nothing on the
		// page to check it against, and saying so is better than a check that
		// passes because it looked at nothing.
		if want.Group == "p_36" {
			continue
		}
		g, ok := byCode[want.Group]
		if !ok {
			return fmt.Errorf("%w: %s is not among the %d groups this query offers, and amazon serves an unfiltered page rather than an error when it is sent one it does not know",
				ErrRefinementUnoffered, want.Group, len(sp.Refinements))
		}
		applied := map[string]bool{}
		for _, v := range g.Applied() {
			applied[v] = true
		}
		for _, v := range want.Values {
			if !applied[v] {
				return fmt.Errorf("%w: sent %s:%s and the page came back without it applied, so these results are not filtered by it",
					ErrRefinementIgnored, want.Group, v)
			}
		}
	}
	return nil
}

// ResolveRefinements turns the name-shaped requests into rh terms.
//
// This costs one request, once, and it is the request that makes --brand
// "Logitech" mean anything: brands go into rh as numeric ids and there is no
// table of them anywhere in this binary. The resolved ids are on the returned
// query, and `amz search -v` prints them so a script can pin the id and skip
// this lookup next time.
func (c *Client) ResolveRefinements(ctx context.Context, query string, q SearchQuery) (SearchQuery, error) {
	lookup := q
	lookup.Brand, lookup.Seller, lookup.Stars, lookup.Condition = "", "", 0, ""
	sp, err := c.fetchSearchPage(ctx, query, lookup, 1)
	if err != nil {
		return q, err
	}
	groups := map[string]RefineGroup{}
	for _, g := range sp.Refinements {
		groups[g.Code] = g
	}

	add := func(code, what, want string, match func(RefineValue) bool) error {
		g, ok := groups[code]
		if !ok {
			return fmt.Errorf("%w: %s does not offer a %s filter (%s), and sending one anyway would return an unfiltered page that claims to be filtered",
				ErrRefinementUnoffered, query, what, code)
		}
		for _, v := range g.Values {
			if match(v) {
				q.Refine = append(q.Refine, Refinement{
					Group: code, Label: g.Label,
					Values: []string{v.ID}, ValueLabels: []string{v.Label},
				})
				return nil
			}
		}
		return fmt.Errorf("%w: %q is not among the %d %s values this query offers (%s)",
			ErrRefinementUnoffered, want, len(g.Values), what, refineValueList(g))
	}

	if q.Brand != "" {
		if err := add("p_123", "brand", q.Brand, func(v RefineValue) bool {
			return v.ID == q.Brand || strings.EqualFold(v.Label, q.Brand)
		}); err != nil {
			return q, err
		}
	}
	if q.Seller != "" {
		if err := add("p_6", "seller", q.Seller, func(v RefineValue) bool {
			return v.ID == q.Seller || strings.EqualFold(v.Label, q.Seller)
		}); err != nil {
			return q, err
		}
	}
	if q.Stars > 0 {
		// The star ids are not guessable and the guess in v0.2.1 was wrong: it
		// sent p_72:1248882011 where this marketplace's sidebar offers
		// 1248879011 for the same four stars and up. Matching on the label
		// Amazon printed is the only form of this that cannot silently drift.
		want := fmt.Sprintf("%d Stars", q.Stars)
		if err := add("p_72", "star rating", want, func(v RefineValue) bool {
			return strings.HasPrefix(strings.ToLower(v.Label), strings.ToLower(want))
		}); err != nil {
			return q, err
		}
	}
	if q.Condition != "" {
		if err := add("p_n_condition-type", "condition", q.Condition, func(v RefineValue) bool {
			return strings.EqualFold(v.Label, q.Condition) ||
				strings.HasPrefix(strings.ToLower(v.Label), strings.ToLower(q.Condition))
		}); err != nil {
			return q, err
		}
	}
	q.Brand, q.Seller, q.Stars, q.Condition = "", "", 0, ""
	return q, nil
}

// Refinements reads the vocabulary a query offers, without walking it.
//
// This is one page. The refinement sidebar is a property of the query rather
// than of the page number, so paging to twenty to collect filters would be
// nineteen requests spent on the same answer.
func (c *Client) Refinements(ctx context.Context, query string, q SearchQuery) ([]RefineGroup, error) {
	sp, err := c.FetchSearchPage(ctx, query, q, 1)
	if err != nil {
		return nil, err
	}
	return sp.Refinements, nil
}

// FetchSearchPage reads one result page and returns it whole, envelope included.
//
// Refinements throws everything but the sidebar away, which is the right shape
// for a caller that wants the vocabulary. It is the wrong shape for `amz refine`
// over the wire, where the envelope is the only thing saying which surface the
// vocabulary came off and when, so this returns the page and lets the caller
// pick.
func (c *Client) FetchSearchPage(ctx context.Context, query string, q SearchQuery, page int) (SearchPage, error) {
	if q.NeedsResolve() {
		resolved, err := c.ResolveRefinements(ctx, query, q)
		if err != nil {
			return SearchPage{}, err
		}
		q = resolved
	}
	return c.fetchSearchPage(ctx, query, q, page)
}

// Departments lists the search scopes this marketplace offers.
//
// The aliases come off the dropdown on a live page and not from a table here,
// because the spellings differ by storefront and by what Amazon thinks the
// client's country is. A search for i=electronics that silently means nothing is
// exactly the shape of failure this whole milestone exists to remove.
//
// There is no department cache of its own. The page this reads is cached like
// every other fetch, under a key built from its URL, and the URL starts with the
// marketplace host, so the list is already per marketplace and a second cache
// would only add a way for the two to disagree.
func (c *Client) Departments(ctx context.Context) ([]Department, error) {
	sp, err := c.fetchSearchPage(ctx, "amazon", SearchQuery{}, 1)
	if err != nil {
		return nil, err
	}
	return sp.Departments, nil
}

func refineValueList(g RefineGroup) string {
	labels := make([]string, 0, len(g.Values))
	for _, v := range g.Values {
		labels = append(labels, v.Label)
		if len(labels) == 8 {
			labels = append(labels, "...")
			break
		}
	}
	return strings.Join(labels, ", ")
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
	if v, ok := e.Value("refinements"); ok {
		sp.Refinements, _ = v.([]RefineGroup)
	}
	if v, ok := e.Value("sorts"); ok {
		sp.Sorts, _ = v.([]SortOption)
	}
	if v, ok := e.Value("departments"); ok {
		sp.Departments, _ = v.([]Department)
	}
	for _, s := range sp.Sorts {
		if s.Selected {
			sp.Sort = s.Value
		}
	}
	sp.Applied = appliedRefinements(sp.Refinements)

	d.Each("s-search-result", func(_ int, r Region) {
		if cd, ok := c.readCard(d, r, card); ok {
			sp.Cards = append(sp.Cards, cd)
		}
	})

	// The page size is read off this page rather than derived from the page
	// number. Sixteen results per page unrefined and twenty-four once a
	// department or a refinement narrows it, measured on the same query, so
	// offset arithmetic from a page number is wrong on any search anybody would
	// actually run.
	if sp.From > 0 && sp.To >= sp.From {
		sp.PageSize = sp.To - sp.From + 1
	}
	sp.SponsoredCount = countSponsored(sp.Cards)
	numberOrganic(sp.Cards, sp.From)

	e.MarkUnread(claimedRegions(append(pageFields, card...)))
	sp.Envelope = e.Envelope()
	sp.Envelope.AgentMap = d.AgentMap()
	return sp, nil
}

// appliedRefinements is the rh this page says it is under, read from the sidebar
// rather than echoed back from the request.
func appliedRefinements(groups []RefineGroup) []Refinement {
	var out []Refinement
	for _, g := range groups {
		vals := g.Applied()
		if len(vals) == 0 {
			continue
		}
		r := Refinement{Group: g.Code, Label: g.Label, Values: vals}
		for _, id := range vals {
			if v, ok := g.Value(id); ok {
				r.ValueLabels = append(r.ValueLabels, v.Label)
			}
		}
		out = append(out, r)
	}
	return out
}

func countSponsored(cards []Card) int {
	n := 0
	for _, c := range cards {
		if c.Sponsored {
			n++
		}
	}
	return n
}

// numberOrganic gives each organic card its position in the result set.
//
// The position comes from the range the page printed and the order the organic
// cards arrived in, never from the card's index in the grid. A page that says
// 1-16 carries twenty-two cards, because six of them are advertising and the
// range does not count them, so the sixteenth card in the grid is not the
// sixteenth result. Sponsored cards are left at zero: an ad has no position in
// a result set, and a number there would be a made-up one.
func numberOrganic(cards []Card, from int) {
	if from < 1 {
		from = 1
	}
	n := from
	for i := range cards {
		if cards[i].Sponsored {
			cards[i].Position = 0
			continue
		}
		cards[i].Position = n
		n++
	}
}

// readCard reads one result card. The second result reports whether the node was
// a product at all: the grid carries placeholder slots whose data-asin is a
// widget name rather than an ASIN.
func (c *Client) readCard(d *Doc, r Region, fields []Field) (Card, bool) {
	asin := r.Attr("data-asin")
	if !isASIN(asin) {
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
		Price:           money(e, "price", c.mkt),
		ListPrice:       money(e, "list_price", c.mkt),
		Rating:          f64OrNil(e.Float("rating")),
		RatingsCount:    i64OrNil(e.Int("ratings_count")),
		Image:           upgradeImage(e.Str("image")),
		Badge:           e.Str("badge"),
		Sponsored:       e.Bool("sponsored") || sponsoredCard(r),
		BoughtPastMonth: e.Str("bought_past_month"),
		Delivery:        e.Str("delivery"),
	}
	// Prime is a pointer and it is set only when the card carried the badge
	// region at all. A search card that Amazon did not draw a Prime icon on is
	// not a statement that the item is ineligible, and false would read as one.
	if e.Has("prime") {
		card.Prime = boolPtr(e.Bool("prime"))
	}
	card.Envelope = e.Envelope()
	return card, true
}

// sponsoredCard is the second and third of the three tells.
//
// The label is the one a person sees and it is drawn by script on some layouts,
// so a card whose label has not rendered yet still has AdHolder in its class
// list and still has no data-index of its own. Any one of the three is enough,
// because the cost of missing one is an advertisement filed as a result and
// there is no way to notice that later.
func sponsoredCard(r Region) bool {
	class := r.Attr("class")
	if strings.Contains(class, "AdHolder") {
		return true
	}
	return r.Attr("data-index") == "" && r.Find(".puis-label-popover-default").Length() > 0
}
