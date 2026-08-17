package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func productCmd(app *App) *cobra.Command {
	var variants, withOffers bool
	var depth string
	cmd := &cobra.Command{
		Use:   "product <ASIN|url>...",
		Short: "Fetch and normalize one or more product detail pages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			d, err := amz.ParseDepth(depth)
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
				p, err := fetchAtDepth(cmd, app, c, a, d)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %s: %v\n", a, err)
					continue
				}
				if err := out.Emit(productRow(app, p)); err != nil {
					return err
				}
				if variants && p.Variation != nil {
					for _, v := range p.Variation.Siblings {
						_ = out.Emit(stringRow("variant_asin", v.ASIN))
					}
				}
				if withOffers {
					_ = c.FetchOffers(cmd.Context(), p.ASIN, amz.OfferQuery{}, func(o amz.OfferListing) error {
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
	cmd.Flags().StringVar(&depth, "depth", string(amz.DepthMeta),
		"how much to read: quick (the 374 KB mobile page), meta, full (adds the recommendation rails), deep (adds a request per variation sibling and one for the seller)")
	cmd.Flags().BoolVar(&variants, "variants", false, "also list variant ASINs as rows")
	cmd.Flags().BoolVar(&withOffers, "with-offers", false, "also pull the offer listing")
	return cmd
}

// fetchAtDepth reads one product, putting the confirmation for a deep read
// between the page and the requests it would spend.
//
// The cost of deep is not knowable before the page is parsed, because it is one
// request per variation sibling and the page is what says how many there are. So
// the page is fetched at full, the bill is added up, and anything over twenty
// requests stops and says what it was about to do. The first fetch is not wasted
// work: it is the same request the deep read needs and it is already in the
// cache when the answer is yes.
func fetchAtDepth(cmd *cobra.Command, app *App, c *amz.Client, arg string, d amz.Depth) (amz.Product, error) {
	if d != amz.DepthDeep || app.Yes {
		return c.FetchProductDepth(cmd.Context(), arg, d)
	}
	p, err := c.FetchProductDepth(cmd.Context(), arg, amz.DepthFull)
	if err != nil {
		return p, err
	}
	siblings := 0
	if p.Variation != nil {
		siblings = len(p.Variation.Siblings)
	}
	if cost := amz.DepthDeep.Cost(siblings); cost > amz.DeepConfirmAt {
		return p, exit(CodeUsage, fmt.Errorf(
			"%s has %d variation siblings, so --depth deep is %d requests: rerun with --yes",
			p.ASIN, siblings, cost))
	}
	c.ResolveDeep(cmd.Context(), &p)
	return p, nil
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
