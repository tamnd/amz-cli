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
			case amz.ExtractASIN(args[0]) != "" || isBareASIN(args[0]):
				target = resolveURL(c, args[0])
			default:
				target = c.BaseURL() + "/s?k=" + url.QueryEscape(joinArgs(args))
			}
			if printOnly || app.DryRun {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
				return nil
			}
			return exit(codeFor(openBrowser(target)), openBrowser(target))
		},
	}
	cmd.Flags().BoolVar(&reviews, "reviews", false, "open the review page for an ASIN")
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the URL instead of opening it")
	return cmd
}

func isBareASIN(s string) bool {
	if len(s) != 10 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
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

func asinCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "asin <url>...",
		Short: "Extract the ASIN from any Amazon URL",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			found := false
			for _, a := range args {
				asin := amz.ExtractASIN(a)
				if asin == "" && isBareASIN(a) {
					asin = a
				}
				if asin == "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: no ASIN in %q\n", a)
					continue
				}
				found = true
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), asin)
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
	case app.Cookies != "":
		return "cookied HTML session (--cookies)"
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
