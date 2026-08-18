package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// searchFlags is the resolved form of everything a search command takes.
//
// It sits between the flags and amz.SearchQuery because three of the flags are
// requests rather than values: --brand names a brand and rh takes a brand id,
// and turning one into the other means reading a page. The conversion is in one
// place so `amz search` and `amz refine` cannot disagree about what --brand
// means.
type searchFlags struct {
	refine    []string
	price     string
	sort      string
	dept      string
	brand     string
	seller    string
	condition string
	stars     int
	page      int
	maxPages  int

	all       bool
	partition string
	depth     int

	sponsored bool
	pages     bool
	enqueue   bool
	listDepts bool

	// The v0.2.x spellings. They still work and they say what to use instead.
	minPrice  int
	maxPrice  int
	minRating int
	prime     bool
}

// registerQuery adds the flags that describe the question.
//
// They are shared with `amz refine`, which asks the same question and prints the
// filters instead of the results. Asking what a filtered search offers next is
// how you get to a second filter, so `amz refine kindle --refine p_123=213704`
// has to mean something.
func (f *searchFlags) registerQuery(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringArrayVar(&f.refine, "refine", nil, "refine by group, repeatable: --refine p_123=213704,111070 (comma is OR within one group)")
	fl.StringVar(&f.price, "price", "", "price range in major units: 50-150, 50-, -150")
	fl.StringVar(&f.sort, "sort", "", "featured|price-asc|price-desc|review|newest|bestselling, or a raw amazon sort value")
	fl.StringVarP(&f.dept, "department", "d", "", "department alias for i=, as listed by --list-departments")
	fl.StringVar(&f.brand, "brand", "", "brand name or id, resolved against the sidebar")
	fl.StringVar(&f.seller, "seller", "", "seller name or merchant id, resolved against the sidebar")
	fl.StringVar(&f.condition, "condition", "", "new|used|renewed, resolved against the sidebar")
	fl.IntVar(&f.stars, "stars", 0, "minimum star rating, resolved against the sidebar")

	fl.IntVar(&f.minPrice, "min-price", 0, "minimum price")
	fl.IntVar(&f.maxPrice, "max-price", 0, "maximum price")
	fl.IntVar(&f.minRating, "min-rating", 0, "minimum star rating (1..4)")
	fl.BoolVar(&f.prime, "prime", false, "Prime-eligible only")
	_ = fl.MarkDeprecated("min-price", "use --price 50-150")
	_ = fl.MarkDeprecated("max-price", "use --price 50-150")
	_ = fl.MarkDeprecated("min-rating", "use --stars 4, which resolves the id from the page instead of guessing it")
}

// registerWalk adds the flags about how far to go, which only `amz search` has.
func (f *searchFlags) registerWalk(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.IntVar(&f.page, "page", 1, "first result page")
	fl.IntVar(&f.maxPages, "max-pages", 0, "stop after this many pages. it can lower the 20 page ceiling and not raise it")
	fl.BoolVar(&f.all, "all", false, "partition the query and union the cells, to get past the 306 result ceiling")
	fl.StringVar(&f.partition, "partition", "", "the refinement group to partition on. the default is the one with the most values")
	fl.IntVar(&f.depth, "partition-depth", 0, "how many times a capped cell may be split again (default 2)")
	fl.BoolVar(&f.sponsored, "include-sponsored", false, "keep the advertising placements, which are excluded by default")
	fl.BoolVar(&f.pages, "pages", false, "emit one record per page (counts, ceiling, refinements) instead of the cards")
	fl.BoolVar(&f.enqueue, "enqueue", false, "enqueue results into the crawl queue instead of printing")
	fl.BoolVar(&f.listDepts, "list-departments", false, "list the department aliases this marketplace offers and exit")
}

// query turns the flags into a search query, refusing the combinations that
// would otherwise resolve into something the user did not ask for.
func (f *searchFlags) query(app *App) (amz.SearchQuery, error) {
	q := amz.SearchQuery{
		Sort:       f.sort,
		Department: f.dept,
		Brand:      f.brand,
		Seller:     f.seller,
		Condition:  f.condition,
		Stars:      f.stars,
		StartPage:  f.page,
		Limit:      app.Limit,
		MaxPages:   f.maxPages,
	}
	for _, s := range f.refine {
		r, err := amz.ParseRefineFlag(s)
		if err != nil {
			return q, exit(CodeUsage, err)
		}
		q.Refine = append(q.Refine, r)
	}
	switch {
	case f.price != "":
		lo, hi, err := amz.ParsePriceFlag(f.price)
		if err != nil {
			return q, exit(CodeUsage, err)
		}
		q.MinPrice, q.MaxPrice = lo, hi
	default:
		q.MinPrice, q.MaxPrice = float64(f.minPrice), float64(f.maxPrice)
	}
	if f.stars == 0 && f.minRating > 0 {
		q.Stars = f.minRating
	}
	if f.prime {
		// p_85 is one of the six codes this package knows the name of, and it is
		// still resolved off the sidebar rather than composed from a remembered
		// id, because the id differs by marketplace and a query that does not
		// offer the group gets an unfiltered page rather than an error. That is
		// exactly what --prime did for two releases, so it is refused here
		// instead of translated.
		return q, exit(CodeUsage, fmt.Errorf(
			"--prime is gone in v0.3.0. no capture taken on 2026-08-17 offered the p_85 group at all, and the id v0.2.1 sent (2470955011) would have been dropped in silence.\n"+
				"run `amz refine <query>` to see whether this query offers a prime filter, then pass it with --refine"))
	}
	if f.partition != "" && !f.all {
		return q, exit(CodeUsage, fmt.Errorf("--partition names the group to split on and only --all does any splitting, so --partition without --all would read one page and throw the plan away"))
	}
	return q, nil
}

