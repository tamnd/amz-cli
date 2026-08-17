package amz

import (
	"fmt"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The field registry: every field declared, none inlined.
//
// The reason to declare rather than inline is the report. `amz extraction`
// prints what rung each field sits on and how long a rung 4 field has been load
// bearing, and it can only do that if the fields are data. A field extracted by
// a line of code buried in a 300 line parser is a field nobody can count.
//
// See notes/Spec/3007/02_extraction.md section 2.

// Field is one declared field and where it comes from.
type Field struct {
	// Name is the record's field name, which is also the JSON key.
	Name string
	// Level is the rung. It is declared rather than inferred, so a field that
	// quietly changes source has to change its declared rung to compile.
	Level Level
	// Regions are the rung 1 anchor names to try, in order. The first that
	// exists answers, and its name is what lands in via.
	//
	// More than one name is not indecision. The detail table is
	// productDetailsHomeAndGarden_Updated on a physical good and detailBullets
	// on a book, and both are names Amazon chose for the same block.
	Regions []string
	// Via is the source string for a field that has no region: the payload
	// anchor at rung 2, the attribute at rung 3, the selector at rung 4.
	Via string
	// Rule reads the value once the region is found.
	Rule FieldRule
	// Alt marks a second source for a field already declared. Both are read. If
	// they disagree the record keeps the primary and records the disagreement.
	Alt bool
	// Fallback marks a second source that is not a cross check. It is read only
	// when the field is still unset, and never produces a disagreement.
	//
	// The distinction is not pedantry. The thumbnail on a browse tile has a
	// variant map naming the 480 pixel rendition and an src naming the 240 pixel
	// one, and those are the same picture at two sizes rather than two claims
	// about which picture it is. Declaring that as a cross check put an image
	// disagreement on all 91 tiles of a browse page and buried the one
	// disagreement that meant something, which was Amazon printing 27% off on a
	// pair of prices that work out to 26.
	Fallback bool
	// Since is the date a rung 4 field was added, so the drift report can say
	// how long a guess has been load bearing.
	Since string
	// Why is one line on what this field is, printed by `amz extraction`.
	Why string
}

// Source is the display form of where a field comes from.
func (f Field) Source() string {
	if len(f.Regions) > 0 {
		return f.Regions[0]
	}
	return f.Via
}

// productFields is the /dp/ registry.
//
// Region names measured against live captures on 2026-08-17. A detail page
// carries 289 distinct data-feature-name regions and this reads 24 of them; the
// other 265 are reported by `amz extraction --unread`, which is the worklist for
// the next version rather than a silence.
func productFields(base, asin string) []Field {
	return []Field{
		{Name: "title", Level: LevelRegion, Regions: []string{"title"},
			Rule: TextOf("h1", "#productTitle"), Why: "the product name as the detail page states it"},
		{Name: "brand", Level: LevelRegion, Regions: []string{"bylineInfo", "brandLogo"},
			Rule: LinkText(), Why: "the byline, with Amazon's Visit the / Store wrapper removed"},
		{Name: "brand_url", Level: LevelRegion, Regions: []string{"bylineInfo", "brandLogo"},
			Rule: LinkHref(base), Why: "the brand's storefront"},

		// Four spellings of the same box, all measured on 2026-08-17. One capture
		// of six carries corePrice and corePriceDisplay_desktop, five carry
		// corePrice_desktop, and the book carries neither: a book puts its price
		// in gsod_singleOfferDisplay_Desktop inside desktop_buybox, because the
		// books team ships its own buy box. Declaring one of these and calling the
		// rest drift would report four out of six pages as broken parsers when
		// what changed is which team rendered the page.
		{Name: "price", Level: LevelRegion, Regions: []string{"corePrice", "corePrice_desktop", "corePriceDisplay_desktop"},
			Rule: Price(), Why: "the price being charged, read from the accessible rendering"},
		{Name: "price", Level: LevelRegion, Regions: []string{"apex_offerDisplay_desktop", "gsod_singleOfferDisplay_Desktop", "desktop_buybox", "apex_desktop"},
			Rule: Price(), Alt: true, Why: "the buy box's own price, which is where a book and a device put it"},
		{Name: "currency", Level: LevelRegion, Regions: []string{"corePrice", "corePrice_desktop", "corePriceDisplay_desktop", "apex_offerDisplay_desktop", "gsod_singleOfferDisplay_Desktop", "apex_desktop"},
			Rule: PriceCurrency(), Why: "the currency token in the price string, which is not always the marketplace's"},
		// corePriceDisplay_desktop comes first here and second for price, which
		// is deliberate: corePrice holds the price being charged and nothing
		// else, and the display block is the one that carries the strike.
		{Name: "list_price", Level: LevelRegion, Regions: []string{"corePriceDisplay_desktop", "corePrice_desktop", "corePrice", "apex_offerDisplay_desktop", "gsod_singleOfferDisplay_Desktop", "apex_desktop"},
			Rule: StrikePrice(), Why: "the struck-through price"},
		{Name: "coupon", Level: LevelRegion, Regions: []string{"couponsInBuybox", "promoPriceBlockMessage"},
			Rule: RegionText(), Why: "a clippable coupon beside the price"},

		{Name: "availability", Level: LevelRegion, Regions: []string{"availabilityInsideBuyBox", "availability"},
			Rule: TextOf("#availability span", "#availability"), Why: "the buy box's stock line"},
		{Name: "sold_by", Level: LevelRegion, Regions: []string{"merchantInfoFeature", "dynamicSourceMerchantInfoFeature"},
			Rule: KeyedRow("Sold by", "Seller"), Why: "who is selling, from the offer display feature Amazon named for it"},
		{Name: "ships_from", Level: LevelRegion, Regions: []string{"fulfillerInfoFeature", "merchantInfoFeature"},
			Rule: KeyedRow("Ships from", "Shipper"), Why: "who is shipping"},
		{Name: "seller_id", Level: LevelRegion, Regions: []string{"merchantInfoFeature", "dynamicSourceMerchantInfoFeature"},
			Rule: HrefParam("seller"), Why: "the seller's merchant id, when the seller is not Amazon and the name is a link"},
		{Name: "returns", Level: LevelRegion, Regions: []string{"returnsInfoFeature", "dynamicReturnsInfoFeature"},
			Rule: KeyedRow("Returns"), Why: "the returns window"},
		{Name: "condition", Level: LevelRegion, Regions: []string{"conditionInfoFeature", "dynamicConditionInfoFeature"},
			Rule: RegionText(), Why: "new, renewed or used"},

		{Name: "rating", Level: LevelRegion, Regions: []string{"averageCustomerReviews"},
			Rule: Rating(), Why: "the star rating, read from the text written for a screen reader"},
		{Name: "ratings_count", Level: LevelRegion, Regions: []string{"averageCustomerReviews"},
			Rule: Count("#acrCustomerReviewText", "#acrCustomerReviewLink"),
			Why:  "how many ratings, from the aria-label rather than the parenthesised text"},
		// customer-reviews first, because that is where the histogram is on all
		// six product captures. reviewsMedley and averageCustomerReviews are the
		// older names and are kept behind it: averageCustomerReviews is the small
		// block beside the title, which carries the rating and the count but not
		// the five bars, and it only answers here on a layout that puts them back.
		{Name: "distribution", Level: LevelRegion, Regions: []string{"customer-reviews", "reviewsMedley", "averageCustomerReviews"},
			Rule: RatingHistogram(),
			Why:  "the five bucket star histogram, read from the aria-label because it is the only place the page says which bar is which"},
		{Name: "bought_past_month", Level: LevelRegion, Regions: []string{"socialProofingAsinFaceout", "socialProofingBadge"},
			Rule: RegionText(), Why: "the N+ bought in past month line"},

		{Name: "bullet_points", Level: LevelRegion, Regions: []string{"featurebullets_nonPets", "featurebullets", "feature-bullets"},
			Rule: ListItems("li span.a-list-item", "li"), Why: "the about-this-item bullets"},
		{Name: "description", Level: LevelRegion, Regions: []string{"productDescription", "bookDescription"},
			Rule: TextOf("p", "#productDescription"), Why: "the long description"},

		{Name: "category_path", Level: LevelRegion, Regions: []string{"wayfinding-breadcrumbs", "desktop-breadcrumbs"},
			Rule: LinkChain(), Why: "the breadcrumb, in order"},
		{Name: "browse_node_ids", Level: LevelRegion, Regions: []string{"wayfinding-breadcrumbs", "desktop-breadcrumbs"},
			Rule: NodeIDs(), Why: "the node ids behind the breadcrumb, which is what a crawl follows"},

		{Name: "specs", Level: LevelRegion,
			Regions: []string{"productDetailsHomeAndGarden_Updated", "detailBullets", "productDetails", "productOverview"},
			Rule:    SpecRows(), Why: "the detail table, whichever of the four names this category uses"},
		{Name: "ranks", Level: LevelRegion,
			Regions: []string{"productDetailsHomeAndGarden_Updated", "detailBullets", "productDetails"},
			Rule:    Ranks(), Why: "every Best Sellers Rank line, not only the first"},

		{Name: "similar_asins", Level: LevelRegion, Regions: []string{"similarities", "sims-consolidated-2_feature_div"},
			Rule: ASINs(asin), Why: "the compare-with-similar table"},

		// Rung 3: an attribute outside any named region.
		{Name: "answered_qs", Level: LevelAttr, Via: "#askATFLink",
			Rule: Count("#askATFLink"), Why: "the answered question count, which survives the login wall on /ask"},

		// Rung 4 is the report. Adding one is allowed and has to be deliberate
		// enough to move the number in TestLevel4CountNotIncreasing.
		{Name: "parent_asin", Level: LevelSelector, Via: "#landingAsin[value], #ppd[data-asin]",
			Rule: firstAttr("#landingAsin", "value", "#ppd", "data-asin"), Since: "2026-08-17",
			Why: "the variation parent. The twister payload carries it too and is preferred when present."},
	}
}

// firstAttr is the one rung 4 shape that survives: two ids, one attribute each.
func firstAttr(sel1, attr1, sel2, attr2 string) FieldRule {
	return func(e *Extractor, _ Region) (any, bool) {
		root := e.Doc().Selection()
		for _, pair := range [][2]string{{sel1, attr1}, {sel2, attr2}} {
			if v := attrOf(root.Find(pair[0]).First(), pair[1]); asinAttrRe.MatchString(v) {
				return v, true
			}
		}
		return "", false
	}
}

// searchPageFields is the /s registry for the page itself rather than its cards.
//
// The pagination strip sits under s-search-results and not under the region
// Amazon named s-pagination, which it ships as an empty div on every capture
// taken. Both names are declared, in that order, so the day the empty one is
// filled in the field moves back to it without a code change and the report says
// which one answered.
func searchPageFields() []Field {
	return []Field{
		{Name: "query_echo", Level: LevelRegion, Regions: []string{"s-result-info-bar"},
			Rule: ResultBar(func(b resultBar) any { return b.Query }),
			Why:  "the query Amazon says it ran, which is not always the query that was sent"},
		{Name: "result_from", Level: LevelRegion, Regions: []string{"s-result-info-bar"},
			Rule: ResultBar(func(b resultBar) any { return int64(b.From) }),
			Why:  "the first result number this page covers"},
		{Name: "result_to", Level: LevelRegion, Regions: []string{"s-result-info-bar"},
			Rule: ResultBar(func(b resultBar) any { return int64(b.To) }),
			Why:  "the last result number this page covers"},
		{Name: "result_total", Level: LevelRegion, Regions: []string{"s-result-info-bar"},
			Rule: ResultBar(func(b resultBar) any { return b.Total }),
			Why:  "how many results Amazon claims, which moves between pages of one query"},
		{Name: "result_total_approx", Level: LevelRegion, Regions: []string{"s-result-info-bar"},
			Rule: ResultBar(func(b resultBar) any { return b.Approx }),
			Why:  "whether the total was printed with the word over in front of it"},

		{Name: "page_current", Level: LevelRegion, Regions: []string{"s-pagination", "s-search-results"},
			Rule: PageNumber("current"), Why: "the page the strip marks as selected"},
		{Name: "page_next", Level: LevelRegion, Regions: []string{"s-pagination", "s-search-results"},
			Rule: PageNumber("next"), Why: "the next page, absent on the last one"},
		{Name: "page_last", Level: LevelRegion, Regions: []string{"s-pagination", "s-search-results"},
			Rule: PageNumber("last"), Why: "the highest page the strip will navigate to"},
	}
}

// cardFields is the /s registry: data-component-type outside, data-cy inside.
//
// A search card carries no data-feature-name at all. It carries data-cy, which
// is the search team's name for its own slots, and reading those is what stops
// bought_past_month from capturing the card's entire accessibility text.
func cardFields(base string) []Field {
	_ = base
	return []Field{
		{Name: "title", Level: LevelRegion, Regions: []string{"title-recipe"},
			Rule: TextOf("h2 span", "h2 a span", "h2"), Why: "the card's title slot"},
		{Name: "price", Level: LevelRegion, Regions: []string{"price-recipe"},
			Rule: Price(), Why: "the card's price slot"},
		{Name: "currency", Level: LevelRegion, Regions: []string{"price-recipe"},
			Rule: PriceCurrency(), Why: "the currency the card printed"},
		{Name: "list_price", Level: LevelRegion, Regions: []string{"price-recipe"},
			Rule: StrikePrice(), Why: "the struck-through price on the card"},
		{Name: "rating", Level: LevelRegion, Regions: []string{"reviews-block"},
			Rule: Rating(), Why: "the card's star rating"},
		{Name: "ratings_count", Level: LevelRegion, Regions: []string{"reviews-block"},
			Rule: Count("a[aria-label]", "[aria-label]", "span"),
			Why:  "the card's rating count, taken from the label because the text is rounded to (1.7K)"},
		{Name: "image", Level: LevelRegion, Regions: []string{"image-container", "s-product-image"},
			Rule: AttrOf("img.s-image", "src"), Why: "the card's thumbnail"},
		{Name: "delivery", Level: LevelRegion, Regions: []string{"delivery-recipe"},
			Rule: RegionText(), Why: "the delivery promise"},
		{Name: "bought_past_month", Level: LevelRegion, Regions: []string{"reviews-block", "secondary-offer-recipe"},
			Rule: boughtLine(), Why: "the N+ bought line, anchored rather than scanned for"},

		// Rung 3: the card's own slot number, and the three badges that have no
		// data-cy slot of their own to sit in.
		{Name: "position", Level: LevelAttr, Via: "[data-csa-c-pos]",
			Rule: IntAttr("[data-csa-c-pos]", "data-csa-c-pos"),
			Why:  "where Amazon says this card sat, which is not where it arrived once sponsored cards are counted"},
		{Name: "sponsored", Level: LevelAttr, Via: ".puis-sponsored-label-text",
			Rule: Present(".puis-sponsored-label-text, .s-sponsored-label-text"),
			Why:  "whether the card is an ad"},
		{Name: "prime", Level: LevelAttr, Via: ".a-icon-prime",
			Rule: Present(".a-icon-prime, [aria-label='Amazon Prime']"), Why: "the Prime badge"},
		{Name: "badge", Level: LevelAttr, Via: ".a-badge-text",
			Rule: TextOf(".a-badge-text", ".puis-badge-text"), Why: "Best Seller, Amazon's Choice and the rest"},
	}
}

// boughtLine finds the social proof line inside a region rather than anywhere on
// the card.
//
// The unanchored version of this rule captured the card's whole accessibility
// text, several hundred words of sustainability certification boilerplate, and
// put it in a field called bought_past_month. That is what rung 4 costs.
func boughtLine() FieldRule {
	return func(_ *Extractor, r Region) (any, bool) {
		if !r.Exists() {
			return "", false
		}
		var out string
		r.Find("span").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			t := collapseSpace(s.Text())
			if len(t) > 60 || !containsFold(t, "bought in past month") {
				return true
			}
			out = t
			return false
		})
		return out, out != ""
	}
}

