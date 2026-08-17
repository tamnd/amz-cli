package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func productCmd(app *App) *cobra.Command {
	var variants, withOffers, light bool
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
			// --light and --depth quick are the same read, and a run that sets
			// both to different things has asked for two answers. Saying so beats
			// silently preferring one, because the loser is invisible in the
			// output and the user finds out from a bill of a hundred requests.
			if light {
				if cmd.Flags().Changed("depth") && depth != string(amz.DepthQuick) {
					return exit(CodeUsage, fmt.Errorf("--light is --depth quick, so it contradicts --depth %s", depth))
				}
				depth = string(amz.DepthQuick)
			}
			d, err := amz.ParseDepth(depth)
			if err != nil {
				return err
			}
			if app.DryRun {
				for _, a := range args {
					u, err := resolveURL(c, a)
					if err != nil {
						return err
					}
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), u)
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
				// A table of one product is a row of fifteen truncated cells,
				// and one product is what this command is usually asked for, so
				// the human format is the card. Every machine format is
				// untouched: Detail falls through to Emit for all of them.
				err = out.Detail(productRow(app, p), func(w io.Writer) {
					printProductCard(w, p, app.Verbose)
				})
				if err != nil {
					return err
				}
				if variants && p.Variation != nil {
					for _, v := range p.Variation.Siblings {
						_ = out.Emit(stringRow("variant_asin", v.ASIN))
					}
				}
				// --with-offers used to fetch /gp/offer-listing/, which is
				// disallowed and redirects to this same page. The buy box is the
				// offer that page carries, so the flag now costs nothing and
				// returns the one row it was ever able to return.
				if withOffers {
					if o := p.BuyBoxListing(); o != nil {
						_ = out.Emit(offerRow(*o))
					}
					noteMisses(cmd.ErrOrStderr(), p, "other_offers")
				}
			}
			if out.Count() == 0 {
				return emitErr(out, firstErr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&depth, "depth", string(amz.DepthMeta),
		"how much to read: quick (the mobile URL, which returns the same page as meta at the same cost today), meta, full (adds the recommendation rails), deep (adds a request per variation sibling and one for the seller)")
	cmd.Flags().BoolVar(&light, "light", false, "shorthand for --depth quick")
	cmd.Flags().BoolVar(&variants, "variants", false, "also list variant ASINs as rows")
	cmd.Flags().BoolVar(&withOffers, "with-offers", false, "also emit the buy box as an offer row")
	return cmd
}

// variantsCmd prints the variation matrix.
//
// The twister on the page carries every sibling it is going to carry, so the
// default costs one request and resolving is opt in. On a large apparel listing
// the page names a dozen of several hundred and each of the rest is a fetch, so
// --resolve goes through the same confirmation --depth deep does rather than
// quietly spending a hundred requests on a command that reads like a lookup.
func variantsCmd(app *App) *cobra.Command {
	var resolve bool
	cmd := &cobra.Command{
		Use:   "variants <ASIN|url>",
		Short: "The variation matrix for a listing, one row per sibling",
		Long: "The twister on a detail page names the siblings near the current selection " +
			"and states how many exist. --resolve fetches each named sibling for its own " +
			"price and availability, which is one request per row.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				u, err := resolveURL(c, args[0])
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), u)
				return nil
			}
			d := amz.DepthMeta
			if resolve {
				d = amz.DepthDeep
			}
			p, ferr := fetchAtDepth(cmd, app, c, args[0], d)
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			w := cmd.ErrOrStderr()
			if p.Variation == nil {
				// A listing that does not vary is not a failure and not an empty
				// result either. It has exactly one variant and that is the
				// answer, so it is emitted rather than reported as nothing found.
				_, _ = fmt.Fprintf(w, "amz: %s does not vary, so it is its own only variant\n", p.ASIN)
				return out.Emit(variantRow(p, amz.Sibling{ASIN: p.ASIN}, true))
			}
			for i, s := range p.Variation.Siblings {
				if app.Limit > 0 && i >= app.Limit {
					break
				}
				if err := out.Emit(variantRow(p, s, s.ASIN == p.ASIN)); err != nil {
					return err
				}
			}
			if p.Variation.TotalCount != nil && !p.Variation.Complete {
				_, _ = fmt.Fprintf(w, "amz: %d of %d. the page ships the variations near the current selection\n",
					len(p.Variation.Siblings), *p.Variation.TotalCount)
				_, _ = fmt.Fprintln(w, "amz: run `amz product` on a sibling to see the ones near it")
			}
			if out.Count() == 0 {
				return exit(CodeNoData, fmt.Errorf("%s has a twister that names no siblings", p.ASIN))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&resolve, "resolve", false, "fetch each sibling for its own price and availability, one request each")
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
	// An argument that is not a page is checked here rather than left to the
	// fetch, which can only answer "not found". A typo in an ASIN and a word that
	// was never an id are different mistakes and only one of them is worth
	// retrying, so they do not get the same sentence. The loop still carries on
	// to the other arguments, because one bad argument in ten is not a reason to
	// throw away nine fetches.
	if _, err := resolveURL(c, arg); err != nil {
		return amz.Product{}, err
	}
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
		u, err := resolveURL(c, a)
		if err != nil {
			return err
		}
		body, err := c.Get(cmd.Context(), u, 0)
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
	var sponsored bool
	cmd := &cobra.Command{
		Use:   "related <ASIN>",
		Short: "List recommendation cards from a product detail page, organic by default",
		Long: "Sponsored cards are left out unless --include-sponsored is given. An " +
			"advertising placement and a recommendation are different facts, and a " +
			"dataset that pools them cannot be used to measure either one.",
		Args: cobra.ExactArgs(1),
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
			dropped := 0
			ferr := c.FetchRelated(cmd.Context(), asin, app.Limit, func(card amz.Card) error {
				if card.Sponsored && !sponsored {
					dropped++
					return nil
				}
				if kind != "" && card.Kind != kind {
					return nil
				}
				return out.Emit(cardRow(card))
			})
			if dropped > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"amz: %d sponsored cards left out. --include-sponsored keeps them, and every row carries a sponsored column\n", dropped)
			}
			return emitErr(out, ferr)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter: related|sponsored|also-bought|also-viewed")
	cmd.Flags().BoolVar(&sponsored, "include-sponsored", false, "keep the advertising placements as well as the organic rails")
	return cmd
}
