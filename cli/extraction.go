package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// The three commands that report on the reading rather than on what was read.
//
// A scraper is a claim about somebody else's HTML, and the useful question is
// not "did it return something" but "how did it know". These print the answer:
// extraction shows the ladder each field was read from, agent-map shows what the
// page says about itself, and verify compares a live page against what the same
// page yielded on a day we recorded.

func extractionCmd(app *App) *cobra.Command {
	var unread, showFields bool
	var family string
	cmd := &cobra.Command{
		Use:   "extraction [asin|url]",
		Short: "How each field is read, and what is on the page that nothing reads",
		Long: "With no argument this prints the ladder over the whole field registry: how many fields\n" +
			"are read from a region Amazon named, from a JavaScript payload, from a data attribute,\n" +
			"and from a bare CSS selector. The last rung is the one that breaks silently when the\n" +
			"site is restyled, so it is listed field by field.\n\n" +
			"With a page it prints what that page actually yielded, which is the same report with\n" +
			"real answers in it: where every field came from, which declared fields the page did not\n" +
			"carry, and how many regions Amazon named that no field reads.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return ladderReport(cmd, app, family)
			}
			return pageReport(cmd, app, args[0], unread, showFields)
		},
	}
	cmd.Flags().BoolVar(&unread, "unread", false, "list the regions Amazon named that no field reads")
	cmd.Flags().BoolVar(&showFields, "fields", false, "list every field this page filled and where it came from")
	cmd.Flags().StringVar(&family, "family", "", "limit the ladder to one family (product|search|chart|browse|store|seller)")
	return cmd
}