// chartFields is the tile registry for /gp/bestsellers and its four siblings.
//
// The rungs here are lower than the product family's and that is the honest
// reading rather than a shortfall to hide. A chart tile carries data-asin and
// nothing else Amazon named, so everything read inside one is rung 3 at best.
// The exception is the rank, which comes from the list Amazon publishes for its
// own renderer and is therefore rung 2, with the printed badge kept beside it as
// a cross check that has to agree.
func chartFields() []Field {
	return []Field{
		{Name: "rank", Level: LevelPayload, Via: PayloadChartList, Rule: ChartRank(),
			Why: "the rank Amazon states in the list it hands its own renderer"},
		{Name: "rank", Level: LevelSelector, Via: ".zg-bdg-text", Alt: true, Since: "2026-08-17",
			Rule: BadgeRank(), Why: "the printed rank badge, kept as a cross check on the payload"},

		{Name: "title", Level: LevelSelector, Via: "a[href*='/dp/']", Since: "2026-08-17",
			Rule: TextOf("a[href*='/dp/']:not([aria-hidden='true'])", "[class*=p13n-sc-css-line-clamp]", "[class*=p13n-sc-truncate]"),
			Why:  "the tile's title. The charts predate data-* and the title link is the closest thing to an anchor they have."},
		{Name: "price", Level: LevelSelector, Via: "[class*=p13n-sc-price]", Since: "2026-08-17",
			Rule: PriceOf("[class*=p13n-sc-price]", ".a-color-price", ".a-offscreen", ".a-price"),
			Why:  "the tile's price, which is not wrapped in the a-price block the rest of the site uses"},
		{Name: "currency", Level: LevelSelector, Via: "[class*=p13n-sc-price]", Since: "2026-08-17",
			Rule: PriceCurrencyOf("[class*=p13n-sc-price]", ".a-color-price", ".a-offscreen", ".a-price"),
			Why:  "the currency token in the tile's price string"},

		// One aria-label carries both numbers: "4.4 out of 5 stars, 279,167
		// ratings". Reading it twice is cheaper than trusting two classes.
		{Name: "rating", Level: LevelAttr, Via: "a[aria-label]", Rule: Rating(), Why: "the tile's star rating"},
		{Name: "ratings_count", Level: LevelAttr, Via: "a[aria-label]",
			Rule: Count("a[aria-label]", "[aria-label]", ".a-size-small"), Why: "the tile's rating count"},

		{Name: "image", Level: LevelAttr, Via: "img[src]", Rule: AttrOf("img", "src"), Why: "the tile's image"},
		{Name: "href", Level: LevelAttr, Via: "a[href*='/dp/']",
			Rule: AttrOf("a[href*='/dp/']", "href"), Why: "the tile's link, which carries Amazon's own ref tag for this slot"},
	}
}

