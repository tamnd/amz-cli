package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// treeCmd walks the browse node graph outward from one node.
//
// It is called tree because that is what people call it, and the record it
// emits is careful not to be one. A /b page links its children and its siblings
// with identical markup and states no parent at all, so the only honest claim is
// "node A links node B". Depth here is distance from the node given, not level
// in a hierarchy, and the same node reached two ways is emitted once with the
// shorter distance.
//
// This costs one request per node. --depth 2 on a broad category is hundreds of
// them, so the default is 1 and --dry-run prices the walk before it runs.
func treeCmd(app *App) *cobra.Command {
	var depth, maxNodes int
	var counts bool
	cmd := &cobra.Command{
		Use:   "tree [node_id|url]",
		Short: "Walk the browse node graph outward from a node",
		Long: "Walk the browse node graph outward from a node.\n\n" +
			"Amazon publishes no parent link and no breadcrumb on a browse page, so these are related nodes\n" +
			"rather than a hierarchy. Depth is distance from the starting node. Run `amz why browse-tree`.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			root := ""
			if len(args) == 1 {
				root = args[0]
			}
			if app.DryRun {
				w := cmd.OutOrStdout()
				_, _ = fmt.Fprintln(w, c.CategoryURL(root))
				_, _ = fmt.Fprintf(w, "depth %d, up to %d nodes, one request each\n", depth, maxNodes)
				_, _ = fmt.Fprintln(w, "nothing was read. drop --dry-run to run.")
				return nil
			}
			out, err := app.Output()
			if err != nil {
				return err
			}
			defer func() { _ = out.Close() }()

			seen := map[string]bool{}
			type visit struct {
				id    string
				depth int
				path  []string
			}
			queue := []visit{{id: root}}
			fetched, skipped := 0, 0

			for len(queue) > 0 {
				v := queue[0]
				queue = queue[1:]
				if fetched >= maxNodes {
					skipped++
					continue
				}
				cat, ferr := c.FetchCategory(cmd.Context(), v.id)
				if ferr != nil {
					// One dead node does not end the walk. A browse id that has
					// been retired 404s while its siblings are all live, and
					// stopping there would report the graph as smaller than it is.
					if !app.Quiet {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "amz: %s: %v\n", v.id, ferr)
					}
					continue
				}
				fetched++
				id := firstNonEmpty(cat.CanonicalNode, cat.NodeID, v.id)
				if seen[id] {
					continue
				}
				seen[id] = true
				if err := out.Emit(treeRow(cat, v.depth, v.path, counts)); err != nil {
					return err
				}
				if v.depth >= depth {
					continue
				}
				for _, r := range cat.Related {
					if r.ID == "" || seen[r.ID] {
						continue
					}
					queue = append(queue, visit{id: r.ID, depth: v.depth + 1, path: append(append([]string(nil), v.path...), cat.Name)})
				}
			}
			if !app.Quiet {
				w := cmd.ErrOrStderr()
				_, _ = fmt.Fprintf(w, "amz: %d %s read to depth %d\n", fetched, plural(fetched, "node", "nodes"), depth)
				if skipped > 0 {
					_, _ = fmt.Fprintf(w, "amz: %d %s left unread at the --max-nodes %d limit, so this walk is partial\n",
						skipped, plural(skipped, "node", "nodes"), maxNodes)
				}
			}
			return emitErr(out, nil)
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "how far from the starting node to walk. every level multiplies the requests")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 200, "stop after this many nodes")
	cmd.Flags().BoolVar(&counts, "counts", false, "include the shelf and ASIN counts for each node")
	return cmd
}

// treeRow is one node of the walk with its distance and the route taken to it.
//
// path is how the walk got here and not the node's ancestry, which is why the
// column is called via.
func treeRow(c amz.Category, depth int, path []string, counts bool) Row {
	v := map[string]any{
		"node_id": c.NodeID, "canonical_node": c.CanonicalNode, "name": c.Name,
		"slug": c.Slug, "depth": depth, "via": path, "related": len(c.Related), "url": c.URL,
	}
	cols := []string{"depth", "node_id", "name", "related", "via", "url"}
	vals := []string{itoa(depth), firstNonEmpty(c.CanonicalNode, c.NodeID), c.Name, itoa(len(c.Related)), strings.Join(path, " > "), c.URL}
	if counts {
		v["shelves"], v["top_asins"] = len(c.Shelves), len(c.TopASINs)
		cols = append(cols[:4:4], "shelves", "top_asins", "via", "url")
		vals = append(vals[:4:4], itoa(len(c.Shelves)), itoa(len(c.TopASINs)), strings.Join(path, " > "), c.URL)
	}
	return Row{Cols: cols, Vals: vals, Value: v, URL: c.URL}
}
