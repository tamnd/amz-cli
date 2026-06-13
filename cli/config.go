package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func configCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage configuration (incl. PA-API credentials)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file location",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), configFile(app))
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print the resolved configuration",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg := app.Config()
				w := cmd.OutOrStdout()
				fmt.Fprintf(w, "marketplace      = %s\n", cfg.Marketplace)
				fmt.Fprintf(w, "rate             = %s\n", cfg.Delay)
				fmt.Fprintf(w, "retries          = %d\n", cfg.Retries)
				fmt.Fprintf(w, "timeout          = %s\n", cfg.Timeout)
				fmt.Fprintf(w, "workers          = %d\n", cfg.Workers)
				fmt.Fprintf(w, "data_dir         = %s\n", cfg.DataDir)
				fmt.Fprintf(w, "cache_dir        = %s\n", cfg.CacheDir)
				fmt.Fprintf(w, "db_path          = %s\n", cfg.DBPath)
				fmt.Fprintf(w, "paapi_host       = %s\n", cfg.PAAPIHost)
				fmt.Fprintf(w, "paapi_region     = %s\n", cfg.PAAPIRegion)
				fmt.Fprintf(w, "paapi_access_key = %s\n", masked(cfg.PAAPIAccessKey))
				fmt.Fprintf(w, "paapi_partner    = %s\n", cfg.PAAPIPartnerTag)
				return nil
			},
		},
		&cobra.Command{
			Use:   "init",
			Short: "Write a starter config file",
			RunE: func(cmd *cobra.Command, args []string) error {
				path := configFile(app)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return exit(CodeRuntime, err)
				}
				if _, err := os.Stat(path); err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "config already exists: %s\n", path)
					return nil
				}
				if err := os.WriteFile(path, []byte(starterConfig), 0o644); err != nil {
					return exit(CodeRuntime, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
				return nil
			},
		},
	)
	return cmd
}

func configFile(app *App) string {
	if app.ConfigPath != "" {
		return app.ConfigPath
	}
	return filepath.Join(amz.ConfigDir(), "config.toml")
}

func masked(s string) string {
	if s == "" {
		return "(unset)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

const starterConfig = `# amz configuration
marketplace = "us"
rate = "3s"
retries = 3

# Official Product Advertising API (opt-in). Prefer environment variables:
#   AMZ_PAAPI_ACCESS_KEY, AMZ_PAAPI_SECRET_KEY, AMZ_PAAPI_PARTNER_TAG
# [paapi]
# access_key = ""
# secret_key = ""
# partner_tag = ""
`
