package amz

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Depth is how much of a product a caller wants.
//
// Four levels and only three cost profiles, because quick and meta are both one
// request and differ in which page they ask for.
//
// Quick was specified as the cheap read, on a measurement that said /gp/aw/d/
// returns 374 KB against 2.2 MB for /dp/. That comparison was wrong, and it was
// wrong in a way worth writing down, because it is a mistake anyone measuring a
// page with curl can make. Amazon gzips both responses. 374 KB is what /gp/aw/d/
// weighs on the wire and 2.2 MB is what /dp/ weighs after decoding, so the two
// numbers were never comparable. Measured on 2026-08-17 for B075F5X8BR:
//
//	/gp/aw/d/  373,945 bytes on the wire, 2,197,291 decoded
//	/dp/       373,980 bytes on the wire, 2,196,553 decoded
//
// It is the same page at the same cost, and it yields the same 26 fields. Quick
// is kept because it is the only thing that reads s2, so the surface stays
// exercised and the day Amazon serves that URL a lighter rendering again the
// saving is one flag away. It is not kept because it saves anything today.
//
// See notes/Spec/3007/03_model.md section 11.
type Depth string

const (
	// DepthQuick reads /gp/aw/d/, which is s2. One request, and byte for byte
	// the same page as meta today, which the envelope states by naming the
	// surface rather than leaving the caller to assume a saving that is not
	// there.
	DepthQuick Depth = "quick"
	// DepthMeta reads the desktop detail page and skips the recommendation
	// strips. One request. This is the default.
	DepthMeta Depth = "meta"
	// DepthFull is meta plus the rails. The same request: the strips are already
	// on the page and the only reason they are not always on is that sixty extra
	// cards make a terminal record unreadable.
	DepthFull Depth = "full"
	// DepthDeep is full plus a request per variation sibling and one for the
	// seller. This is the level that multiplies, so it is the level that has to
	// say what it will cost before it spends it.
	DepthDeep Depth = "deep"
)

// Depths lists the levels in increasing cost, which is the order they are
// printed in and the order the flag help reads.
func Depths() []Depth { return []Depth{DepthQuick, DepthMeta, DepthFull, DepthDeep} }

// ParseDepth reads a depth name, naming the alternatives when it cannot.
func ParseDepth(s string) (Depth, error) {
	switch d := Depth(strings.ToLower(strings.TrimSpace(s))); d {
	case "":
		return DepthMeta, nil
	case DepthQuick, DepthMeta, DepthFull, DepthDeep:
		return d, nil
	default:
		names := make([]string, 0, 4)
		for _, v := range Depths() {
			names = append(names, string(v))
		}
		return "", fmt.Errorf("unknown depth %q, want one of %s", s, strings.Join(names, ", "))
	}
}

// Valid reports whether d is one of the four.
func (d Depth) Valid() bool {
	_, err := ParseDepth(string(d))
	return err == nil && d != ""
}

// Surface is the op this depth reads first: s2 for quick and s1 for the rest.
func (d Depth) Surface() string {
	if d == DepthQuick {
		return "s2"
	}
	return "s1"
}

// Rails reports whether the recommendation strips are kept.
//
// They are read either way, because reading them is free once the page is
// parsed and because dropping them would mean the envelope claiming their
// regions were unread when they were not. This decides whether they stay in the
// record.
func (d Depth) Rails() bool { return d == DepthFull || d == DepthDeep }

// Siblings reports whether each variation sibling is fetched in its own right.
func (d Depth) Siblings() bool { return d == DepthDeep }

// Cost is the number of requests this depth will make for a product with n
// variation siblings, which is what the caller is asked to approve before deep
// spends it.
func (d Depth) Cost(siblings int) int {
	if d != DepthDeep {
		return 1
	}
	// The page itself, one per sibling, and one for the seller.
	return 1 + siblings + 1
}

// DeepConfirmAt is where deep stops being obviously cheap. A product with four
// colours costs six requests and nobody needs to be asked; the apparel capture
// measured on 2026-08-17 claims 88 siblings, which is a minute and a half of
// polite crawling for one record.
const DeepConfirmAt = 20

// FetchProductDepth fetches one product at the requested depth.
//
// Every level returns the same record type. A quick read is not a different
// shape, it is the same shape with fewer fields set and an envelope that names
// the surface it came from, so a consumer can tell a field that is absent from a
// field that was never on the page it read.
func (c *Client) FetchProductDepth(ctx context.Context, asinOrURL string, d Depth) (Product, error) {
	if !d.Valid() {
		return Product{}, fmt.Errorf("depth %q is not one of quick, meta, full, deep", d)
	}
	asin, url := c.ResolveProductURL(asinOrURL)
	if url == "" {
		return Product{}, ErrNotFound
	}
	if d == DepthQuick {
		url = c.LightProductURL(asin)
	}
	body, src, err := c.GetSource(ctx, url, 6*time.Hour)
	if err != nil {
		return Product{}, err
	}
	p, err := c.parseProduct(asin, url, body)
	if err != nil {
		return p, err
	}
	p.Envelope.Depth = string(d)
	c.record(ctx, &p.Envelope, src)
	if d.Siblings() {
		c.ResolveDeep(ctx, &p)
	}
	if !d.Rails() {
		// The rails were read and are being dropped, which is a choice this
		// record has to own. Saying so costs one line and stops a reader
		// concluding that a product with twelve strips on the page has none.
		if n := len(p.Rails); n > 0 {
			p.Envelope.Missed = append(p.Envelope.Missed, Miss{
				Field: "rails",
				Why:   fmt.Sprintf("the page carries %d recommendation strips and this depth drops them", n),
				Have:  0,
				Total: int64(n),
				Fix:   "amz product --depth full",
			})
			p.Rails = nil
		}
	}
	return p, nil
}

