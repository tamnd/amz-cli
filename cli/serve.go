package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// The server is the command tree with a different front door.
//
// Every tool below dispatches by re-entering the cobra tree with `-o json` and
// an argv this file builds. Nothing is reimplemented, so `amz search` over HTTP
// and `amz search` at a terminal cannot answer differently, and a parser fix
// reaches all three front doors at once.
//
// The table is also the allowlist. crawl, config, cache clear, open and export
// are not in it, so they are unreachable rather than refused: there is no name a
// caller can send that resolves to one. Same for --no-robots, which is not an
// argument of any tool, is rejected as an unknown key, and could not survive
// argv construction anyway because this file writes the argv.

// notExposed is the deny list, kept as data so the banner, the help text and the
// tests all read the same list instead of restating it.
//
// A crawl is a long running job with a cost, and a tool call that quietly starts
// one is the wrong shape. seed is the queue that feeds it. config and cache clear
// change the operator's machine. open launches a browser on a host nobody is
// sitting at. export dumps the whole store, which is a file to copy and not an
// answer to a question.
var notExposed = []string{"crawl", "seed", "config", "cache clear", "open", "export"}

// toolSpec is one exposed command: how it is spelled on the wire and how that
// becomes an argv.
type toolSpec struct {
	name    string
	path    []string
	summary string
	// pos are the command's positional arguments, in order. flags are its flags.
	// The split matters only when building the argv; on the wire they are one
	// flat set of named arguments.
	pos   []amz.ToolArg
	flags []amz.ToolArg
}

func (s toolSpec) tool() amz.Tool {
	args := make([]amz.ToolArg, 0, len(s.pos)+len(s.flags))
	args = append(args, s.pos...)
	args = append(args, s.flags...)
	return amz.Tool{Name: s.name, Summary: s.summary, Args: args}
}

func (s toolSpec) arg(name string) (amz.ToolArg, bool) {
	for _, a := range s.pos {
		if a.Name == name {
			return a, true
		}
	}
	for _, a := range s.flags {
		if a.Name == name {
			return a, true
		}
	}
	return amz.ToolArg{}, false
}

// argument constructors. The Name always matches the flag the CLI uses, hyphens
// included, so a caller who has read `amz search --help` already knows the
// spelling.
func str(name, desc string) amz.ToolArg {
	return amz.ToolArg{Name: name, Type: "string", Desc: desc}
}

func enum(name, desc string, values ...string) amz.ToolArg {
	return amz.ToolArg{Name: name, Type: "string", Desc: desc, Enum: values}
}

func num(name, desc string) amz.ToolArg {
	return amz.ToolArg{Name: name, Type: "integer", Desc: desc}
}

func yes(name, desc string) amz.ToolArg {
	return amz.ToolArg{Name: name, Type: "boolean", Desc: desc}
}

func required(a amz.ToolArg) amz.ToolArg { a.Required = true; return a }
func repeated(a amz.ToolArg) amz.ToolArg { a.Repeated = true; return a }

// searchFilters are the refinement flags search and refine share.
func searchFilters() []amz.ToolArg {
	return []amz.ToolArg{
		str("department", "department alias for i=, as listed by search --list-departments"),
		enum("sort", "result order", "featured", "price-asc", "price-desc", "review", "newest", "bestselling"),
		str("price", "price range in major units: 50-150, 50-, -150"),
		// No prime here. `search --prime` is still a flag so that a v0.2.1 script
		// gets told what happened, but it only ever returns an error now, and an
		// argument whose entire behaviour is to fail has no business in a schema
		// a model reads. `amz refine <query>` is what finds the group instead.
		num("stars", "minimum star rating, resolved against the sidebar"),
		str("brand", "brand name or id, resolved against the sidebar"),
		str("seller", "seller name or merchant id, resolved against the sidebar"),
		enum("condition", "item condition", "new", "used", "renewed"),
		repeated(str("refine", "refine by group: p_123=213704,111070. comma is OR within one group")),
	}
}

