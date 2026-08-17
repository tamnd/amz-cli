package cli

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func openCmd(app *App) *cobra.Command {
	var reviews, printOnly bool
	cmd := &cobra.Command{
		Use:   "open <ASIN|query>",
		Short: "Open the relevant amazon.com page in a browser",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			var target string
			switch {
			case reviews:
				target = c.ReviewURL(asinArg(args[0]), amz.ReviewQuery{}, 1)
			case amz.ExtractASIN(args[0]) != "":
				if target, err = resolveURL(c, args[0]); err != nil {
					return err
				}
			default:
				target = c.BaseURL() + "/s?k=" + url.QueryEscape(joinArgs(args))
			}
			if printOnly || app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
				return nil
			}
			// Once. This used to call openBrowser twice, once for the exit code
			// and once for the effect, which opened two tabs.
			err = openBrowser(target)
			return exit(codeFor(err), err)
		},
	}
	cmd.Flags().BoolVar(&reviews, "reviews", false, "open the review page for an ASIN")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the URL instead of opening it")
	return cmd
}

func openBrowser(target string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, target)
	return exec.Command(cmd, args...).Start()
}

// asinCmd is the offline identity command: it reads ids and never fetches.
//
// Two behaviours, chosen by whether a format was named. With no -o it prints one
// bare ASIN per line, because that is what it has always done and what every
// shell pipeline built on it expects. Name a format and it emits the whole
// identity: what kind of id it is, which storefront the input pointed at, the
// ISBN-13 when the id is a book, and the amz: URI the rest of the tool files
// things under.
//
// Nothing here goes to the network, so it works on a plane and it is the fastest
// way to see what amz thinks a link is.
func asinCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "asin <ASIN|ISBN|url|amz: URI>...",
		Short: "Read Amazon ids out of URLs, ISBNs and amz: URIs, without fetching",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bare := Format(app.OutputFmt) == FormatAuto
			var out *Output
			if !bare {
				o, err := app.Output()
				if err != nil {
					return err
				}
				defer func() { _ = o.Close() }()
				out = o
			}
			found := false
			for _, a := range args {
				id, err := amz.ParseID(a)
				if err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %q is not an ASIN, an ISBN or an amazon.com product URL\n", a)
					continue
				}
				found = true
				if bare {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), id.Value)
					continue
				}
				if err := out.Emit(idRow(app, id)); err != nil {
					return err
				}
			}
			if !found {
				return exit(CodeNoData, fmt.Errorf("no ASIN found"))
			}
			return nil
		},
	}
}

func infoCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show access tiers, marketplace, and config summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Config()
			mkt, _ := amz.LookupMarketplace(app.Marketplace)
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "amz %s (%s)\n", Version, Commit)
			_, _ = fmt.Fprintf(w, "marketplace:  %s  %s  (%s, %s)\n", mkt.Slug, mkt.Host, mkt.Currency, mkt.Language)
			_, _ = fmt.Fprintf(w, "access tier:  %s\n", accessTier(app))
			_, _ = fmt.Fprintf(w, "rate limit:   %s between requests, %d retries\n", cfg.Delay, cfg.Retries)
			_, _ = fmt.Fprintf(w, "cache dir:    %s\n", cfg.CacheDir)
			_, _ = fmt.Fprintf(w, "data dir:     %s\n", cfg.DataDir)
			_, _ = fmt.Fprintf(w, "db path:      %s\n", cfg.DBPath)
			_, _ = fmt.Fprintf(w, "marketplaces: %s\n", marketplaceSlugs())
			_, _ = fmt.Fprintln(w, "etiquette:    public pages only; respect robots and ToS; this is a polite, rate-limited reader.")
			return nil
		},
	}
}

func accessTier(app *App) string {
	switch {
	case app.UseAPI:
		return "official PA-API 5.0 (--api)"
	default:
		return "public HTML (default)"
	}
}

func cacheCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clear the on-disk page cache",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show cache location and size",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := app.Config().CacheDir
			files, bytes := dirStats(dir)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cache dir: %s\nfiles:     %d\nsize:      %s\n", dir, files, humanBytes(bytes))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Delete every cached page",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := app.Config().CacheDir
			if err := removeContents(dir); err != nil {
				return exit(CodeRuntime, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared %s\n", dir)
			return nil
		},
	})
	return cmd
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
