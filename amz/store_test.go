package amz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "amz.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func count(t *testing.T, s *Store, table string) int64 {
	t.Helper()
	rows, err := s.Query(context.Background(), `SELECT count(*) AS n FROM "`+table+`"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("count over %s returned %d rows", table, len(rows))
	}
	switch n := rows[0]["n"].(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		t.Fatalf("count over %s returned %T", table, rows[0]["n"])
		return 0
	}
}

// Many goroutines writing one store must all land. The DuckDB store needed a
// mutex around every write because the binary took an exclusive file lock;
// SQLite serialises writers itself, so this is the test that says the mutex was
// removed on purpose and not by accident.
func TestStoreConcurrentWrites(t *testing.T) {
	s := testStore(t)
	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.PutProduct(context.Background(), Product{
				ASIN:        "B" + string(rune('A'+i)) + "000000001",
				Marketplace: "us",
				Title:       "concurrent write " + strconv.Itoa(i),
				FetchedAt:   time.Unix(int64(i), 0).UTC(),
			})
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Errorf("write %d failed: %v", i, e)
		}
	}
	if got := count(t, s, "product"); got != n {
		t.Fatalf("want %d rows, got %d", n, got)
	}
}

// The whole point of the merge policy. A light read of /gp/aw/d/ carries no
// description, no bullets and no rails, and it must not delete a full read's.
// Before this policy existed, the second crawl of any product deleted most of
// what the first one found and reported success.
func TestQuickDoesNotEraseFull(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	full := Product{
		ASIN:        "B075F5X8BR",
		Marketplace: "us",
		Title:       "Skullcandy Jib Wired Earbuds",
		Description: "A+ content that only the full page carries",
		Bullets:     []string{"3.5mm plug", "noise isolating"},
		Rails:       []Rail{{Region: "sims-simsContainer", Title: "Customers also viewed", Cards: []Card{{ASIN: "B0ABCDEFGH"}}}},
		Rating:      f64(4.4),
		Offer:       &Offer{Price: &Money{Value: 12.99, Currency: "USD", Display: "$12.99"}},
		FetchedAt:   time.Unix(1000, 0).UTC(),
	}
	if err := s.PutProductVia(ctx, full, "s1"); err != nil {
		t.Fatal(err)
	}

	// The light surface. It carries asin, title, price, rating, ratings_count
	// and images, and it carries nothing else. See Ops s2.
	light := Product{
		ASIN:        "B075F5X8BR",
		Marketplace: "us",
		Title:       "Skullcandy Jib Wired Earbuds",
		Rating:      f64(4.5),
		Offer:       &Offer{Price: &Money{Value: 11.49, Currency: "USD", Display: "$11.49"}},
		FetchedAt:   time.Unix(2000, 0).UTC(),
	}
	if err := s.PutProductVia(ctx, light, "s2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProduct(ctx, "us", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description == "" {
		t.Error("the light read deleted the description, which that surface never had")
	}
	if len(got.Bullets) != 2 {
		t.Errorf("bullets = %v, the light read deleted them", got.Bullets)
	}
	if len(got.Rails) != 1 {
		t.Errorf("rails = %v, the light read deleted them", got.Rails)
	}
	// And the fields the light surface does carry are the newest ones, because
	// that surface looked and this is what it saw.
	if got.Rating == nil || *got.Rating != 4.5 {
		t.Errorf("rating = %v, want the light read's 4.5", got.Rating)
	}
	if got.Offer == nil || got.Offer.Price == nil || got.Offer.Price.Value != 11.49 {
		t.Errorf("price did not take the newest value: %+v", got.Offer)
	}
}

// The price table is append only, so the old price is still there after the new
// one lands. A merge bug cannot reach it, because nothing ever updates it.
func TestFullOverwritesStalePrice(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i, v := range []float64{12.99, 11.49, 14.00} {
		if err := s.PutProductVia(ctx, Product{
			ASIN:        "B075F5X8BR",
			Marketplace: "us",
			Title:       "Skullcandy Jib",
			Offer:       &Offer{Price: &Money{Value: v, Currency: "USD"}},
			FetchedAt:   time.Unix(int64(1000+i*1000), 0).UTC(),
		}, "s1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := count(t, s, "product"); got != 1 {
		t.Errorf("three reads of one product made %d rows", got)
	}
	series, err := s.PriceSeries(ctx, "us", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 3 {
		t.Fatalf("price series has %d points, want 3: %+v", len(series), series)
	}
	if series[0].Amount != 12.99 || series[2].Amount != 14.00 {
		t.Errorf("the series is not in observation order: %+v", series)
	}
	p, err := s.GetProduct(ctx, "us", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	if p.Offer.Price.Value != 14.00 {
		t.Errorf("the current record holds %v, want the newest 14.00", p.Offer.Price.Value)
	}
}

// A surface amz does not have in its registry carries everything. Guessing the
// other way would mean a record from an unregistered surface writing nothing and
// reporting success, which is the failure mode this whole policy exists to stop.
func TestUnknownSurfaceCarriesEverything(t *testing.T) {
	full := Product{ASIN: "B1", Marketplace: "us", Description: "kept", Bullets: []string{"a"}}
	empty := Product{ASIN: "B1", Marketplace: "us"}

	if got := Merge(full, empty, OpByID("s2")); got.Description == "" {
		t.Error("s2 does not carry description, so it must not clear one")
	}
	if got := Merge(full, empty, OpByID("nonesuch")); got.Description != "" {
		t.Error("an unknown surface has to be treated as carrying everything")
	}
	if got := Merge(full, empty, nil); got.Description != "" {
		t.Error("a nil surface has to be treated as carrying everything")
	}
}

// Every field on Product is either governed by an entry in mergeField or is one
// of the five the merge handles by name. A field added later with neither is
// silently ungoverned, which is exactly the shape of the bug this policy exists
// to prevent, so the test names them rather than counting them.
func TestEveryProductFieldIsGovernedOrExempt(t *testing.T) {
	exempt := map[string]bool{
		// The key and the provenance. Never merged.
		"ASIN": true, "Marketplace": true, "URL": true, "FetchedAt": true, "Envelope": true,
		// Identity that no surface declares and that a partial read never
		// contradicts. Merge leaves these alone on any surface with a field
		// list, which is the same behaviour a mergeField entry would give.
		"ParentASIN": true, "Extra": true,
	}
	rt := reflect.TypeOf(Product{})
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		if exempt[name] {
			continue
		}
		if _, ok := mergeField[name]; !ok {
			t.Errorf("Product.%s is governed by nothing: add it to mergeField or to the exempt list here with a reason", name)
		}
	}
}

// Every value in mergeField has to be a field name some surface actually
// declares. A typo there is invisible: op.Carries returns false for a name no
// Op lists, so the field would be frozen forever and never updated by anything.
func TestMergeFieldNamesExistInOps(t *testing.T) {
	known := map[string]bool{}
	for _, o := range Ops() {
		for _, f := range o.Fields {
			known[f] = true
		}
	}
	for structField, opField := range mergeField {
		if !known[opField] {
			t.Errorf("mergeField[%q] = %q, which no surface declares, so that field can never be updated", structField, opField)
		}
	}
}

// A crawl that is killed leaves items claimed. Restarting recovers them, and the
// URLs already done are not fetched again, so a resumed crawl neither loses work
// nor duplicates it.
func TestCrawlResumesWithoutLossOrDuplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amz.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for i := range 10 {
		if err := s.Enqueue(ctx, "https://www.amazon.com/dp/B"+strconv.Itoa(1000000000+i), EntityProduct, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Four get done, three are claimed when the process dies, three never got
	// looked at.
	batch, err := s.NextBatch(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 7 {
		t.Fatalf("claimed %d, want 7", len(batch))
	}
	for _, it := range batch[:4] {
		if err := s.MarkStatus(ctx, it.ID, "done", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart.
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	n, err := s2.Recover(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("recovered %d items, want the 3 that were in flight", n)
	}
	counts, err := s2.QueueCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["done"] != 4 {
		t.Errorf("done = %d, want 4: the restart lost completed work", counts["done"])
	}
	if counts["pending"] != 6 {
		t.Errorf("pending = %d, want 6: 3 recovered and 3 never claimed", counts["pending"])
	}

	// Re-seeding the same URLs is free, which is what lets a crawl be restarted
	// with the same command line.
	for i := range 10 {
		if err := s2.Enqueue(ctx, "https://www.amazon.com/dp/B"+strconv.Itoa(1000000000+i), EntityProduct, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := count(t, s2, "queue"); got != 10 {
		t.Errorf("re-seeding made the queue %d rows, want 10", got)
	}
}

// An item that keeps failing is parked rather than retried forever, because an
// infinite retry on a page that always 404s is how a resumable crawl becomes a
// crawl that never terminates.
func TestRepeatedlyClaimedItemIsParked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Enqueue(ctx, "https://www.amazon.com/dp/BFAILFAILS", EntityProduct, 0); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.NextBatch(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Recover(ctx, 3); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := s.QueueCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["stuck"] != 1 {
		t.Errorf("after three attempts the item is %v, want it parked as stuck", counts)
	}
}

// amz query is read only. The store holds hours of somebody's bandwidth and
// Amazon's, and a typo at an interactive prompt should not be able to empty it.
func TestQueryRefusesToWrite(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, q := range []string{
		"DELETE FROM product",
		"drop table product",
		"UPDATE product SET title=''",
		"SELECT 1; DELETE FROM product",
		"INSERT INTO product (marketplace, asin, json, fetched_at) VALUES ('us','B1','{}','')",
		"VACUUM",
		"ATTACH DATABASE '/tmp/x.db' AS x",
	} {
		if _, err := s.Query(ctx, q); !errors.Is(err, ErrNotReadOnly) {
			t.Errorf("%q was not refused: %v", q, err)
		}
	}
	if _, err := s.Query(ctx, "SELECT count(*) FROM product"); err != nil {
		t.Errorf("a plain read was refused: %v", err)
	}
}

// The json column is the record and the typed columns are an index over it. A
// lookup returns the bytes, so a field this build does not know about survives a
// round trip and comes back to a build that does.
func TestFullRecordSurvivesTheRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := Product{
		ASIN:        "B075F5X8BR",
		Marketplace: "us",
		Title:       "Skullcandy Jib",
		Description: "long text",
		Details:     map[string]string{"Item Weight": "0.32 ounces"},
		Images:      []Image{{HiRes: "https://m.media-amazon.com/images/I/1.jpg"}},
		FetchedAt:   time.Unix(1000, 0).UTC(),
	}
	if err := s.PutProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	raw, err := s.LookupJSON(ctx, "us", "B075F5X8BR")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Item Weight"`, "0.32 ounces", "long text", "media-amazon.com"} {
		if !strings.Contains(raw, want) {
			t.Errorf("the stored record lost %s:\n%s", want, raw)
		}
	}
	// And by URI as well as by ASIN, because that is the identifier every other
	// command prints.
	if _, err := s.LookupJSON(ctx, "us", "amz:us/product/B075F5X8BR"); err != nil {
		t.Errorf("lookup by uri: %v", err)
	}
}