// ladderReport prints the registry, which needs no network and no page.
func ladderReport(cmd *cobra.Command, app *App, family string) error {
	fams := amz.Families()
	if family != "" {
		var want []amz.Family
		for _, f := range fams {
			if string(f) == family {
				want = append(want, f)
			}
		}
		if len(want) == 0 {
			return exit(CodeUsage, fmt.Errorf("unknown family %q", family))
		}
		fams = want
	}

	w := cmd.OutOrStdout()
	c, err := app.Client()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !app.NoHeader {
		_, _ = fmt.Fprintln(tw, "FAMILY\tREGION\tPAYLOAD\tATTR\tSELECTOR\tTOTAL")
	}
	type guess struct {
		fam amz.Family
		f   amz.Field
	}
	var level4 []guess
	for _, fam := range fams {
		counts := map[amz.Level]int{}
		fields := amz.Registry(fam, c.BaseURL(), "B075F5X8BR")
		for _, f := range fields {
			counts[f.Level]++
			if f.Level == amz.LevelSelector {
				level4 = append(level4, guess{fam, f})
			}
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n", fam,
			counts[amz.LevelRegion], counts[amz.LevelPayload],
			counts[amz.LevelAttr], counts[amz.LevelSelector], len(fields))
	}
	_ = tw.Flush()

	if len(level4) == 0 {
		return nil
	}
	// The bottom rung is printed in full because it is the debt. A field here
	// works today and says nothing about tomorrow, and the only way it gets paid
	// down is if somebody can see the list. Oldest first, because a selector that
	// has survived a year of Amazon's restyling is a different risk from one
	// written last week, and the date is the only evidence either way.
	sort.SliceStable(level4, func(i, j int) bool {
		if level4[i].f.Since != level4[j].f.Since {
			return level4[i].f.Since < level4[j].f.Since
		}
		return level4[i].f.Name < level4[j].f.Name
	})
	_, _ = fmt.Fprintf(w, "\n%d fields are read from a bare CSS selector, which is a guess that happens to be right:\n", len(level4))
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !app.NoHeader {
		_, _ = fmt.Fprintln(tw, "  FAMILY\tFIELD\tSINCE\tSELECTOR")
	}
	for _, g := range level4 {
		since := g.f.Since
		if since == "" {
			since = "unrecorded"
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", g.fam, g.f.Name, since, g.f.Source())
	}
	_ = tw.Flush()
	return nil
}

// pageReport reads one page and prints what the parser made of it.
func pageReport(cmd *cobra.Command, app *App, arg string, unread, showFields bool) error {
	c, err := app.Client()
	if err != nil {
		return err
	}
	url := arg
	if !amz.IsURL(arg) {
		url = resolveURL(c, arg)
	}
	if app.DryRun {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), url)
		return nil
	}
	body, err := c.Get(cmd.Context(), url, 0)
	if err != nil {
		return exit(codeFor(err), err)
	}
	x, err := c.ExtractBody(url, body)
	if err != nil {
		return exit(codeFor(err), err)
	}

	if Format(app.OutputFmt) == FormatJSON || Format(app.OutputFmt) == FormatJSONL {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(x)
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "%s  %s  %s  %d bytes\n", x.Op, x.Family, x.URL, x.Bytes)
	_, _ = fmt.Fprintf(w, "%d fields set, %d missed, %d regions Amazon named that nothing reads",
		x.Set, x.Missed, x.Unread)
	if x.Records > 0 {
		_, _ = fmt.Fprintf(w, ", %d records", x.Records)
	}
	_, _ = fmt.Fprintln(w)

	if showFields {
		_, _ = fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, name := range x.Fields() {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", name, x.Envelope.Via[name])
		}
		_ = tw.Flush()
	}

	// A miss is a field the registry declared and the page did not carry, and
	// the sentence beside it is the parser saying what it looked for. That is
	// the difference between "no price" and "no price because the buy box is
	// not on this page", and only the second one tells a caller what to do.
	if len(x.Envelope.Missed) > 0 {
		_, _ = fmt.Fprintln(w, "\nnot on this page:")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, m := range x.Envelope.Missed {
			_, _ = fmt.Fprintf(tw, "  %s\t%s%s\n", m.Field, m.Why, missTail(m))
		}
		_ = tw.Flush()
	}

	if unread {
		if len(x.Envelope.Unread) == 0 {
			_, _ = fmt.Fprintln(w, "\nevery region this page names is read by a field")
			return nil
		}
		_, _ = fmt.Fprintf(w, "\n%d regions on the page that no field reads, which is the worklist:\n", len(x.Envelope.Unread))
		names := append([]string(nil), x.Envelope.Unread...)
		sort.Strings(names)
		for _, n := range names {
			_, _ = fmt.Fprintf(w, "  %s\n", n)
		}
	}
	return nil
}

