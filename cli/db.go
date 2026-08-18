package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func openStore(app *App) (*amz.Store, error) {
	s, err := amz.OpenStore(app.Config().DBPath)
	if err != nil {
		return nil, exit(CodeRuntime, err)
	}
	return s, nil
}

func dbCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Inspect and maintain the local store",
		Long: strings.TrimSpace(`
The store's own housekeeping: where it is, what is in it, compact it, delete it.

The store is SQLite through modernc.org/sqlite, which is a Go implementation and
not a binding, so there is nothing to install and nothing to be missing. Through
v0.2.1 this was DuckDB driven by shelling out to a duckdb executable, which meant
` + "`amz db query`" + ` failed on a machine that had amz and nothing else.

Reading the data is ` + "`amz query`" + `, ` + "`amz find`" + `, ` + "`amz lookup`" + ` and ` + "`amz series`" + `.`),
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the database file location",
			RunE: func(cmd *cobra.Command, args []string) error {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), app.Config().DBPath)
				return nil
			},
		},
		&cobra.Command{
			Use:   "stats",
			Short: "Row counts per table",
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := openStore(app)
				if err != nil {
					return err
				}
				rows, err := s.Stats(cmd.Context())
				if err != nil {
					return exit(CodeRuntime, err)
				}
				for _, r := range rows {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-14s %v\n", r["table"], r["rows"])
				}
				return nil
			},
		},
		// `amz db query` is `amz query` under its v0.2 name. It is kept as an
		// alias rather than removed, because it is in people's shell history and
		// a command that vanishes teaches less than one that says where it went.
		&cobra.Command{
			Use:        "query <sql>",
			Short:      "Run a read-only SQL query and print JSON rows",
			Deprecated: "use `amz query`, which is the same command one word shorter",
			Args:       cobra.ExactArgs(1),
			RunE:       queryCmd(app).RunE,
		},
		&cobra.Command{
			Use:   "vacuum",
			Short: "Compact the database",
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := openStore(app)
				if err != nil {
					return err
				}
				if err := s.Vacuum(cmd.Context()); err != nil {
					return exit(CodeRuntime, err)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ok")
				return nil
			},
		},
		&cobra.Command{
			Use:   "reset",
			Short: "Delete the database file",
			RunE: func(cmd *cobra.Command, args []string) error {
				path := app.Config().DBPath
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return exit(CodeRuntime, err)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
				return nil
			},
		},
	)
	return cmd
}
