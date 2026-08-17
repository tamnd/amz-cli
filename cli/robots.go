package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func robotsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "robots",
		Short: "Show the marketplace's robots.txt and what amz reads from it",
		Long: "amz fetches robots.txt per marketplace, caches it for 24 hours, and asks it before every request.\n" +
			"There is no compiled-in copy: a stale copy that says yes is worse than no answer.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			r, err := c.Robots(cmd.Context())
			if err != nil {
				return exit(codeFor(err), err)
			}
			w := cmd.OutOrStdout()
			if app.Raw {
				_, _ = fmt.Fprint(w, r.Raw)
				return nil
			}
			rules := r.Rules()
			allow, disallow := 0, 0
			for _, rule := range rules {
				if rule.Allow {
					allow++
				} else {
					disallow++
				}
			}
			_, _ = fmt.Fprintf(w, "host:      %s\n", r.Host)
			_, _ = fmt.Fprintf(w, "fetched:   %s (cached %s)\n", r.FetchedAt.Format("2006-01-02 15:04:05"), amz.RobotsTTL)
			_, _ = fmt.Fprintf(w, "agent:     %s\n", amz.UserAgent())
			_, _ = fmt.Fprintf(w, "group:     %q of %d\n", r.GroupName(), len(r.Groups()))
			_, _ = fmt.Fprintf(w, "rules:     %d disallow, %d allow\n", disallow, allow)
			_, _ = fmt.Fprintln(w)
			for _, rule := range rules {
				_, _ = fmt.Fprintf(w, "  %s\n", rule)
			}
			return nil
		},
	}
	cmd.AddCommand(robotsCheckCmd(app))
	return cmd
}

func robotsCheckCmd(app *App) *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "check <url|path>...",
		Short: "Ask robots.txt about a URL and print the rule that decided it",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			r, err := c.Robots(cmd.Context())
			if err != nil {
				return exit(codeFor(err), err)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			refused := 0
			for _, a := range args {
				target := a
				if strings.HasPrefix(target, "/") {
					target = c.BaseURL() + target
				}
				allowed, rule := r.TestAgent(agent, target)
				verdict := "allowed"
				if !allowed {
					verdict = "disallowed"
					refused++
				}
				reason := rule.String()
				if reason == "" {
					reason = "(no rule matches)"
				}
				op := amz.OpFor(target)
				surface := "-"
				if op != nil {
					surface = op.Name + " " + op.ID
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", verdict, target, reason, surface)
			}
			_ = tw.Flush()
			if refused > 0 {
				// Exit 7 so a script can branch on the answer without parsing the
				// table above.
				return exit(CodeDisallowed, fmt.Errorf("%d of %d disallowed", refused, len(args)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", amz.AgentToken, "test as another user agent token")
	return cmd
}

func surfacesCmd(app *App) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "surfaces",
		Short: "List every Amazon surface amz knows how to read",
		Long: "This is the Ops registry printed, not a hand-written list, so it cannot drift from the code.\n" +
			"The robots column is what the last measurement found. The live robots.txt decides at request time.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			if !app.NoHeader {
				_, _ = fmt.Fprintln(tw, "ID\tSURFACE\tPATH\tROBOTS\tLOGIN\tMEASURED\tFIELDS")
			}
			for _, op := range amz.Ops() {
				login := "-"
				if op.Login {
					login = "yes"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
					op.ID, op.Name, op.Path, op.Robots, login, op.Since, len(op.Fields))
			}
			_ = tw.Flush()
			if !verbose {
				return nil
			}
			_, _ = fmt.Fprintln(w)
			for _, op := range amz.Ops() {
				if op.Note == "" {
					continue
				}
				_, _ = fmt.Fprintf(w, "%-4s %-16s %s\n", op.ID, op.Name, op.Note)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&verbose, "notes", false, "print what was measured about each surface")
	return cmd
}
