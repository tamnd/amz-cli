package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// The three commands in this file used to fetch three surfaces of their own.
// None of those three can be fetched any more: /product-reviews/ and
// /portal/customer-reviews/ and /ask/questions/asin/ all redirect to a sign-in,
// and /gp/offer-listing/ is disallowed by robots.txt and redirects to the detail
// page regardless. So all three read the detail page, which carries the reviews
// medley, the histogram, the question count and the buy box.
//
// Each of them prints what it could not read on stderr, from the envelope rather
// than from a string written here, and the records go to stdout as before. A
// pipeline sees exactly what it saw in v0.2.1, minus the rows Amazon stopped
// serving, and a person sees why.

func reviewsCmd(app *App) *cobra.Command {
	var q amz.ReviewQuery
	var deep bool
	cmd := &cobra.Command{
		Use:   "reviews <ASIN>",
		Short: "The reviews the detail page carries, with the histogram",
		Long: "Amazon serves a handful of full reviews in the detail page medley and " +
			"keeps the rest behind a sign-in. This reads the ones on the page and says " +
			"how many it did not get. Run `amz why reviews` for the measurement.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			id := asinArg(args[0])
			if deep {
				return deepReviews(cmd, app, c, id)
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.ProductURL(id))
				return nil
			}
			p, ferr := c.FetchProduct(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			rs := filterReviews(p.ReviewSample, q)
			for i, r := range rs {
				if app.Limit > 0 && i >= app.Limit {
					break
				}
				if err := out.Emit(reviewRow(r)); err != nil {
					return err
				}
			}
			w := cmd.ErrOrStderr()
			if n := len(p.ReviewSample) - len(rs); n > 0 {
				// Saying so matters because the two are different data. Asking
				// Amazon for the one star reviews returns the one star reviews;
				// filtering thirteen of them locally returns whichever of the
				// thirteen happen to be one star, which is usually none.
				_, _ = fmt.Fprintf(w, "amz: %d of %d reviews filtered out here, not by amazon: the filter applies to the ones on the page\n",
					n, len(p.ReviewSample))
			}
			noteMisses(w, p, "reviews")
			return emitErr(out, nil)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&q.Sort, "sort", "", "order the reviews on the page: recent|helpful")
	fl.IntVar(&q.Stars, "stars", 0, "keep N-star reviews (1..5), applied locally")
	fl.BoolVar(&q.Verified, "verified", false, "keep verified purchases only, applied locally")
	fl.BoolVar(&q.WithImages, "with-images", false, "keep reviews with images only, applied locally")
	fl.IntVar(&q.StartPage, "page", 1, "removed: there is one page of reviews and it is the product page")
	_ = fl.MarkDeprecated("page", "the review corpus is behind a sign-in, so there is nothing to page through")
	fl.BoolVar(&deep, "deep", false, "attempt the full review corpus, which is behind a sign-in and will fail")
	return cmd
}

// deepReviews reproduces the failure rather than describing it.
//
// The note on every reviews run says the corpus redirects to a sign-in. Somebody
// is going to disbelieve that, and they are right to: it is a claim about a
// third party made by the tool that benefits from being believed. This makes the
// request, under the flag that says so out loud, and prints what came back.
func deepReviews(cmd *cobra.Command, app *App, c *amz.Client, id string) error {
	u := c.ReviewURL(id, amz.ReviewQuery{}, 1)
	if app.DryRun {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), u)
		return nil
	}
	w := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(w, "amz: requesting %s, which redirected to a sign-in every time it was measured\n", u)
	_, err := c.Get(cmd.Context(), u, 0)
	if err != nil {
		return exit(codeFor(err), fmt.Errorf("%s: %w. run `amz why reviews` for the measurement", u, err))
	}
	// A 200 here would mean the wall moved, which is worth more than a silent
	// success. The reviews on the page are still read and returned.
	_, _ = fmt.Fprintf(w, "amz: %s answered without a redirect, which it has not done since 2026-08-17. please open an issue.\n", u)
	out, oerr := app.Output()
	if oerr != nil {
		return oerr
	}
	defer func() { _ = out.Close() }()
	ferr := c.FetchReviews(cmd.Context(), id, amz.ReviewQuery{Limit: app.Limit}, func(r amz.Review) error {
		return out.Emit(reviewRow(r))
	})
	return emitErr(out, ferr)
}