// common are the two arguments every tool accepts.
//
// The rest of the global flags are not negotiable over the wire. --output is
// pinned to json because the wire format is not the caller's to choose, and
// --no-robots, --yes, --dry-run and --out are absent for the reasons at the top
// of this file.
func common() []amz.ToolArg {
	return []amz.ToolArg{
		str("marketplace", "marketplace slug, defaults to the one the server was started with"),
		num("limit", "cap the number of records, 0 for unlimited"),
	}
}

func chartSpec(name, summary string) toolSpec {
	return toolSpec{
		name: name, path: []string{name}, summary: summary,
		pos:   []amz.ToolArg{str("category", "category name or browse node, or empty for the whole store")},
		flags: []amz.ToolArg{str("node", "browse node id override")},
	}
}

// toolSpecs is the registry, in the order the spec lists it.
func toolSpecs() []toolSpec {
	specs := []toolSpec{
		{
			name: "product", path: []string{"product"},
			summary: "One or more product detail pages, normalized",
			pos:     []amz.ToolArg{repeated(required(str("asin", "ASIN or product URL")))},
			flags: []amz.ToolArg{
				enum("depth", "how much to read. deep costs a request per variation sibling", "quick", "meta", "full", "deep"),
				yes("light", "shorthand for depth=quick"),
				yes("variants", "also list variant ASINs as rows"),
				yes("with-offers", "also emit the buy box as an offer row"),
			},
		},
		{
			name: "price", path: []string{"price"},
			summary: "Just the price block for one or more ASINs",
			pos:     []amz.ToolArg{repeated(required(str("asin", "ASIN or product URL")))},
		},
		{
			name: "offers", path: []string{"offers"},
			summary: "The other sellers listing for one ASIN",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
			flags: []amz.ToolArg{
				str("condition", "keep this condition only: new, used, ..."),
				yes("prime", "keep the offer only when Amazon fulfils it"),
			},
		},
		{
			name: "reviews", path: []string{"reviews"},
			summary: "The reviews Amazon shows a logged out reader, which is the eight on the detail page",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
			flags: []amz.ToolArg{
				enum("sort", "order the reviews on the page", "recent", "helpful"),
				num("stars", "keep N star reviews, 1 to 5, applied locally"),
				yes("verified", "keep verified purchases only, applied locally"),
				yes("with-images", "keep reviews with images only, applied locally"),
				yes("deep", "attempt the full corpus, which is behind a sign-in and will fail"),
			},
		},
		{
			name: "qa", path: []string{"qa"},
			summary: "Customer questions and answers for one ASIN",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
		},
		{
			name: "variants", path: []string{"variants"},
			summary: "The variation family of one ASIN",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
			flags: []amz.ToolArg{
				yes("resolve", "fetch each sibling for its own price and availability, one request each"),
			},
		},
		{
			name: "related", path: []string{"related"},
			summary: "The recommendation rails on a detail page",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
			flags: []amz.ToolArg{
				enum("kind", "filter the rails", "related", "sponsored", "also-bought", "also-viewed"),
				yes("include-sponsored", "keep the advertising placements as well as the organic rails"),
			},
		},
		{
			name: "search", path: []string{"search"},
			summary: "Search results with the full refinement vocabulary and pagination",
			pos:     []amz.ToolArg{required(str("query", "the search text"))},
			flags: append(searchFilters(),
				num("page", "first result page"),
				num("max-pages", "stop after this many pages. it can lower the 20 page ceiling and not raise it"),
				yes("pages", "emit one record per page: counts, ceiling, refinements, instead of the cards"),
				yes("all", "partition the query and union the cells, to get past the 306 result ceiling"),
				str("partition", "the refinement group to partition on. the default is the one with the most values"),
				num("partition-depth", "how many times a capped cell may be split again"),
				yes("include-sponsored", "keep the advertising placements, which are excluded by default"),
				yes("list-departments", "list the department aliases this marketplace offers and stop"),
			),
		},
		{
			name: "refine", path: []string{"refine"},
			summary: "The refinement groups and values a query offers, without the results",
			pos:     []amz.ToolArg{required(str("query", "the search text"))},
			flags:   append(searchFilters(), str("group", "list the values of one group instead of the groups")),
		},
		{
			name: "category", path: []string{"category"},
			summary: "One browse node: its name, its path and what it links",
			pos:     []amz.ToolArg{required(str("node", "browse node id or category URL"))},
			flags: []amz.ToolArg{
				yes("related", "list the browse nodes this page links instead of the record"),
				yes("top", "list top ASINs instead of the record"),
			},
		},
		{
			name: "tree", path: []string{"tree"},
			summary: "Walk the browse node tree outward from one node",
			pos:     []amz.ToolArg{str("node", "browse node id or URL, or empty for the root")},
			flags: []amz.ToolArg{
				num("depth", "how far to walk. every level multiplies the requests"),
				yes("counts", "include the shelf and ASIN counts for each node"),
				num("max-nodes", "stop after this many nodes"),
			},
		},
		{
			name: "brand", path: []string{"brand"},
			summary: "A brand storefront",
			pos:     []amz.ToolArg{required(str("slug", "brand slug or storefront URL"))},
			flags:   []amz.ToolArg{yes("featured", "list featured ASINs instead of the record")},
		},
		{
			name: "seller", path: []string{"seller"},
			summary: "A seller profile: name, rating, feedback counts",
			pos:     []amz.ToolArg{required(str("id", "merchant id or seller URL"))},
		},
		{
			name: "author", path: []string{"author"},
			summary: "An author page: biography, follow count, books",
			pos:     []amz.ToolArg{required(str("slug", "author slug or URL"))},
			flags:   []amz.ToolArg{yes("books", "list the author's book ASINs instead of the record")},
		},
		chartSpec("bestsellers", "Top sellers in the store or a category"),
		chartSpec("new-releases", "Newest releases in the store or a category"),
		chartSpec("movers", "Biggest 24h rank movers"),
		chartSpec("wished", "Most wished for items"),
		chartSpec("gifted", "Most gifted items"),
		{
			name: "deals", path: []string{"deals"},
			summary: "The deals page",
			flags: []amz.ToolArg{
				str("department", "limit to a department"),
				num("min-discount", "minimum discount percent"),
			},
		},
		{
			name: "lookup", path: []string{"lookup"},
			summary: "Read one node back out of the local store, without fetching",
			pos:     []amz.ToolArg{required(str("uri", "an amz: URI or an ASIN"))},
		},
		{
			name: "find", path: []string{"find"},
			summary: "Full text search over what is already in the local store",
			pos:     []amz.ToolArg{required(str("text", "the text to look for"))},
		},
		{
			name: "query", path: []string{"query"},
			summary: "Run one read only SQL statement against the local store",
			pos:     []amz.ToolArg{required(str("sql", "a SELECT statement"))},
		},
		{
			name: "series", path: []string{"series"},
			summary: "The price and rank history the store has recorded for one ASIN",
			pos:     []amz.ToolArg{required(str("asin", "ASIN or product URL"))},
			flags:   []amz.ToolArg{enum("series", "which series to return, or both when unset", "price", "rank")},
		},
		{
			name: "graph", path: []string{"graph"},
			summary: "Walk the claim graph outward from one node, using only what a crawl already recorded",
			pos:     []amz.ToolArg{required(str("uri", "an amz: URI or an ASIN"))},
			flags: []amz.ToolArg{
				num("depth", "hops from the starting node"),
				repeated(str("predicate", "limit the walk to these predicates")),
				yes("include-sponsored", "follow paid placements too"),
				yes("symmetric", "follow variant_of and parent_of backwards as well"),
				yes("edges", "return the edges rather than the nodes"),
				yes("predicates", "return the sixteen predicates and stop"),
			},
		},
	}
	for i := range specs {
		specs[i].flags = append(specs[i].flags, common()...)
	}
	return specs
}

