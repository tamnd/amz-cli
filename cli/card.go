package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/amz-cli/amz"
)

// The human product card.
//
// A row of a table is the right shape for twenty products and the wrong shape
// for one, because the interesting things about a single listing are a price
// against a list price, a rating against its distribution, and the gap between
// what the page said exists and what could be read. None of those fit in a cell.
//
// Two rules hold this block together. The histogram is drawn because Amazon
// publishes it and v0.2.1 threw it away, and a distribution says more about a
// product's reviews than any average does. The not read block is generated from
// the envelope's misses and never written here, which is the difference between
// a tool that returns less than you wanted and a tool that lies about it.

// cardGutter is where the right hand column of the header starts, and cardBar is
// how wide the fullest histogram bar is drawn.
const (
	cardGutter = 32
	cardBar    = 36
	cardLabel  = 10
	// cardWidth is what the block wraps to. It is fixed rather than read from
	// the terminal because a card that reflows with the window is a card whose
	// output nobody can diff, and this text ends up in issues and in tests.
	cardWidth = 84
)

// printProductCard writes one product as a block of text.
//
// verbose is the -v count. At two it appends the provenance of each line, which
// is the envelope's via map, so a reader can see that a price came from a region
// and a rank came from a selector without going to the JSON for it.
func printProductCard(w io.Writer, p amz.Product, verbose int) {
	title := p.Title
	if title == "" {
		title = "(no title on the page)"
	}
	_, _ = fmt.Fprintln(w, title)

	id := []string{p.ASIN, marketplaceHost(p.Marketplace)}
	if !p.FetchedAt.IsZero() {
		id = append(id, "read "+p.FetchedAt.Local().Format("2006-01-02 15:04"))
	}
	_, _ = fmt.Fprintf(w, "  %s\n\n", strings.Join(id, "  ·  "))

	cardPricing(w, p, verbose)
	cardHistogram(w, p)
	cardFacts(w, p, verbose)
	cardNotRead(w, p)
}

// cardPricing writes the three header lines: what it costs, whether it can be
// bought, and what people rated it.
func cardPricing(w io.Writer, p amz.Product, verbose int) {
	var wrote bool
	pair := func(left, right string) {
		if left == "" && right == "" {
			return
		}
		wrote = true
		line := "  " + left
		if right != "" {
			for len(line) < cardGutter {
				line += " "
			}
			line += right
		}
		_, _ = fmt.Fprintln(w, strings.TrimRight(line, " "))
	}

	if o := p.Offer; o != nil {
		pair(moneyText(o.Price), savingsText(o))
		pair(availabilityText(o), sellerText(o))
	}
	pair(ratingText(p), countsText(p))
	if verbose >= 2 {
		if via := viaOf(p, "price", "availability", "rating"); via != "" {
			pair("", "via "+via)
		}
	}
	if wrote {
		_, _ = fmt.Fprintln(w)
	}
}

func moneyText(m *amz.Money) string {
	if m == nil {
		return ""
	}
	return m.Display
}

// savingsText prints the list price and the discount amz derived from it, not
// the saving Amazon printed. The two disagree often enough that the derived one
// is the honest number to show next to a price this card also derived.
func savingsText(o *amz.Offer) string {
	if o.ListPrice == nil {
		return ""
	}
	s := "was " + o.ListPrice.Display
	if o.SavingsPct != nil {
		s += fmt.Sprintf(", save %d%%", *o.SavingsPct)
	}
	return s
}

func availabilityText(o *amz.Offer) string {
	if o.Availability != "" {
		return o.Availability
	}
	if o.InStock != nil && *o.InStock {
		return "In Stock"
	}
	return ""
}

// sellerText collapses the two parties into the sentence Amazon writes when they
// are the same, because "ships from Amazon.com, sold by Amazon.com" is a fact
// stated twice.
func sellerText(o *amz.Offer) string {
	from, by := refName(o.ShipsFrom), refName(o.SoldBy)
	switch {
	case from != "" && from == by:
		return "ships from and sold by " + from
	case from != "" && by != "":
		return "ships from " + from + ", sold by " + by
	case by != "":
		return "sold by " + by
	case from != "":
		return "ships from " + from
	}
	return ""
}

func refName(r *amz.Ref) string {
	if r == nil {
		return ""
	}
	return r.Name
}

func ratingText(p amz.Product) string {
	if p.Rating == nil {
		return ""
	}
	return strconv.FormatFloat(*p.Rating, 'f', -1, 64) + " out of 5"
}

// countsText prints both counts, because rating something and writing about it
// are different acts and the two numbers are never the same.
func countsText(p amz.Product) string {
	var parts []string
	if p.RatingsCount != nil {
		parts = append(parts, groupDigits(*p.RatingsCount)+" ratings")
	}
	if p.ReviewsCount != nil {
		parts = append(parts, groupDigits(*p.ReviewsCount)+" reviews")
	}
	return strings.Join(parts, ", ")
}

