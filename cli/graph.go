package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
	"github.com/tamnd/amz-cli/pkg/graph"
	"github.com/tamnd/amz-cli/pkg/rdf"
	"github.com/tamnd/amz-cli/pkg/uri"
)

// amz graph and amz export: the two commands that read the edge table.
//
// Like the rest of the local commands, neither of them makes a request. A
// traversal that fetched what it had not crawled would turn `--depth 2` from a
// query into an unbounded crawl, and the number of requests would depend on
// what happened to be in the store already, which is the worst possible way for
// a command to decide how much of somebody's bandwidth to spend.

// nodeWarnThreshold is where a traversal stops and asks.
//
// 500 comes from 04_graph.md section 6 and the number matters less than the
// behaviour: past this point the output is not something a person reads, so the
// command prints the count and waits rather than filling a terminal.
const nodeWarnThreshold = 500

func graphCmd(app *App) *cobra.Command {
	var depth int
	var includeSponsored, symmetric, showPredicates, edgesOnly bool
	var predicates []string

	cmd := &cobra.Command{
		Use:   "graph <uri|asin>",
		Short: "Traverse the crawled graph outward from one node",
		Long: strings.TrimSpace(`
Walk the edges stored by a crawl, outward from one node.

Sixteen predicates, each carrying the surface that asserted it and the time it
was read. Run with --predicates to print the vocabulary.

Cycles are normal here and are not an error: variant_of is symmetric in practice,
related_to frequently points back, and a browse node tree has cross links that
make it a DAG at best. So the walk is visited-set based, --depth defaults to 1,
and a node reached two ways is reported once at the shorter distance.

Sponsored edges are excluded unless --include-sponsored. A "customers also
bought" graph polluted with paid placements is not a graph of anything.

This reads the store and never the site. A node with no edges was either never
crawled or was crawled without --follow-rails.`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if showPredicates {
				return printPredicates(cmd)
			}
			if len(args) == 0 {
				return exit(CodeUsage, errors.New("give a node: an amz: URI or an ASIN. run `amz graph --predicates` for the vocabulary"))
			}
			for _, p := range predicates {
				if !graph.Known(p) {
					return exit(CodeUsage, fmt.Errorf("%q: %w. run `amz graph --predicates`", p, graph.ErrUnknownPredicate))
				}
			}

			start, err := nodeURI(app, args[0])
			if err != nil {
				return exit(CodeUsage, err)
			}

			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			ctx := cmd.Context()
			g, err := s.LoadGraph(ctx, amz.EdgeQuery{IncludeSponsored: includeSponsored, Predicates: predicates})
			if err != nil {
				return exit(CodeRuntime, err)
			}

			hits := g.Traverse(start, graph.Walk{
				Depth:            depth,
				IncludeSponsored: includeSponsored,
				Predicates:       predicates,
				Symmetric:        symmetric,
			})
			if len(hits) > nodeWarnThreshold && !app.Yes {
				return exit(CodeUsage, fmt.Errorf(
					"%d nodes within %d hops, which is more than a terminal is for. narrow it with --predicate or --depth, or pass --yes",
					len(hits), depth))
			}
			// One hit is the start node reaching nothing, which is a different
			// answer from an empty walk and is reported as one. It is exit 3 with
			// the command that would fill the gap, rather than a bare start node
			// the caller has to notice is alone.
			if len(hits) <= 1 {
				return exit(CodeNoData, fmt.Errorf(
					"%s has no edges the walk could follow. crawl it with `amz crawl --asin %s --follow-rails`, or widen the walk with --depth, --include-sponsored or --symmetric",
					start, amz.ExtractASIN(args[0])))
			}

			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()

			if edgesOnly {
				reached := map[string]bool{}
				for _, h := range hits {
					reached[h.URI] = true
				}
				for _, e := range g.Edges() {
					if !reached[e.Src] {
						continue
					}
					_ = out.Emit(Row{
						Cols:  []string{"src", "predicate", "dst", "via", "sponsored", "observed_at"},
						Vals:  []string{e.Src, e.Predicate, e.Dst, e.Via, strconv.FormatBool(e.Sponsored), e.ObservedAt.Format("2006-01-02")},
						Value: e,
					})
				}
			} else {
				for _, h := range hits {
					pred, via := "", ""
					if h.Via != nil {
						pred, via = h.Via.Predicate, h.Via.Src
					}
					_ = out.Emit(Row{
						Cols:  []string{"depth", "uri", "predicate", "from"},
						Vals:  []string{strconv.Itoa(h.Depth), h.URI, pred, via},
						Value: h,
					})
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "hops from the starting node")
	cmd.Flags().BoolVar(&includeSponsored, "include-sponsored", false, "follow paid placements too")
	cmd.Flags().BoolVar(&symmetric, "symmetric", false, "follow variant_of and parent_of backwards as well")
	cmd.Flags().StringSliceVar(&predicates, "predicate", nil, "limit the walk to these predicates")
	cmd.Flags().BoolVar(&showPredicates, "predicates", false, "print the sixteen predicates and exit")
	cmd.Flags().BoolVar(&edgesOnly, "edges", false, "print the edges rather than the nodes")
	return cmd
}

func printPredicates(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	for _, p := range graph.Predicates() {
		notes := p.Carries
		switch {
		case p.Inferred:
			notes = "inferred. " + notes
		case p.Symmetric:
			notes = "symmetric. " + notes
		}
		_, _ = fmt.Fprintf(w, "%-14s %-8s -> %-16s %s\n", p.Name, p.From, p.To, notes)
	}
	return nil
}

// nodeURI turns whatever the user typed into a node.
//
// A bare ASIN is scoped to the current --marketplace, and a full amz: URI wins
// over the flag, because the same ASIN in two storefronts is two listings and
// picking one of them silently is how they get confused.
func nodeURI(app *App, arg string) (string, error) {
	if _, err := uri.Parse(arg); err == nil {
		return arg, nil
	}
	asin := amz.ExtractASIN(arg)
	if asin == "" {
		asin = arg
	}
	u, err := uri.Product(app.Marketplace, asin)
	if err != nil {
		return "", fmt.Errorf("%q is neither an amz: URI nor an ASIN: %w", arg, err)
	}
	return u, nil
}

func exportCmd(app *App) *cobra.Command {
	var format string
	var withText, includeSponsored bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the store as JSONL, turtle or n-triples",
		Long: strings.TrimSpace(`
Write everything in the store to stdout.

jsonl is the default: one record per line, a header line first carrying the
export time, the tool version and the marketplace, then the nodes, then the
edges. It is the format that keeps every field, including the ones this build
does not have a schema.org term for.

turtle and ntriples are RDF, schema.org where it fits and amzv: for what does
not. Three things are worth knowing before you load them somewhere.

Every offer carries amzv:retrievedAt, without exception and without a flag,
because a price without a timestamp is not a fact.

amzv:distributionDerived is on every rating histogram, because the bucket counts
are reconstructed from the integer percentages Amazon publishes and a consumer
has to be able to see that.

An organic recommendation is schema:isRelatedTo and a paid one is
amzv:sponsoredPlacement. Never both, never the same predicate.

Product descriptions and review text are only included with --with-text.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opt := amz.ExportOpts{WithText: withText, IncludeSponsored: includeSponsored}
			s, err := openStore(app)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			ctx := cmd.Context()
			switch format {
			case "jsonl", "json":
				return exportJSONL(cmd, app, s, opt)
			case "turtle", "ttl", "ntriples", "nt":
				w, closeW, err := rawWriter(app, cmd)
				if err != nil {
					return err
				}
				defer func() { _ = closeW() }()

				g := rdf.NewGraph()
				if err := s.EachProduct(ctx, app.Marketplace, func(p amz.Product) error {
					amz.ProductTriples(g, p, opt)
					return nil
				}); err != nil {
					return exit(CodeRuntime, err)
				}
				if err := s.EachReview(ctx, app.Marketplace, func(r amz.Review) error {
					amz.ReviewTriples(g, r, opt)
					return nil
				}); err != nil {
					return exit(CodeRuntime, err)
				}
				edges, err := s.Edges(ctx, amz.EdgeQuery{IncludeSponsored: includeSponsored})
				if err != nil {
					return exit(CodeRuntime, err)
				}
				seen := map[string]bool{}
				for _, e := range edges {
					amz.EdgeTriples(g, e, opt)
					for _, n := range []string{e.Src, e.Dst} {
						if !seen[n] {
							seen[n] = true
							amz.NodeTriples(g, n)
						}
					}
				}
				if g.Len() == 0 {
					return exit(CodeNoData, errors.New("the store is empty. run `amz crawl` first"))
				}
				if format == "turtle" || format == "ttl" {
					return g.WriteTurtle(w)
				}
				return g.WriteNTriples(w)
			default:
				return exit(CodeUsage, fmt.Errorf("%q: --format is jsonl, turtle or ntriples", format))
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "jsonl", "jsonl|turtle|ntriples")
	cmd.Flags().BoolVar(&withText, "with-text", false, "include descriptions and review text")
	cmd.Flags().BoolVar(&includeSponsored, "include-sponsored", false, "include paid placements")
	return cmd
}

// rawWriter is where turtle and n-triples go.
//
// They do not go through Output, because Output renders rows and RDF is not
// rows. It still has to honour -O, though: an export that ignored the flag every
// other command respects would be a surprise nobody needs at the end of a long
// crawl.
func rawWriter(app *App, cmd *cobra.Command) (io.Writer, func() error, error) {
	noop := func() error { return nil }
	if app.OutFile != "" {
		f, err := os.Create(app.OutFile)
		if err != nil {
			return nil, noop, exit(CodeRuntime, err)
		}
		return f, f.Close, nil
	}
	if app.Out != nil {
		return app.Out, noop, nil
	}
	return cmd.OutOrStdout(), noop, nil
}

// exportJSONL writes the header, then the records, then the edges.
//
// The header is a line and not a sidecar file, because an export that gets
// piped, split and reassembled should carry its own provenance. Somebody opening
// this file in a year needs to know which tool wrote it, when, and which
// storefront it is about, and none of those are derivable from the records.
func exportJSONL(cmd *cobra.Command, app *App, s *amz.Store, opt amz.ExportOpts) error {
	ctx := cmd.Context()
	out, err := app.Output()
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	mkt := app.Marketplace
	if mkt == "" {
		mkt = "all"
	}
	_ = out.Emit(Row{
		Cols: []string{"type", "tool", "version", "marketplace"},
		Vals: []string{"header", "amz", Version, mkt},
		Value: map[string]any{
			"type": "header", "tool": "amz", "version": Version,
			"marketplace": mkt, "with_text": opt.WithText,
			"include_sponsored": opt.IncludeSponsored,
		},
	})

	if err := s.EachProduct(ctx, app.Marketplace, func(p amz.Product) error {
		if !opt.WithText {
			p.Description = ""
			for i := range p.ReviewSample {
				p.ReviewSample[i].Text = ""
			}
		}
		u, _ := uri.Product(p.Marketplace, p.ASIN)
		return out.Emit(Row{
			Cols:  []string{"type", "uri", "title"},
			Vals:  []string{"product", u, p.Title},
			Value: map[string]any{"type": "product", "uri": u, "record": p},
		})
	}); err != nil {
		return exit(CodeRuntime, err)
	}

	edges, err := s.Edges(ctx, amz.EdgeQuery{IncludeSponsored: opt.IncludeSponsored})
	if err != nil {
		return exit(CodeRuntime, err)
	}
	for _, e := range edges {
		_ = out.Emit(Row{
			Cols:  []string{"type", "src", "predicate", "dst"},
			Vals:  []string{"edge", e.Src, e.Predicate, e.Dst},
			Value: map[string]any{"type": "edge", "record": e},
		})
	}
	// One line is always written, so an empty store is one header and nothing
	// else rather than exit 3. That is the honest answer: the export ran, and
	// this is what there was.
	return nil
}
