package amz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Getting past 306 results is not a pagination problem.
//
// Amazon serves twenty pages and 306 results per search whatever the corpus is,
// so a query with forty thousand results has 39,694 of them behind a wall that
// no amount of paging opens. The way through is to ask smaller questions: one
// search per brand, or per price band, unioned on ASIN. Each of those searches
// gets its own 306, and a cell that still hits the ceiling gets split again.
//
// This is expensive and it is meant to be visible. --dry-run prints the request
// count and the wall clock before anything is fetched, and the recursion stops
// at depth 2 unless somebody says otherwise, because depth 3 on a broad query is
// tens of thousands of requests and that should be a decision rather than a
// default.
//
// See notes/Spec/3007/07_search.md section 4.

// PartitionOptions controls a partitioned search.
type PartitionOptions struct {
	// Group is the refinement code to split on. Empty means the group with the
	// most values, which is usually brands.
	Group string
	// Depth bounds the recursion. 1 splits once, 2 splits the cells that still
	// hit the ceiling, and 0 means the default of 2.
	Depth int
	// DryRun plans the walk and fetches nothing beyond the one page needed to
	// read the vocabulary.
	DryRun bool
}

// PartitionPlan is what a partitioned search intends to do, priced up.
type PartitionPlan struct {
	Query string `json:"query"`
	// Group and Label name the refinement being split on.
	Group string `json:"group"`
	Label string `json:"label"`
	// Cells is how many searches the split produces.
	Cells int `json:"cells"`
	// WorstCase is cells times the page cap, which is the number to be scared
	// of. Most cells finish in one or two pages.
	WorstCase int `json:"worst_case_requests"`
	// WorstCaseTime is WorstCase at the client's configured pace.
	WorstCaseTime time.Duration `json:"worst_case_duration"`
	// Alternatives are the other groups this query could be split on, smallest
	// first, so a caller who does not want 164 searches can pick 8.
	Alternatives []PartitionAlt `json:"alternatives,omitempty"`
}

// PartitionAlt is one other group a query could be split on.
type PartitionAlt struct {
	Group  string `json:"group"`
	Label  string `json:"label"`
	Values int    `json:"values"`
}

// PartitionSummary is what a partitioned search actually did.
type PartitionSummary struct {
	Plan PartitionPlan `json:"plan"`
	// Cells is one entry per search that was run, in the order they finished.
	Cells []CellResult `json:"cells"`
	// Unique is how many distinct ASINs came out, and Duplicates how many times
	// a result was seen in a cell it had already been found in. The second
	// number is the reason the union is on ASIN rather than a concatenation.
	Unique     int `json:"unique"`
	Duplicates int `json:"duplicates"`
	// Capped is the cells that hit the ceiling and were not split further,
	// which is exactly the list of places this walk is known to be incomplete.
	Capped []string `json:"capped,omitempty"`
	// Ignored is the cells whose refinement Amazon did not apply. The sidebar
	// offered the value, the rh term went out, and the page came back unfiltered,
	// so the cell read nothing and the ground it was meant to cover is uncovered.
	Ignored []string `json:"ignored,omitempty"`
}

// CellResult is one cell of the partition and what it returned.
type CellResult struct {
	Cell    string        `json:"cell"`
	Results int           `json:"results"`
	Summary SearchSummary `json:"summary"`
	// Ignored means Amazon served this cell unfiltered, so it read nothing.
	Ignored bool `json:"ignored,omitempty"`
}

// defaultPartitionDepth is two. One split is usually not enough on a broad
// query and three is a bill nobody asked for.
const defaultPartitionDepth = 2

// PlanPartition reads a query's vocabulary and prices up the split.
//
// It fetches one page. That page is the only way to know what the query offers,
// and it is the same page --dry-run would have to fetch to tell the truth about
// what a run would cost.
func (c *Client) PlanPartition(ctx context.Context, query string, q SearchQuery, opts PartitionOptions) (PartitionPlan, []RefineGroup, error) {
	if q.NeedsResolve() {
		resolved, err := c.ResolveRefinements(ctx, query, q)
		if err != nil {
			return PartitionPlan{}, nil, err
		}
		q = resolved
	}
	sp, err := c.fetchSearchPage(ctx, query, q, 1)
	if err != nil {
		return PartitionPlan{}, nil, err
	}
	plan, err := planFrom(query, sp.Refinements, opts.Group, c.delay)
	return plan, sp.Refinements, err
}

// planFrom picks the group and prices the split.
func planFrom(query string, groups []RefineGroup, want string, pace time.Duration) (PartitionPlan, error) {
	usable := partitionable(groups, nil)
	if len(usable) == 0 {
		return PartitionPlan{Query: query}, fmt.Errorf("%q offers no refinement group that can be split on, so there is no way past the %d result ceiling for this query",
			query, searchResultCeiling)
	}
	var pick RefineGroup
	if want != "" {
		found := false
		for _, g := range usable {
			if g.Code == want {
				pick, found = g, true
				break
			}
		}
		if !found {
			return PartitionPlan{Query: query}, fmt.Errorf("%w: --partition %s, and this query offers %s",
				ErrRefinementUnoffered, want, groupCodeList(usable))
		}
	} else {
		pick = usable[0]
	}

	plan := PartitionPlan{
		Query:     query,
		Group:     pick.Code,
		Label:     pick.Label,
		Cells:     len(pick.Values),
		WorstCase: len(pick.Values) * searchPageCap,
	}
	plan.WorstCaseTime = time.Duration(plan.WorstCase) * pace
	for _, g := range usable {
		if g.Code == pick.Code {
			continue
		}
		plan.Alternatives = append(plan.Alternatives, PartitionAlt{Group: g.Code, Label: g.Label, Values: len(g.Values)})
	}
	sort.SliceStable(plan.Alternatives, func(i, j int) bool {
		return plan.Alternatives[i].Values < plan.Alternatives[j].Values
	})
	return plan, nil
}

