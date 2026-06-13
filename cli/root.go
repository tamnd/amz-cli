// Package cli is the cobra command tree for the amz CLI.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// Version metadata, overridable at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Exit codes (mirrors spec §6).
const (
	CodeOK      = 0
	CodeRuntime = 1
	CodeUsage   = 2
	CodeNoData  = 3
	CodePartial = 4
	CodeBlocked = 5
)

// ExitError carries a specific process exit code out of a command.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

func exit(code int, err error) error { return &ExitError{Code: code, Err: err} }

// codeFor maps a library error to its process exit code.
func codeFor(err error) int {
	var ee *ExitError
	switch {
	case err == nil:
		return CodeOK
	case errors.As(err, &ee):
		return ee.Code
	case errors.Is(err, amz.ErrBlocked):
		return CodeBlocked
	case errors.Is(err, amz.ErrNotFound):
		return CodeNoData
	default:
		return CodeRuntime
	}
}

// App holds the resolved global flags shared by every command.
type App struct {
	Marketplace string
	OutputFmt   string
	Fields      string
	Limit       int
	DataDir     string
	Workers     int
	Rate        time.Duration
	Retries     int
	Timeout     time.Duration
	Cookies     string
	UseAPI      bool
	Quiet       bool
	Verbose     bool
	Color       string
	NoCache     bool
	Refresh     bool
	DryRun      bool
	Raw         bool
	OutFile     string
	NoHeader    bool
	Template    string
	ConfigPath  string

	// Out is where rendered records go (cobra's stdout, or a file for -O).
	Out io.Writer
}

// Config builds an amz.Config from the resolved global flags.
func (a *App) Config() amz.Config {
	cfg := amz.DefaultConfig()
	cfg.Marketplace = a.Marketplace
	cfg.Workers = a.Workers
	cfg.Delay = a.Rate
	cfg.Retries = a.Retries
	cfg.Timeout = a.Timeout
	cfg.Cookies = a.Cookies
	cfg.UseAPI = a.UseAPI
	cfg.NoCache = a.NoCache
	cfg.Refresh = a.Refresh
	if a.DataDir != "" {
		cfg.DataDir = a.DataDir
		cfg.CacheDir = a.DataDir + "/cache"
		cfg.DBPath = a.DataDir + "/amz.duckdb"
	}
	return cfg
}

// Client builds a polite, block-aware client for the resolved marketplace.
func (a *App) Client() (*amz.Client, error) {
	if _, ok := amz.LookupMarketplace(a.Marketplace); !ok {
		return nil, exit(CodeUsage, fmt.Errorf("unknown marketplace %q (try: %s)", a.Marketplace, marketplaceSlugs()))
	}
	c := amz.NewClient(a.Config())
	if base := os.Getenv("AMZ_BASE_URL"); base != "" {
		c.SetBaseURL(base)
	}
	return c, nil
}

// resolveURL returns the product URL for an ASIN or URL argument.
func resolveURL(c *amz.Client, asinOrURL string) string {
	_, url := c.ResolveProductURL(asinOrURL)
	return url
}

func marketplaceSlugs() string {
	var slugs []string
	for _, m := range amz.Marketplaces() {
		slugs = append(slugs, m.Slug)
	}
	return strings.Join(slugs, ", ")
}

// stdoutTTY reports whether stdout is an interactive terminal.
func stdoutTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// Output builds the output renderer for this run.
func (a *App) Output() (*Output, error) {
	var w io.Writer = os.Stdout
	if a.Out != nil {
		w = a.Out
	}
	isTTY := stdoutTTY() && a.Out == nil
	if a.OutFile != "" {
		f, err := os.Create(a.OutFile)
		if err != nil {
			return nil, exit(CodeRuntime, err)
		}
		w = f
		isTTY = false
	}
	var fields []string
	if a.Fields != "" {
		fields = strings.Split(a.Fields, ",")
	}
	format := Format(a.OutputFmt)
	if a.Raw {
		format = FormatRaw
	}
	return NewOutput(w, format, isTTY, fields, a.NoHeader, a.Template)
}

