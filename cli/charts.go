package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func chartCmd(app *App, kind amz.ChartKind, name, short string) *cobra.Command {
	var node string
	cmd := &cobra.Command{
		Use:   name + " [category]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			category := ""
			if len(args) == 1 {
				category = args[0]
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.ChartURL(kind, category, node, 1))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			ferr := c.FetchChart(cmd.Context(), kind, category, node, app.Limit, func(e amz.BestsellerEntry) error {
				return out.Emit(chartRow(e))
			})
			return emitErr(out, ferr)
		},
	}
	cmd.Flags().StringVar(&node, "node", "", "browse-node id override")
	return cmd
}
