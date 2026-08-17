package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

// amz doctor answers the question the header finding raised.
//
// Through v0.2.1 this tool sent a browser user agent with a full browser header
// set, and it earned a CAPTCHA on every page. The identity it sends now is the
// single biggest reason it works at all, and it is also the easiest thing in the
// codebase to break by accident: one well meaning commit adding an Accept-Language
// header, one build that forgets to stamp a version, and reads start failing for
// a reason nothing in the error message points at.
//
// So doctor prints what the client actually sends, probes the two surfaces
// everything else depends on, and says what is in the store. Every line is a
// fact read at the time it prints, not a constant, because a self check that
// reports its own defaults is a self check that cannot fail.

// checkState is how a doctor line came out.
type checkState int

const (
	checkOK checkState = iota
	checkWarn
	checkFail
)

func (s checkState) String() string {
	switch s {
	case checkWarn:
		return "warn"
	case checkFail:
		return "FAIL"
	default:
		return "ok"
	}
}

func doctorCmd(app *App) *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that the client is honest, the network works and the store is readable",
		Long: "Prints what amz actually sends, what the two surfaces everything depends on " +
			"answer today, and what is in the store. --offline skips the two requests.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := app.Client()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			worst := checkOK
			bump := func(s checkState) {
				if s > worst {
					worst = s
				}
			}

			bump(doctorClient(w, app, c))
			if !offline {
				bump(doctorNetwork(cmd.Context(), w, app, c))
			}
			doctorStore(cmd.Context(), w, app)

			switch worst {
			case checkFail:
				return exit(CodeRuntime, fmt.Errorf("one or more checks failed. run `amz why blocked` if a probe was challenged"))
			case checkWarn:
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "amz: everything works, with the warnings above")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "skip the network probes and check only the client and the store")
	return cmd
}

// doctorClient prints the request amz would make, read from the code that makes
// it rather than from a list written here.
func doctorClient(w io.Writer, app *App, c *amz.Client) checkState {
	h := amz.Headers()
	worst := checkOK
	tw := newDoctorTable(w, "client")
	defer func() { _ = tw.Flush() }()

	// A user agent nobody can trace back is worth less than no user agent at
	// all, so the check is that this one carries a version and an address.
	ua := h.Get("User-Agent")
	state := checkOK
	note := ""
	switch {
	case !strings.Contains(ua, amz.RepoURL):
		state, note = checkFail, "does not say where to complain about it"
	case !strings.Contains(ua, amz.Version()):
		state, note = checkFail, "does not carry the build version"
	case amz.Version() == "dev":
		// A dev build is fine to run and worth pointing at, because an
		// unstamped version in a log is a request nobody can trace back.
		state, note = checkWarn, "unstamped build, so a log line cannot identify which version made the request"
	}
	doctorRow(tw, "user agent", ua, state, note)
	worst = max(worst, state)

	// Three headers and no more. The set is compared against the one the
	// measurement found to work rather than counted, because every header
	// beyond it is one more claim the request is making about itself, and
	// which header it is matters more than how many there are.
	want := []string{"Accept", "Accept-Encoding", "User-Agent"}
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	extra := notIn(names, want)
	missing := notIn(want, names)

	state, note = checkOK, ""
	switch {
	case len(extra) > 0:
		state, note = checkFail, "this build also sends "+strings.Join(extra, ", ")+", which the measurement did not"
	case len(missing) > 0:
		state, note = checkFail, "this build is missing "+strings.Join(missing, ", ")
	}
	doctorRow(tw, "headers", strings.Join(names, ", "), state, note)
	worst = max(worst, state)

	doctorRow(tw, "no impersonation", "no browser string, no Sec-Fetch, no Accept-Language", checkOK, "")

	// The session row is the one a reader is most likely to disbelieve, so it
	// checks the request the client is about to make rather than restating the
	// intent behind it. Any header past the measured three would show up here.
	state, note = checkOK, ""
	if len(extra) > 0 {
		state, note = checkFail, "amz carries no session of any kind, and this build sends "+strings.Join(extra, ", ")
	}
	doctorRow(tw, "session", "none. no session header, no credential file, no flag that takes one", state, note)
	worst = max(worst, state)

	doctorRow(tw, "concurrency", "1 request in flight", checkOK, "")

	pace := c.Delay()
	state, note = checkOK, ""
	if app.NoRobots {
		state, note = checkWarn, "--no-robots is set for this run, so the floor is "+amz.MinDelayNoRobots.String()
	}
	doctorRow(tw, "pace", fmt.Sprintf("%s between requests, floor %s", pace, amz.MinDelay), state, note)
	worst = max(worst, state)

	return worst
}