// A file this build cannot read is refused before it is written to, and the
// message says what to do. A driver error saying "file is not a database" about
// a file amz itself wrote is not an answer anybody can act on.
func TestForeignFileIsRefusedWithAnExplanation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amz.duckdb")
	// The DuckDB magic, which is what is sitting at the v0.2 default path.
	if err := os.WriteFile(path, append([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, []byte("DUCK")...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenStore(path)
	if err == nil {
		t.Fatal("a duckdb file was opened as if it were a sqlite store")
	}
	if !errors.Is(err, ErrNotADatabase) && !errors.Is(err, ErrOldSchema) {
		t.Fatalf("unhelpful error: %v", err)
	}
	if !strings.Contains(err.Error(), "new path") {
		t.Errorf("the error does not say what to do next: %v", err)
	}
}

// Full text search over the store, which is what amz find runs.
func TestFindMatchesTitleAndDescription(t *testing.T) {
	s := testStore(t)
	if !s.HasFTS() {
		t.Skip("this build has no fts5")
	}
	ctx := context.Background()
	for _, p := range []Product{
		{ASIN: "B1", Marketplace: "us", Title: "Skullcandy Jib Wired Earbuds", Description: "3.5mm aux plug"},
		{ASIN: "B2", Marketplace: "us", Title: "Keychron K8 Mechanical Keyboard", Description: "tenkeyless hot swappable"},
	} {
		if err := s.PutProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.Find(ctx, "tenkeyless", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ASIN != "B2" {
		t.Fatalf("find over the description returned %+v", hits)
	}
	// Re-storing a product replaces its index entry rather than adding a
	// second, or every re-crawl would return the same product N times.
	if err := s.PutProduct(ctx, Product{ASIN: "B2", Marketplace: "us", Title: "Keychron K8", Description: "tenkeyless hot swappable"}); err != nil {
		t.Fatal(err)
	}
	hits, err = s.Find(ctx, "tenkeyless", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Errorf("re-crawling a product duplicated it in the index: %+v", hits)
	}
}

// Chart membership is a fact about a moment, so a second crawl of the same chart
// is a second set of rows and not an overwrite. A product that was number three
// last Tuesday still was.
func TestChartEntriesAreAppendOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for i, day := range []int64{0, 86400} {
		for rank := 1; rank <= 3; rank++ {
			if err := s.PutChartEntry(ctx, BestsellerEntry{
				Marketplace: "us",
				ListType:    "bestsellers",
				NodeID:      "172282",
				Rank:        rank,
				ASIN:        "B" + strconv.Itoa(rank+i),
				FetchedAt:   time.Unix(day, 0).UTC(),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := count(t, s, "chart_entry"); got != 6 {
		t.Errorf("two crawls of a three item chart made %d rows, want 6", got)
	}
}

func f64(v float64) *float64 { return &v }
