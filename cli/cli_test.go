package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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
		// One ASIN answers with a listing that has no twister on it. Every
		// other fixture here varies, and "this product has one version" is a
		// distinct case the commands have to get right rather than a page
		// nobody thought to capture.
		if strings.HasPrefix(r.URL.Path, "/dp/"+asinNoVariants) {
			_, _ = w.Write([]byte(pageNoVariants))
			return
		}
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

// asinNoVariants is served by fixtureServer as a listing that does not vary.
const asinNoVariants = "B0NOVARY01"

const pageNoVariants = `<html><body>
<div id="dp">
  <div id="title_feature_div" class="celwidget" data-feature-name="title">
    <h1 id="title"><span id="productTitle">A Product Sold In One Version Only</span></h1>
  </div>
  <input type="hidden" id="ASIN" value="` + asinNoVariants + `">
  <div id="corePrice_feature_div" class="celwidget" data-feature-name="corePrice">
    <span class="a-price"><span class="a-offscreen">$5.00</span></span>
  </div>
</div>
</body></html>`

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

// amz reviews reads the detail page now, because /product-reviews/ redirects to
// a sign-in. The records still go to stdout in the shape they always had, and
// the sentence explaining what was not read goes to stderr, so a script piping
// this into jq is unaffected by the change and a person running it is told.
func TestCmdReviewsNoteGoesToStderr(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "reviews", "B084DWG2VQ", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var r map[string]any
		if uerr := json.Unmarshal([]byte(line), &r); uerr != nil {
			t.Fatalf("stdout is not jsonl, so the note leaked into it: %v\n%s", uerr, out)
		}
		rows = append(rows, r)
	}
	if len(rows) != 2 {
		t.Fatalf("the fixture medley carries two reviews, got %d", len(rows))
	}
	if rows[0]["title"] == "" || rows[0]["text"] == "" {
		t.Errorf("the medley spells its hooks differently and the parser must know both:\n%s", out)
	}
	if !strings.Contains(errOut, "sign-in") || !strings.Contains(errOut, "/product-reviews/") {
		t.Errorf("the note has to say what was not read and where it is:\n%s", errOut)
	}
	if !strings.Contains(errOut, "amz why reviews") {
		t.Errorf("the note has to name the topic carrying the measurement:\n%s", errOut)
	}
}

// The star filter is applied here and not by Amazon, and the difference matters
// enough that the command says so out loud. Asking Amazon for the two star
// reviews returns the two star reviews; filtering the two on the page returns
// whichever of those two happen to be two star, which is one.
func TestCmdReviewsFilterSaysItIsLocal(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "reviews", "B084DWG2VQ", "-o", "jsonl", "--stars", "2")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n != 1 {
		t.Fatalf("one of the two fixture reviews is two star, got %d rows:\n%s", n, out)
	}
	if !strings.Contains(errOut, "not by amazon") {
		t.Errorf("a local filter has to admit it is local:\n%s", errOut)
	}
}

// The ask region on the fixture states a count and carries no pairs, which is
// what almost every product page does today. Nothing on stdout, the count and
// the reason on stderr, and exit 3 rather than a silent success.
func TestCmdQACountWithoutPairs(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "qa", "B084DWG2VQ", "-o", "jsonl")
	if code := codeFor(err); code != CodeNoData {
		t.Fatalf("want exit %d, got %d (%v)", CodeNoData, code, err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("there are no pairs to emit:\n%s", out)
	}
	if !strings.Contains(errOut, "1204") && !strings.Contains(errOut, "1,204") {
		t.Errorf("the page states 1,204 answered questions and the note must carry it:\n%s", errOut)
	}
	if !strings.Contains(errOut, "amz why qa") {
		t.Errorf("the note has to name the topic:\n%s", errOut)
	}
}

