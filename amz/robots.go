package amz

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// robots.txt is a surface, not a config file.
//
// amz fetches it per marketplace, parses it, and asks it before every request.
// There is no compiled-in fallback copy: a stale copy that says yes is worse
// than no answer at all, so an unfetchable robots.txt stops the run.
//
// See notes/Spec/3007/01_surfaces.md section 2 for the measured rule set.

// RobotsTTL is how long a fetched robots.txt is trusted on disk.
const RobotsTTL = 24 * time.Hour

// AgentToken is the product token amz matches robots.txt groups against. It is
// the part of the User-Agent before the slash.
//
// amz does not look for a group named after itself in order to route around one.
// If amazon.com ever adds `User-agent: amz-cli / Disallow: /`, that group applies
// and the default becomes "read nothing". Renaming the tool to fall back to `*`
// is the exact behaviour this package exists to not have.
const AgentToken = "amz-cli"

// ErrDisallowed is the sentinel behind a DisallowedError. It maps to exit 7.
var ErrDisallowed = errors.New("disallowed by robots.txt")

// ErrRobotsUnavailable means robots.txt could not be fetched or parsed. It maps
// to exit 8, and nothing is read.
//
// Treating a failed fetch as permission is the single worst thing a crawler can
// do, and it is a two-line mistake to make.
var ErrRobotsUnavailable = errors.New("robots.txt could not be fetched; refusing to guess")

// DisallowedError names the rule that refused a URL, so the message can be acted
// on rather than argued with.
type DisallowedError struct {
	URL   string
	Agent string
	Rule  Rule
}

func (e *DisallowedError) Error() string {
	return fmt.Sprintf("%s is disallowed: %q matches %q in the %q group (override with --no-robots)",
		e.URL, pathAndQuery(e.URL), e.Rule.String(), e.Agent)
}

func (e *DisallowedError) Is(target error) bool { return target == ErrDisallowed }

// Rule is one Allow or Disallow line.
type Rule struct {
	Allow   bool
	Pattern string
	Line    int
}

func (r Rule) String() string {
	if r.Pattern == "" {
		return ""
	}
	if r.Allow {
		return "Allow: " + r.Pattern
	}
	return "Disallow: " + r.Pattern
}

// Robots is one parsed robots.txt for one host.
type Robots struct {
	Host      string
	FetchedAt time.Time
	Raw       string

	groups map[string][]Rule // lowercased agent token -> its rules
	order  []string          // group names in file order, for reporting
}

// Groups returns the agent tokens the file defines, in file order.
func (r *Robots) Groups() []string { return r.order }

// Rules returns the rules of the group amz falls under.
func (r *Robots) Rules() []Rule { return r.groups[r.groupFor(AgentToken)] }

// GroupName returns the name of the group amz falls under, "*" for the fallback.
func (r *Robots) GroupName() string { return r.groupFor(AgentToken) }

// ParseRobots parses a robots.txt body.
//
// Directive names are matched case-insensitively, which is not pedantry:
// amazon.com's own file writes `User-Agent: PerplexityBot` on line 148 while the
// other hundred groups write `User-agent:`. A case-sensitive parser silently
// merges that group's rules into its predecessor.
func ParseRobots(host string, body []byte, fetchedAt time.Time) *Robots {
	r := &Robots{
		Host:      host,
		FetchedAt: fetchedAt,
		Raw:       string(body),
		groups:    map[string][]Rule{},
	}

	var current []string // agent tokens sharing the rule block being read
	newGroup := true     // a rule line ends the header run of a group

	for i, raw := range strings.Split(r.Raw, "\n") {
		line := raw
		if h := strings.IndexByte(line, '#'); h >= 0 {
			line = line[:h]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])

		switch key {
		case "user-agent":
			if !newGroup {
				current = nil
				newGroup = true
			}
			name := strings.ToLower(val)
			if name == "" {
				continue
			}
			current = append(current, name)
			if _, seen := r.groups[name]; !seen {
				r.groups[name] = nil
				r.order = append(r.order, name)
			}
		case "allow", "disallow":
			if len(current) == 0 {
				continue // a rule before any group header belongs to nobody
			}
			newGroup = false
			rule := Rule{Allow: key == "allow", Pattern: val, Line: i + 1}
			for _, name := range current {
				r.groups[name] = append(r.groups[name], rule)
			}
		}
	}
	return r
}