// browsePageFields is what a /b or /deals page says about itself.
//
// It says it in the head rather than in the body, which is the finding here. The
// h1 on a browse node is either the literal word "Department" or empty, and the
// breadcrumb trail the detail pages carry is absent, so a parser that reads
// headings gets nothing. The canonical link carries the slug and the node, and it
// is the only place the page states which node it actually served.
func browsePageFields() []Field {
	return []Field{
		{Name: "canonical_url", Level: LevelAttr, Via: "link[rel=canonical]",
			Rule: AttrOf("link[rel=canonical]", "href"), Why: "the URL Amazon says this page is"},
		{Name: "canonical_node", Level: LevelAttr, Via: "link[rel=canonical]",
			Rule: CanonicalNode(), Why: "the browse node the canonical URL resolved to, which is not always the one asked for"},
		{Name: "slug", Level: LevelAttr, Via: "link[rel=canonical]",
			Rule: CanonicalSlug(), Why: "the readable segment of the canonical URL, electronics-store"},
		{Name: "name", Level: LevelAttr, Via: "link[rel=canonical]",
			Rule: CanonicalName(), Why: "the slug turned back into words, because no heading on the page carries the name"},
		{Name: "name", Level: LevelAttr, Via: "meta[name=description]", Alt: true,
			Rule: DescriptionName(), Why: "the name inside Amazon's own description sentence, kept as a cross check"},
	}
}

