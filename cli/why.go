package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// amz why exists because four of this tool's headline features got smaller in
// v0.3.0 for reasons that are Amazon's and not the tool's.
//
// A user who upgrades, runs amz reviews and gets thirteen rows where they used
// to get four thousand deserves a real answer rather than a changelog entry, and
// the answer has to be checkable. So every topic below carries the request that
// was made, what came back, and the date it was measured, in a form somebody can
// paste into curl and see for themselves.
//
// These are facts with dates on them, not opinions. When one of them stops being
// true the fix is to measure it again and change the text, and the date is what
// tells a reader how much to trust it in the meantime.

// whyTopic is one explanation.
type whyTopic struct {
	Name string
	// Headline is the one line answer, printed next to the topic name. It says
	// what happens, not what Amazon did, because that is the user's question.
	Headline string
	// Body is printed verbatim under a two space indent, so a console block
	// inside it keeps its own alignment.
	Body string
	// Do is what the user can actually run. A topic with nothing here is a topic
	// that ends in a shrug, which is the thing this command exists to avoid.
	Do []string
}

// measured is the date every topic in this file was checked against
// www.amazon.com with an honest user agent and a cold cookie jar.
const measured = "2026-08-17"

var whyTopics = []whyTopic{{
	Name:     "reviews",
	Headline: "amz returns the reviews the product page carries and cannot get the rest.",
	Body: `the product page embeds full reviews and the 5 bucket histogram, and amz
reads all of it. on B075F5X8BR that is 13 reviews with ids, ratings, titles,
bodies, dates and countries, 8 written in this marketplace and 5 translated
in from other Amazon storefronts.

the full corpus lives at two paths and both of them redirect to a sign-in:

  /product-reviews/<asin>            302 -> /ap/signin
  /portal/customer-reviews/<asin>    302 -> /ap/signin

measured ` + measured + `. neither is disallowed by robots.txt. this is a login
wall, not a crawling rule, and amz does not carry a session or go past
access controls. see ` + "`amz why policy`" + `.

the count in the record is what was read and the count next to it is the
ratings count, which is the largest number the page states. rating something
and writing about it are different acts, so that total is an upper bound on
the reviews and not the number of them.`,
	Do: []string{
		"amz product <asin>            the reviews and the distribution, one request",
		"amz reviews <asin>            the same reviews as rows",
		"amz reviews <asin> --deep     attempt the corpus and watch it fail",
	},
}, {
	Name:     "qa",
	Headline: "amz returns the answered question count and any pairs the page carries.",
	Body: `the ask region on the product page states how many questions have been
answered and carries the pairs themselves only on some products. none of the
six pages in the test corpus carries any, so the count is usually the whole
of it.

the standalone page redirects to a sign-in:

  /ask/questions/asin/<asin>    302 -> /ap/signin?...&openid.assoc_handle=amzn_ask_us

measured ` + measured + `. the assoc_handle names the Q&A service, so this is that
service asking for a sign-in rather than a generic redirect.

v0.2.1 said "no Q&A section on this product (Amazon has removed it for many
items)", which was a guess and was wrong. the section is there and the
answers behind it need a login.`,
	Do: []string{
		"amz qa <asin>          the count, and the pairs when there are any",
		"amz product <asin>     the same count in the questions connection",
	},
}, {
	Name:     "offers",
	Headline: "amz returns the buy box winner and the count of the other offers.",
	Body: `the all-offers panel is drawn by javascript. the endpoint its own page
calls does not answer a direct request, in either of the two forms it takes:

  /gp/aod/ajax?asin=<asin>&pc=dp                        404
  /gp/aod/ajax/ref=dp_aod_NEW_mbc?asin=<asin>&pc=dp     404

the old offer listing page is disallowed by robots.txt and redirects to the
product page regardless:

  /gp/offer-listing/<asin>    301 -> /dp/<asin>/ref=olp-opf-redir?aod=1

measured ` + measured + `. the redirect is Amazon saying the offers are on the
product page now, and they are: the buy box, the seller, the fulfiller, the
condition and the delivery promise all read cleanly from it.

what is not on the page is the other sellers' rows. the ingress link states
how many there are, which is why the record says 1 of 22 rather than 1.

no flag makes this better, because there is nothing to read.`,
	Do: []string{
		"amz offers <asin>      the buy box row and the count of the rest",
		"amz product <asin>     the same offer inside the record",
	},
}, {
	Name:     "blocked",
	Headline: "exit 5 means a CAPTCHA came back, and it is worth reporting.",
	Body: `amz identifies itself honestly, sends no cookies, makes one request at a
time and waits 3 seconds between them. a client behaving that way getting a
CAPTCHA is unusual, and the useful response is to stop rather than to try
harder.

amz does not solve, bypass or wait out a challenge. an interstitial is
exit 6 and a CAPTCHA is exit 5, and they are separate codes because the
right response differs: 6 is worth retrying later and 5 is worth reporting.

if you see exit 5 from a normal read, please open an issue with the URL and
the date. it is the kind of thing this project wants to know about.`,
	Do: []string{
		"amz doctor                                     probe the surfaces and check the client",
		"https://github.com/tamnd/amz-cli/issues        report a CAPTCHA on an honest read",
	},
}, {
	Name:     "captcha",
	Headline: "a challenge page is a stop sign, and amz stops.",
	Body: `Amazon serves two shapes of challenge and amz tells them apart. one is a
CAPTCHA form asking a person to type characters from an image, which is
exit 5. the other is an Akamai interstitial that sets a bm-verify token by
running javascript, which is exit 6.

neither is defeated, worked around or retried in a tighter loop. an access
control is a statement about who is allowed to read a page, and going past
one is not a technical problem to be solved. see ` + "`amz why policy`" + `.

waiting and trying again later is the correct response to exit 6. exit 5
should be rare enough to be a bug report.`,
	Do: []string{
		"amz doctor                       check whether a plain read is being challenged",
		"amz --rate 10s <command>         slow down further before retrying",
	},
}, {
	Name:     "robots",
	Headline: "exit 7 means robots.txt disallows the path you asked for.",
	Body: `amz fetches robots.txt per marketplace, parses it at request time, caches
it 24 hours and refetches. there is no compiled in copy, because a stale
copy that says yes is worse than no answer at all. a fetch failure is exit 8
and nothing is attempted.

www.amazon.com/robots.txt is 436 lines and 7,887 bytes, with 101 user agent
lines forming 100 groups, measured ` + measured + `. amz sends a token that
matches none of them, so it reads under ` + "`User-agent: *`" + ` and honours its 118
disallow and 17 allow rules, longest match winning and allow winning ties.

--no-robots overrides this for one run. it is a flag and only a flag: not a
config key, not an environment variable, not available over MCP. it prints
every rule it breaks and it does not lower the pace floor.`,
	Do: []string{
		"amz robots                     print the rules that apply to amz",
		"amz robots check <url>         answer for one URL, with the rule that decided it",
		"amz surfaces                   every surface and its robots status",
	},
}, {
	Name:     "policy",
	Headline: "amz reads what a signed out browser can read, and stops where it stops.",
	Body: `three rules, written into the tests rather than into a readme.

an access control is a stop sign. a login wall, a CAPTCHA and an
interstitial each end the read and name the code. amz carries no session,
accepts no cookie file, and the flag that used to supply one is gone.

robots.txt is obeyed as fetched. it is never hardcoded, never assumed, and
the only override is --no-robots, which is explicit, loud, and lasts one
run.

the tool does not rename itself to escape a rule. if amz-cli ever appears in
a named group with Disallow: /, amz obeys it and the default becomes reading
nothing. written ` + measured + `.

the point of all three is that a record from amz should be a record a person
could have collected by hand, more patiently.`,
	Do: []string{
		"amz robots               the rules as fetched today",
		"amz doctor               what the client actually sends",
	},
}, {
	Name:     "sitemap",
	Headline: "there is no sitemap, so there is no enumeration.",
	Body: `robots.txt carries no Sitemap directive in its 436 lines, and both
conventional paths answer with Amazon's error page rather than a 404:

  /sitemap.xml     500
  /sitemaps.xml    500

measured ` + measured + `. a 500 where a 404 belongs is its own small signal that
nothing was ever served there.

so every ASIN comes from a chart, a browse node, a search, a rail on a
product page, or a seed you supply. the rails are the cheap one: a page
fetched for its own sake hands back 20 to 60 related ASINs at no extra
request, which makes a crawl that starts from one chart and follows rails
the least expensive way to cover a category.

amz crawl --from-sitemap is a usage error with a one line explanation,
because somebody will try it.`,
	Do: []string{
		"amz bestsellers <category>          50 per page, 100 per chart",
		"amz related <asin>                  the rails off a page you already have",
		"amz crawl --chart bestsellers --dry-run    price a walk before running it",
	},
}, {
	Name:     "brand",
	Headline: "a brand storefront is a javascript application, so amz returns the brand and not its catalogue.",
	Body: `/stores/<name>/page/<uuid> answers 200 with 128 KB of server rendered
HTML carrying the brand name, the logo and the navigation. the product grid
is drawn by javascript and is not in that HTML.

the uuid is the id and the name in the path is decoration. a wrong uuid does
not 404, it redirects into a search:

  /stores/Skullcandy/page/<wrong-uuid>
    302 -> /s/ref=ams_pages?url=search-alias%3Daps&field-keywords=Skullcandy

measured ` + measured + `. v0.2.1 follows that redirect and returns the search
results as if they were the brand's catalogue, which is worse than returning
nothing. amz treats it as not found.

the short path has no uuid on it and 404s outright:

  /stores/anker                  404, 1,147 bytes

so a bare name has to be resolved. nothing derives the uuid from the name,
and the only public page that states it is the byline on a product the brand
sells. amz searches the name, opens up to three organic results and follows
the byline link on the first one whose brand is the brand asked for. that is
four requests, and a slug that already carries the uuid costs one.

the brand's products are readable, just not from the storefront.`,
	Do: []string{
		"amz brand <name>               resolved through a product byline, four requests",
		"amz brand <slug|uuid|url>      the brand record, one request",
		"amz search --brand <name>      the products, which is the route that works",
	},
}, {
	Name:     "author",
	Headline: "an author page carries the biography and not the bibliography.",
	Body: `Author Central pages moved under /stores/, so they are the same kind of
javascript application as a brand storefront. /stores/author/<id> answers
200 with 263 KB, and the biography and follow count are in the HTML.

the books are not:

  $ grep -c 'data-asin' author.html
  0

zero ASINs on a 263 KB page, measured ` + measured + `. so amz returns the author
record and says in missed that the works list needs another route, rather
than returning an author with an empty book list and no explanation, which
is what v0.2.1 does.`,
	Do: []string{
		"amz author <id|url>            the author record",
		"amz search --author <name>     the books",
	},
}, {
	Name:     "search-depth",
	Headline: "every search stops at 306 results, whatever total it prints.",
	Body: `a search says "over 40,000 results" and serves 20 pages of about 16 each.
that is 306 results and it is the same 306 whether the corpus is forty
thousand or four hundred. the total is Amazon's number for how many things
match, not an offer to show them to you.

measured ` + measured + `. page 20 is the last real page. page 21 returns six filler
cards, no result strip, and a range that reads "321-306", which is the page
telling you it has gone past its own end. amz treats an inverted range as
terminal and stops there rather than emitting six cards that look like data.

so paging is not the way through and narrowing is. each refinement gets its
own 306, which is why --all splits the query into one search per value of a
refinement group and unions the results on ASIN. that is expensive and
--dry-run prices it before anything is fetched.`,
	Do: []string{
		"amz refine <query>                     every refinement group this query offers",
		"amz search <query> --refine p_123=id   narrow at the source",
		"amz search <query> --all --dry-run     what a partitioned run would cost",
		"amz search <query> --all               the union, past the ceiling",
	},
}, {
	Name:     "refinements",
	Headline: "the filter codes are per query, so amz reads them instead of shipping a table.",
	Body: `rh terms look global and almost none of them are. p_123 is brand and p_6 is
seller everywhere, but p_n_feature_thirteen_browse-bin means one thing under
laptops and something else under coffee, and p_n_g-1003532609111 is Key Count
on a keyboard search and does not exist anywhere else.

six codes are compiled into this binary and every other code amz sends came
off the page it is about to filter.

that matters because Amazon does not reject an rh term it does not
understand. it drops the term and returns the unfiltered result set with a
200 and a full grid, so a wrong code gives you a search that looks filtered
and is not. v0.2.1 sent p_72:1248882011 for four stars and up where this
marketplace offers 1248879011, measured ` + measured + `, and every "4 stars and up"
search it ever ran was unfiltered.

so amz resolves --brand, --seller, --stars and --condition against the
sidebar of the query you asked about, and after the filtered page comes back
it checks that the sidebar marks each term applied. if it does not, the run
fails rather than handing you rows.`,
	Do: []string{
		"amz refine <query>                          the groups this query offers",
		"amz refine <query> --group p_123            the values of one group",
		"amz search <query> --refine p_123=213704    filter by an id you read",
		"amz search <query> --brand Logitech         resolve the id, one extra request",
	},
}, {
	Name:     "browse-tree",
	Headline: "a browse page links its neighbours and never says which are children.",
	Body: `amz tree walks browse nodes outward from one node. it is not a hierarchy
and the record does not claim to be.

a /b page carries no breadcrumb, no a-breadcrumb region and no subnav
element, measured ` + measured + ` on both browse captures. children and siblings are
linked with identical markup and nothing distinguishes them, so the field is
called related and the walk reports distance from where it started rather
than depth in a tree.

the edge upwards exists elsewhere. a product's best sellers rank line names
the node it ranks in and links it, so the way to find a node's parent is to
come at it from an item inside it.

one request per node. depth 2 on a broad category is hundreds of them, which
is why the default depth is 1 and --max-nodes stops the walk out loud.`,
	Do: []string{
		"amz tree <node>                     the neighbours, one hop",
		"amz tree <node> --depth 2           two hops, hundreds of requests",
		"amz category <node> --related       the links off a single page",
		"amz product <asin> --json           the rank line, which does name a node",
	},
}}