// groupFor picks the group that applies to an agent token.
//
// The longest matching group name wins and "*" is the fallback, which is the
// convention every robots.txt on the web is written against.
func (r *Robots) groupFor(agent string) string {
	agent = strings.ToLower(agent)
	best := ""
	for name := range r.groups {
		if name == "*" {
			continue
		}
		if strings.HasPrefix(agent, name) && len(name) > len(best) {
			best = name
		}
	}
	if best != "" {
		return best
	}
	if _, ok := r.groups["*"]; ok {
		return "*"
	}
	return ""
}

// Test reports whether a URL may be fetched, and names the rule that decided it.
//
// A zero Rule means no rule matched, which is an allow.
func (r *Robots) Test(rawURL string) (bool, Rule) {
	return r.TestAgent(AgentToken, rawURL)
}

// TestAgent is Test for an arbitrary agent token, which is what `amz robots
// check --agent` uses.
func (r *Robots) TestAgent(agent, rawURL string) (bool, Rule) {
	target := pathAndQuery(rawURL)
	var best Rule
	for _, rule := range r.groups[r.groupFor(agent)] {
		// An empty Disallow means "allow everything" and carries no pattern to
		// match, so it can never be the most specific rule. amazon.com's file
		// has none today; other marketplaces and other hosts do.
		if rule.Pattern == "" {
			continue
		}
		if !matchPattern(rule.Pattern, target) {
			continue
		}
		switch {
		case best.Pattern == "":
			best = rule
		case len(rule.Pattern) > len(best.Pattern):
			best = rule
		case len(rule.Pattern) == len(best.Pattern) && rule.Allow:
			// Allow beats Disallow at equal length.
			best = rule
		}
	}
	if best.Pattern == "" {
		return true, Rule{}
	}
	return best.Allow, best
}

// pathAndQuery returns what a pattern is matched against: the path plus the
// query string.
//
// Not the path alone. amazon.com disallows five browse nodes with rules like
// `/b?*node=7454917011`, which can only ever match inside a query string, and a
// path-only matcher gets all five wrong.
func pathAndQuery(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

// matchPattern implements the `*` and `$` wildcard grammar.
//
// A pattern without `$` is a prefix match. amazon.com uses both wildcards:
// `Allow: /-/en$` and `Disallow: /slp/*/b$` are real lines in the `*` group.
func matchPattern(pattern, target string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	if anchored {
		pattern = pattern[:len(pattern)-1]
	}
	parts := strings.Split(pattern, "*")

	// The first segment is anchored at the start.
	if !strings.HasPrefix(target, parts[0]) {
		return false
	}
	rest := target[len(parts[0]):]

	for i := 1; i < len(parts); i++ {
		seg := parts[i]
		if seg == "" {
			continue // a trailing or doubled * matches anything
		}
		if anchored && i == len(parts)-1 {
			// The final segment of an anchored pattern must land on the end.
			if !strings.HasSuffix(rest, seg) {
				return false
			}
			return true
		}
		idx := strings.Index(rest, seg)
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(seg):]
	}
	if anchored && !strings.HasSuffix(pattern, "*") {
		return rest == ""
	}
	return true
}

// robotsStore fetches and caches robots.txt per host.
type robotsStore struct {
	dir string

	mu  sync.Mutex
	mem map[string]*Robots
}

func newRobotsStore(cacheDir string) *robotsStore {
	return &robotsStore{dir: cacheDir, mem: map[string]*Robots{}}
}

func (s *robotsStore) diskPath(host string) string {
	if s.dir == "" {
		return ""
	}
	return filepath.Join(s.dir, "robots", host+".txt")
}

// get returns the robots.txt for a host, from memory, then from disk if it is
// fresher than RobotsTTL, then from the network.
//
// fetch is the raw HTTP read. It deliberately does not go through the robots
// gate, which would be a loop, and no robots.txt has ever disallowed itself.
func (s *robotsStore) get(ctx context.Context, origin, host string, fetch func(context.Context, string) ([]byte, error)) (*Robots, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r, ok := s.mem[host]; ok && time.Since(r.FetchedAt) < RobotsTTL {
		return r, nil
	}

	if p := s.diskPath(host); p != "" {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) < RobotsTTL {
			if b, err := os.ReadFile(p); err == nil {
				r := ParseRobots(host, b, fi.ModTime())
				s.mem[host] = r
				return r, nil
			}
		}
	}

	body, err := fetch(ctx, origin+"/robots.txt")
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRobotsUnavailable, host, err)
	}
	r := ParseRobots(host, body, time.Now())
	s.mem[host] = r
	if p := s.diskPath(host); p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
			_ = os.WriteFile(p, body, 0o644)
		}
	}
	return r, nil
}
