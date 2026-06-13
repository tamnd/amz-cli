package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func productCmd(app *App) *cobra.Command {
	var variants, withOffers bool
	cmd := &cobra.Command{
		Use:   "product <ASIN|url>...",
		Short: "Fetch and normalize one or more product detail pages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				for _, a := range args {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), resolveURL(c, a))
				}
				return nil
			}
			if app.Raw {
				return rawProduct(cmd, app, c, args)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			var firstErr error
			for _, a := range args {
				p, err := c.FetchProduct(cmd.Context(), a)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %s: %v\n", a, err)
					continue
				}
				if err := out.Emit(productRow(p)); err != nil {
					return err
				}
				if variants {
					for _, v := range p.VariantASINs {
						_ = out.Emit(stringRow("variant_asin", v))
					}
				}
				if withOffers {
					_ = c.FetchOffers(cmd.Context(), p.ASIN, amz.OfferQuery{}, func(o amz.Offer) error {
						return out.Emit(offerRow(o))
					})
				}
			}
			if out.Count() == 0 {
				return emitErr(out, firstErr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&variants, "variants", false, "also list variant ASINs as rows")
	cmd.Flags().BoolVar(&withOffers, "with-offers", false, "also pull the offer listing")
	return cmd
}

func rawProduct(cmd *cobra.Command, app *App, c *amz.Client, args []string) error {
	out := cmd.OutOrStdout()
	for _, a := range args {
		body, err := c.Get(cmd.Context(), resolveURL(c, a), 0)
		if err != nil {
			return exit(codeFor(err), err)
		}
		_, _ = out.Write(body)
	}
	return nil
}

func priceCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "price <ASIN|url>...",
		Short: "Print just the current price for one or more products",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			var firstErr error
			for _, a := range args {
				p, err := c.FetchProduct(cmd.Context(), a)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %s: %v\n", a, err)
					continue
				}
				_ = out.Emit(priceRow(p))
			}
			if out.Count() == 0 {
				return emitErr(out, firstErr)
			}
			return nil
		},
	}
	return cmd
}

func relatedCmd(app *App) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "related <ASIN>",
		Short: "List recommendation cards from a product detail page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			asin := amz.ExtractASIN(args[0])
			if asin == "" {
				asin = args[0]
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.ProductURL(asin))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			ferr := c.FetchRelated(cmd.Context(), asin, app.Limit, func(card amz.Card) error {
				if kind != "" && card.Kind != kind {
					return nil
				}
				return out.Emit(cardRow(card))
			})
			return emitErr(out, ferr)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter: related|sponsored|also-bought|also-viewed")
	return cmd
}