// whyCmd prints one topic, or lists them.
func whyCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "why [topic]",
		Short: "Explain why something returns less than you expected",
		Long: "Each topic is a measurement with a date on it, not an opinion. Run it with " +
			"no argument to list the topics.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: whyTopicNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				listWhyTopics(w)
				return nil
			}
			name := strings.ToLower(args[0])
			for _, t := range whyTopics {
				if t.Name == name {
					printWhy(w, t)
					return nil
				}
			}
			// A near miss is worth catching, because the topic names are the
			// words in the error messages that sent the user here and it is easy
			// to type the plural.
			if near := nearestTopic(name); near != "" {
				return exit(CodeUsage, fmt.Errorf("no topic %q. did you mean %q", args[0], near))
			}
			return exit(CodeUsage, fmt.Errorf("no topic %q. run `amz why` for the list", args[0]))
		},
	}
	return cmd
}

// topicForCode maps an exit code to the topic that explains it.
//
// Only the codes that mean Amazon said no are here. A usage error explains
// itself and a runtime error is this tool's problem rather than something to
// read an essay about, so those get nothing appended and the message stays the
// message.
func topicForCode(code int) string {
	switch code {
	case CodeBlocked:
		return "blocked"
	case CodeInterstitial:
		return "captcha"
	case CodeDisallowed, CodeNoRobots:
		return "robots"
	case CodeSignIn:
		return "policy"
	}
	return ""
}

