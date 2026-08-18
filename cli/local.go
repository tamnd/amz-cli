package cli

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// The four commands that read the store and touch no network.
//
// They are grouped in one file because they share one property that matters
// more than what each of them does: none of them makes a request. A crawl is
// expensive and slow, and the point of paying for it is that afterwards the
// answers are local, instant and repeatable. A command here that quietly fetched
// a missing record would take that away, so a record that is not in the store is
// reported as absent rather than fetched.

func queryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <sql>",
		Short: "Run a read-only SQL query against the local store",
		Long: strings.TrimSpace(`
Run SQL against the store and print the rows as JSON.

The query is read only, and that is enforced rather than documented: a statement
that starts with anything that writes is refused before it reaches SQLite. The
store holds hours of somebody's bandwidth and Amazon's, and a mistyped statement
at a shell prompt should not be able to empty it.

The tables are product, price, "rank", chart_entry, edge, review, qa, offer,
category, brand, seller, author and queue. Every scoped table keys on
(marketplace, asin), because the same ASIN is a different listing in every
storefront. price, "rank" and chart_entry are append only, so they are history
and not current state.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			rows, err := s.Query(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, amz.ErrNotReadOnly) {
					return exit(CodeUsage, err)
				}
				return exit(CodeRuntime, err)
			}
			if len(rows) == 0 {
				return exit(CodeNoData, nil)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			for _, r := range rows {
				if err := enc.Encode(r); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return cmd
}

func findCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "find <text>",
		Short: "Full text search over the local store",
		Long: strings.TrimSpace(`
Search the titles, brands, bullets and descriptions of everything crawled.

This is FTS5, so the query syntax is SQLite's: quoted phrases, AND, OR, NOT, and
a trailing * for a prefix. It searches what is on disk and never the site, so a
product nobody has crawled is not here and no amount of retrying will find it.

Description text is only in the index if the crawl that stored the product ran
with --with-text.`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if limit <= 0 {
				limit = app.Limit
			}
			hits, err := s.Find(cmd.Context(), strings.Join(args, " "), limit)
			if err != nil {
				if errors.Is(err, amz.ErrNoFTS) {
					return exit(CodeRuntime, fmt.Errorf("%w. run `amz doctor` to confirm, then recreate the store with a build that has fts5", err))
				}
				return exit(CodeRuntime, err)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			for _, h := range hits {
				_ = out.Emit(Row{
					Cols:  []string{"asin", "marketplace", "title", "snippet"},
					Vals:  []string{h.ASIN, h.Marketplace, h.Title, h.Snippet},
					Value: h,
				})
			}
			if out.Count() == 0 {
				return exit(CodeNoData, nil)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "most hits to print (default --limit)")
	return cmd
}

func lookupCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lookup <uri|asin>",
		Short: "Read one record out of the local store, with no network at all",
		Long: strings.TrimSpace(`
Print the stored record for a URI or an ASIN, byte for byte as it was stored.

It prints the JSON that is in the store rather than a record rebuilt from the
typed columns, which means a field this build does not know about still comes
back. The typed columns are an index over that JSON, not the record itself.

An ASIN is looked up in the current --marketplace. A full amz: URI carries its
own storefront and ignores the flag, because the same ASIN in two storefronts is
two listings at two prices and picking one of them silently is how they get
confused.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			raw, err := s.LookupJSON(cmd.Context(), app.Marketplace, args[0])
			if errors.Is(err, sql.ErrNoRows) {
				return exit(CodeNoData, fmt.Errorf(
					"%s is not in the store. this command never fetches: run `amz product %s` to read it from the site, or `amz crawl --asin %s`",
					args[0], args[0], args[0]))
			}
			if err != nil {
				return exit(CodeRuntime, err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	return cmd
}

func seriesCmd(app *App) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "series <asin>",
		Short: "The price and rank history for one product, from the local store",
		Long: strings.TrimSpace(`
Print every price and rank ever observed for a product, oldest first.

Amazon publishes no price history and no rank history, so this is not a read of
anything: it is what this machine saw, on the days it looked. A series with two
points is two crawls, and the gap between them is unmeasured rather than flat.

The rows are append only in the store. Nothing here is ever rewritten by a later
crawl, so a merge bug in the current record cannot reach the history.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			asin := amz.ExtractASIN(args[0])
			if asin == "" {
				asin = args[0]
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()

			ctx := cmd.Context()
			if kind == "" || kind == "price" {
				obs, err := s.PriceSeries(ctx, app.Marketplace, asin)
				if err != nil {
					return exit(CodeRuntime, err)
				}
				for _, o := range obs {
					_ = out.Emit(Row{
						Cols:  []string{"at", "series", "kind", "amount", "currency", "seller_id"},
						Vals:  []string{o.At, "price", o.Kind, strconv.FormatFloat(o.Amount, 'f', -1, 64), o.Currency, o.Seller},
						Value: o,
					})
				}
			}
			if kind == "" || kind == "rank" {
				obs, err := s.RankSeries(ctx, app.Marketplace, asin)
				if err != nil {
					return exit(CodeRuntime, err)
				}
				for _, o := range obs {
					_ = out.Emit(Row{
						Cols:  []string{"at", "series", "rank", "category", "node_id"},
						Vals:  []string{o.At, "rank", strconv.Itoa(o.Rank), o.Category, o.NodeID},
						Value: o,
					})
				}
			}
			if out.Count() == 0 {
				return exit(CodeNoData, fmt.Errorf(
					"no observations for %s in the %s store. a series needs at least one crawl, and two before it says anything",
					asin, app.Marketplace))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "series", "", "price|rank, or both when unset")
	return cmd
}
