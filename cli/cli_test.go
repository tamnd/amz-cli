package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fixtureServer serves the amz package's testdata fixtures over HTTP so the
// whole command tree can be exercised offline.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := filepath.Join("..", "amz", "testdata")
	route := func(p string) string {
		switch {
		case strings.HasPrefix(p, "/dp/"):
			return "product.html"
		case strings.HasPrefix(p, "/product-reviews/"):
			return "reviews.html"
		case strings.HasPrefix(p, "/ask/"):
			return "qa.html"
		case strings.HasPrefix(p, "/gp/offer-listing/"):
			return "offers.html"
		case strings.HasPrefix(p, "/gp/"):
			return "bestsellers.html"
		case strings.HasPrefix(p, "/stores/"):
			return "brand.html"
		case strings.HasPrefix(p, "/sp"):
			return "seller.html"
		case strings.HasPrefix(p, "/author/"):
			return "author.html"
		case strings.HasPrefix(p, "/deals"):
			return "deals.html"
		case strings.HasPrefix(p, "/b"):
			return "category.html"
		case p == "/s" || strings.HasPrefix(p, "/s?"):
			return "search.html"
		}
		return ""
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if pg := q.Get("page"); pg != "" && pg != "1" {
			_, _ = w.Write([]byte("<html></html>"))
			return
		}
		if pg := q.Get("pageNumber"); pg != "" && pg != "1" {
			_, _ = w.Write([]byte("<html></html>"))
			return
		}
		name := route(r.URL.Path)
		if name == "" {
			http.NotFound(w, r)
			return
		}
		b, _ := os.ReadFile(filepath.Join(dir, name))
		_, _ = w.Write(b)
	}))
	t.Setenv("AMZ_BASE_URL", srv.URL)
	t.Setenv("AMZ_CACHE_DIR", t.TempDir())
	t.Cleanup(srv.Close)
	return srv
}

// run executes the root command with args and returns stdout.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--rate", "0"}, args...))
	err := root.Execute()
	return out.String(), err
}

// runSplit keeps stdout and stderr apart, which run deliberately does not.
// Most assertions here do not care, but a deprecation notice goes to stderr and
// a caller piping stdout into jq must not see it, so that separation has to be
// testable.
func runSplit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := Root()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"--rate", "0"}, args...))
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestCmdProductJSON(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "product", "B084DWG2VQ", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["asin"] != "B084DWG2VQ" {
		t.Fatalf("rows = %v", rows)
	}
	// The price now lives on the buy box and serialises as an object, not as a
	// bare number. That is the breaking change --flat exists to soften, so the
	// same assertion is made twice: once against the nested record and once
	// against the shape a v0.2.1 script is reading.
	offer, ok := rows[0]["offer"].(map[string]any)
	if !ok {
		t.Fatalf("no offer on the record: %v", rows[0])
	}
	price, ok := offer["price"].(map[string]any)
	if !ok {
		t.Fatalf("no price on the buy box: %v", offer)
	}
	if price["value"].(float64) != 49.99 || price["currency"] != "USD" {
		t.Errorf("price = %v", price)
	}
}

func TestCmdProductFlatKeepsTheOldShape(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "product", "B084DWG2VQ", "-o", "json", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0]["price"].(float64) != 49.99 || rows[0]["currency"] != "USD" {
		t.Errorf("price = %v %v", rows[0]["price"], rows[0]["currency"])
	}
	if rows[0]["brand"] != "Amazon" || rows[0]["seller_name"] != "Amazon.com" {
		t.Errorf("flat record = %v", rows[0])
	}
}

// TestFlatIsAnnouncedAsDeprecated pins the notice. The flag buys scripts one
// version to move and that promise is only kept if the deadline is printed
// where somebody using it will see it.
func TestFlatIsAnnouncedAsDeprecated(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "product", "B084DWG2VQ", "-o", "json", "--flat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "v0.4.0") {
		t.Errorf("--flat did not say when it goes away:\n%s", errOut)
	}
	// And the notice goes nowhere near the records, because the whole point of
	// the flag is that a v0.2.1 pipeline keeps working unchanged.
	if strings.Contains(out, "deprecated") {
		t.Errorf("the notice leaked into stdout:\n%s", out)
	}
}