// partitionable is the groups worth splitting on, most values first.
//
// A group with one value splits nothing. The browse node group is left out
// because its values nest, so splitting on it would run the parent category and
// each of its children as separate searches and count everything twice.
func partitionable(groups []RefineGroup, used map[string]bool) []RefineGroup {
	var out []RefineGroup
	for _, g := range groups {
		if len(g.Values) < 2 || g.Scope == ScopeNode || used[g.Code] {
			continue
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Values) > len(out[j].Values) })
	return out
}

func groupCodeList(groups []RefineGroup) string {
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, fmt.Sprintf("%s (%s, %d values)", g.Code, g.Label, len(g.Values)))
		if len(parts) == 6 {
			parts = append(parts, "...")
			break
		}
	}
	return strings.Join(parts, ", ")
}

// SearchAll runs a partitioned search and emits each distinct ASIN once.
//
// The union is exact on ASIN and every card carries the cells it was found in,
// so a result that turns up under three brands is one record that says so rather
// than three records that look like three products.
func (c *Client) SearchAll(ctx context.Context, query string, q SearchQuery, opts PartitionOptions, emit func(Card) error) (PartitionSummary, error) {
	var sum PartitionSummary
	plan, groups, err := c.PlanPartition(ctx, query, q, opts)
	if err != nil {
		return sum, err
	}
	sum.Plan = plan
	if opts.DryRun {
		return sum, nil
	}

	depth := opts.Depth
	if depth <= 0 {
		depth = defaultPartitionDepth
	}
	seen := map[string]*Card{}
	perr := c.partition(ctx, query, q, groups, plan.Group, depth, nil, map[string]bool{}, seen, &sum)

	// The union is emitted whatever the walk did, and the walk's error is
	// returned after. A partitioned search is fifty searches, and one of them
	// failing on cell forty is not a reason to throw away the other forty nine.
	// Returning early here meant a run that had read 263 cards printed none of
	// them and reported "0 unique results" beside a duplicate count that could
	// only have come from cards it had already thrown away.
	asins := make([]string, 0, len(seen))
	for a := range seen {
		asins = append(asins, a)
	}
	sort.Strings(asins)
	sum.Unique = len(asins)
	for _, a := range asins {
		if err := emit(*seen[a]); err != nil {
			return sum, err
		}
	}
	return sum, perr
}

// partition runs one level of the split and recurses into the cells that are
// still capped.
func (c *Client) partition(ctx context.Context, query string, q SearchQuery, groups []RefineGroup,
	code string, depth int, path []string, used map[string]bool, seen map[string]*Card, sum *PartitionSummary) error {

	var group RefineGroup
	for _, g := range groups {
		if g.Code == code {
			group = g
		}
	}
	if len(group.Values) == 0 {
		return nil
	}
	used[code] = true

	for _, v := range group.Values {
		cell := append(append([]string(nil), path...), group.Code+":"+v.ID)
		name := strings.Join(cell, " + ")

		sub := q
		sub.Refine = append(append([]Refinement(nil), q.Refine...), Refinement{
			Group: group.Code, Label: group.Label,
			Values: []string{v.ID}, ValueLabels: []string{v.Label},
		})

		var cellGroups []RefineGroup
		res := CellResult{Cell: name}
		s, err := c.SearchWalk(ctx, query, sub, func(p SearchPage) error {
			if cellGroups == nil {
				cellGroups = p.Refinements
			}
			for _, card := range p.Cards {
				res.Results++
				if prev, ok := seen[card.ASIN]; ok {
					sum.Duplicates++
					prev.Cells = appendCell(prev.Cells, name)
					continue
				}
				c := card
				c.Cells = []string{name}
				seen[card.ASIN] = &c
			}
			return nil
		})
		if err != nil {
			// Amazon does not reject an rh term it will not honour. It drops the
			// term and serves the unfiltered grid with a 200, SearchWalk catches
			// that on the first page, and the cell yields no cards at all. For a
			// search somebody asked for that is an error and it stays one. Inside
			// --all it is one cell out of fifty, generated from the sidebar rather
			// than typed by anybody, and the honest answer is to record which
			// ground went uncovered and keep reading the rest.
			//
			// Measured on 2026-08-18: "usb-c hub" partitioned on p_123 into 50
			// cells, and cell 41 sub-split on p_n_cpf_labels, which amazon offered
			// in the sidebar and then ignored. That one cell aborted the union and
			// the run printed nothing.
			if errors.Is(err, ErrRefinementIgnored) || errors.Is(err, ErrRefinementUnoffered) {
				res.Ignored = true
				sum.Ignored = append(sum.Ignored, name)
				sum.Cells = append(sum.Cells, res)
				continue
			}
			return err
		}
		res.Summary = s
		sum.Cells = append(sum.Cells, res)

		if !s.Ceiling.Hit {
			continue
		}
		if depth <= 1 {
			sum.Capped = append(sum.Capped, name)
			continue
		}
		next := partitionable(cellGroups, used)
		if len(next) == 0 {
			sum.Capped = append(sum.Capped, name)
			continue
		}
		if err := c.partition(ctx, query, sub, cellGroups, next[0].Code, depth-1, cell, used, seen, sum); err != nil {
			return err
		}
	}
	return nil
}

func appendCell(cells []string, name string) []string {
	for _, c := range cells {
		if c == name {
			return cells
		}
	}
	return append(cells, name)
}