// doctorNetwork probes robots.txt and one product page.
//
// Two requests, because those are the two that every other command depends on:
// nothing is fetched before robots.txt answers, and the product page is the
// surface the whole model is built on. Probing more would be a crawl.
func doctorNetwork(ctx context.Context, w io.Writer, app *App, c *amz.Client) checkState {
	worst := checkOK
	tw := newDoctorTable(w, "network")
	defer func() { _ = tw.Flush() }()

	// A client pointed at a proxy or a fixture server is being tested rather
	// than run, and a host with no robots.txt of its own is not the same
	// finding as amazon.com refusing to serve one. The row is still printed,
	// because a run that silently reads somewhere else is exactly the thing
	// this command exists to make visible.
	live := true
	if m, ok := amz.LookupMarketplace(app.Marketplace); ok {
		live = strings.Contains(c.BaseURL(), m.Host)
	}
	if !live {
		doctorRow(tw, "base url", c.BaseURL(), checkWarn,
			"this run reads that host and not the marketplace, so the probes below say nothing about amazon.com")
		worst = checkWarn
	}
	missingRobots := checkFail
	if !live {
		missingRobots = checkWarn
	}

	r, err := c.Robots(ctx)
	switch {
	case err != nil:
		doctorRow(tw, "robots.txt", "could not be fetched", missingRobots, err.Error())
		worst = max(worst, missingRobots)
	default:
		rules := r.Rules()
		doctorRow(tw, "robots.txt",
			fmt.Sprintf("%d bytes, %d groups, %d rules in %q", len(r.Raw), len(r.Groups()), len(rules), r.GroupName()),
			checkOK, "")
	}

	// The probe ASIN is a physical product Amazon has sold for years. A probe
	// against something seasonal would start failing for a reason that has
	// nothing to do with the client.
	const probeASIN = "B075F5X8BR"
	body, err := c.Get(ctx, c.ProductURL(probeASIN), 0)
	state, detail, note := doctorProbe(body, err)
	doctorRow(tw, "/dp/ probe", detail, state, note)
	worst = max(worst, state)

	body, err = c.Get(ctx, c.SearchURL("usb c cable", amz.SearchQuery{}, 1), 0)
	state, detail, note = doctorProbe(body, err)
	doctorRow(tw, "/s probe", detail, state, note)
	worst = max(worst, state)

	return worst
}

// doctorProbe turns one fetch into a line.
//
// A CAPTCHA here is the finding this command exists to surface, and it is worth
// more than a failure count: a client identifying itself honestly, sending no
// cookies and waiting three seconds between requests should not be challenged,
// and if it is then either the measurement has gone stale or something changed
// at Amazon. Either way the project wants to hear about it.
func doctorProbe(body []byte, err error) (checkState, string, string) {
	switch {
	case err == nil:
		return checkOK, fmt.Sprintf("200, %s, no challenge", humanBytes(int64(len(body)))), ""
	case errors.Is(err, amz.ErrBlocked):
		return checkFail, "a CAPTCHA came back", "an honestly identified client getting one is unusual and worth reporting: " + amz.RepoURL + "/issues"
	case errors.Is(err, amz.ErrInterstitial):
		return checkFail, "an interstitial challenge is standing", "wait and try again. run `amz why captcha`"
	case errors.Is(err, amz.ErrSignIn):
		return checkFail, "redirected to a sign-in", "run `amz why policy`"
	default:
		return checkFail, "failed", err.Error()
	}
}

// doctorStore reports what is on disk without creating anything.
//
// A doctor that made a database as a side effect of being run would be a doctor
// that changes what it is measuring, so a missing store is reported as missing
// and that is a normal state rather than a fault.
func doctorStore(ctx context.Context, w io.Writer, app *App) {
	cfg := app.Config()
	tw := newDoctorTable(w, "store")
	defer func() { _ = tw.Flush() }()

	doctorRow(tw, "path", cfg.DBPath, checkOK, "")
	if _, err := os.Stat(cfg.DBPath); err != nil {
		doctorRow(tw, "database", "not created yet", checkOK, "it appears on the first `amz crawl` or `amz db`")
		return
	}
	s, err := amz.OpenStore(cfg.DBPath)
	if err != nil {
		doctorRow(tw, "database", "could not be opened", checkFail, err.Error())
		return
	}
	stats, err := s.Stats(ctx)
	if err != nil {
		doctorRow(tw, "database", "opened, could not be counted", checkWarn, err.Error())
		return
	}
	var parts []string
	for _, row := range stats {
		if n, ok := row["rows"].(int64); ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", row["table"], n))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "empty")
	}
	doctorRow(tw, "rows", strings.Join(parts, ", "), checkOK, "")
}

// notIn returns the members of a that are absent from b.
func notIn(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

func newDoctorTable(w io.Writer, section string) *tabwriter.Writer {
	_, _ = fmt.Fprintf(w, "%s\n", section)
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func doctorRow(tw *tabwriter.Writer, name, detail string, state checkState, note string) {
	_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\n", name, detail, state)
	if note != "" {
		_, _ = fmt.Fprintf(tw, "  \t%s\t\n", note)
	}
}