func TestCmdSearchJSONL(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "search", "kindle", "-o", "jsonl", "-n", "2")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines:\n%s", len(lines), out)
	}
}

func TestCmdBestsellersTable(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "bestsellers", "electronics", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "B08C1W5N87") || !strings.Contains(out, "RANK") {
		t.Errorf("table missing data:\n%s", out)
	}
}

func TestCmdReviewsURLFormat(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "reviews", "B084DWG2VQ", "-o", "url")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(out), "\n") < 1 {
		t.Errorf("expected multiple review urls:\n%s", out)
	}
}

func TestCmdFieldsProjection(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "product", "B084DWG2VQ", "-o", "csv", "--fields", "asin,price,rating")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "asin,price,rating") {
		t.Errorf("csv header wrong:\n%s", out)
	}
	if !strings.Contains(out, "B084DWG2VQ,49.99,4.70") {
		t.Errorf("csv row wrong:\n%s", out)
	}
}

func TestCmdTemplate(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "product", "B084DWG2VQ", "--template", "{{.asin}}={{.price}}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "B084DWG2VQ=49.99" {
		t.Errorf("template output = %q", out)
	}
}

// A price is an object in the record, and a template is a line of text. A card
// rendered through {{.price}} has to print the price and not Go's rendering of
// the map the price decodes to.
func TestCmdTemplatePrintsPricesAndNotMaps(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "search", "usb c cable", "--template", "{{.asin}} {{.price}}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "map[") {
		t.Errorf("a structured field reached the template as a map:\n%s", out)
	}
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	if !regexp.MustCompile(`^B[A-Z0-9]{9} \d+\.\d{2}$`).MatchString(line) {
		t.Errorf("template line = %q, want an asin and a price", line)
	}
}

func TestCmdSeller(t *testing.T) {
	fixtureServer(t)
	out, err := run(t, "seller", "A1XYZSELLER22", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Anker Direct") {
		t.Errorf("seller missing:\n%s", out)
	}
}

func TestCmdAsinUtility(t *testing.T) {
	out, err := run(t, "asin", "https://www.amazon.com/Some-Title/dp/B08N5WRWNW/ref=x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "B08N5WRWNW" {
		t.Errorf("asin = %q", out)
	}
}

func TestCmdDryRun(t *testing.T) {
	out, err := run(t, "product", "B08N5WRWNW", "--dry-run", "-m", "uk")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "https://www.amazon.co.uk/dp/B08N5WRWNW" {
		t.Errorf("dry-run url = %q", out)
	}
}

// An argument that is not an id and not a URL used to reach the transport,
// which fetched the empty string and reported `unsupported protocol scheme ""`.
// That sentence describes the plumbing and leaves the person who typed the wrong
// thing to work out what the right thing was.
func TestCmdArgumentThatIsNotAPage(t *testing.T) {
	for _, args := range [][]string{
		{"product", "product"},
		{"product", "product", "--dry-run"},
		{"extraction", "nonsense"},
		{"agent-map", "product"},
	} {
		out, err := run(t, args...)
		if codeFor(err) != CodeUsage {
			t.Errorf("%v: expected usage exit, got %v (code %d)", args, err, codeFor(err))
		}
		if err != nil && strings.Contains(err.Error(), "protocol scheme") {
			t.Errorf("%v: error names the transport instead of the argument: %v", args, err)
		}
		if strings.Contains(out, "protocol scheme") {
			t.Errorf("%v: output names the transport instead of the argument: %q", args, out)
		}
	}
}

// `amz extraction product` reads as the per family report and is not: the family
// is a flag and the argument is a page. Answering that with "not an ASIN" is
// true and unhelpful.
func TestCmdExtractionFamilyAsArgument(t *testing.T) {
	_, err := run(t, "extraction", "product")
	if codeFor(err) != CodeUsage {
		t.Fatalf("expected usage exit, got %v (code %d)", err, codeFor(err))
	}
	if !strings.Contains(err.Error(), "--family product") {
		t.Errorf("error does not name the flag that does what was meant: %v", err)
	}
}

