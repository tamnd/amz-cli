package cli

import (
	"encoding/json"
	"fmt"
	"os"

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
		Short: "Inspect the optional local DuckDB store",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the database file location",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), app.Config().DBPath)
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
					fmt.Fprintf(cmd.OutOrStdout(), "%-14s %v\n", r["table"], r["rows"])
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "query <sql>",
			Short: "Run a read-only SQL query and print JSON rows",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := openStore(app)
				if err != nil {
					return err
				}
				rows, err := s.Query(cmd.Context(), args[0])
				if err != nil {
					return exit(CodeRuntime, err)
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				for _, r := range rows {
					enc.Encode(r)
				}
				return nil
			},
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
				fmt.Fprintln(cmd.OutOrStdout(), "ok")
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
				fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
				return nil
			},
		},
	)
	return cmd
}