// explain appends the exit code and the topic that covers it to a failure.
//
// The errors themselves already name the URL and the rule or the redirect that
// produced it, because that is what makes a failure checkable. What they cannot
// carry is the essay, so this adds the one line that points at it: a user who
// hits exit 7 at eleven at night wants to know whether they did something wrong,
// and the answer takes three paragraphs that do not belong in an error string.
func explain(err error) error {
	if err == nil {
		return nil
	}
	code := codeFor(err)
	topic := topicForCode(code)
	if topic == "" || strings.Contains(err.Error(), "amz why ") {
		return err
	}
	return exit(code, fmt.Errorf("%w\nexit %d. run `amz why %s` for what this is and what to do about it", err, code, topic))
}

func whyTopicNames() []string {
	out := make([]string, 0, len(whyTopics))
	for _, t := range whyTopics {
		out = append(out, t.Name)
	}
	return out
}

func listWhyTopics(w io.Writer) {
	names := whyTopicNames()
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	_, _ = fmt.Fprintf(w, "%d topics, each measured against www.amazon.com on %s.\n\n", len(whyTopics), measured)
	for _, t := range whyTopics {
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, t.Name, t.Headline)
	}
	_, _ = fmt.Fprintf(w, "\nrun `amz why <topic>` for the measurement behind one of them.\n")
}

func printWhy(w io.Writer, t whyTopic) {
	_, _ = fmt.Fprintf(w, "%s: %s\n\n", t.Name, t.Headline)
	for _, line := range strings.Split(t.Body, "\n") {
		if line == "" {
			_, _ = fmt.Fprintln(w)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s\n", line)
	}
	if len(t.Do) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\n  what you can do:\n")
	for _, d := range t.Do {
		_, _ = fmt.Fprintf(w, "    %s\n", d)
	}
}

// nearestTopic matches a topic the user nearly typed: a prefix, a suffix, or the
// same word with an s on the end.
func nearestTopic(name string) string {
	names := whyTopicNames()
	sort.Strings(names)
	for _, n := range names {
		if strings.HasPrefix(n, name) || strings.HasPrefix(name, n) ||
			strings.TrimSuffix(name, "s") == n || strings.Contains(n, name) {
			return n
		}
	}
	return ""
}
