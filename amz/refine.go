package amz

import (
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// The refinement vocabulary is read, never compiled in.
//
// One query on 2026-08-17 offered thirty-three refinement groups. Four of the
// codes mean the same thing on every search and the rest do not:
// p_n_g-1003532609111 is "Key Count" on a keyboard search and does not exist on
// a search for shoes. A table of those codes in this package would be a table of
// one category's attributes, right for keyboards, wrong everywhere else, and
// stale the first time Amazon renumbers an attribute.
//
// So the sidebar is the source. Every group, its code, its human label and its
// values are read off the page, and the six codes below are the only ones this
// package claims to know without looking.
//
// See notes/Spec/3007/07_search.md section 2.

// RefineValue is one selectable value in a refinement group.
type RefineValue struct {
	// ID is the value as it goes into rh, taken from the list item's id rather
	// than from the href, because the href carries ref, qid, rnid and ds and
	// replaces rh wholesale.
	ID string `json:"id"`
	// Label is what a person reads: "Logitech", "4 Stars & Up".
	Label string `json:"label"`
	// Applied is true when this search already has the value on. Amazon marks it
	// with aria-current and flips the link from Apply to Remove.
	Applied bool `json:"applied,omitempty"`
}

// RefineGroup is one refinement group as the page offered it.
type RefineGroup struct {
	// Code is the rh group code: p_123, p_n_condition-type, n.
	Code string `json:"code"`
	// Label is Amazon's heading for the group: "Brands", "Condition".
	Label string `json:"label"`
	// Scope says how far this code travels, per RefineScope.
	Scope  string        `json:"scope"`
	Values []RefineValue `json:"values"`
}

// Applied is the values in this group the current search already carries.
func (g RefineGroup) Applied() []string {
	var out []string
	for _, v := range g.Values {
		if v.Applied {
			out = append(out, v.ID)
		}
	}
	return out
}

// Value finds a value by id or by label, case insensitively on the label.
//
// Both are accepted because a person types "Logitech" and a script pins 213704,
// and the id is the one that survives a rerun: Amazon's brand ids are stable and
// its labels are not, so --brand prints the id it resolved to.
func (g RefineGroup) Value(s string) (RefineValue, bool) {
	for _, v := range g.Values {
		if v.ID == s {
			return v, true
		}
	}
	for _, v := range g.Values {
		if strings.EqualFold(v.Label, s) {
			return v, true
		}
	}
	return RefineValue{}, false
}

// The four scopes a refinement code can have. A consumer that wants to cache a
// code needs to know how far it travels, and the answer is not the same for
// p_123 and for p_n_g-1003532609111.
const (
	// ScopeGlobal means the code means the same thing on every search in this
	// marketplace. Six of them, listed in globalRefinements.
	ScopeGlobal = "global"
	// ScopeAttribute is p_n_g-<numeric>, one product attribute of one category.
	// The number is meaningless outside the category that offered it.
	ScopeAttribute = "attribute"
	// ScopeFeature is p_n_feature_browse-bin and its numbered siblings, which
	// are a category's own feature bins.
	ScopeFeature = "feature"
	// ScopeNamed is a named attribute like p_n_condition-type: global in
	// spelling, and its values are still per category.
	ScopeNamed = "named"
	// ScopeNode is n, the browse node, which is a category id and not a
	// refinement value at all.
	ScopeNode = "node"
)

// globalRefinements is the whole compiled-in vocabulary: six codes, each one
// measured to mean the same thing on any query.
//
// This map is deliberately tiny and the test that guards it is
// TestOnlySixGlobalRefinementCodes. Anything else a search offers is discovered
// at read time or not used.
var globalRefinements = map[string]string{
	"p_72":  "minimum average customer review",
	"p_123": "brand, by brand id",
	"p_6":   "seller, by merchant id",
	"p_36":  "price, a range in the marketplace's minor unit",
	"p_85":  "Prime eligibility",
	"p_76":  "free shipping eligibility",
}

// GlobalRefinements is the six codes this package knows without reading a page,
// with a line each on what they mean.
func GlobalRefinements() map[string]string {
	out := make(map[string]string, len(globalRefinements))
	for k, v := range globalRefinements {
		out[k] = v
	}
	return out
}

var attrCodeRe = regexp.MustCompile(`^p_n_g-\d+$`)

// RefineScope says how far a refinement code travels.
func RefineScope(code string) string {
	switch {
	case code == "n":
		return ScopeNode
	case globalRefinements[code] != "":
		return ScopeGlobal
	case attrCodeRe.MatchString(code):
		return ScopeAttribute
	case strings.Contains(code, "browse-bin"):
		return ScopeFeature
	default:
		return ScopeNamed
	}
}

// applyLabelRe pulls the value's name out of the link's own description.
//
// The visible text is not reliable for every group. A star filter draws "4
// Stars" in an icon's alt text and "& Up" in the span beside it, and joining
// those two gives the right answer only by accident of ordering. The aria-label
// is one string Amazon wrote for the same purpose, and it says whether the
// filter is being offered or withdrawn, which is where Applied comes from.
var applyLabelRe = regexp.MustCompile(`^(Apply|Remove)\s+(.*?)\s+filter\b`)

// readRefinements reads every refinement group the page offered.
//
// It walks the filter lists rather than every link in the sidebar. Amazon puts
// the group code on the list and the group code plus the value id on each item,
// so both halves of an rh term are read from ids that Amazon generated from its
// own data, and neither is parsed out of an href.
func readRefinements(r Region) []RefineGroup {
	if !r.Exists() {
		return nil
	}
	var out []RefineGroup
	r.Find("ul[id^='filter-']").Each(func(_ int, ul *goquery.Selection) {
		code := strings.TrimPrefix(attrOf(ul, "id"), "filter-")
		if code == "" {
			// The "Popular Shopping Ideas" strip is a filter list with no group
			// code, because its links are new queries rather than refinements of
			// this one. It has no rh term and is not a refinement.
			return
		}
		g := RefineGroup{Code: code, Scope: RefineScope(code), Label: refineGroupLabel(r, code)}
		ul.Find("li[id^='" + code + "/']").Each(func(_ int, li *goquery.Selection) {
			id := strings.TrimPrefix(attrOf(li, "id"), code+"/")
			if id == "" {
				return
			}
			v := RefineValue{ID: id}
			a := li.Find("a[aria-label]").First()
			if m := applyLabelRe.FindStringSubmatch(collapseSpace(attrOf(a, "aria-label"))); m != nil {
				v.Applied = m[1] == "Remove"
				v.Label = m[2]
			}
			// The department group states the applied node differently from
			// every other group. There is no anchor and no remove label on it,
			// because Amazon has nothing to link a node to except itself: the
			// value is drawn as bold text with its children indented under it.
			// Reading only the aria-label would report a search that plainly is
			// filtered to Electronics as having no department applied.
			if !v.Applied && li.Find("a").Length() == 0 {
				if bold := li.Find("span.a-text-bold").First(); bold.Length() > 0 {
					v.Applied = true
					if v.Label == "" {
						v.Label = collapseSpace(bold.Text())
					}
				}
			}
			if v.Label == "" {
				v.Label = collapseSpace(nodeText(li))
			}
			g.Values = append(g.Values, v)
		})
		if len(g.Values) > 0 {
			out = append(out, g)
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// refineGroupLabel finds the heading Amazon wrote above a group.
func refineGroupLabel(r Region, code string) string {
	return collapseSpace(r.Find("#" + cssEscapeID(code) + "-title").First().Text())
}

// cssEscapeID escapes the characters an Amazon refinement code carries that a
// CSS id selector would otherwise read as syntax.
//
// p_n_g-1003532609111 is fine. p_n_feature_thirty-four_browse-bin is fine. The
// one that is not is any code with a dot or a colon in it, and rather than
// asserting none exists this escapes what CSS says to escape, because the day
// one does the failure would be a group with no label and no explanation.
func cssEscapeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SortOption is one entry of the sort dropdown, read from the page.
type SortOption struct {
	// Value is what goes in s=, "price-asc-rank".
	Value string `json:"value"`
	// Label is Amazon's own wording, "Price: Low to High".
	Label string `json:"label"`
	// Selected marks the sort this page was served with, which is how the record
	// states its sort rather than echoing the flag that was passed.
	Selected bool `json:"selected,omitempty"`
}

// readSorts reads the sort dropdown.
//
// Six values on every capture taken. They are read rather than compiled in for
// the same reason as everything else here: "exact-aware-popularity-rank" is not
// a name anybody would guess, and a sort value Amazon no longer accepts is
// silently ignored rather than refused, so a guess would return the default
// ordering under a label claiming otherwise.
func readSorts(r Region) []SortOption {
	if !r.Exists() {
		return nil
	}
	var out []SortOption
	r.Find("option[value]").Each(func(_ int, o *goquery.Selection) {
		v := strings.TrimSpace(attrOf(o, "value"))
		if v == "" {
			return
		}
		_, selected := o.Attr("selected")
		out = append(out, SortOption{Value: v, Label: collapseSpace(o.Text()), Selected: selected})
	})
	return out
}

// Department is one entry of the search scope dropdown.
type Department struct {
	// Alias is the value of i=, "electronics-intl-ship".
	Alias string `json:"alias"`
	// Label is the department name, "Electronics".
	Label string `json:"label"`
}

var searchAliasRe = regexp.MustCompile(`^search-alias=(.+)$`)

// readDepartments reads the department dropdown beside the search box.
//
// The aliases carry an -intl-ship suffix on every capture, because the client
// sends no location and no Accept-Language and Amazon serves the international
// shipping variant of the list. A client in a US state gets "electronics" where
// this gets "electronics-intl-ship". Both work as i=, and that difference is the
// whole reason the list is read per marketplace and cached rather than shipped
// in the binary.
func readDepartments(r Region) []Department {
	if !r.Exists() {
		return nil
	}
	var out []Department
	r.Find("option[value^='search-alias=']").Each(func(_ int, o *goquery.Selection) {
		m := searchAliasRe.FindStringSubmatch(strings.TrimSpace(attrOf(o, "value")))
		if m == nil {
			return
		}
		out = append(out, Department{Alias: m[1], Label: collapseSpace(o.Text())})
	})
	return out
}
