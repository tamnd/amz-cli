package amz

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The recommendation strips on a detail page.
//
// These cost nothing. The page carrying them has already been fetched, parsed
// and paid for, and every one of them is a set of edges between this product and
// others that Amazon itself drew. Dropping them meant a crawler had to discover
// the same neighbours by search, which is more requests to learn less.
//
// The rails are read off the regions Amazon named, so the record says which
// strip a card came from rather than flattening twelve strips into one list of
// ASINs with no provenance.

// railRe matches the region names Amazon gives its recommendation strips. It is
// a pattern rather than a list because the names are generated and the tail
// changes between renders: sims-consolidated-2_feature_div on one capture and
// sims-consolidated-3_feature_div on the next, for the same strip.
var railRe = regexp.MustCompile(`^(sims-|similarities|desktop-dp-lpo|sp_detail|HLCXComparison|purchase-sims|session-sims|dp-neighbor)`)

// sponsoredRailRe matches the ones Amazon sells. sp is sponsored products, and
// the distinction has to survive into the record: an advertising strip and an
// organic recommendation strip are different data and a dataset that mixes them
// is not usable for anything.
var sponsoredRailRe = regexp.MustCompile(`^sp_`)

// readRails collects every recommendation strip on the page.
//
// Each rail that yields at least one card is recorded and its region name is
// marked as claimed, so a strip that was read does not also show up in the
// `amz extraction --unread` worklist. A rail whose region exists and yields
// nothing is left unclaimed on purpose, because that is exactly the case worth
// looking at.
func readRails(e *Extractor, base string) []Rail {
	d := e.Doc()
	var out []Rail
	for _, name := range d.SortedRegionNames() {
		if !railRe.MatchString(name) {
			continue
		}
		r := d.Region(name)
		if !r.Exists() {
			continue
		}
		rail := Rail{
			Region:    name,
			Title:     railTitle(r),
			Sponsored: sponsoredRailRe.MatchString(name) || r.Find(".s-sponsored-label-text, .sponsored-products-label").Length() > 0,
			Cards:     railCards(r, base, name),
		}
		if len(rail.Cards) == 0 {
			continue
		}
		e.set("rail:"+name, len(rail.Cards), LevelRegion, name)
		out = append(out, rail)
	}
	return out
}

// railTitle is the heading above the strip. Amazon writes it in one of three
// wrappers depending on which team built the carousel, and none of them is a
// region of its own.
func railTitle(r Region) string {
	for _, sel := range []string{".a-carousel-heading", "h2", ".a-size-medium.a-color-base"} {
		if t := collapseSpace(nodeText(r.Find(sel).First())); t != "" {
			return t
		}
	}
	return ""
}

// railCards reads the tiles in one strip.
//
// A card here is deliberately thin. The strip gives an ASIN, a title, a picture
// and usually a price, and anything more would be a guess: the tile has no
// availability, no seller and no rating count worth trusting. The ASIN is the
// valuable part, because it is a link to a page that has all of that.
func railCards(r Region, base, region string) []Card {
	seen := map[string]bool{}
	var out []Card
	r.Find("li.a-carousel-card, .a-carousel-card, div[data-asin]").Each(func(i int, s *goquery.Selection) {
		asin := attrOf(s, "data-asin")
		if asin == "" {
			asin = ExtractASIN(attrOf(s.Find("a[href*='/dp/']").First(), "href"))
		}
		if !isASIN(asin) || seen[asin] {
			return
		}
		seen[asin] = true
		out = append(out, Card{
			Position: len(out) + 1,
			ASIN:     asin,
			Title:    collapseSpace(firstSelText(s, ".p13n-sc-truncate", ".a-truncate-full", "img[alt]")),
			Image:    upgradeImage(attrSel(s, "img", "src")),
			Kind:     "rail",
			URL:      base + "/dp/" + asin,
			// Sponsored travels down onto the card as well as sitting on the
			// rail, because a card that gets copied out of its rail into a
			// products table must not lose the one fact that makes it different
			// from an organic result.
			Sponsored: strings.HasPrefix(region, "sp_"),
		})
	})
	return out
}