// Somebody who pastes an amazon.co.uk link and leaves --marketplace at its
// default meant the link. Honouring the flag there gives a record for a
// different listing at a different price under an id that looks right, which is
// the kind of wrong that never announces itself.
func TestCmdURLMarketplaceBeatsTheFlag(t *testing.T) {
	out, errOut, err := runSplit(t, "product", "https://www.amazon.co.uk/dp/B08N5WRWNW", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "uk") || !strings.Contains(errOut, "not us") {
		t.Errorf("nothing on stderr said the marketplace changed:\n%s", errOut)
	}
	// The note goes to stderr and nowhere near the records, because a caller
	// piping stdout into jq must not see it.
	if strings.TrimSpace(out) != "https://www.amazon.co.uk/dp/B08N5WRWNW" {
		t.Errorf("stdout = %q, want the URL and nothing else", out)
	}

	// And the record it produces is filed under the storefront the link named.
	rec, _, err := runSplit(t, "asin", "https://www.amazon.co.uk/dp/B08N5WRWNW", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec, `"marketplace":"uk"`) {
		t.Errorf("the record did not take the URL's storefront:\n%s", rec)
	}
}

// And an explicit flag that agrees with nothing in the arguments is left alone,
// because there is no URL to argue with it.
func TestCmdBareASINKeepsTheFlag(t *testing.T) {
	out, errOut, err := runSplit(t, "product", "B08N5WRWNW", "--dry-run", "-m", "de")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "https://www.amazon.de/dp/B08N5WRWNW" {
		t.Errorf("url = %q", out)
	}
	if errOut != "" {
		t.Errorf("a bare id produced a note it had no reason to produce:\n%s", errOut)
	}
}

// One run reads one marketplace: the currency, the number format and the
// availability strings all come from it, so two URLs naming two storefronts have
// no answer that is right for both.
func TestCmdTwoMarketplacesInOneRun(t *testing.T) {
	_, err := run(t, "product",
		"https://www.amazon.com/dp/B08N5WRWNW",
		"https://www.amazon.co.uk/dp/B08N5WRWNW", "--dry-run")
	if codeFor(err) != CodeUsage {
		t.Fatalf("expected usage exit, got %v (code %d)", err, codeFor(err))
	}
	if err == nil || !strings.Contains(err.Error(), "one run reads one") {
		t.Errorf("error does not explain why: %v", err)
	}
}

// `amz asin` stays the shell utility it has always been: one bare id per line,
// nothing else, so `amz asin "$url" | xargs amz product` keeps working.
func TestCmdAsinStaysPipeable(t *testing.T) {
	out, _, err := runSplit(t, "asin",
		"https://www.amazon.com/Some-Title/dp/B08N5WRWNW/ref=x",
		"0-439-02348-3")
	if err != nil {
		t.Fatal(err)
	}
	if out != "B08N5WRWNW\n0439023483\n" {
		t.Errorf("output = %q", out)
	}
}

// Name a format and it gives the whole identity instead: what kind of id it is,
// which storefront the input pointed at, the ISBN-13 for a book, and the URI the
// rest of the tool files things under.
func TestCmdAsinRecord(t *testing.T) {
	out, _, err := runSplit(t, "asin", "https://www.amazon.co.uk/dp/0439023483", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &row); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	if row["asin"] != "0439023483" || row["kind"] != "isbn10" {
		t.Errorf("record = %v", row)
	}
	if row["isbn13"] != "9780439023481" {
		t.Errorf("isbn13 = %v, want it computed from the check digit", row["isbn13"])
	}
	if row["marketplace"] != "uk" || row["uri"] != "amz:uk/product/0439023483" {
		t.Errorf("the URL's storefront did not reach the record: %v", row)
	}
}

// A bare id belongs to every storefront equally, so the marketplace column is
// blank rather than filled with the default dressed up as a fact.
func TestCmdAsinDoesNotInventAMarketplace(t *testing.T) {
	out, err := run(t, "asin", "B08N5WRWNW", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &row); err != nil {
		t.Fatal(err)
	}
	if row["marketplace"] != "" {
		t.Errorf("a bare id came back scoped to %v", row["marketplace"])
	}
}

func TestCmdUnknownMarketplace(t *testing.T) {
	_, err := run(t, "product", "B08N5WRWNW", "-m", "zz")
	if codeFor(err) != CodeUsage {
		t.Errorf("expected usage exit, got %v (code %d)", err, codeFor(err))
	}
}