func searchCmd(app *App) *cobra.Command {
	var f searchFlags
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the catalog and stream result cards",
		Long: "Search the catalog and stream result cards.\n\n" +
			"Amazon serves at most 306 results over 20 pages for any query, whatever it says the total is.\n" +
			"Use --all to partition the query and get past that, and `amz why search-depth` for the measurement.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if f.listDepts {
				return listDepartments(cmd, app, c)
			}
			query := joinArgs(args)
			q, err := f.query(app)
			if err != nil {
				return err
			}
			if app.DryRun && !f.all {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.SearchURL(query, q, q.StartPage))
				return nil
			}
			if f.enqueue {
				return enqueueSearch(cmd, app, c, query, q)
			}
			if f.all {
				return searchAll(cmd, app, c, query, q, f)
			}
			return searchWalk(cmd, app, c, query, q, f)
		},
	}
	f.registerQuery(cmd)
	f.registerWalk(cmd)
	return cmd
}

// searchWalk is the ordinary paged search.
func searchWalk(cmd *cobra.Command, app *App, c *amz.Client, query string, q amz.SearchQuery, f searchFlags) error {
	out, err := app.Output()
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	w := cmd.ErrOrStderr()

	kept, dropped := 0, 0
	sum, ferr := c.SearchWalk(cmd.Context(), query, q, func(p amz.SearchPage) error {
		if f.pages {
			return out.Emit(searchPageRow(p))
		}
		for _, card := range p.Cards {
			if card.Sponsored && !f.sponsored {
				dropped++
				continue
			}
			kept++
			if err := out.Emit(cardRow(card)); err != nil {
				return err
			}
			if app.Limit > 0 && kept >= app.Limit {
				return nil
			}
		}
		return nil
	})
	reportSearch(w, app, sum, dropped, f)
	return emitErr(out, ferr)
}

// reportSearch writes the things about a search that are not results.
//
// On stderr, because they are notes about the read rather than data from it, and
// out loud rather than at -v, because a run that stopped at the ceiling and a
// run that reached the end of the results look identical in the rows.
func reportSearch(w io.Writer, app *App, sum amz.SearchSummary, dropped int, f searchFlags) {
	if app.Quiet {
		return
	}
	if dropped > 0 {
		_, _ = fmt.Fprintf(w, "amz: %d sponsored %s left out. pass --include-sponsored to keep them\n",
			dropped, plural(dropped, "placement", "placements"))
	}
	for _, r := range sum.Applied {
		if app.Verbose > 0 {
			_, _ = fmt.Fprintf(w, "amz: applied %s=%s (%s: %s)\n",
				r.Group, strings.Join(r.Values, ","), r.Label, strings.Join(r.ValueLabels, ", "))
		}
	}
	for _, g := range sum.Gaps {
		_, _ = fmt.Fprintf(w, "amz: results %s were not on any page fetched, so the pages did not join up\n", g)
	}
	if sum.Ceiling.Hit {
		_, _ = fmt.Fprintf(w, "amz: %s\n", sum.Ceiling.Reason)
		_, _ = fmt.Fprintf(w, "amz: run `amz why search-depth`, narrow with --refine, or partition with --all to reach the rest\n")
	}
	if app.Verbose > 0 {
		_, _ = fmt.Fprintf(w, "amz: %d %s, %d organic and %d sponsored, across %d %s\n",
			sum.Cards, plural(sum.Cards, "card", "cards"), sum.Organic, sum.Sponsored,
			sum.Pages, plural(sum.Pages, "page", "pages"))
		if len(sum.Refinements) > 0 {
			_, _ = fmt.Fprintf(w, "amz: %d refinement %s offered. run `amz refine %q` to see them\n",
				len(sum.Refinements), plural(len(sum.Refinements), "group", "groups"), sum.Query)
		}
	}
}