// emitErr converts a "produced nothing" situation into the right exit code.
func emitErr(out *Output, fetchErr error) error {
	if fetchErr != nil {
		return exit(codeFor(fetchErr), fetchErr)
	}
	if out.Count() == 0 {
		return exit(CodeNoData, errors.New("no results"))
	}
	return nil
}

// Root builds the full command tree.
func Root() *cobra.Command {
	app := &App{}
	root := &cobra.Command{
		Use:           "amz",
		Short:         "A delightful CLI for Amazon.com",
		Long:          "amz fetches every public Amazon surface (products, search, reviews, Q&A, offers, charts, categories, brands, sellers, authors, deals) and normalizes it into rich, structured data.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			app.Out = cmd.OutOrStdout()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&app.Marketplace, "marketplace", "m", "us", "marketplace slug (us|uk|de|fr|jp|ca|in|it|es|...)")
	pf.StringVarP(&app.OutputFmt, "output", "o", "auto", "output format: table|json|jsonl|csv|tsv|url|raw")
	pf.StringVar(&app.Fields, "fields", "", "comma-separated columns to show")
	pf.IntVarP(&app.Limit, "limit", "n", 0, "cap results (0 = unlimited)")
	pf.StringVar(&app.DataDir, "data-dir", "", "root cache/data dir (default: XDG)")
	pf.IntVarP(&app.Workers, "workers", "j", amz.DefaultWorkers, "concurrency for multi-page/bulk")
	pf.DurationVar(&app.Rate, "rate", amz.DefaultDelay, "min delay between requests")
	pf.IntVar(&app.Retries, "retries", amz.DefaultRetries, "retry attempts on 429/503")
	pf.DurationVar(&app.Timeout, "timeout", amz.DefaultTimeout, "per-request timeout")
	pf.StringVar(&app.Cookies, "cookies", "", "cookie file to lend a signed-in session")
	pf.BoolVar(&app.UseAPI, "api", false, "use the official PA-API path (needs credentials)")
	pf.BoolVarP(&app.Quiet, "quiet", "q", false, "quiet output")
	pf.BoolVarP(&app.Verbose, "verbose", "v", false, "verbose output")
	pf.StringVar(&app.Color, "color", "auto", "color: auto|always|never")
	pf.BoolVar(&app.NoCache, "no-cache", false, "bypass the on-disk cache")
	pf.BoolVar(&app.Refresh, "refresh", false, "ignore cached copy but repopulate it")
	pf.BoolVar(&app.DryRun, "dry-run", false, "print the URL(s) that would be fetched, then stop")
	pf.BoolVar(&app.Raw, "raw", false, "emit the underlying HTML/JSON instead of a parsed record")
	pf.StringVarP(&app.OutFile, "out", "O", "", "write output to a file")
	pf.BoolVar(&app.NoHeader, "no-header", false, "omit the table/CSV header row")
	pf.StringVar(&app.Template, "template", "", "Go text/template applied per row")
	pf.StringVar(&app.ConfigPath, "config", "", "config file (default: XDG config)")

	root.AddCommand(
		productCmd(app),
		searchCmd(app),
		reviewsCmd(app),
		qaCmd(app),
		offersCmd(app),
		chartCmd(app, amz.ChartBestsellers, "bestsellers", "Top sellers in the store or a category"),
		chartCmd(app, amz.ChartNewReleases, "new-releases", "Newest releases in the store or a category"),
		chartCmd(app, amz.ChartMovers, "movers", "Biggest 24h rank movers"),
		chartCmd(app, amz.ChartWished, "wished", "Most wished-for items"),
		chartCmd(app, amz.ChartGifted, "gifted", "Most gifted items"),
		categoryCmd(app),
		brandCmd(app),
		sellerCmd(app),
		authorCmd(app),
		dealsCmd(app),
		relatedCmd(app),
		priceCmd(app),
		openCmd(app),
		seedCmd(app),
		crawlCmd(app),
		dbCmd(app),
		configCmd(app),
		cacheCmd(app),
		infoCmd(app),
		asinCmd(app),
	)
	return root
}