func specFor(name string) (toolSpec, bool) {
	for _, s := range toolSpecs() {
		if s.name == name {
			return s, true
		}
	}
	return toolSpec{}, false
}

func toolRegistry() []amz.Tool {
	specs := toolSpecs()
	out := make([]amz.Tool, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.tool())
	}
	return out
}

// dispatcher runs tool calls by re-entering the command tree.
type dispatcher struct {
	marketplace string
	inherited   []string

	// mu serializes calls. amz reads one page at a time everywhere else, for the
	// reason --workers was removed: two requests in flight make --rate a lie by a
	// factor of two and Amazon scores the session. A server that answered ten
	// clients at once would undo that quietly, so it does not.
	mu sync.Mutex
}

// inheritedFlags carries the operator's global flags into every tool call.
//
// Absent on purpose: --no-robots, --yes, --dry-run, --raw, --out, --output,
// --fields, --limit and --template. The first two are decisions a person makes
// at their own terminal for one run, and the rest belong to the call rather than
// to the process.
func inheritedFlags(app *App) []string {
	var out []string
	if app.DataDir != "" {
		out = append(out, "--data-dir="+app.DataDir)
	}
	if app.ConfigPath != "" {
		out = append(out, "--config="+app.ConfigPath)
	}
	// Pace, timeout and retries are carried across unconditionally rather than
	// when they differ from the default. An operator who started the server with
	// --rate 10s meant it, and a tool call that quietly reverted to 3s would be
	// the server crawling faster than the person running it asked for.
	out = append(out,
		"--rate="+app.Rate.String(),
		"--timeout="+app.Timeout.String(),
		"--retries="+strconv.Itoa(app.Retries),
	)
	if app.NoCache {
		out = append(out, "--no-cache")
	}
	if app.Refresh {
		out = append(out, "--refresh")
	}
	if app.UseAPI {
		out = append(out, "--api")
	}
	return out
}

