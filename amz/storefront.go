package amz

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The store family, and where a storefront actually keeps its data.
//
// Measured on 2026-08-17 against /stores/Skullcandy/page/9F16B940, the Michael
// Lewis author page, and /sp?seller=A2L77EE7U53NWQ. Four things came out of that
// and the first one is a bug the old parser shipped.
//
// The old brand and author parsers collected every a[href*='/dp/'] on the page.
// On all three captures the only /dp/ links in the DOM outside the content are
// B07984JN3L and B0CHTVMXZJ, which are the Amazon Business Card and Reload Your
// Balance, and they are in the footer of every page on the site. So the author
// page reported three books by Michael Lewis and two of them were Amazon's own
// credit card and gift card top-up. The brand page reported four Skullcandy
// products and the same two were among them. The seller page carries those two
// and nothing else. This is worse than returning nothing, because a wrong ASIN
// in a bibliography looks exactly like a right one.
//
// The real data is in payloads. A stores page is a React app that inlines one
// `var config = {...}` per widget, twelve of them on the author page and eleven
// on the brand page, each naming itself with widgetType, sectionType and
// widgetId. That is rung 2 and it is a much better contract than the DOM: the
// author page's product grid renders ProductGrid__no-matches in the served HTML
// while its payload carries 70 full product records and says the author has 135.
//
// A brand storefront is a navigation page rather than a catalogue. Skullcandy's
// landing page holds two ASINs in 508 KB, ten editorial rows of images and
// video, and a nav tree naming seven sub-pages. Reporting the two ASINs as the
// brand's products would be a smaller lie than the footer one and still a lie,
// so what this returns is the nav tree, which is what a crawl needs to find the
// products.
//
// A seller profile is not a stores page at all. It has one `var config` with no
// widget in it, no data-testid anywhere, and a plain server rendered DOM whose
// sections are named by id^="page-section-". It gets its own vocabulary below.
//
// See notes/Spec/3007/02_extraction.md section 11.

// storeConfigAnchor is the registration every stores widget writes before its
// own payload. Unlike ImageBlockATF it is not a name, it is a variable
// declaration repeated once per widget, so the identity is inside the object.
const storeConfigAnchor = "var config = "

// StoreWidget is one widget of a stores page, as the page described itself.
//
// The three identifiers are not interchangeable and the difference matters when
// deciding which one to key on. widgetType is the component ("ProductGrid"),
// sectionType is the component in its role on this page
// ("AuthorProductGrid"), and widgetId is an identifier for this instance. On the
// author page widgetId is author-productgrid-B000APZ33E and carries the author,
// but on the brand page it is fiq6kt7q7p and carries nothing at all. So lookups
// go through WidgetType and SectionType, and WidgetID is recorded as identity
// rather than indexed as a name. This is the same distinction that the chart and
// browse families both got wrong before they were measured.
type StoreWidget struct {
	WidgetID    string `json:"widget_id,omitempty"`
	WidgetType  string `json:"widget_type,omitempty"`
	SectionType string `json:"section_type,omitempty"`
	// Content is the widget's data object, for the widgets that carry one.
	Content json.RawMessage `json:"-"`
	// Tiles is the widget's item list, for the editorial rows that carry one.
	Tiles []json.RawMessage `json:"-"`
	// PageContext is what the widget says about the page it is on rather than
	// about itself, and every widget on a page repeats the same copy of it. It
	// holds the author id, the store UUID and the storefront's root path, which
	// are the three identifiers a crawl needs and none of which are anywhere
	// else in the served HTML.
	PageContext json.RawMessage `json:"-"`
	// Size is the payload's length in bytes, kept so a report can say which
	// widget on a page is worth reading without reading it.
	Size int `json:"size,omitempty"`
}

// readStoreWidgets returns every widget payload on a stores page, in document
// order. A page that is not a stores page yields none, which is the answer for
// a seller profile.
func readStoreWidgets(body []byte) []StoreWidget {
	objs := FindJSObjects(string(body), storeConfigAnchor)
	out := make([]StoreWidget, 0, len(objs))
	for _, o := range objs {
		w := StoreWidget{
			WidgetID:    o.String("widgetId"),
			WidgetType:  o.String("widgetType"),
			SectionType: o.String("sectionType"),
		}
		if c, ok := o.Get("content"); ok {
			w.Content, w.Size = c, len(c)
		}
		if t, ok := o.Get("tiles"); ok {
			_ = json.Unmarshal(t, &w.Tiles)
			w.Size += len(t)
		}
		if p, ok := o.Get("pageContext"); ok {
			w.PageContext = p
		}
		if w.WidgetType == "" && w.SectionType == "" && len(w.Tiles) == 0 {
			// A config with no widget in it is the page's own bootstrap, which
			// every page has and only a stores page follows with widgets.
			continue
		}
		out = append(out, w)
	}
	return out
}