// browseFields is the tile registry for /b and /deals.
//
// A browse tile names three things and nothing else: its identity, its slot, and
// its deal badge. Everything else is a dcl- class, which is a design system token
// rather than a contract, so title, price, rating and delivery are rung 4 and are
// counted as rung 4. Pretending otherwise would put a redesign's worth of silent
// breakage behind a green test suite.
func browseFields() []Field {
	return []Field{
		{Name: "position", Level: LevelAttr, Via: "data-csa-c-pos",
			Rule: CSAPosition(), Why: "the tile's slot in its shelf, which is Amazon's own record of the order it chose"},

		{Name: "title", Level: LevelSelector, Via: ".dcl-product-title", Since: "2026-08-17",
			Rule: TextOf(".dcl-product-title", ".dcl-product-label"),
			Why:  "the tile's title. A plain tile uses dcl-product-title and a deal tile uses dcl-product-label for the same string."},
		{Name: "href", Level: LevelSelector, Via: "a.dcl-product-link", Since: "2026-08-17",
			Rule: AttrOf("a.dcl-product-link", "href"), Why: "the tile's link, carrying Amazon's ref tag for this slot"},
		{Name: "image", Level: LevelPayload, Via: "data-a-dynamic-image",
			Rule: DynamicImage(), Why: "the largest rendition in the variant map Amazon puts on every thumbnail"},
		{Name: "image", Level: LevelSelector, Via: "img[src]", Fallback: true, Since: "2026-08-17",
			Rule: AttrOf("img", "src"), Why: "the rendition actually drawn, for the tiles with no variant map"},

		{Name: "price", Level: LevelSelector, Via: ".dcl-product-price-new", Since: "2026-08-17",
			Rule: PriceOf(".dcl-product-price-new .a-offscreen", ".a-price:not(.a-text-price) .a-offscreen"),
			Why:  "the price being charged. The selector excludes a-text-price because the struck through price is in an a-price block too."},
		{Name: "currency", Level: LevelSelector, Via: ".dcl-product-price-new", Since: "2026-08-17",
			Rule: PriceCurrencyOf(".dcl-product-price-new .a-offscreen", ".a-price:not(.a-text-price) .a-offscreen"),
			Why:  "the currency token in the tile's price"},
		{Name: "was_price", Level: LevelSelector, Via: ".dcl-product-price-old", Since: "2026-08-17",
			Rule: PriceOf(".dcl-product-price-old .a-offscreen", ".a-text-price .a-offscreen"),
			Why:  "the struck through price"},
		{Name: "was_price_label", Level: LevelSelector, Via: ".dcl-product-old-price-label", Since: "2026-08-17",
			Rule: TextOf(".dcl-product-old-price-label"),
			Why:  "what Amazon called the struck through price. It reads List:, List Price: or Typical:, and a typical price is a computed average rather than a list price."},

		{Name: "discount_pct", Level: LevelRegion, Regions: []string{browseBadge},
			Rule: BadgePercent(), Why: "the discount Amazon printed on the badge"},
		{Name: "discount_pct", Level: LevelSelector, Via: "price and was_price", Alt: true, Since: "2026-08-17",
			Rule: DiscountFromPrices(), Why: "the discount the two prices imply, for the tiles with no badge"},
		{Name: "deal_type", Level: LevelRegion, Regions: []string{browseBadge},
			Rule: BadgeMessage(), Why: "the badge's second line, Limited time deal or Ends in"},
		{Name: "ends_soon", Level: LevelRegion, Regions: []string{browseTimer},
			Rule: RegionPresent(), Why: "whether Amazon drew a countdown. The clock is filled in by script, so the slot is all the served HTML states."},

		{Name: "rating", Level: LevelSelector, Via: ".dcl-product-rating-value", Since: "2026-08-17",
			Rule: RatingOf(".dcl-product-rating-value", ".a-icon-alt"), Why: "the tile's star rating"},
		{Name: "ratings_count", Level: LevelSelector, Via: ".dcl-product-rating-count", Since: "2026-08-17",
			Rule: Count(".dcl-product-rating-count"), Why: "the tile's rating count"},
		{Name: "delivery", Level: LevelSelector, Via: ".udm-primary-delivery-message", Since: "2026-08-17",
			Rule: TextOf(".udm-primary-delivery-message"), Why: "the delivery line, which a deal tile never carries"},
	}
}

