package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			w.Write([]byte("<html></html>"))
			return
		}
		if pg := q.Get("pageNumber"); pg != "" && pg != "1" {
			w.Write([]byte("<html></html>"))
			return
		}
		name := route(r.URL.Path)
		if name == "" {
			http.NotFound(w, r)
			return
		}
		b, _ := os.ReadFile(filepath.Join(dir, name))
		w.Write(b)
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
	if rows[0]["price"].(float64) != 49.99 {
		t.Errorf("price = %v", rows[0]["price"])
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

func TestCmdUnknownMarketplace(t *testing.T) {
	_, err := run(t, "product", "B08N5WRWNW", "-m", "zz")
	if codeFor(err) != CodeUsage {
		t.Errorf("expected usage exit, got %v (code %d)", err, codeFor(err))
	}
}