// cardHistogram draws the five buckets, scaled so the fullest one fills the bar.
//
// Scaling to the largest bucket rather than to a hundred is what makes the shape
// readable: almost every product on Amazon has a majority of five star ratings,
// and a chart drawn to an absolute scale is four short stubs under one long bar
// on every product anybody looks at.
func cardHistogram(w io.Writer, p amz.Product) {
	d := p.Distribution
	if d == nil {
		return
	}
	maxPct := 0
	for _, pct := range d.Percent {
		if pct > maxPct {
			maxPct = pct
		}
	}
	if maxPct == 0 {
		return
	}
	for star := 5; star >= 1; star-- {
		pct := d.Percent[star-1]
		// A bucket with a percentage in it always gets a block. Two percent of
		// a hundred thousand ratings is two thousand people, and a bar that
		// rounds them off the chart reads as nobody.
		n := pct * cardBar / maxPct
		if n == 0 && pct > 0 {
			n = 1
		}
		_, _ = fmt.Fprintf(w, "  %d ★ %-*s %3d%%\n", star, cardBar, strings.Repeat("█", n), pct)
	}
	// The counts on this record are percent times total, and the record says so
	// on every copy of itself. Saying it here too means nobody reads a bucket off
	// a terminal and writes it down as a number Amazon published.
	if d.Count != nil {
		line := strings.Repeat(" ", cardGutter) + "counts derived from integer percentages"
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintln(w)
}

// cardFacts writes the labelled lines under the histogram.
func cardFacts(w io.Writer, p amz.Product, verbose int) {
	var wrote bool
	fact := func(label, value, via string) {
		if value == "" {
			return
		}
		wrote = true
		if verbose >= 2 && via != "" {
			value += "  (" + via + ")"
		}
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", cardLabel, label, value)
	}

	fact("Brand", refName(p.Brand), viaOf(p, "brand"))
	fact("Authors", refNames(p.Authors), viaOf(p, "authors"))
	fact("Rank", rankText(p), viaOf(p, "ranks"))
	fact("Variants", variantText(p), viaOf(p, "variation"))
	fact("Category", breadcrumbText(p), viaOf(p, "breadcrumb"))
	fact("Bought", p.BoughtPastMonth, viaOf(p, "bought_past_month"))
	if wrote {
		_, _ = fmt.Fprintln(w)
	}
}

// marketplaceHost prints the storefront the way somebody would say it out loud.
// The slug is what the record carries and "us" is not a thing anybody types.
func marketplaceHost(slug string) string {
	if m, ok := amz.LookupMarketplace(slug); ok {
		return strings.TrimPrefix(m.Host, "www.")
	}
	return slug
}

func refNames(refs []amz.Ref) string {
	var names []string
	for _, r := range refs {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	return strings.Join(names, ", ")
}

// rankText prints the department rank first, because that is what people mean by
// sales rank, and then the subcategories in the order the page listed them.
func rankText(p amz.Product) string {
	var overall, rest []string
	for _, r := range p.Ranks {
		s := "#" + groupDigits(int64(r.Rank))
		if r.Category != "" {
			s += " in " + r.Category
		}
		if r.Overall {
			overall = append(overall, s)
		} else {
			rest = append(rest, s)
		}
	}
	return strings.Join(append(overall, rest...), " · ")
}

// variantText says how many of the family the page shipped against how many
// exist, and names the axes it varies along.
func variantText(p amz.Product) string {
	v := p.Variation
	if v == nil {
		return ""
	}
	shown := strconv.Itoa(len(v.Siblings))
	if v.TotalCount != nil {
		shown = fmt.Sprintf("%d of %d", len(v.Siblings), *v.TotalCount)
	}
	s := shown + " shown"
	var dims []string
	for _, d := range v.Dimensions {
		dims = append(dims, dimensionLabel(d.Name))
	}
	if len(dims) > 0 {
		s += "  ·  " + strings.Join(dims, ", ")
	}
	return s
}

// dimensionLabel turns the twister's key into the word the page prints above the
// swatches. Amazon declares color_name and size_name and shows Color and Size,
// and the raw key belongs in the JSON rather than on a card a person reads.
func dimensionLabel(name string) string {
	s := strings.TrimSuffix(name, "_name")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return name
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func breadcrumbText(p amz.Product) string {
	var names []string
	for _, b := range p.Breadcrumb {
		if b.Name != "" {
			names = append(names, b.Name)
		}
	}
	return strings.Join(names, " › ")
}

// cardNotRead prints the envelope's misses.
//
// Every line here is generated from the record. Nothing in this function knows
// what a review corpus is or why the offers panel cannot be read, so the day
// Amazon opens one of them back up the line disappears on its own and no stale
// sentence is left behind in the source.
func cardNotRead(w io.Writer, p amz.Product) {
	misses := p.Envelope.Missed
	if len(misses) == 0 {
		return
	}
	sorted := make([]amz.Miss, len(misses))
	copy(sorted, misses)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })

	width := 0
	for _, m := range sorted {
		if len(m.Field) > width {
			width = len(m.Field)
		}
	}
	_, _ = fmt.Fprintln(w, "  not read")
	hang := 4 + width + 1
	for _, m := range sorted {
		detail := m.Why
		if m.Total > 0 {
			detail = fmt.Sprintf("%s of %s. %s", groupDigits(int64(m.Have)), groupDigits(m.Total), m.Why)
		}
		lines := wrapText(detail, cardWidth-hang)
		_, _ = fmt.Fprintf(w, "    %-*s %s\n", width, m.Field, lines[0])
		for _, l := range lines[1:] {
			_, _ = fmt.Fprintf(w, "%s%s\n", strings.Repeat(" ", hang), l)
		}
		if m.Fix != "" {
			_, _ = fmt.Fprintf(w, "%srun `%s` for the detail\n", strings.Repeat(" ", hang), m.Fix)
		}
	}
}

// wrapText breaks a sentence at word boundaries so a long explanation reads as a
// paragraph rather than as one line the terminal folds wherever it runs out.
//
// It always returns at least one element, so the caller can print the first line
// on the label's row without checking.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	if width < 20 {
		width = 20
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}

// viaOf returns the provenance of the first of these fields the record has one
// for, which is what -vv prints.
func viaOf(p amz.Product, fields ...string) string {
	for _, f := range fields {
		if via := p.Envelope.Via[f]; via != "" {
			return via
		}
	}
	return ""
}

// groupDigits writes a count the way a person reads one. Amazon prints 48,127
// and a card that prints 48127 next to it looks like a different number.
func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