// argvFor turns a tool call into a command line.
//
// An unrecognised argument is a usage error rather than something dropped
// quietly, because a caller that misspells `include-sponsored` should be told
// so and not handed organic results that look complete. It is also the check
// that stops --no-robots: the flag is an argument of no tool, so it never gets
// as far as the argv.
func (d *dispatcher) argvFor(s toolSpec, args map[string]any) ([]string, error) {
	names := make([]string, 0, len(args))
	for k := range args {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, ok := s.arg(k); !ok {
			return nil, exit(CodeUsage, fmt.Errorf("%s has no argument %q. GET /v1/tools for the ones it does", s.name, k))
		}
	}

	argv := append([]string(nil), s.path...)
	var positional []string
	for _, a := range s.pos {
		v, ok := args[a.Name]
		if !ok || v == nil {
			if a.Required {
				return nil, exit(CodeUsage, fmt.Errorf("%s needs %q", s.name, a.Name))
			}
			continue
		}
		vals, err := argStrings(s.name, a, v)
		if err != nil {
			return nil, err
		}
		positional = append(positional, vals...)
	}

	for _, a := range s.flags {
		v, ok := args[a.Name]
		if !ok || v == nil {
			continue
		}
		if a.Type == "boolean" {
			b, ok := v.(bool)
			if !ok {
				return nil, exit(CodeUsage, fmt.Errorf("%s: %q is a true/false argument", s.name, a.Name))
			}
			if b {
				argv = append(argv, "--"+a.Name)
			}
			continue
		}
		vals, err := argStrings(s.name, a, v)
		if err != nil {
			return nil, err
		}
		for _, val := range vals {
			argv = append(argv, "--"+a.Name+"="+val)
		}
	}

	if _, ok := args["marketplace"]; !ok && d.marketplace != "" {
		argv = append(argv, "--marketplace="+d.marketplace)
	}
	argv = append(argv, d.inherited...)
	argv = append(argv, "--output=json")
	if len(positional) > 0 {
		// Everything after -- is a value. A query that starts with a hyphen is a
		// real search on Amazon and must not become a flag here.
		argv = append(argv, "--")
		argv = append(argv, positional...)
	}
	return argv, nil
}