// asinValueRe matches an ASIN where a payload states one, which is as the value
// of the key "asin" and nowhere else.
//
// Matching a bare ten character run instead would be the same mistake the DOM
// scan made in a different place: a storefront payload is full of ten character
// tokens that are widget ids, image hashes and marketplace ids, and half of what
// a loose pattern returns would be those. An ASIN can begin with a digit and end
// with an X, which is why books like 030796695X are inside the character class
// rather than outside it.
var asinValueRe = regexp.MustCompile(`"asin"\s*:\s*"([A-Z0-9]{10})"`)

// payloadASINs is every ASIN the given payload fragments state.
func payloadASINs(raws ...json.RawMessage) []string {
	var out []string
	for _, r := range raws {
		for _, m := range asinValueRe.FindAllStringSubmatch(string(r), -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// widget returns the first widget of a given type, by widgetType then
// sectionType, since the brand page names its rows only by the former.
func storeWidget(ws []StoreWidget, name string) (StoreWidget, bool) {
	for _, w := range ws {
		if w.WidgetType == name || w.SectionType == name {
			return w, true
		}
	}
	return StoreWidget{}, false
}

// storePageContext reads one key of the page context, from the first widget
// that carries one.
//
// Every widget repeats the same page context, so the first is as good as any,
// and taking the first is what makes this cheap on a page with a 562 KB grid on
// it. A widget with no page context is skipped rather than ending the search,
// because the bootstrap config sits ahead of the real widgets on some pages.
func storePageContext(ws []StoreWidget, key string) string {
	for _, w := range ws {
		if len(w.PageContext) == 0 {
			continue
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(w.PageContext, &m) != nil {
			continue
		}
		var s string
		if json.Unmarshal(m[key], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// unmarshalContent decodes a widget's content object into v.
func (w StoreWidget) unmarshalContent(v any) bool {
	return len(w.Content) > 0 && json.Unmarshal(w.Content, v) == nil
}

// StoreNavPage is one page of a storefront's navigation tree.
//
// The tree is the useful half of a brand storefront. Amazon publishes it whole,
// with parent and children on every node, so a crawl can walk a brand's pages
// without guessing at URLs.
type StoreNavPage struct {
	PageID   string   `json:"page_id"`
	Title    string   `json:"title,omitempty"`
	URL      string   `json:"url,omitempty"`
	Parent   string   `json:"parent,omitempty"`
	Children []string `json:"children,omitempty"`
	// Level is the depth Amazon assigned, 1 for the storefront home.
	Level int `json:"level,omitempty"`
}

type rawNavPage struct {
	PageID   string   `json:"pageId"`
	Title    string   `json:"title"`
	Href     string   `json:"href"`
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
	Level    int      `json:"level"`
}

// storeNav reads the nav tree out of the Header widget.
//
// The nav arrives as a map keyed by page id rather than as a list, so document
// order is not available and the pages are sorted by level and then title. A map
// iterated in Go's order would put a different page first on every run and make
// two captures of one storefront look like two different storefronts.
func storeNav(ws []StoreWidget) []StoreNavPage {
	w, ok := storeWidget(ws, "Header")
	if !ok {
		return nil
	}
	var content struct {
		Nav map[string]rawNavPage `json:"nav"`
	}
	if !w.unmarshalContent(&content) {
		return nil
	}
	out := make([]StoreNavPage, 0, len(content.Nav))
	for id, p := range content.Nav {
		if p.PageID == "" {
			p.PageID = id
		}
		out = append(out, StoreNavPage{
			PageID: p.PageID, Title: collapseSpace(p.Title), URL: p.Href,
			Parent: p.Parent, Children: p.Children, Level: p.Level,
		})
	}
	sortNav(out)
	return out
}

func sortNav(p []StoreNavPage) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && navLess(p[j], p[j-1]); j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

func navLess(a, b StoreNavPage) bool {
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	if a.Title != b.Title {
		return a.Title < b.Title
	}
	return a.PageID < b.PageID
}

// StoreProduct is one product from a stores product grid payload.
//
// The payload is far richer than any tile the DOM would have carried: it names
// the binding, the merchant behind the offer, the availability, and the award
// Amazon hung on the book. Those are kept because they are published, and
// because the grid is the only place on the page they appear.
type StoreProduct struct {
	ASIN     string  `json:"asin"`
	Title    string  `json:"title,omitempty"`
	URL      string  `json:"url,omitempty"`
	Image    string  `json:"image,omitempty"`
	Binding  string  `json:"binding,omitempty"`
	Price    float64 `json:"price,omitempty"`
	Currency string  `json:"currency,omitempty"`
	// PriceLabel is what Amazon called the price, "Kindle Price:" rather than a
	// bare number. A book has several prices at once and the label is which one.
	PriceLabel   string  `json:"price_label,omitempty"`
	Merchant     string  `json:"merchant,omitempty"`
	MerchantID   string  `json:"merchant_id,omitempty"`
	Availability string  `json:"availability,omitempty"`
	Rating       float64 `json:"rating,omitempty"`
	RatingsCount int64   `json:"ratings_count,omitempty"`
	// Accolade is an award Amazon publishes on the tile, "Best Books of the Year
	// 2025", with the badge type that qualifies it. 44 of the 70 products on the
	// measured page carried one.
	Accolade     string `json:"accolade,omitempty"`
	AccoladeType string `json:"accolade_type,omitempty"`
	// BestSellerRank and BestSellerIn are the chart badge, "#1 Best Seller" in
	// "Bonds Investing". This is a rank inside a named category rather than an
	// overall one, so the category travels with the number or the number means
	// nothing.
	BestSellerRank int    `json:"best_seller_rank,omitempty"`
	BestSellerIn   string `json:"best_seller_in,omitempty"`
	// Series is the book series, with this book's place in it.
	Series         string `json:"series,omitempty"`
	SeriesPosition int    `json:"series_position,omitempty"`
	SeriesTotal    int    `json:"series_total,omitempty"`
	// Contributors are the people credited, with the role Amazon gave each. A
	// name without its role would make an editor look like an author, which on
	// an author page is exactly the distinction being asked about.
	Contributors []StoreContributor `json:"contributors,omitempty"`
	// Editions are the other bindings of this work, which is how a Kindle
	// edition points at its hardcover and its audiobook.
	Editions []StoreEdition `json:"editions,omitempty"`
	// OfferCount is how many offers exist for this product, with the price range
	// across them. One offer and a range that spans nothing is the common case;
	// more than one means the single price above is one of several.
	OfferCount int     `json:"offer_count,omitempty"`
	MinPrice   float64 `json:"min_price,omitempty"`
	MaxPrice   float64 `json:"max_price,omitempty"`
	// ProductType and DisplayGroup are Amazon's own classification, ABIS_EBOOKS
	// and eBooks, which is a better answer to "what kind of thing is this" than
	// anything inferable from the title.
	ProductType  string `json:"product_type,omitempty"`
	DisplayGroup string `json:"display_group,omitempty"`
}

// StoreContributor is one credited person and the role Amazon gave them.
type StoreContributor struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	URL  string `json:"url,omitempty"`
}

// StoreEdition is one other binding of the same work.
type StoreEdition struct {
	ASIN    string `json:"asin,omitempty"`
	Binding string `json:"binding,omitempty"`
}

// StoreGrid is a stores product grid: what it shipped, and what it says exists.
//
// Shipped and Total are both kept and they disagree on the measured capture, 70
// against 135. A record that reported 70 books would be quietly wrong about an
// author's bibliography, and the payload states the real figure, so both travel
// together the same way the twister carries shipped and total variants.
type StoreGrid struct {
	Products []StoreProduct `json:"products,omitempty"`
	// ASINList is the identifiers the grid knows about, which runs ahead of the
	// products it shipped: 112 against 70 on the measured page.
	ASINList []string `json:"asin_list,omitempty"`
	Total    int      `json:"total,omitempty"`
	// SortOptions and Languages are the filters the grid offers, which is the
	// page telling a crawler how it can be re-queried.
	SortOptions []string `json:"sort_options,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	// PageSize is the page size the grid asked for, which bounds what one fetch
	// can return.
	PageSize int `json:"page_size,omitempty"`
}

// Shipped is how many full product records the page actually carried.
func (g *StoreGrid) Shipped() int {
	if g == nil {
		return 0
	}
	return len(g.Products)
}

// Complete reports whether the grid shipped every product it claims to have.
func (g *StoreGrid) Complete() bool {
	return g != nil && g.Total > 0 && g.Shipped() >= g.Total
}

// The payload shapes, named as Amazon writes them.
type rawStoreGrid struct {
	Products    []rawStoreProduct `json:"products"`
	ASINList    []string          `json:"ASINList"`
	TotalCount  int               `json:"totalCount"`
	TotalResult int               `json:"totalResultCount"`
	SortOptions []struct {
		Default string `json:"defaultDisplayText"`
		Value   string `json:"sortValue"`
	} `json:"sortOptions"`
	Languages    []string `json:"languageFilter"`
	AuthorSearch struct {
		PageSize int `json:"pageSize"`
	} `json:"authorSearch"`
}

// rawStoreProduct is the product record as the grid payload writes it.
//
// Every name here was read off the capture rather than guessed at, which was not
// a formality: the obvious guesses for four of these (images, detailPageUrl,
// releaseDate, a flat title string) are all absent, and a struct built on them
// unmarshals without error into an empty record. A payload reader that silently
// yields blanks is the failure mode this whole package is built to avoid, so the
// field names came from the bytes.
type rawStoreProduct struct {
	ASIN  string `json:"asin"`
	Title struct {
		DisplayString string `json:"displayString"`
	} `json:"title"`
	DetailPageLinkURL string `json:"detailPageLinkURL"`
	ProductImages     struct {
		Images []struct {
			HiRes  rawStoreImage `json:"hiRes"`
			LowRes rawStoreImage `json:"lowRes"`
		} `json:"images"`
	} `json:"productImages"`
	BindingInformation struct {
		Binding struct {
			DisplayString string `json:"displayString"`
		} `json:"binding"`
	} `json:"bindingInformation"`
	BuyingOptions          []rawBuyingOption `json:"buyingOptions"`
	CustomerReviewsSummary struct {
		Rating struct {
			Value float64 `json:"value"`
		} `json:"rating"`
		Count struct {
			Value int64 `json:"value"`
		} `json:"count"`
	} `json:"customerReviewsSummary"`
	AccoladesBadge struct {
		BadgeType string `json:"badgeType"`
		Category  string `json:"category"`
	} `json:"accoladesBadge"`
	BestSellers struct {
		Badges []struct {
			Rank         int `json:"rank"`
			ViewOnAmazon struct {
				Data struct {
					DisplayString string `json:"displayString"`
				} `json:"data"`
			} `json:"viewOnAmazon"`
		} `json:"badges"`
	} `json:"bestSellers"`
	BookSeriesInfo struct {
		Position    int    `json:"position"`
		Total       int    `json:"total"`
		SeriesTitle string `json:"seriesTitle"`
	} `json:"bookSeriesInfo"`
	ByLine struct {
		Contributors []struct {
			Name  string `json:"name"`
			Roles []struct {
				DisplayString string `json:"displayString"`
			} `json:"roles"`
			Links []struct {
				URL string `json:"url"`
			} `json:"links"`
		} `json:"contributors"`
	} `json:"byLine"`
	MediaMatrix struct {
		Items []struct {
			Product string `json:"product"`
			Binding struct {
				DisplayString string `json:"displayString"`
			} `json:"binding"`
		} `json:"items"`
	} `json:"mediaMatrix"`
	MarketplaceOfferSummary struct {
		AnyOfferSummary struct {
			OfferCount int           `json:"offerCount"`
			MinPrice   rawStoreMoney `json:"minPrice"`
			MaxPrice   rawStoreMoney `json:"maxPrice"`
		} `json:"anyOfferSummary"`
	} `json:"marketplaceOfferSummary"`
	ProductCategory struct {
		ProductType         string `json:"productType"`
		WebsiteDisplayGroup struct {
			DisplayString string `json:"displayString"`
		} `json:"websiteDisplayGroup"`
	} `json:"productCategory"`
}

// rawBuyingOption is one offer, and its fields are raw because Amazon publishes
// each one either inline or by reference.
//
// This payload is hypermedia: a sub-resource is an object when the page has it
// and a URL string when the page does not. Measured on the author capture, of 79
// buying options the price is an object 67 times and a string 12 times, the
// merchant 67 and 12, the quantity 33 and 46. The same key, two types.
//
// A struct typed to the object form fails the whole decode on the first string,
// which is how a 562 KB grid of 70 products was silently returning nothing. So
// the fields are decoded per option, and the 12 products whose price is a URL
// end up with no price, which is the true answer: their price is not on this
// page. Substituting a zero would be a claim that they are free.
type rawBuyingOption struct {
	Price        json.RawMessage `json:"price"`
	Merchant     json.RawMessage `json:"merchant"`
	Availability json.RawMessage `json:"availability"`
}

// asObject decodes a hypermedia field into v, and reports false when the field
// was a reference rather than a value.
func asObject(raw json.RawMessage, v any) bool {
	t := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(t, "{") {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

type rawPrice struct {
	PriceToPay struct {
		Label             string `json:"label"`
		MoneyValueOrRange struct {
			Value rawStoreMoney `json:"value"`
		} `json:"moneyValueOrRange"`
	} `json:"priceToPay"`
}

type rawMerchant struct {
	Name string `json:"merchantName"`
	ID   string `json:"encryptedMerchantId"`
}

type rawAvailability struct {
	PrimaryMessage string `json:"primaryMessage"`
}

type rawStoreImage struct {
	URL string `json:"url"`
}

type rawStoreMoney struct {
	Amount       float64 `json:"amount"`
	CurrencyCode string  `json:"currencyCode"`
}

// readStoreGrid parses a product grid payload into the normalized shape.
// storeGrid is the page's product grid, found by widget so a caller does not
// have to know which of the twelve payloads on the page holds it.
//
// The lookup is by widgetType rather than sectionType because sectionType names
// the grid's role and changes with it: it is AuthorProductGrid on an author page
// and something else on a brand page that has one. widgetType is ProductGrid in
// both places.
func storeGrid(ws []StoreWidget, base string) *StoreGrid {
	w, ok := storeWidget(ws, "ProductGrid")
	if !ok {
		return nil
	}
	return readStoreGrid(w.Content, base)
}

func readStoreGrid(raw json.RawMessage, base string) *StoreGrid {
	var r rawStoreGrid
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil {
		return nil
	}
	g := &StoreGrid{
		ASINList: r.ASINList,
		Total:    firstNonZero(r.TotalCount, r.TotalResult),
		PageSize: r.AuthorSearch.PageSize,
		// Language names arrive as tokens, ChineseTraditional rather than
		// Chinese (Traditional), and are passed through unchanged because
		// splitting them would be this parser inventing a spelling.
		Languages: r.Languages,
	}
	for _, s := range r.SortOptions {
		if s.Value != "" {
			g.SortOptions = append(g.SortOptions, s.Value)
		}
	}
	for _, p := range r.Products {
		if p.ASIN == "" {
			continue
		}
		sp := StoreProduct{
			ASIN:           p.ASIN,
			Title:          collapseSpace(p.Title.DisplayString),
			URL:            absoluteURL(base, firstNonEmpty(p.DetailPageLinkURL, "/dp/"+p.ASIN)),
			Binding:        collapseSpace(p.BindingInformation.Binding.DisplayString),
			Rating:         p.CustomerReviewsSummary.Rating.Value,
			RatingsCount:   p.CustomerReviewsSummary.Count.Value,
			Accolade:       collapseSpace(p.AccoladesBadge.Category),
			AccoladeType:   p.AccoladesBadge.BadgeType,
			Series:         collapseSpace(p.BookSeriesInfo.SeriesTitle),
			SeriesPosition: p.BookSeriesInfo.Position,
			SeriesTotal:    p.BookSeriesInfo.Total,
			ProductType:    p.ProductCategory.ProductType,
			DisplayGroup:   collapseSpace(p.ProductCategory.WebsiteDisplayGroup.DisplayString),
		}
		if imgs := p.ProductImages.Images; len(imgs) > 0 {
			sp.Image = firstNonEmpty(imgs[0].HiRes.URL, imgs[0].LowRes.URL)
		}
		// Each field takes the first offer that publishes it inline rather than
		// the first offer. A product often ships two offers where the first
		// carries every field by reference and the second carries them inline,
		// and reading offer zero and stopping lost a real price on 9 of the 70
		// products measured. Which offer answers is recorded per field because
		// the offers are alternatives rather than a record split across rows.
		for _, b := range p.BuyingOptions {
			var pr rawPrice
			if sp.Price == 0 && asObject(b.Price, &pr) {
				sp.Price = round2(pr.PriceToPay.MoneyValueOrRange.Value.Amount)
				sp.Currency = pr.PriceToPay.MoneyValueOrRange.Value.CurrencyCode
				sp.PriceLabel = strings.TrimSuffix(collapseSpace(pr.PriceToPay.Label), ":")
			}
			var mc rawMerchant
			if sp.Merchant == "" && asObject(b.Merchant, &mc) {
				sp.Merchant = collapseSpace(mc.Name)
				sp.MerchantID = mc.ID
			}
			var av rawAvailability
			if sp.Availability == "" && asObject(b.Availability, &av) {
				sp.Availability = collapseSpace(av.PrimaryMessage)
			}
		}
		if bs := p.BestSellers.Badges; len(bs) > 0 {
			sp.BestSellerRank = bs[0].Rank
			sp.BestSellerIn = collapseSpace(bs[0].ViewOnAmazon.Data.DisplayString)
		}
		if o := p.MarketplaceOfferSummary.AnyOfferSummary; o.OfferCount > 0 {
			sp.OfferCount = o.OfferCount
			sp.MinPrice = round2(o.MinPrice.Amount)
			sp.MaxPrice = round2(o.MaxPrice.Amount)
			if sp.Currency == "" {
				sp.Currency = o.MinPrice.CurrencyCode
			}
		}
		for _, c := range p.ByLine.Contributors {
			if c.Name == "" {
				continue
			}
			sc := StoreContributor{Name: collapseSpace(c.Name)}
			if len(c.Roles) > 0 {
				sc.Role = collapseSpace(c.Roles[0].DisplayString)
			}
			// The link list mixes a search URL, a photo and the author page, so
			// the author page is picked by shape rather than by position.
			for _, l := range c.Links {
				if strings.Contains(l.URL, "/e/") && !strings.Contains(l.URL, "field-author") {
					sc.URL = absoluteURL(base, unescapeAmp(l.URL))
					break
				}
			}
			sp.Contributors = append(sp.Contributors, sc)
		}
		for _, m := range p.MediaMatrix.Items {
			// The edition names its product by resource path,
			// /marketplaces/ATVPDKIKX0DER/products/B0DK62KD1Y, not by ASIN.
			asin := m.Product
			if i := strings.LastIndex(asin, "/"); i >= 0 {
				asin = asin[i+1:]
			}
			if asin == "" && m.Binding.DisplayString == "" {
				continue
			}
			sp.Editions = append(sp.Editions, StoreEdition{
				ASIN: asin, Binding: collapseSpace(m.Binding.DisplayString),
			})
		}
		g.Products = append(g.Products, sp)
	}
	return g
}

// storeTile is one tile of an editorial row, which is where the author page
// keeps the biography and the most popular book.
type storeTile struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

// tilesOfType returns the tiles of one kind across every editorial row.
func tilesOfType(ws []StoreWidget, kind string) []storeTile {
	var out []storeTile
	for _, w := range ws {
		for _, raw := range w.Tiles {
			var t storeTile
			if json.Unmarshal(raw, &t) == nil && t.Type == kind {
				out = append(out, t)
			}
		}
	}
	return out
}

// unescapeAmp undoes the HTML escaping Amazon applies inside JSON string values.
//
// The contributor links arrive as /-/e/B000APZ33E?ie=UTF8&amp;field-author=...,
// which is an HTML entity inside a JSON string inside a script tag. Nothing has
// unescaped it by the time it reaches here, because the JSON decoder correctly
// left it alone, so a URL used as-is would carry a literal &amp; and fetch the
// wrong thing. Only the ampersand is measured, so only the ampersand is undone.
func unescapeAmp(s string) string { return strings.ReplaceAll(s, "&amp;", "&") }

func firstNonZero(v ...int) int {
	for _, x := range v {
		if x != 0 {
			return x
		}
	}
	return 0
}
