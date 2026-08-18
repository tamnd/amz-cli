package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func seedCmd(app *App) *cobra.Command {
	var file, entity string
	var priority int
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Enqueue ASINs/URLs into the crawl queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			c, err := app.Client()
			if err != nil {
				return err
			}
			lines, err := readSeeds(file, args)
			if err != nil {
				return exit(CodeUsage, err)
			}
			n := 0
			for _, line := range lines {
				url := seedURL(c, line, entity)
				if url == "" {
					continue
				}
				if err := s.Enqueue(cmd.Context(), url, entity, priority); err != nil {
					return exit(CodeRuntime, err)
				}
				n++
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "enqueued %d item(s)\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file of ASINs/URLs, one per line (- for stdin)")
	cmd.Flags().StringVar(&entity, "entity", amz.EntityProduct, "entity kind: product|reviews|qa|offers")
	cmd.Flags().IntVar(&priority, "priority", 0, "queue priority (higher drains first)")
	return cmd
}

func readSeeds(file string, args []string) ([]string, error) {
	var lines []string
	lines = append(lines, args...)
	if file == "" {
		if len(lines) == 0 {
			return nil, errors.New("provide --file or positional ASINs/URLs")
		}
		return lines, nil
	}
	var r *bufio.Scanner
	if file == "-" {
		r = bufio.NewScanner(os.Stdin)
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = bufio.NewScanner(f)
	}
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, r.Err()
}

func seedURL(c *amz.Client, line, entity string) string {
	asin := asinArg(line)
	switch entity {
	case amz.EntityReviews:
		return c.ReviewURL(asin, amz.ReviewQuery{}, 1)
	case amz.EntityQA:
		return c.QAURL(asin)
	case amz.EntityOffers:
		return c.OffersURL(asin)
	default:
		return c.ProductURL(asin)
	}
}

func enqueueSearch(cmd *cobra.Command, app *App, c *amz.Client, query string, q amz.SearchQuery) error {
	s, err := openStore(app)
	if err != nil {
		return err
	}
	n := 0
	ferr := c.Search(cmd.Context(), query, q, func(card amz.Card) error {
		if card.ASIN == "" {
			return nil
		}
		n++
		return s.Enqueue(cmd.Context(), c.ProductURL(card.ASIN), amz.EntityProduct, 0)
	})
	if ferr != nil {
		return exit(codeFor(ferr), ferr)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "enqueued %d product(s)\n", n)
	return nil
}

// crawlOpts is everything `amz crawl` was asked to do, in one value.
//
// It is a struct rather than a closure over a dozen flag variables because the
// planner and the crawler both read it, and a plan built from different values
// than the run would be worse than no plan at all.
type crawlOpts struct {
	seeds     []string
	seedFile  string
	asins     []string
	chart     string
	category  string
	search    string
	limit     int
	fromSite  bool
	rails     bool
	railDepth int
	railBudge int
	sponsored bool
	withText  bool
	depth     string
	kinds     string
	attempts  int
	resume    bool
	quiet     bool
}

