package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func reviewsCmd(app *App) *cobra.Command {
	var q amz.ReviewQuery
	cmd := &cobra.Command{
		Use:   "reviews <ASIN>",
		Short: "Stream the review corpus for a product",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			asin := asinArg(args[0])
			q.Limit = app.Limit
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.ReviewURL(asin, q, max(q.StartPage, 1)))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			ferr := c.FetchReviews(cmd.Context(), asin, q, func(r amz.Review) error {
				return out.Emit(reviewRow(r))
			})
			return emitErr(out, ferr)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&q.Sort, "sort", "recent", "recent|helpful")
	fl.IntVar(&q.Stars, "stars", 0, "filter to N-star reviews (1..5)")
	fl.BoolVar(&q.Verified, "verified", false, "verified purchases only")
	fl.BoolVar(&q.WithImages, "with-images", false, "reviews with images only")
	fl.IntVar(&q.StartPage, "page", 1, "first review page")
	return cmd
}

func qaCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qa <ASIN>",
		Short: "Fetch classic question-and-answer pairs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			asin := asinArg(args[0])
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.QAURL(asin))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			n := 0
			ferr := c.FetchQA(cmd.Context(), asin, func(q amz.QA) error {
				if app.Limit > 0 && n >= app.Limit {
					return nil
				}
				n++
				return out.Emit(qaRow(q))
			})
			if errors.Is(ferr, amz.ErrNoQA) {
				fmt.Fprintln(cmd.ErrOrStderr(), "amz: no Q&A section on this product (Amazon has removed it for many items)")
				return exit(CodeNoData, ferr)
			}
			return emitErr(out, ferr)
		},
	}
	return cmd
}

func offersCmd(app *App) *cobra.Command {
	var q amz.OfferQuery
	cmd := &cobra.Command{
		Use:   "offers <ASIN>",
		Short: "List every buying option (seller/condition/price)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			asin := asinArg(args[0])
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.OffersURL(asin))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			n := 0
			ferr := c.FetchOffers(cmd.Context(), asin, q, func(o amz.Offer) error {
				if app.Limit > 0 && n >= app.Limit {
					return nil
				}
				n++
				return out.Emit(offerRow(o))
			})
			return emitErr(out, ferr)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&q.Condition, "condition", "", "filter by condition (new|used|...)")
	fl.BoolVar(&q.Prime, "prime", false, "Prime/FBA-eligible only")
	return cmd
}

// asinArg extracts an ASIN from a URL argument, or returns it unchanged.
func asinArg(s string) string {
	if a := amz.ExtractASIN(s); a != "" {
		return a
	}
	return s
}