// filterReviews applies the flags to the reviews in hand.
//
// Every one of these is a local filter over the handful the page carried, which
// is not the same thing as asking Amazon for them, and the command says so.
func filterReviews(rs []amz.Review, q amz.ReviewQuery) []amz.Review {
	out := make([]amz.Review, 0, len(rs))
	for _, r := range rs {
		switch {
		case q.Stars >= 1 && q.Stars <= 5 && r.Rating != q.Stars:
		case q.Verified && !r.VerifiedPurchase:
		case q.WithImages && len(r.Images) == 0:
		default:
			out = append(out, r)
		}
	}
	switch q.Sort {
	case "helpful":
		sort.SliceStable(out, func(i, j int) bool { return out[i].HelpfulVotes > out[j].HelpfulVotes })
	case "recent":
		sort.SliceStable(out, func(i, j int) bool { return reviewTime(out[i]) > reviewTime(out[j]) })
	}
	return out
}

// reviewTime is a sortable stamp, and it is empty for a review whose date did
// not parse. Those sort last rather than to the epoch, because a 2015 review and
// an unparsed one are different facts.
func reviewTime(r amz.Review) string {
	if r.Date == nil || r.Date.Parsed == nil {
		return ""
	}
	return r.Date.Parsed.Format("2006-01-02")
}

func qaCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qa <ASIN>",
		Short: "The question and answer pairs the detail page carries, with the count",
		Long: "The ask region states how many questions have been answered and carries the " +
			"pairs themselves only sometimes. /ask/questions/asin/ redirects to a sign-in, " +
			"so the count is usually all there is. Run `amz why qa`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.ProductURL(asinArg(args[0])))
				return nil
			}
			p, ferr := c.FetchProduct(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			for i, q := range p.QASample {
				if app.Limit > 0 && i >= app.Limit {
					break
				}
				if err := out.Emit(qaRow(q)); err != nil {
					return err
				}
			}
			w := cmd.ErrOrStderr()
			noteMisses(w, p, "questions")
			if out.Count() == 0 {
				if p.Questions == nil {
					_, _ = fmt.Fprintln(w, "amz: this product has no ask region at all, which is now the usual case")
				}
				return exit(CodeNoData, fmt.Errorf("no question and answer pairs on the page"))
			}
			return nil
		},
	}
	return cmd
}

func offersCmd(app *App) *cobra.Command {
	var q amz.OfferQuery
	cmd := &cobra.Command{
		Use:   "offers <ASIN>",
		Short: "The buy box, and the count of the offers behind it",
		Long: "The all-offers panel is assembled by javascript and /gp/offer-listing/ is " +
			"disallowed by robots.txt and redirects to the detail page anyway. So this is " +
			"the winning offer plus how many others exist. Run `amz why offers`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), c.ProductURL(asinArg(args[0])))
				return nil
			}
			p, ferr := c.FetchProduct(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()
			w := cmd.ErrOrStderr()
			if o := p.BuyBoxListing(); o != nil && keepOffer(*o, q) {
				if err := out.Emit(offerRow(*o)); err != nil {
					return err
				}
			}
			noteMisses(w, p, "other_offers")
			if out.Count() == 0 {
				return exit(CodeNoData, fmt.Errorf("this listing has no buy box"))
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&q.Condition, "condition", "", "keep this condition only (new|used|...)")
	fl.BoolVar(&q.Prime, "prime", false, "keep the offer only when Amazon fulfils it")
	return cmd
}

func keepOffer(o amz.OfferListing, q amz.OfferQuery) bool {
	if q.Condition != "" && !strings.Contains(strings.ToLower(o.Condition), strings.ToLower(q.Condition)) {
		return false
	}
	return !q.Prime || strings.Contains(strings.ToLower(o.FulfilledBy), "amazon")
}

// noteMisses prints the envelope's entry for one field as a note on stderr.
//
// The wording comes from the record and not from a constant here, so a note is
// always a statement about the page that was actually read. When Amazon opens a
// surface back up the miss stops being recorded and the note stops being
// printed, with no code change and no stale sentence left behind.
func noteMisses(w io.Writer, p amz.Product, field string) {
	for _, m := range p.Envelope.Missed {
		if m.Field != field {
			continue
		}
		if m.Total > 0 {
			_, _ = fmt.Fprintf(w, "amz: %d of %d. %s\n", m.Have, m.Total, m.Why)
		} else {
			_, _ = fmt.Fprintf(w, "amz: %s\n", m.Why)
		}
		for _, s := range m.Surfaces {
			_, _ = fmt.Fprintf(w, "amz:   %s is not readable without a session\n", s)
		}
		if m.Fix != "" {
			_, _ = fmt.Fprintf(w, "amz: run `%s` for the detail\n", m.Fix)
		}
	}
}

// asinArg extracts an ASIN from a URL argument, or returns it unchanged.
func asinArg(s string) string {
	if a := amz.ExtractASIN(s); a != "" {
		return a
	}
	return s
}
