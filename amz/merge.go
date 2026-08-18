package amz

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// The merge policy, from notes/Spec/3007/06_implementation.md section 6.
//
// The problem it exists for is one sentence long. A full read of /dp/ carries A+
// content, rails, the variant matrix and the details table. A light read of
// /gp/aw/d/ carries none of them, because that surface never had them. If a
// light read overwrote a full one field by field, the second crawl of a product
// would delete most of what the first one found, and nothing would report an
// error, because from the store's point of view a newer record simply said less.
//
// So absence is not a value. A field the incoming surface never carries is left
// alone. A field the incoming surface does carry is written even when it is
// empty, because that surface looking and finding nothing is a real observation:
// a product that went out of stock has to be able to say so.
//
// `op.Carries` reads `Ops.Fields`. An unknown surface carries everything, which
// is the safe direction: the alternative is a record from a surface nobody
// registered silently failing to update anything.
//
// Prices and ranks are not here at all. They are append only rows in their own
// tables, so no merge bug can reach the history.

// mergeField maps a Product struct field to the Ops.Fields name that governs it.
//
// It is written out rather than derived from the json tag, because the two
// vocabularies are different on purpose. Ops.Fields is the spec's list of what a
// surface carries, which is coarser than the struct: one entry named "price"
// governs the whole Offer, because no surface ships a price without shipping the
// availability next to it. A struct field with no entry here is governed by
// nothing and always takes the newest value.
var mergeField = map[string]string{
	"Title":            "title",
	"Brand":            "brand",
	"Manufacturer":     "brand",
	"Byline":           "brand",
	"Authors":          "author",
	"Offer":            "price",
	"OtherOffers":      "price",
	"Rating":           "rating",
	"RatingsCount":     "ratings_count",
	"ReviewsCount":     "ratings_count",
	"Distribution":     "rating_histogram",
	"Reviews":          "reviews",
	"Questions":        "reviews",
	"ReviewSample":     "reviews",
	"QASample":         "reviews",
	"Breadcrumb":       "category_path",
	"Ranks":            "browse_node_ids",
	"Bullets":          "bullet_points",
	"Description":      "description",
	"Details":          "description",
	"Variation":        "variant_asins",
	"Images":           "images",
	"ImageURLs":        "images",
	"Videos":           "images",
	"Rails":            "rails",
	"SimilarASINs":     "similar_asins",
	"BoughtPastMonth":  "bought_past_month",
	"BoughtPastMonthN": "bought_past_month",
	"ISBN10":           "description",
	"ISBN13":           "description",
	"ModelNumber":      "description",
	"UPC":              "description",
	"EAN":              "description",
}

// Carries reports whether this surface can produce a field.
//
// A nil Op, or one with no declared field list, carries everything. That is the
// direction that cannot lose data: guessing that an unknown surface carries a
// field means a newer read wins, and guessing the other way means a whole crawl
// writes nothing and says it succeeded.
func (o *Op) Carries(field string) bool {
	if o == nil || len(o.Fields) == 0 {
		return true
	}
	for _, f := range o.Fields {
		if f == field {
			return true
		}
	}
	return false
}

// OpByID returns the registered surface with this id or name, or nil.
//
// It takes either spelling because the envelope records surfaces by name and the
// spec talks about them by id, and a merge that silently did nothing because it
// was handed "product" where it wanted "s1" would be invisible.
func OpByID(id string) *Op {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for _, o := range ops {
		if o.ID == id || o.Name == id {
			return o
		}
	}
	return nil
}

// Without returns a copy of this surface that no longer claims to carry the
// named fields.
//
// It exists for `amz crawl` without --with-text. That crawl reads the whole
// detail page and then throws the review and description text away before
// storing, which under the policy above would be indistinguishable from a page
// that had no description: the surface carries the field, the value is empty, so
// the merge clears it. A crawl that dropped text on purpose would then delete
// the text an earlier --with-text crawl stored. Narrowing the surface says the
// true thing instead, which is that this read is not evidence about description.
//
// A nil or unrestricted surface is widened to every field any surface declares
// before the subtraction, because "carries everything except description" cannot
// be expressed as an empty list.
func (o *Op) Without(fields ...string) *Op {
	drop := make(map[string]bool, len(fields))
	for _, f := range fields {
		drop[f] = true
	}
	have := o.fieldList()
	out := &Op{Fields: make([]string, 0, len(have))}
	if o != nil {
		out.ID, out.Name = o.ID, o.Name
	}
	for _, f := range have {
		if !drop[f] {
			out.Fields = append(out.Fields, f)
		}
	}
	return out
}

// fieldList is this surface's declared fields, or every field any surface
// declares when it declares none.
func (o *Op) fieldList() []string {
	if o != nil && len(o.Fields) > 0 {
		return o.Fields
	}
	seen := map[string]bool{}
	var all []string
	for _, op := range ops {
		for _, f := range op.Fields {
			if !seen[f] {
				seen[f] = true
				all = append(all, f)
			}
		}
	}
	sort.Strings(all)
	return all
}

// Merge folds an incoming product into the stored one under the policy above.
//
// Newest wins on every field the incoming surface carries. Every other field is
// left as it was stored, because that surface never had it and absence from a
// surface that cannot see a field is not evidence the field is gone.
func Merge(existing, incoming Product, op *Op) Product {
	// The identity fields are never merged: they are the key, and a record
	// arriving with a different ASIN is a bug in the caller rather than an
	// update. Taking the incoming ones keeps this a pure function of its
	// arguments even when the caller has passed something inconsistent.
	out := existing
	out.ASIN = incoming.ASIN
	out.Marketplace = incoming.Marketplace
	out.URL = firstNonEmptyStr(incoming.URL, existing.URL)
	out.FetchedAt = incoming.FetchedAt
	out.Envelope = incoming.Envelope

	ev := reflect.ValueOf(&out).Elem()
	iv := reflect.ValueOf(incoming)
	t := ev.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Name
		switch name {
		case "ASIN", "Marketplace", "URL", "FetchedAt", "Envelope":
			continue
		}
		in := iv.Field(i)
		if !in.IsZero() {
			// The surface produced a value. Newest wins, whatever the field list
			// says, because a value that exists is not something to argue with.
			ev.Field(i).Set(in)
			continue
		}
		// Absent. Only a surface that carries the field may clear it.
		if f, ok := mergeField[name]; ok && !op.Carries(f) {
			continue
		}
		if _, governed := mergeField[name]; !governed && op != nil && len(op.Fields) > 0 {
			// Ungoverned fields on a known partial surface are left alone too.
			// ParentASIN is the example: no surface declares it, and a light
			// read that omits it is not saying the product has no parent.
			continue
		}
		ev.Field(i).Set(in)
	}

	// Extra is a union rather than a replacement. Each blob is keyed by the
	// a-state name Amazon gave it, so two surfaces shipping different blobs is
	// the normal case and keeping only the newest surface's set would drop the
	// other's for no reason.
	if len(existing.Extra) > 0 {
		merged := make(map[string]json.RawMessage, len(existing.Extra)+len(incoming.Extra))
		for k, v := range existing.Extra {
			merged[k] = v
		}
		for k, v := range incoming.Extra {
			merged[k] = v
		}
		out.Extra = merged
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