// storeFields is the registry for /stores/ and author pages.
//
// The registry this replaced declared six regions: store-name, brand-name,
// hero-title, about-section, store-description and followers. None of the six is
// on either capture. Not moved, not renamed, absent, so the whole registry read
// nothing and reported nothing, the same silent failure the deals grid had. The
// names below were taken off the captures.
//
// The split between rungs is the point of this family. Amazon's own name for the
// storefront, its description and its image are in the head, which is rung 3 and
// dependable. The nav tree, the product grid and the biography are in the var
// config payloads, which is rung 2. The DOM in between is layout: editorial rows
// of images, a follow button whose text is the word "Follow" and no count, and a
// product grid container whose served content reads "We couldn't find anything
// matching these filters".
func storeFields() []Field {
	return []Field{
		{Name: "canonical_url", Level: LevelAttr, Via: "link[rel=canonical]",
			Rule: AttrOf("link[rel=canonical]", "href"), Why: "the URL Amazon says this storefront is, which carries the page id"},
		// The two names are a fallback rather than a cross check, and the
		// difference is measurable. The author capture's og:title reads "Michael
		// Lewis: books, biography, latest update" while its payload reads
		// "Michael Lewis". That is one name with a marketing tail on it, not two
		// claims about who wrote the books, so declaring it as a cross check
		// would put a disagreement on every author page on the site and say
		// nothing. The payload leads because it is the clean one, and the head
		// answers for the brand pages, which carry no author widget and no h1
		// either.
		{Name: "name", Level: LevelPayload, Via: "AuthorSubHeader",
			Rule: StoreWidgetString("AuthorSubHeader", "authorName"),
			Why:  "the name the author widget states, which is the bare name with no marketing tail"},
		{Name: "name", Level: LevelAttr, Via: "meta[property=og:title]", Fallback: true,
			Rule: AttrOf("meta[property='og:title']", "content"),
			Why:  "the storefront's name for the pages with no author widget, where the head is the only source"},
		{Name: "description", Level: LevelAttr, Via: "meta[name=description]",
			Rule: AttrOf("meta[name='description']", "content"), Why: "Amazon's own summary of the storefront"},
		{Name: "image", Level: LevelPayload, Via: "AuthorFooter",
			Rule: StoreWidgetString("AuthorFooter", "image"),
			Why:  "the author's photograph at the size the footer widget publishes, which is the original rather than the social card crop"},
		{Name: "image", Level: LevelAttr, Via: "meta[property=og:image]", Fallback: true,
			Rule: AttrOf("meta[property='og:image']", "content"), Why: "the storefront's hero, for the pages with no author footer"},

		{Name: "page_id", Level: LevelPayload, Via: "pageContext.authorId",
			Rule: StorePageContext("authorId"),
			Why:  "the author id, which is the identifier every widget on the page is named after"},
		{Name: "page_id", Level: LevelAttr, Via: "link[rel=canonical]", Fallback: true,
			Rule: CanonicalPageID(),
			Why:  "the storefront page's UUID, for the brand pages, which have no author id and put the id of the page you are on in the canonical URL and nowhere else"},
		{Name: "root_path", Level: LevelPayload, Via: "pageContext.rootPagePath",
			Rule: StorePageContext("rootPagePath"),
			Why:  "the storefront's own root, which is the path a crawl should treat as this store's home rather than whichever sub page it landed on"},
		{Name: "store_id", Level: LevelPayload, Via: "pageContext.storeId",
			Rule: StorePageContext("storeId"),
			Why:  "the store's UUID, which is stable across the slug changes Amazon makes to the URL"},
		{Name: "about_url", Level: LevelPayload, Via: "aboutAuthor",
			Rule: StoreTileString("aboutAuthor", "aboutSubpageURL"),
			Why:  "the full profile page, where the biography this tile truncates is published whole"},

		{Name: "bio", Level: LevelPayload, Via: "aboutAuthor",
			Rule: AuthorBio(), Why: "the biography, from the tile that holds it rather than from the truncated copy the DOM shows"},
		{Name: "bio", Level: LevelRegion, Regions: []string{"aboutAuthorText"}, Alt: true,
			Rule: RegionText(), Why: "the biography as rendered, which is clipped and is kept only to notice when the payload one goes away"},

		// follower_count is declared and it does not resolve. That is deliberate.
		// Amazon renders a follow button with the single word "Follow" and no
		// number anywhere on either capture, so the field reports a miss and the
		// miss says why. Dropping the declaration would make an absent number
		// look like a number nobody wanted.
		{Name: "follower_count", Level: LevelRegion, Regions: []string{"follow-button"},
			Rule: Count("span", "[aria-label]"),
			Why:  "the follower count. Measured absent: the button carries the word Follow and no figure."},
	}
}