// amz offers emits the one offer that can still be read, in the row shape it has
// always emitted, and reports the count of the ones it cannot.
func TestCmdOffersBuyBoxAndCount(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "offers", "B084DWG2VQ", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if uerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &row); uerr != nil {
		t.Fatalf("not jsonl: %v\n%s", uerr, out)
	}
	if row["is_buybox"] != true {
		t.Errorf("the one readable offer is the buy box: %v", row)
	}
	if row["price"] != 49.99 || row["seller_name"] != "Amazon.com" {
		t.Errorf("the buy box row carries the buy box: %v", row)
	}
	// The ingress reads "New & Used (22) from $9.21" and only the bracketed
	// number is a count, so a note claiming 9 or 21 is the bug this guards.
	if !strings.Contains(errOut, "1 of 22") {
		t.Errorf("one offer read of the twenty two the page states:\n%s", errOut)
	}
	if !strings.Contains(errOut, "amz why offers") {
		t.Errorf("the note has to name the topic:\n%s", errOut)
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

// The human card is what most people see, so it gets the same treatment as the
// data formats: the histogram is drawn, the counts are labelled as derived, and
// the not read block is generated from the record rather than written out.
func TestCmdProductCardDrawsTheHistogramAndTheGaps(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "product", "B075F5X8BR", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"5 ★", "1 ★", "█", "counts derived from integer percentages"} {
		if !strings.Contains(out, want) {
			t.Errorf("the card is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "not read") {
		t.Fatalf("the card has no not read block:\n%s", out)
	}
	// Every line of that block comes from a miss on the record. Naming reviews
	// here rather than a fixed sentence is the point: when Amazon reopens the
	// corpus the miss goes and the line goes with it.
	for _, want := range []string{"reviews", "questions", "amz why reviews"} {
		if !strings.Contains(out, want) {
			t.Errorf("the not read block is missing %q:\n%s", want, out)
		}
	}
	// 284,512 and not 284512. The page prints the grouped form and a card that
	// prints the bare digits next to it reads as a different number.
	if !strings.Contains(out, "284,512 ratings") {
		t.Errorf("counts are not grouped:\n%s", out)
	}
}

// A card is for a person and every other format is for a program, so asking for
// one of those has to keep getting rows.
func TestCmdProductCardOnlyWhenTheFormatIsATable(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "product", "B075F5X8BR", "-o", "table", "--fields", "asin,title")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "not read") || strings.Contains(out, "█") {
		t.Errorf("--fields asked for columns and got a card:\n%s", out)
	}
	if !strings.Contains(out, "ASIN") || !strings.Contains(out, "B075F5X8BR") {
		t.Errorf("--fields did not produce its columns:\n%s", out)
	}
}

func TestCmdVariantsRowsAndTheShortfall(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "variants", "B084DWG2VQ", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no variant rows:\n%s", out)
	}
	var current int
	for _, line := range lines {
		var row map[string]any
		if uerr := json.Unmarshal([]byte(line), &row); uerr != nil {
			t.Fatalf("not jsonl: %v\n%s", uerr, line)
		}
		if row["asin"] == "" {
			t.Errorf("a variant row with no asin: %v", row)
		}
		if row["current"] == true {
			current++
		}
	}
	// Exactly one row is the listing that was read. Without it a caller cannot
	// tell which of the siblings the prices on the page belong to.
	if current != 1 {
		t.Errorf("%d rows claim to be the current selection, want 1:\n%s", current, out)
	}
	_ = errOut
}

// A listing with no twister has one variant, which is itself. Reporting nothing
// found would be wrong: the question was answered and the answer is one.
func TestCmdVariantsOnAListingThatDoesNotVary(t *testing.T) {
	fixtureServer(t)
	out, errOut, err := runSplit(t, "variants", asinNoVariants, "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 1 {
		t.Errorf("want the one row it is, got %d:\n%s", n, out)
	}
	if !strings.Contains(errOut, "does not vary") {
		t.Errorf("the note has to say why there is one row:\n%s", errOut)
	}
}

// An advertising placement and a recommendation are different facts, so the
// sponsored ones are left out and their absence is stated rather than silent.
func TestCmdRelatedIsOrganicByDefault(t *testing.T) {
	fixtureServer(t)
	organic, errOut, err := runSplit(t, "related", "B084DWG2VQ", "-o", "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(organic), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if uerr := json.Unmarshal([]byte(line), &row); uerr != nil {
			t.Fatalf("not jsonl: %v\n%s", uerr, line)
		}
		if row["sponsored"] == true {
			t.Errorf("a sponsored card came back without --include-sponsored: %v", row)
		}
	}
	if strings.Contains(errOut, "sponsored") && !strings.Contains(errOut, "--include-sponsored") {
		t.Errorf("a note about dropped cards has to name the flag that keeps them:\n%s", errOut)
	}
}

func TestCmdWhyListsAndExplains(t *testing.T) {
	list, _, err := runSplit(t, "why")
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range whyTopicNames() {
		if !strings.Contains(list, topic) {
			t.Errorf("the list is missing %q:\n%s", topic, list)
		}
	}
	body, _, err := runSplit(t, "why", "reviews")
	if err != nil {
		t.Fatal(err)
	}
	// A topic with no date is an opinion. The measurement date is what tells a
	// reader how much of this to still believe.
	if !strings.Contains(body, measured) {
		t.Errorf("the topic does not say when it was measured:\n%s", body)
	}
	if !strings.Contains(body, "/product-reviews/") {
		t.Errorf("the topic does not name the surface it is about:\n%s", body)
	}
}

