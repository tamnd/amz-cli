package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func categoryCmd(app *App) *cobra.Command {
	var children, top bool
	cmd := &cobra.Command{
		Use:   "category <node_id|url>",
		Short: "Fetch a browse node: name, breadcrumb, children, top ASINs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.CategoryURL(args[0]))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			cat, ferr := c.FetchCategory(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			switch {
			case children:
				for _, n := range cat.ChildNodeIDs {
					out.Emit(stringRow("child_node_id", n))
				}
			case top:
				for _, a := range cat.TopASINs {
					out.Emit(Row{Cols: []string{"asin"}, Vals: []string{a}, Value: map[string]string{"asin": a}, URL: c.ProductURL(a)})
				}
			default:
				out.Emit(categoryRow(cat))
			}
			return emitErr(out, nil)
		},
	}
	cmd.Flags().BoolVar(&children, "children", false, "list child node ids instead of the record")
	cmd.Flags().BoolVar(&top, "top", false, "list top ASINs instead of the record")
	return cmd
}

func brandCmd(app *App) *cobra.Command {
	var featured bool
	cmd := &cobra.Command{
		Use:   "brand <slug|url>",
		Short: "Fetch a brand storefront",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.BrandURL(args[0]))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			b, ferr := c.FetchBrand(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			if featured {
				for _, a := range b.FeaturedASINs {
					out.Emit(Row{Cols: []string{"asin"}, Vals: []string{a}, Value: map[string]string{"asin": a}, URL: c.ProductURL(a)})
				}
			} else {
				out.Emit(brandRow(b))
			}
			return emitErr(out, nil)
		},
	}
	cmd.Flags().BoolVar(&featured, "featured", false, "list featured ASINs instead of the record")
	return cmd
}

func sellerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seller <id|url>",
		Short: "Fetch a third-party seller profile and rating breakdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.SellerURL(args[0]))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			s, ferr := c.FetchSeller(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			out.Emit(sellerRow(s))
			return emitErr(out, nil)
		},
	}
	return cmd
}

func authorCmd(app *App) *cobra.Command {
	var books bool
	cmd := &cobra.Command{
		Use:   "author <slug|url>",
		Short: "Fetch an Author Central page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.AuthorURL(args[0]))
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			a, ferr := c.FetchAuthor(cmd.Context(), args[0])
			if ferr != nil {
				return exit(codeFor(ferr), ferr)
			}
			if books {
				for _, x := range a.BookASINs {
					out.Emit(Row{Cols: []string{"asin"}, Vals: []string{x}, Value: map[string]string{"asin": x}, URL: c.ProductURL(x)})
				}
			} else {
				out.Emit(authorRow(a))
			}
			return emitErr(out, nil)
		},
	}
	cmd.Flags().BoolVar(&books, "books", false, "list the author's book ASINs instead of the record")
	return cmd
}

func dealsCmd(app *App) *cobra.Command {
	var minDiscount int
	var department string
	cmd := &cobra.Command{
		Use:   "deals",
		Short: "Stream today's deals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			if app.DryRun {
				fmt.Fprintln(cmd.OutOrStdout(), c.DealsURL())
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer out.Close()
			ferr := c.FetchDeals(cmd.Context(), app.Limit, func(d amz.Deal) error {
				if minDiscount > 0 && d.DiscountPct < minDiscount {
					return nil
				}
				return out.Emit(dealRow(d))
			})
			_ = department
			return emitErr(out, ferr)
		},
	}
	cmd.Flags().IntVar(&minDiscount, "min-discount", 0, "minimum discount percent")
	cmd.Flags().StringVar(&department, "department", "", "limit to a department")
	return cmd
}