// sellerFields is the registry for /sp?seller=.
//
// Everything here is rung 1, which is unusual and is a property of the page
// rather than of this registry: a seller profile is server rendered and names
// its sections with ids. The old parser read four of these with CSS selectors
// and three of the four matched nothing.
func sellerFields() []Field {
	return []Field{
		{Name: "name", Level: LevelRegion, Regions: []string{"seller-name"},
			Rule: RegionText(), Why: "the seller's display name"},
		{Name: "logo", Level: LevelRegion, Regions: []string{"seller-logo-img"},
			Rule: RegionAttr("src"), Why: "the seller's logo"},
		{Name: "storefront_url", Level: LevelRegion, Regions: []string{"seller-info-storefront-link"},
			Rule: RegionHref(), Why: "the link to the seller's own listings, which is how a crawl gets from a seller to their catalogue"},
		{Name: "about", Level: LevelRegion, Regions: []string{"page-section-about-seller"},
			Rule: ExpandedSectionBody("#seller-contact-text", "#seller-contact-button"),
			Why:  "the seller's own description of itself, from behind the See more expander rather than the clipped copy in front of it. The heading and the ask-a-question widget are dropped, because Amazon puts the contact form inside this section and it ended every seller description with the words \"Ask a question\"."},
		{Name: "business_name", Level: LevelRegion, Regions: []string{"page-section-detail-seller-info"},
			Rule: LabeledValue("Business Name"), Why: "the legal entity behind the seller, which is not the display name"},
		{Name: "business_address", Level: LevelRegion, Regions: []string{"page-section-detail-seller-info"},
			Rule: LabeledValue("Business Address"), Why: "the registered address, joined from the spans Amazon splits it across"},
		{Name: "shipping_policy", Level: LevelRegion, Regions: []string{"page-section-shipping-policies"},
			Rule: SectionBody(), Why: "the seller's stated shipping policy"},
		{Name: "return_policy", Level: LevelRegion, Regions: []string{"page-section-return-refunds"},
			Rule: SectionBody(), Why: "the seller's stated return policy"},

		// The feedback block, measured on 2026-08-17 against
		// /sp?seller=AKI54NNZ6PH23, a third party seller trading as SIKAI CASE.
		//
		// It took two captures to write these, and the first one is why. The
		// Amazon owned seller captured earlier carries no feedback section at
		// all: no rating, no percentage, and zero occurrences of the word
		// "ratings" anywhere in 100 KB. Declaring these fields against region
		// names taken from that page would have been declaring them against
		// nothing, which is the mistake this package already recorded once over
		// data-widget. So the second capture was taken from a seller Amazon does
		// rate, and every name below is off that page.
		//
		// The two captures together are also what the fields are for. One seller
		// publishes feedback and one does not, and the difference between "not
		// rated" and "rated zero" is only visible because both fields are
		// declared and the absent one reports a miss.
		{Name: "rating", Level: LevelRegion, Regions: []string{"seller-feedback-summary-rating"},
			Rule: SellerPeriodRating("year"),
			Why:  "the twelve month star rating, which is the window Amazon shows in the header and the one a buyer is judging"},
		{Name: "ratings_count", Level: LevelRegion, Regions: []string{"seller-feedback-summary-rating"},
			Rule: SellerPeriodCount("year"),
			Why:  "how many ratings the twelve month figure is drawn from. A 5.0 from 44 and a 5.0 from 4 are not the same claim."},
		{Name: "positive_pct", Level: LevelRegion, Regions: []string{"seller-info-feedback-summary"},
			Rule: SellerPositivePct(),
			Why:  "the share Amazon counts as positive, which is a separate statistic from the star average rather than a restatement of it"},
		{Name: "feedback", Level: LevelRegion, Regions: []string{"seller-feedback-summary-rating"},
			Rule: SellerFeedbackPeriods(),
			Why:  "the rating and count over all four windows Amazon publishes, so a long history and a short one are distinguishable"},
		{Name: "rating_histogram", Level: LevelRegion, Regions: []string{"seller-feedback-summary-histogram"},
			Rule: SellerHistogram(),
			Why:  "the star breakdown, which is where a 4.8 with a two percent tail of one star ratings stops looking like a 4.8"},
		{Name: "reviews", Level: LevelRegion, Regions: []string{"page-section-feedback"},
			Rule: SellerReviews(),
			Why:  "the written feedback Amazon serves, which is the first page of five and not the whole history"},
	}
}