// missTail is the part of a miss that says what to do about it.
//
// The sentence alone leaves the three interesting cases looking the same in a
// terminal. A field that lives on a page this fetch did not read is a request
// away, a field that came back partial is a count the reader has to not mistake
// for the total, and a field the page simply does not carry is neither.
func missTail(m amz.Miss) string {
	var parts []string
	if m.Total > 0 {
		parts = append(parts, fmt.Sprintf("have %d of %d", m.Have, m.Total))
	}
	if len(m.Surfaces) > 0 {
		parts = append(parts, "on "+strings.Join(m.Surfaces, " and "))
	}
	if m.Fix != "" {
		parts = append(parts, m.Fix)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func verifyCmd(app *App) *cobra.Command {
	var live, strict bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Compare pages against what they yielded when they were captured",
		Long: "Every golden capture in this repository records how many fields it filled on the day it\n" +
			"was taken. This fetches the same pages now and reports what changed.\n\n" +
			"Without --live it only reads pages already in the cache, so it costs Amazon nothing and\n" +
			"reports on what you happen to have. With --live it fetches every page in the ledger, one\n" +
			"at a time at the configured rate, which is twenty one requests.\n\n" +
			"Fewer fields than the ledger is a regression. More unread regions is Amazon adding a\n" +
			"section, which is worth knowing and is not a failure, so --strict only fails on the\n" +
			"first kind.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := amz.Ledger()
			if err != nil {
				return exit(CodeRuntime, err)
			}
			c, err := app.Client()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if app.DryRun {
				for _, e := range entries {
					_, _ = fmt.Fprintln(w, e.URL)
				}
				return nil
			}

			var checked, drifted, worse, skipped int
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			if !app.NoHeader {
				_, _ = fmt.Fprintln(tw, "CAPTURE\tSTATUS\tDETAIL")
			}
			for _, e := range entries {
				if e.URL == "" {
					skipped++
					continue
				}
				// A capture whose right answer is a refusal has no field counts
				// to compare, and re-fetching it would spend a request to learn
				// that a page that did not exist still does not.
				if e.Stop != "" {
					skipped++
					continue
				}
				body, err := cachedOrLive(cmd, c, e.URL, live)
				if err != nil {
					if !live {
						skipped++
						continue
					}
					_, _ = fmt.Fprintf(tw, "%s\tunread\t%v\n", e.Name, err)
					worse++
					continue
				}
				x, err := c.ExtractBody(e.URL, body)
				if err != nil {
					_, _ = fmt.Fprintf(tw, "%s\tunread\t%v\n", e.Name, err)
					worse++
					continue
				}
				checked++
				d := amz.Compare(e, x)
				switch {
				case len(d.Notes) == 0:
					_, _ = fmt.Fprintf(tw, "%s\tsame\t%d fields, %d records\n", e.Name, x.Set, x.Records)
				case d.Worse:
					worse++
					drifted++
					_, _ = fmt.Fprintf(tw, "%s\tworse\t%s\n", e.Name, strings.Join(d.Notes, ", "))
				default:
					drifted++
					_, _ = fmt.Fprintf(tw, "%s\tmoved\t%s\n", e.Name, strings.Join(d.Notes, ", "))
				}
			}
			_ = tw.Flush()
			_, _ = fmt.Fprintf(w, "\n%d checked, %d drifted, %d worse than the ledger, %d skipped\n",
				checked, drifted, worse, skipped)
			if !live && checked == 0 {
				_, _ = fmt.Fprintln(w, "nothing was in the cache. run with --live to fetch the ledger's pages.")
			}
			if strict && worse > 0 {
				return exit(CodePartial, fmt.Errorf("%d pages yield less than the ledger records", worse))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&live, "live", false, "fetch the ledger's pages instead of only reading the cache")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero when a page yields less than it used to")
	return cmd
}

// cachedOrLive reads a page from the cache, and from Amazon only when asked.
//
// The default has to be the cache. `amz verify` is a command people will run out
// of curiosity, and a curiosity that fetches twenty one pages from a site that
// did not ask to be measured is not a polite default.
func cachedOrLive(cmd *cobra.Command, c *amz.Client, url string, live bool) ([]byte, error) {
	if live {
		return c.Get(cmd.Context(), url, 0)
	}
	return c.Cached(url)
}

func agentMapCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-map <url|asin>",
		Short: "Print Amazon's own description of a page, verbatim",
		Long: "Amazon ships an interface map on some pages: its own list of what the regions are and\n" +
			"what they are for. This prints it exactly as served.\n\n" +
			"It is recorded and never trusted. It is a statement by the site about the site, useful\n" +
			"for finding a region worth reading and worthless as evidence that the region holds what\n" +
			"it says. Every field in amz is measured against the HTML instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			url := args[0]
			if !amz.IsURL(url) {
				url = resolveURL(c, url)
			}
			if app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), url)
				return nil
			}
			body, err := c.Get(cmd.Context(), url, 0)
			if err != nil {
				return exit(codeFor(err), err)
			}
			d, err := amz.ParseDoc(amz.FamilyFor(url), body)
			if err != nil {
				return exit(CodeRuntime, err)
			}
			m := d.AgentMap()
			if len(m) == 0 {
				return exit(CodeNoData, fmt.Errorf("no agent map on %s", url))
			}
			var pretty any
			if json.Unmarshal(m, &pretty) == nil {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(pretty)
			}
			_, _ = cmd.OutOrStdout().Write(m)
			return nil
		},
	}
	return cmd
}
