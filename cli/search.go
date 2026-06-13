package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func searchCmd(app *App) *cobra.Command {
	var q amz.SearchQuery
	var enqueue bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the catalog and stream result cards",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			query := joinArgs(args)
			q.Limit = app.Limit
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.SearchURL(query, q, q.StartPage))
				return nil
			}
			if enqueue {
				return enqueueSearch(cmd, app, c, query, q)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			ferr := c.Search(cmd.Context(), query, q, func(card amz.Card) error {
				return out.Emit(cardRow(card))
			})
			return emitErr(out, ferr)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&q.Sort, "sort", "relevance", "relevance|price-asc|price-desc|review|newest")
	fl.IntVar(&q.MinPrice, "min-price", 0, "minimum price")
	fl.IntVar(&q.MaxPrice, "max-price", 0, "maximum price")
	fl.IntVar(&q.MinRating, "min-rating", 0, "minimum star rating (1..4)")
	fl.BoolVar(&q.Prime, "prime", false, "Prime-eligible only")
	fl.StringVar(&q.Brand, "brand", "", "filter by brand")
	fl.StringVar(&q.Department, "department", "", "limit to a department/search alias")
	fl.IntVar(&q.StartPage, "page", 1, "first result page")
	fl.BoolVar(&enqueue, "enqueue", false, "enqueue results into the crawl queue instead of printing")
	return cmd
}

func joinArgs(args []string) string {
	s := args[0]
	for _, a := range args[1:] {
		s += " " + a
	}
	return s
}