// ResolveDeep spends the extra requests on a record that has already been read.
//
// It is separate from FetchProductDepth because the cost is not knowable until
// the page is parsed: a product with four colours costs six requests and one
// with eighty eight costs ninety. A caller that has to ask a person before
// spending that fetches at full, counts the siblings, and calls this when the
// answer is yes.
func (c *Client) ResolveDeep(ctx context.Context, p *Product) {
	c.resolveSiblings(ctx, p)
	c.resolveSeller(ctx, p)
	p.Envelope.Depth = string(DepthDeep)
}

// resolveSiblings fetches every variation sibling and fills in the three facts
// the parent page will not state about them.
//
// The twister names the siblings and gives their dimension values, and it says
// nothing about what any of them costs or whether it can be bought. A record
// that filled those in from the parent would be quoting the selected variant's
// price against every colour in the family, which is wrong in a way nobody
// downstream can detect.
//
// A sibling that fails to fetch is left as it was and recorded, because one
// timeout in eighty eight is not a reason to throw away the other eighty seven.
func (c *Client) resolveSiblings(ctx context.Context, p *Product) {
	if p.Variation == nil {
		return
	}
	failed := 0
	for i := range p.Variation.Siblings {
		s := &p.Variation.Siblings[i]
		if s.ASIN == "" || s.ASIN == p.ASIN {
			continue
		}
		url := c.ProductURL(s.ASIN)
		body, src, err := c.GetSource(ctx, url, 6*time.Hour)
		if err != nil {
			failed++
			continue
		}
		sp, err := c.parseProduct(s.ASIN, url, body)
		if err != nil {
			failed++
			continue
		}
		if sp.Offer != nil {
			s.Price = sp.Offer.Price
			s.Available = sp.Offer.InStock
		}
		if len(sp.ImageURLs) > 0 {
			s.Image = sp.ImageURLs[0]
		}
		c.record(ctx, &p.Envelope, src)
	}
	if failed > 0 {
		p.Envelope.Missed = append(p.Envelope.Missed, Miss{
			Field: "variant_asins",
			Why:   fmt.Sprintf("%d of %d siblings did not fetch", failed, len(p.Variation.Siblings)),
			Have:  len(p.Variation.Siblings) - failed,
			Total: int64(len(p.Variation.Siblings)),
			Fix:   "amz product on each sibling asin",
		})
	}
}

// resolveSeller turns the buy box's seller name into a resolved reference.
//
// The detail page gives a merchant id and a display name, which is enough to
// point at a seller and not enough to say anything about them. The seller page
// gives the rating and the feedback counts, and this is the only depth that
// spends a request to get them.
func (c *Client) resolveSeller(ctx context.Context, p *Product) {
	if p.Offer == nil || p.Offer.SoldBy == nil || p.Offer.SoldBy.ID == "" {
		return
	}
	s, err := c.FetchSeller(ctx, p.Offer.SoldBy.ID)
	if err != nil {
		p.Envelope.Missed = append(p.Envelope.Missed, Miss{
			Field:    "sold_by",
			Why:      "the seller page did not fetch: " + err.Error(),
			Surfaces: []string{"/sp?seller="},
			Fix:      "amz seller " + p.Offer.SoldBy.ID,
		})
		return
	}
	if s.Name != "" {
		p.Offer.SoldBy.Name = s.Name
	}
	p.Offer.SoldBy.URL = s.URL
	p.Offer.SoldBy.Resolved = true
	// The seller record already carries the response it was built from, with the
	// size and the cache flag the fetch measured. Copying those across beats
	// synthesizing a source here that knows the URL and nothing else.
	for _, src := range s.Envelope.Sources {
		c.record(ctx, &p.Envelope, src)
	}
}

// surfaceOf names the op a URL belongs to, so a source carries the surface id
// the registry uses rather than one written out by hand at the call site and
// left behind when the registry moves.
func surfaceOf(rawURL string) string {
	if op := OpFor(rawURL); op != nil {
		return op.ID
	}
	return ""
}

// LightProductURL is the URL Amazon serves its mobile rendering from, which is
// s2, and which returns the same page as /dp/ to this client.
func (c *Client) LightProductURL(asin string) string {
	return c.BaseURL() + "/gp/aw/d/" + asin
}