// Registry returns the declared fields for a family.
func Registry(fam Family, base, asin string) []Field {
	switch fam {
	case FamilySearch:
		return append(searchPageFields(), cardFields(base)...)
	case FamilyChart:
		return chartFields()
	case FamilyBrowse:
		return append(browsePageFields(), browseFields()...)
	case FamilyStore:
		return storeFields()
	case FamilySeller:
		return sellerFields()
	default:
		return productFields(base, asin)
	}
}

// Run reads every declared field into the extractor.
//
// This is the only caller of set, and set is the only writer of the record, so
// every value in every record arrives with a rung and a source attached.
func (e *Extractor) Run(fields []Field) {
	e.RunFields(fields)
	e.MarkUnread(claimedRegions(fields))
}

// RunFields reads every declared field against the page without marking the
// unread regions, which a page of repeated records does once at the end so the
// regions its cards read are not reported as untouched.
func (e *Extractor) RunFields(fields []Field) {
	for _, f := range fields {
		e.runField(f, nil)
	}
}

// RunIn reads every declared field inside one region, which is how a page of
// repeated cards is read: each card is its own root.
func (e *Extractor) RunIn(root Region, fields []Field) {
	for _, f := range fields {
		e.runField(f, root)
	}
}

func (e *Extractor) runField(f Field, root Region) {
	r, via := e.resolve(f, root)
	if f.Rule == nil {
		return
	}
	if f.Fallback && e.Has(f.Name) {
		return
	}
	v, ok := f.Rule(e, r)
	if !ok {
		if len(f.Regions) > 0 && !r.Exists() {
			// The region is not on the page, which is a different fact from the
			// region being present and empty, and only this one is a parser
			// problem worth looking at.
			e.miss(f.Name, fmt.Sprintf("%s region %s not present on this page", e.fam, orList(f.Regions)))
		}
		return
	}
	e.set(f.Name, v, f.Level, via)
}