func crawlCmd(app *App) *cobra.Command {
	o := &crawlOpts{}
	cmd := &cobra.Command{
		Use:   "crawl",
		Short: "Drain a frontier into the local store",
		Long: strings.TrimSpace(`
Read a set of seeds and everything they lead to, into the local store.

A crawl is the one command here that spends a long time and a lot of somebody
else's bandwidth, so it says what it will cost before it starts. Run it with
--dry-run to see the plan and read nothing.

It is resumable. The frontier lives in the store, an item is claimed before it
is read, and a crawl that is killed mid-item returns that item to the queue on
the next start rather than losing it or doing it twice.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.seeds = append(o.seeds, args...)
			o.quiet = app.Quiet
			return runCrawl(cmd, app, o)
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&o.seeds, "seed", nil, "seed uri, url or asin, repeatable")
	fl.StringVar(&o.seedFile, "seed-file", "", "file of seeds, one per line (- for stdin)")
	fl.StringArrayVar(&o.asins, "asin", nil, "seed one asin, repeatable")
	fl.StringVar(&o.chart, "chart", "", "seed from a chart: bestsellers|new-releases|movers-and-shakers|most-wished-for|most-gifted")
	fl.StringVar(&o.category, "category", "", "category slug or browse node id, for --chart or on its own")
	fl.StringVar(&o.search, "search", "", "seed from a search query")
	fl.IntVar(&o.limit, "limit", 100, "how many products to take from each chart, category or search seed")
	fl.BoolVar(&o.fromSite, "from-sitemap", false, "")
	_ = fl.MarkHidden("from-sitemap")

	fl.BoolVar(&o.rails, "follow-rails", false, "add rail cards to the frontier, which is the only free source of asins")
	fl.IntVar(&o.railDepth, "rail-depth", 1, "how many rail hops from a seed to follow")
	fl.IntVar(&o.railBudge, "rail-budget", 500, "most products to pull in through rails, because a rail walk over amazon does not terminate")
	fl.BoolVar(&o.sponsored, "include-sponsored", false, "follow sponsored rail cards too")

	fl.BoolVar(&o.withText, "with-text", false, "store review and description text, which is not stored by default")
	fl.StringVar(&o.depth, "depth", "meta", "quick|meta|full|deep, as `amz product --depth`")
	fl.StringVar(&o.kinds, "kinds", "", "restrict entity kinds already in the queue (comma-separated)")
	fl.IntVar(&o.attempts, "max-attempts", 3, "park an item after this many claims, so one bad url cannot stop the crawl finishing")
	fl.BoolVar(&o.resume, "resume", true, "drain what is already in the queue as well as the new seeds")
	return cmd
}

func runCrawl(cmd *cobra.Command, app *App, o *crawlOpts) error {
	// There is no sitemap. Somebody will try this, and a flag that silently did
	// nothing would send them looking for the bug in their shell quoting.
	if o.fromSite {
		return exit(CodeUsage, errors.New(
			"there is no --from-sitemap, because amazon publishes no sitemap: robots.txt names none and /sitemap.xml is a 404.\n"+
				"every asin comes from a chart, a browse node, a search, a rail or a seed. run `amz why sitemap`"))
	}
	// A single disallowed read is one request. A crawl under --no-robots is
	// thousands, unattended, and the flag is easy to leave in a shell history.
	// --yes is the second hand on the switch.
	if app.NoRobots && !app.Yes {
		return exit(CodeUsage, errors.New("crawl with --no-robots needs --yes: this ignores robots.txt for every page in the queue"))
	}
	depth, err := amz.ParseDepth(o.depth)
	if err != nil {
		return exit(CodeUsage, err)
	}
	if o.chart != "" && o.category == "" {
		// The root chart is a real thing to crawl, so this is a note and not an
		// error, but it is the difference between 100 products and 50.
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "amz: --chart with no --category reads the root chart, which is one page of 50")
	}
	if o.category == "" && o.chart == "" && o.search == "" && len(o.seeds) == 0 && len(o.asins) == 0 && o.seedFile == "" && !o.resume {
		return exit(CodeUsage, errors.New("nothing to crawl: give --seed, --seed-file, --asin, --chart, --category or --search"))
	}

	c, err := app.Client()
	if err != nil {
		return err
	}
	s, err := openStore(app)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	ctx := cmd.Context()
	pending, err := s.PendingCount(ctx)
	if err != nil {
		return exit(CodeRuntime, err)
	}
	plan := planCrawl(app, o, depth, pending)

	if app.DryRun {
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprint(out, plan)
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintf(out, "  %-11s %s\n", "seeds", describeSeeds(o, pending))
		_, _ = fmt.Fprintf(out, "  %-11s %s\n", "frontier", describeFrontier(o))
		_, _ = fmt.Fprintf(out, "  %-11s %s\n", "store", app.Config().DBPath)
		_, _ = fmt.Fprintf(out, "  %-11s %s\n", "text", textLine(o.withText))
		_, _ = fmt.Fprintln(out, "\nnothing was read. drop --dry-run to run.")
		return nil
	}
	if !o.withText {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "amz: review and description text is read and not stored. --with-text keeps it")
	}

	// Anything left claimed by a crawl that died is nobody's work until it is
	// released, so a resumed crawl starts by taking it back.
	if n, err := s.Recover(ctx, o.attempts); err != nil {
		return exit(CodeRuntime, err)
	} else if n > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: recovered %d item(s) a previous crawl left in flight\n", n)
	}

	if err := seedFrontier(ctx, cmd, s, c, o); err != nil {
		return err
	}

	allow := map[string]bool{}
	for _, k := range splitCSV(o.kinds) {
		allow[k] = true
	}
	return drainQueue(ctx, cmd, s, c, o, depth, allow)
}

// planCrawl prices the run before it happens.
//
// Every number here is a count of requests, not a guess about them: a chart of N
// products is ceil(N/50) pages because a chart page holds 50, a search of N is
// ceil(N/24) pages because 24 is the low end of the measured page size and
// under-quoting the request count is the direction that surprises people.
func planCrawl(app *App, o *crawlOpts, depth amz.Depth, pending int) amz.Plan {
	p := amz.Plan{Pace: amz.ClampDelayWith(app.Rate, app.NoRobots)}
	products := 0

	if o.chart != "" {
		pages := ceilDiv(o.limit, 50)
		p.Add("chart", "chart pages", pages, "")
		products += min(o.limit, pages*50)
	}
	if o.category != "" && o.chart == "" {
		p.Add("category", "browse nodes", 1, "")
		products += min(o.limit, 30)
	}
	if o.search != "" {
		pages := ceilDiv(o.limit, 24)
		p.Add("search", "search pages", pages, "")
		products += min(o.limit, pages*24)
	}
	products += len(o.seeds) + len(o.asins) + pending
	if o.seedFile != "" {
		// The file is not read here. Counting its lines would mean reading it
		// twice and disagreeing with itself if it is stdin, so the plan says the
		// seeds it can count and the run says what it found.
		p.Add("product", "detail pages", 0, "")
	}

	surface, note := "product", ""
	if depth == amz.DepthQuick {
		surface, note = "light", "light, /gp/aw/d/"
	}
	if depth == amz.DepthDeep {
		note = "deep, plus a request per variation sibling and one per seller"
	}
	p.Add(surface, "detail pages", products, note)

	if o.rails {
		// A rail hop costs no request of its own. The products it finds each
		// cost one, and the budget is the ceiling on how many that can be.
		p.Add(surface, "detail pages", min(o.railBudge, products*o.railDepth*20), "via rails, up to the budget")
	}
	return p
}

func describeSeeds(o *crawlOpts, pending int) string {
	var parts []string
	if o.chart != "" {
		if o.category != "" {
			parts = append(parts, "chart "+o.chart+"/"+o.category)
		} else {
			parts = append(parts, "chart "+o.chart)
		}
	} else if o.category != "" {
		parts = append(parts, "category "+o.category)
	}
	if o.search != "" {
		parts = append(parts, "search "+strconv.Quote(o.search))
	}
	if n := len(o.seeds) + len(o.asins); n > 0 {
		parts = append(parts, fmt.Sprintf("%d on the command line", n))
	}
	if o.seedFile != "" {
		parts = append(parts, "file "+o.seedFile)
	}
	if pending > 0 && o.resume {
		parts = append(parts, fmt.Sprintf("%d already queued", pending))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func describeFrontier(o *crawlOpts) string {
	if !o.rails {
		return "rails off, depth 0"
	}
	s := fmt.Sprintf("rails on, depth %d, budget %d", o.railDepth, o.railBudge)
	if !o.sponsored {
		s += ", sponsored cards excluded"
	}
	return s
}

func textLine(withText bool) string {
	if withText {
		return "review bodies and description text are stored"
	}
	return "not stored. --with-text to keep review and description text"
}

// seedFrontier turns the seed flags into queue rows.
//
// The expansions read pages, which is why they are here and not in the planner:
// a chart has to be fetched before anybody knows which 50 products it names.
func seedFrontier(ctx context.Context, cmd *cobra.Command, s *amz.Store, c *amz.Client, o *crawlOpts) error {
	n := 0
	add := func(url string) error {
		if url == "" {
			return nil
		}
		n++
		return s.Enqueue(ctx, url, amz.EntityProduct, 0)
	}

	lines := append([]string(nil), o.seeds...)
	lines = append(lines, o.asins...)
	if o.seedFile != "" {
		fromFile, err := readSeeds(o.seedFile, nil)
		if err != nil {
			return exit(CodeUsage, err)
		}
		lines = append(lines, fromFile...)
	}
	for _, line := range lines {
		if err := add(c.ProductURL(asinArg(line))); err != nil {
			return exit(CodeRuntime, err)
		}
	}

	if o.chart != "" {
		kind := amz.ChartKind(o.chart)
		err := c.FetchChart(ctx, kind, o.category, "", o.limit, func(e amz.BestsellerEntry) error {
			if err := s.PutChartEntry(ctx, e); err != nil {
				return err
			}
			return add(c.ProductURL(e.ASIN))
		})
		if err != nil {
			return exit(codeFor(err), err)
		}
	} else if o.category != "" {
		cat, err := c.FetchCategory(ctx, o.category)
		if err != nil {
			return exit(codeFor(err), err)
		}
		if err := s.PutCategory(ctx, cat); err != nil {
			return exit(CodeRuntime, err)
		}
		for _, a := range cat.TopASINs {
			if n >= o.limit {
				break
			}
			if err := add(c.ProductURL(a)); err != nil {
				return exit(CodeRuntime, err)
			}
		}
	}

	if o.search != "" {
		q := amz.SearchQuery{Limit: o.limit}
		err := c.Search(ctx, o.search, q, func(card amz.Card) error {
			if card.Sponsored && !o.sponsored {
				return nil
			}
			return add(c.ProductURL(card.ASIN))
		})
		if err != nil {
			return exit(codeFor(err), err)
		}
	}

	if n > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %d seed(s) queued\n", n)
	}
	return nil
}

// progress is the running total a crawl prints to stderr.
type progress struct {
	done, failed, skipped int
	requests, cached      int
	bytes                 int64
	byKind                map[string]int
	started               time.Time
}

func (p *progress) note(env amz.Envelope) {
	for _, src := range env.Sources {
		p.requests++
		p.bytes += int64(src.Bytes)
		if src.Cached {
			p.cached++
		}
	}
}

func (p *progress) line(remaining int) string {
	var kinds []string
	for _, k := range []string{amz.EntityProduct, amz.EntityReviews, amz.EntityQA, amz.EntityOffers} {
		if n := p.byKind[k]; n > 0 {
			kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
		}
	}
	s := fmt.Sprintf("amz: %s | %d req, %d cached, %s | %d left, %d errors | %s",
		strings.Join(kinds, " "), p.requests, p.cached, amz.HumanBytes(p.bytes),
		remaining, p.failed, amz.HumanDuration(time.Since(p.started)))
	return s
}

// drainQueue works the frontier one item at a time.
//
// It used to run app.Workers goroutines against one client. That made --rate
// meaningless, since the throttle spaces requests but N workers each waited their
// own turn, and it made the crawl indistinguishable from a burst. amz now reads
// sequentially and controls its pace with one number.
func drainQueue(ctx context.Context, cmd *cobra.Command, s *amz.Store, c *amz.Client, o *crawlOpts, depth amz.Depth, allow map[string]bool) error {
	const batchSize = 16
	p := &progress{byKind: map[string]int{}, started: time.Now()}
	railed := 0
	errOut := cmd.ErrOrStderr()

	for {
		batch, err := s.NextBatch(ctx, batchSize)
		if err != nil {
			return exit(CodeRuntime, err)
		}
		if len(batch) == 0 {
			break
		}
		for _, it := range batch {
			if err := ctx.Err(); err != nil {
				// Hand the item back before leaving, so a Ctrl-C is a pause and
				// not a lost page.
				_ = s.MarkStatus(ctx, it.ID, "pending", nil)
				return err
			}
			if len(allow) > 0 && !allow[it.Entity] {
				p.skipped++
				_ = s.MarkStatus(ctx, it.ID, "skipped", nil)
				continue
			}
			found, err := crawlOne(ctx, s, c, it, o, depth, p)
			if err != nil {
				p.failed++
				if errors.Is(err, amz.ErrBlocked) {
					// Blocked is not this item's fault and not a reason to burn
					// its attempt count, so it goes straight back.
					_ = s.MarkStatus(ctx, it.ID, "pending", nil)
					_, _ = fmt.Fprintln(errOut, "amz: blocked, backing off 60s")
					select {
					case <-time.After(60 * time.Second):
					case <-ctx.Done():
						return ctx.Err()
					}
					continue
				}
				_, _ = fmt.Fprintf(errOut, "amz: %s: %v\n", it.URL, err)
				_ = s.MarkStatus(ctx, it.ID, "error", err)
				continue
			}
			p.done++
			p.byKind[it.Entity]++
			_ = s.MarkStatus(ctx, it.ID, "done", nil)

			// Rails are free ASINs, and free is exactly why they need a ceiling.
			if o.rails && it.Depth < o.railDepth {
				for _, u := range found {
					if railed >= o.railBudge {
						break
					}
					if err := s.EnqueueDepth(ctx, u, amz.EntityProduct, 0, it.Depth+1); err != nil {
						return exit(CodeRuntime, err)
					}
					railed++
				}
				if railed >= o.railBudge {
					noteOnce(errOut, fmt.Sprintf("amz: rail budget of %d reached, no more rail cards will be followed\n", o.railBudge))
				}
			}
		}
		left, err := s.PendingCount(ctx)
		if err != nil {
			return exit(CodeRuntime, err)
		}
		if !o.quiet {
			_, _ = fmt.Fprintln(errOut, p.line(left))
		}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "crawl complete: %d done, %d failed, %d skipped, %d requests in %s\n",
		p.done, p.failed, p.skipped, p.requests, amz.HumanDuration(time.Since(p.started)))
	if p.done == 0 && p.failed > 0 {
		return exit(CodePartial, nil)
	}
	return nil
}

// crawlOne reads one queue item and returns the product URLs it found on rails.
func crawlOne(ctx context.Context, s *amz.Store, c *amz.Client, it amz.QueueItem, o *crawlOpts, depth amz.Depth, p *progress) ([]string, error) {
	asin := amz.ExtractASIN(it.URL)
	switch it.Entity {
	case amz.EntityReviews:
		return nil, c.FetchReviews(ctx, asin, amz.ReviewQuery{Limit: 50}, func(r amz.Review) error {
			if !o.withText {
				r.Text, r.Title = "", ""
			}
			return s.PutReview(ctx, r)
		})
	case amz.EntityQA:
		err := c.FetchQA(ctx, asin, func(q amz.QA) error { return s.PutQA(ctx, q) })
		if errors.Is(err, amz.ErrNoQA) {
			return nil, nil
		}
		return nil, err
	case amz.EntityOffers:
		return nil, c.FetchOffers(ctx, asin, amz.OfferQuery{}, func(l amz.OfferListing) error {
			return s.PutOfferListing(ctx, l)
		})
	default:
		// Rails ride along on the detail page, so following them costs nothing
		// extra and the depth is raised to full only when they are wanted.
		d := depth
		if o.rails && d == amz.DepthMeta {
			d = amz.DepthFull
		}
		prod, err := c.FetchProductDepth(ctx, it.URL, d)
		if err != nil {
			return nil, err
		}
		p.note(prod.Envelope)

		op := amz.OpFor(it.URL)
		if !o.withText {
			prod.Description = ""
			for i := range prod.ReviewSample {
				prod.ReviewSample[i].Text, prod.ReviewSample[i].Title = "", ""
			}
			// The surface is narrowed to match what is being stored. Without
			// this, a no-text crawl would delete the text a --with-text crawl
			// stored, because an empty field from a surface that carries it
			// reads as "the page no longer has one". See amz.Op.Without.
			op = op.Without("description", "reviews")
		}
		if err := s.PutProductWith(ctx, prod, op); err != nil {
			return nil, err
		}
		return railURLs(c, prod, o), nil
	}
}

// railURLs is the free half of the frontier: every ASIN the page already handed
// over, minus the advertising unless it was asked for.
func railURLs(c *amz.Client, p amz.Product, o *crawlOpts) []string {
	if !o.rails {
		return nil
	}
	seen := map[string]bool{p.ASIN: true}
	var out []string
	for _, rail := range p.Rails {
		if rail.Sponsored && !o.sponsored {
			continue
		}
		for _, card := range rail.Cards {
			if card.ASIN == "" || seen[card.ASIN] {
				continue
			}
			if card.Sponsored && !o.sponsored {
				continue
			}
			seen[card.ASIN] = true
			out = append(out, c.ProductURL(card.ASIN))
		}
	}
	for _, a := range p.SimilarASINs {
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, c.ProductURL(a))
		}
	}
	return out
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}

var noted = map[string]bool{}

// noteOnce prints a line the first time it is asked to. A budget message on
// every one of four hundred remaining cards is noise, and noise is how the one
// line that mattered gets scrolled away.
func noteOnce(w io.Writer, s string) {
	if noted[s] {
		return
	}
	noted[s] = true
	_, _ = fmt.Fprint(w, s)
}