// searchAll is the partitioned search.
func searchAll(cmd *cobra.Command, app *App, c *amz.Client, query string, q amz.SearchQuery, f searchFlags) error {
	opts := amz.PartitionOptions{Group: f.partition, Depth: f.depth, DryRun: app.DryRun}
	if app.DryRun {
		plan, _, err := c.PlanPartition(cmd.Context(), query, q, opts)
		if err != nil {
			return exit(codeFor(err), err)
		}
		printPartitionPlan(cmd.OutOrStdout(), plan)
		return nil
	}
	out, err := app.Output()
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	dropped := 0
	sum, ferr := c.SearchAll(cmd.Context(), query, q, opts, func(card amz.Card) error {
		if card.Sponsored && !f.sponsored {
			dropped++
			return nil
		}
		return out.Emit(cardRow(card))
	})
	if !app.Quiet {
		w := cmd.ErrOrStderr()
		// The union counts every distinct ASIN and the rows are what survived the
		// sponsored filter, so without this line a run that printed 1,434 rows
		// under a summary saying 1,508 unique looks like it lost 74 of them.
		if dropped > 0 {
			_, _ = fmt.Fprintf(w, "amz: %d sponsored %s left out. pass --include-sponsored to keep them\n",
				dropped, plural(dropped, "placement", "placements"))
		}
		_, _ = fmt.Fprintf(w, "amz: partitioned on %s (%s), %d cells, %d unique %s, %d repeat sightings\n",
			sum.Plan.Group, sum.Plan.Label, len(sum.Cells), sum.Unique,
			plural(sum.Unique, "result", "results"), sum.Duplicates)
		if len(sum.Capped) > 0 {
			_, _ = fmt.Fprintf(w, "amz: %d %s still hit the ceiling and were not split further, so this union is incomplete: %s\n",
				len(sum.Capped), plural(len(sum.Capped), "cell", "cells"), strings.Join(sum.Capped, ", "))
		}
		if len(sum.Ignored) > 0 {
			_, _ = fmt.Fprintf(w, "amz: %d %s offered in the sidebar and then served unfiltered, so they read nothing: %s\n",
				len(sum.Ignored), plural(len(sum.Ignored), "cell was", "cells were"), strings.Join(sum.Ignored, ", "))
		}
	}
	return emitErr(out, ferr)
}

func printPartitionPlan(w io.Writer, plan amz.PartitionPlan) {
	_, _ = fmt.Fprintf(w, "partition %s %s, %d values\n", plan.Group, plan.Label, plan.Cells)
	_, _ = fmt.Fprintf(w, "  %d searches, up to %d pages each\n", plan.Cells, 20)
	_, _ = fmt.Fprintf(w, "  worst case %d requests, %s at this pace\n", plan.WorstCase, plan.WorstCaseTime.Round(60_000_000_000))
	if len(plan.Alternatives) > 0 {
		_, _ = fmt.Fprintln(w, "\n  narrower partitions available:")
		for i, a := range plan.Alternatives {
			if i == 3 {
				_, _ = fmt.Fprintf(w, "    and %d more\n", len(plan.Alternatives)-3)
				break
			}
			_, _ = fmt.Fprintf(w, "    %-24s %s, %d values\n", a.Group, a.Label, a.Values)
		}
	}
	_, _ = fmt.Fprintln(w, "\nnothing was read. drop --dry-run to run.")
}

// listDepartments prints the search aliases this marketplace offers.
func listDepartments(cmd *cobra.Command, app *App, c *amz.Client) error {
	out, err := app.Output()
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	depts, ferr := c.Departments(cmd.Context())
	if ferr != nil {
		return exit(codeFor(ferr), ferr)
	}
	for _, d := range depts {
		if err := out.Emit(departmentRow(d)); err != nil {
			return err
		}
	}
	return emitErr(out, nil)
}

func refineCmd(app *App) *cobra.Command {
	var f searchFlags
	var groupOnly string
	cmd := &cobra.Command{
		Use:   "refine <query>",
		Short: "List the refinement groups and values a query offers",
		Long: "List the refinement groups and values a query offers.\n\n" +
			"Almost none of these codes are global. p_n_g-1003532609111 is Key Count on a keyboard search and\n" +
			"does not exist on a search for shoes, which is why amz reads them rather than shipping a table.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			query := joinArgs(args)
			q, err := f.query(app)
			if err != nil {
				return err
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.SearchURL(query, q, 1))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()

			sp, ferr := c.FetchSearchPage(cmd.Context(), query, q, 1)
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			app.observe(sp.Envelope)
			groups := sp.Refinements
			values := 0
			for _, g := range groups {
				values += len(g.Values)
				if groupOnly != "" && g.Code != groupOnly {
					continue
				}
				if groupOnly == "" {
					if err := out.Emit(refineGroupRow(g)); err != nil {
						return err
					}
					continue
				}
				for _, v := range g.Values {
					if err := out.Emit(refineValueRow(g, v)); err != nil {
						return err
					}
				}
			}
			if !app.Quiet {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %d %s, %d values\n",
					len(groups), plural(len(groups), "group", "groups"), values)
			}
			return emitErr(out, nil)
		},
	}
	// Only the query flags. `amz refine` reads one page and prints its sidebar,
	// so --max-pages, --all and --enqueue have nothing to act on here and listing
	// them in the help would be advertising work this command never does.
	f.registerQuery(cmd)
	cmd.Flags().StringVar(&groupOnly, "group", "", "list the values of one group instead of the groups")
	return cmd
}

func joinArgs(args []string) string {
	s := args[0]
	for _, a := range args[1:] {
		s += " " + a
	}
	return s
}

// plural picks the singular or the plural for a count, so a note about one
// sponsored placement does not read as a note about one placements.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
