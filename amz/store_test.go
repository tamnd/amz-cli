package amz

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestStoreConcurrentWrites guards the DuckDB single-writer lock fix: many
// goroutines writing the same store at once must all land, none lost to a lock
// conflict. Skips when the duckdb binary is not installed.
func TestStoreConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amz.duckdb")
	s, err := OpenStore(path)
	if err == ErrNoDuckDB {
		t.Skip("duckdb not installed")
	}
	if err != nil {
		t.Fatal(err)
	}

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := Product{
				ASIN:        "B" + string(rune('A'+i)) + "000000001",
				Marketplace: "us",
				Price:       float64(i),
				FetchedAt:   time.Unix(int64(i), 0).UTC(),
			}
			errs[i] = s.PutProduct(context.Background(), p)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("write %d failed: %v", i, e)
		}
	}
	rows, err := s.Query(context.Background(), "select count(*) n from products")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || asInt64(rows[0]["n"]) != n {
		t.Fatalf("want %d rows, got %v", n, rows)
	}
}