// The topic names are the words in the error messages that send people here, so
// a near miss is worth catching rather than answering with the whole list.
func TestCmdWhyUnknownTopicSuggests(t *testing.T) {
	out, _, err := runSplit(t, "why", "review")
	if codeFor(err) != CodeUsage {
		t.Fatalf("expected a usage exit, got %v:\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "reviews") {
		t.Errorf("no suggestion in %q", err.Error())
	}
}

func TestCmdDoctorOfflineReportsTheClient(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "doctor", "--offline", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	// The identity and the header set are the reason this tool is served at
	// all, so doctor reads them from the code that sends them.
	for _, want := range []string{"user agent", "headers", "Accept-Encoding", "session", "pace", "store"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor did not report %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("an unmodified build fails its own check:\n%s", out)
	}
	// --offline means no requests, so nothing about a probe can appear.
	if strings.Contains(out, "probe") {
		t.Errorf("--offline made a request:\n%s", out)
	}
}

func TestCmdDoctorProbesTheTwoSurfaces(t *testing.T) {
	fixtureServer(t)
	out, _, err := runSplit(t, "doctor", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"robots.txt", "/dp/ probe", "/s probe"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor did not probe %q:\n%s", want, out)
		}
	}
}

// A failure that names a code and nothing else leaves the reader with a number
// to search for. Every stop Amazon can hand back names the topic that covers it.
func TestExitCodesNameTheirWhyTopic(t *testing.T) {
	cases := map[int]string{
		CodeBlocked:      "blocked",
		CodeInterstitial: "captcha",
		CodeDisallowed:   "robots",
		CodeNoRobots:     "robots",
		CodeSignIn:       "policy",
	}
	for code, topic := range cases {
		err := explain(exit(code, errors.New("something amazon said")))
		if codeFor(err) != code {
			t.Errorf("explain changed the exit code from %d to %d", code, codeFor(err))
		}
		if !strings.Contains(err.Error(), "amz why "+topic) {
			t.Errorf("exit %d does not name its topic: %q", code, err.Error())
		}
	}
	// A usage error explains itself, and a runtime error is this tool's problem
	// rather than something to read an essay about.
	for _, code := range []int{CodeUsage, CodeRuntime, CodeNoData} {
		err := explain(exit(code, errors.New("plain")))
		if strings.Contains(err.Error(), "amz why") {
			t.Errorf("exit %d was given a topic it has no use for: %q", code, err.Error())
		}
	}
}

// Every topic named by an exit code has to exist, or the message sends people to
// a command that tells them the topic does not exist.
func TestEveryNamedTopicExists(t *testing.T) {
	names := map[string]bool{}
	for _, n := range whyTopicNames() {
		names[n] = true
	}
	for _, code := range []int{CodeBlocked, CodeInterstitial, CodeDisallowed, CodeNoRobots, CodeSignIn} {
		if topic := topicForCode(code); topic != "" && !names[topic] {
			t.Errorf("exit %d points at %q, which is not a topic", code, topic)
		}
	}
}

// Every entity and chart command reads a different anchor, and v0.3.0 moved
// several of them. One row from each is a thin assertion on purpose: the shape
// of the record is tested in the amz package, and what this guards is that the
// command still points at a page that exists.
func TestCmdEntityAnchorsStillResolve(t *testing.T) {
	fixtureServer(t)
	cases := []struct {
		name string
		args []string
	}{
		{"category", []string{"category", "electronics"}},
		{"brand", []string{"brand", "Anker"}},
		{"seller", []string{"seller", "A2L77EE7U53NWQ"}},
		{"author", []string{"author", "B000AP9A6K"}},
		{"deals", []string{"deals"}},
		{"bestsellers", []string{"bestsellers"}},
		{"new-releases", []string{"new-releases"}},
		{"movers", []string{"movers"}},
		{"wished", []string{"wished"}},
		{"gifted", []string{"gifted"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, err := runSplit(t, append(tc.args, "-o", "jsonl")...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, errOut)
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("%v returned nothing\n%s", tc.args, errOut)
			}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				var row map[string]any
				if uerr := json.Unmarshal([]byte(line), &row); uerr != nil {
					t.Fatalf("%v is not jsonl: %v\n%s", tc.args, uerr, line)
				}
			}
		})
	}
}