// resolve finds the region a field declares, and the source string that goes
// into via.
func (e *Extractor) resolve(f Field, root Region) (Region, string) {
	if len(f.Regions) == 0 {
		if root != nil {
			return root, f.Via
		}
		// No region and no card to sit in means the whole page, which is what
		// rung 3 and rung 4 are: a field that had to leave the named regions to
		// find its value.
		return e.doc.Root(), f.Via
	}
	// Two passes over the candidates. The first wants a region that is present
	// and has something in it, the second settles for present.
	//
	// The difference is load bearing. Amazon ships fulfillerInfoFeature as an
	// empty div and puts both the shipper and the seller in merchantInfoFeature
	// when the page is served without a delivery address, so stopping at the
	// first region that merely exists loses the field to an empty box that is
	// only there to be filled in later by script.
	for pass := 0; pass < 2; pass++ {
		for _, name := range f.Regions {
			var r Region
			if root != nil {
				r = root.Sub(name)
			} else {
				r = e.doc.Region(name)
			}
			if !r.Exists() {
				continue
			}
			if pass == 0 && len(f.Regions) > 1 && r.Text() == "" {
				continue
			}
			return r, name
		}
	}
	// Nothing matched. Hand back the first candidate so the rule sees a region
	// that reports Exists false rather than a nil.
	if root != nil {
		return root.Sub(f.Regions[0]), f.Regions[0]
	}
	return e.doc.Region(f.Regions[0]), f.Regions[0]
}

func claimedRegions(fields []Field) map[string]bool {
	out := map[string]bool{}
	for _, f := range fields {
		for _, r := range f.Regions {
			out[r] = true
		}
	}
	return out
}

// claimedWith is the regions a registry declares plus the ones a parser knows
// about and deliberately does not read.
//
// The second set is needed wherever a page names its layout as loudly as its
// data. A brand storefront names twenty five editorial containers and a seller
// profile names three blocks of Amazon's own boilerplate, and none of those is a
// field anybody is missing. Listing them here keeps the unread worklist a list
// of work rather than a list of furniture.
func claimedWith(fields []Field, extra map[string]bool) map[string]bool {
	out := claimedRegions(fields)
	for k, v := range extra {
		if v {
			out[k] = true
		}
	}
	return out
}

func orList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return `"` + names[0] + `"`
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + n + `"`
	}
	return joinWithOr(quoted)
}

func joinWithOr(parts []string) string {
	if len(parts) < 2 {
		return parts[0]
	}
	head := parts[:len(parts)-1]
	out := head[0]
	for _, p := range head[1:] {
		out += ", " + p
	}
	return out + " or " + parts[len(parts)-1]
}

// Level4Fields returns every declared rung 4 field across every family, sorted
// by the date it was added, which is what `amz extraction` prints and what
// TestLevel4CountNotIncreasing counts.
func Level4Fields() []Field {
	var out []Field
	for _, fam := range Families() {
		for _, f := range Registry(fam, "https://www.amazon.com", "") {
			if f.Level == LevelSelector {
				out = append(out, f)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Since != out[j].Since {
			return out[i].Since < out[j].Since
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