// argStrings renders one argument's value as command line strings.
func argStrings(tool string, a amz.ToolArg, v any) ([]string, error) {
	if list, ok := v.([]any); ok {
		if !a.Repeated {
			return nil, exit(CodeUsage, fmt.Errorf("%s: %q takes one value, not a list", tool, a.Name))
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			one, err := argStrings(tool, amz.ToolArg{Name: a.Name, Type: a.Type, Enum: a.Enum}, item)
			if err != nil {
				return nil, err
			}
			out = append(out, one...)
		}
		return out, nil
	}
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case float64:
		if a.Type == "integer" && t != float64(int64(t)) {
			return nil, exit(CodeUsage, fmt.Errorf("%s: %q is a whole number", tool, a.Name))
		}
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		s = t.String()
	case bool:
		return nil, exit(CodeUsage, fmt.Errorf("%s: %q is not a true/false argument", tool, a.Name))
	default:
		return nil, exit(CodeUsage, fmt.Errorf("%s: %q got a value it cannot use", tool, a.Name))
	}
	if len(a.Enum) > 0 && !containsFold(a.Enum, s) {
		return nil, exit(CodeUsage, fmt.Errorf("%s: %q is not one of %s for %q", tool, s, strings.Join(a.Enum, ", "), a.Name))
	}
	return []string{s}, nil
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// run executes one tool call and returns the records it printed.
func (d *dispatcher) run(ctx context.Context, name string, args map[string]any) (amz.Records, error) {
	s, ok := specFor(name)
	if !ok {
		return amz.Records{}, exit(CodeUsage, fmt.Errorf("no tool named %q", name))
	}
	argv, err := d.argvFor(s, args)
	if err != nil {
		return amz.Records{}, err
	}
	// The argv is built from a fixed table, so this cannot fire. It is here
	// because the cost of being wrong about that is amz ignoring robots.txt on
	// somebody else's behalf, and that is not a thing to leave to a code review.
	for _, a := range argv {
		if a == "--no-robots" || strings.HasPrefix(a, "--no-robots=") {
			return amz.Records{}, exit(CodeRuntime, errors.New("refusing to run: --no-robots reached a server argv"))
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	var buf bytes.Buffer
	root, app := newRoot()
	root.SetArgs(argv)
	root.SetOut(&buf)
	root.SetErr(io.Discard)
	runErr := root.ExecuteContext(ctx)
	// Nothing found and partly found are answers, not failures. The envelope on
	// the result already carries `missed`, which says what was incomplete and
	// why, so turning either into an error would throw that away and tell the
	// caller less.
	if code := codeFor(runErr); runErr != nil && code != CodeNoData && code != CodePartial {
		return amz.Records{}, &amz.ToolError{Code: code, Err: runErr}
	}
	return amz.Records{Rows: decodeRecords(buf.Bytes()), Pages: app.observed}, nil
}

// decodeRecords reads what the command printed.
//
// Two flags, search --list-departments and graph --predicates, print a listing
// rather than records. Those come back as a single record holding the text,
// which is a truthful answer to a question whose answer really is a listing.
func decodeRecords(b []byte) []json.RawMessage {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 {
		return []json.RawMessage{}
	}
	var recs []json.RawMessage
	if json.Unmarshal(trimmed, &recs) == nil {
		if recs == nil {
			recs = []json.RawMessage{}
		}
		return recs
	}
	text, err := json.Marshal(map[string]string{"text": string(trimmed)})
	if err != nil {
		return []json.RawMessage{}
	}
	return []json.RawMessage{text}
}

func newServer(app *App) *amz.Server {
	return &amz.Server{
		Tools:       toolRegistry(),
		Dispatch:    (&dispatcher{marketplace: app.Marketplace, inherited: inheritedFlags(app)}).run,
		Marketplace: app.Marketplace,
		Version:     Version,
	}
}

const defaultAddr = "127.0.0.1:8787"

func serveCmd(app *App) *cobra.Command {
	var addr string
	var showTools bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the read commands over HTTP",
		Long: "Serve the read commands over HTTP as JSON.\n\n" +
			"GET /v1/tools lists them, GET or POST /v1/tools/{name} runs one, and every result carries the\n" +
			"envelope: the surfaces that were read, the clock, and `missed`, which names what the page did\n" +
			"not give up.\n\n" +
			"Read only. " + strings.Join(notExposed, ", ") + " are not exposed.\n\n" +
			amz.NoRobotsNotice + "\n\n" +
			"It binds loopback by default and answers one call at a time, because two requests in flight\n" +
			"make --rate a lie by a factor of two.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := newServer(app)
			if showTools {
				return printTools(cmd, app, srv.Tools)
			}
			if err := checkBind(addr, app.Yes); err != nil {
				return err
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return exit(CodeRuntime, err)
			}
			defer func() { _ = ln.Close() }()

			h := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz serve %s on http://%s, %d read tools, marketplace %s\n",
				Version, ln.Addr(), len(srv.Tools), app.Marketplace)
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", amz.NoRobotsNotice)

			ctx := cmd.Context()
			errc := make(chan error, 1)
			go func() { errc <- h.Serve(ln) }()
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = h.Shutdown(shutdown)
				return nil
			case err := <-errc:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return exit(CodeRuntime, err)
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to bind")
	cmd.Flags().BoolVar(&showTools, "tools", false, "print the tool registry and exit")
	return cmd
}

// checkBind refuses a public bind without --yes.
//
// The server fetches from Amazon on request. Binding it to every interface makes
// anyone who can reach the port a crawler using this machine's address and this
// machine's reputation, which is a decision worth typing out.
func checkBind(addr string, confirmed bool) error {
	if confirmed {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return exit(CodeUsage, fmt.Errorf("--addr wants host:port, got %q", addr))
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return exit(CodeUsage, fmt.Errorf(
			"refusing to bind %q, which is every interface. anyone who reaches that port fetches from Amazon using this machine's address. pass --yes if that is what you want", addr))
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && !ip.IsLoopback() {
		return exit(CodeUsage, fmt.Errorf(
			"refusing to bind %q, which is not loopback. anyone who reaches that port fetches from Amazon using this machine's address. pass --yes if that is what you want", addr))
	}
	return nil
}

func mcpCmd(app *App) *cobra.Command {
	var showTools bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the read commands over stdio as Model Context Protocol",
		Long: "Serve the read commands to a model over stdio, as Model Context Protocol.\n\n" +
			"One registration, one process. Every result carries the envelope, including `missed`, because\n" +
			"a model that calls reviews and gets 8 rows needs to know there are 4,812.\n\n" +
			"Read only. " + strings.Join(notExposed, ", ") + " are not exposed.\n\n" +
			amz.NoRobotsNotice,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := newServer(app)
			if showTools {
				return printTools(cmd, app, srv.Tools)
			}
			m := &amz.MCP{Server: srv, In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Log: cmd.ErrOrStderr()}
			if err := m.Serve(cmd.Context()); err != nil && !errors.Is(err, context.Canceled) {
				return exit(CodeRuntime, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showTools, "tools", false, "print the tool registry and exit")
	return cmd
}

// printTools renders the registry through the ordinary output path, so
// `amz mcp --tools -o json` is a machine readable answer like every other
// command's.
func printTools(cmd *cobra.Command, app *App, tools []amz.Tool) error {
	out, err := app.Output()
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	for _, t := range tools {
		names := make([]string, 0, len(t.Args))
		for _, a := range t.Args {
			n := a.Name
			if a.Required {
				n += "*"
			}
			names = append(names, n)
		}
		if err := out.Emit(Row{
			Cols: []string{"tool", "args", "summary"},
			Vals: []string{t.Name, strings.Join(names, " "), t.Summary},
			Value: map[string]any{
				"tool": t.Name, "summary": t.Summary, "args": t.Args,
			},
		}); err != nil {
			return err
		}
	}
	return emitErr(out, nil)
}
