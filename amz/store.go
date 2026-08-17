package amz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Store is an optional DuckDB-backed sink. It shells out to the `duckdb` binary
// so the build never depends on cgo. A missing binary yields ErrNoDuckDB.
//
// DuckDB takes an exclusive lock on the database file, so two `duckdb`
// processes cannot write it at once. A crawl fetches with many workers but
// funnels every write through mu, so concurrency stays on the network where it
// pays off and the single-writer database never sees a lock conflict.
type Store struct {
	path string
	bin  string
	mu   sync.Mutex
}

// ErrNoDuckDB is returned when the duckdb binary is not on PATH.
var ErrNoDuckDB = errors.New("duckdb binary not found on PATH (install it to use --db)")

// OpenStore locates the duckdb binary and ensures the schema exists.
func OpenStore(path string) (*Store, error) {
	bin, err := exec.LookPath("duckdb")
	if err != nil {
		return nil, ErrNoDuckDB
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	s := &Store{path: path, bin: bin}
	ctx := context.Background()
	if err := s.checkSchema(ctx); err != nil {
		return nil, err
	}
	if err := s.exec(ctx, schemaSQL); err != nil {
		return nil, err
	}
	return s, nil
}

// checkSchema refuses a database written by an older build.
//
// The test is whether a products table exists without a marketplace in its key,
// which is exactly the v0.2 shape. A file that does not exist yet, or one with
// no products table, is new and gets the current schema. CREATE TABLE IF NOT
// EXISTS would otherwise leave the old table in place and every write would
// carry on merging storefronts, quietly, forever.
func (s *Store) checkSchema(ctx context.Context) error {
	if _, err := os.Stat(s.path); err != nil {
		return nil
	}
	rows, err := s.Query(ctx,
		"SELECT count(*) AS n FROM information_schema.columns WHERE table_name='products' AND column_name='uri';")
	if err != nil {
		// An unreadable file is not an old schema, and saying so would send
		// somebody to delete a database whose real problem is a permission bit.
		return nil
	}
	tables, err := s.Query(ctx,
		"SELECT count(*) AS n FROM information_schema.tables WHERE table_name='products';")
	if err != nil || len(tables) == 0 || asInt64(tables[0]["n"]) == 0 {
		return nil
	}
	if len(rows) > 0 && asInt64(rows[0]["n"]) > 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", s.path, ErrOldSchema)
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// The schema, and the one thing about it worth reading before the columns.
//
// Every table whose id space is per marketplace carries the marketplace in its
// primary key. B075F5X8BR on amazon.com and B075F5X8BR on amazon.co.uk are two
// listings with different prices, different sellers and sometimes different
// products, and the previous schema keyed products on the ASIN alone, so a crawl
// of both storefronts kept whichever ran last and reported one row. That is the
// worst kind of wrong: no error, no warning, and a number that looks like an
// answer.
//
// Browse nodes are per marketplace for the same reason, 172282 being Electronics
// on .com and something else on .de, so categories and charts are scoped too.
//
// Sellers, brands and authors are not scoped, and that is deliberate rather than
// an oversight. A merchant id is global across Amazon: A2L77EE7U53NWQ is one
// company everywhere it trades. What varies by storefront is the feedback and
// the storefront page, and those live in the record, which carries the
// marketplace it was measured in. See notes/Spec/3007/04_graph.md section 1.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS products (marketplace TEXT, asin TEXT, uri TEXT, data JSON, fetched_at TIMESTAMP, PRIMARY KEY (marketplace, asin));
CREATE TABLE IF NOT EXISTS reviews (review_id TEXT PRIMARY KEY, marketplace TEXT, asin TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS qa (qa_id TEXT PRIMARY KEY, marketplace TEXT, asin TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS offers (marketplace TEXT, asin TEXT, seller_id TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS bestsellers (marketplace TEXT, list_type TEXT, node_id TEXT, rank INT, asin TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS categories (marketplace TEXT, node_id TEXT, uri TEXT, data JSON, fetched_at TIMESTAMP, PRIMARY KEY (marketplace, node_id));
CREATE TABLE IF NOT EXISTS brands (slug TEXT PRIMARY KEY, uri TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS sellers (seller_id TEXT PRIMARY KEY, uri TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS authors (slug TEXT PRIMARY KEY, uri TEXT, data JSON, fetched_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS queue (id BIGINT, url TEXT, entity TEXT, priority INT, status TEXT);
INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', '2');
`

// SchemaVersion is the shape of the tables this build writes.
//
// Version 1 keyed products on the ASIN alone. There is no migration from it and
// there is not going to be one, because a version 1 database has already lost
// the data a migration would need: two storefronts collapsed into one row and
// the row that lost is gone. Opening one is refused with a message that says to
// delete it and crawl again, which is honest about the cost and does not pretend
// a rebuild is a recovery.
const SchemaVersion = 2

// ErrOldSchema is returned when the database on disk predates the marketplace
// scoped keys.
var ErrOldSchema = errors.New(
	"this database was written with the v0.2 schema, which keyed products on the ASIN alone and so merged marketplaces into one row: delete it and crawl again")

// exec runs a SQL script against the database file. Writes are serialized so
// concurrent crawl workers never collide on DuckDB's exclusive file lock.
func (s *Store) exec(ctx context.Context, sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.CommandContext(ctx, s.bin, s.path)
	cmd.Stdin = strings.NewReader(sql)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("duckdb: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Query runs SQL and returns rows as JSON objects.
func (s *Store) Query(ctx context.Context, sql string) ([]map[string]any, error) {
	cmd := exec.CommandContext(ctx, s.bin, "-json", s.path)
	cmd.Stdin = strings.NewReader(sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("duckdb: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// upsert writes one JSON row into a table keyed on its primary key column(s).
func (s *Store) upsert(ctx context.Context, table string, cols []string, vals []any) error {
	var b strings.Builder
	b.WriteString("INSERT OR REPLACE INTO ")
	b.WriteString(table)
	b.WriteString(" (")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(") VALUES (")
	for i, v := range vals {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(sqlLiteral(v))
	}
	b.WriteString(");")
	return s.exec(ctx, b.String())
}

// ErrUnscoped is returned when a record that belongs to one storefront arrives
// without saying which.
//
// The store rejects it rather than filing it under an empty marketplace, because
// an empty marketplace is its own key: the record would come back from a query
// for neither the US nor the UK, and the next crawl of either storefront would
// leave it there as a permanent orphan that nothing overwrites.
var ErrUnscoped = errors.New("record has no marketplace, so it cannot be stored under one")

// PutProduct upserts a product record, keyed on the marketplace and the ASIN.
//
// The two together are the key, so writing the same ASIN from two storefronts
// gives two rows rather than one row that changes price depending on which crawl
// ran last. That is the entire point of this milestone and it is enforced here
// and by the primary key, not by asking callers to remember.
func (s *Store) PutProduct(ctx context.Context, p Product) error {
	if p.Marketplace == "" {
		return fmt.Errorf("product %s: %w", p.ASIN, ErrUnscoped)
	}
	uri := ""
	if r := p.Ref(); r != nil {
		uri = r.URI
	}
	return s.upsert(ctx, "products",
		[]string{"marketplace", "asin", "uri", "data", "fetched_at"},
		[]any{p.Marketplace, p.ASIN, uri, jsonOf(p), p.FetchedAt.Format("2006-01-02 15:04:05")})
}

// PutReview upserts a review record.
//
// A review id is global, so it stays the primary key. The marketplace is carried
// alongside because the product it is about is scoped, and a join back to
// products needs both halves of that key.
func (s *Store) PutReview(ctx context.Context, r Review) error {
	return s.upsert(ctx, "reviews",
		[]string{"review_id", "marketplace", "asin", "data", "fetched_at"},
		[]any{r.ReviewID, r.Marketplace, r.ASIN, jsonOf(r), r.FetchedAt.Format("2006-01-02 15:04:05")})
}

// PutQA upserts a Q&A record.
func (s *Store) PutQA(ctx context.Context, q QA) error {
	return s.upsert(ctx, "qa",
		[]string{"qa_id", "marketplace", "asin", "data", "fetched_at"},
		[]any{q.QAID, q.Marketplace, q.ASIN, jsonOf(q), q.FetchedAt.Format("2006-01-02 15:04:05")})
}

// PutBestseller appends a chart entry.
func (s *Store) PutBestseller(ctx context.Context, e BestsellerEntry) error {
	return s.upsert(ctx, "bestsellers",
		[]string{"marketplace", "list_type", "node_id", "rank", "asin", "data", "fetched_at"},
		[]any{e.Marketplace, e.ListType, e.NodeID, e.Rank, e.ASIN, jsonOf(e), e.FetchedAt.Format("2006-01-02 15:04:05")})
}

// Enqueue inserts a queue item if its URL is not already present.
func (s *Store) Enqueue(ctx context.Context, url, entity string, priority int) error {
	sql := fmt.Sprintf(
		"INSERT INTO queue (id, url, entity, priority, status) "+
			"SELECT COALESCE((SELECT MAX(id) FROM queue),0)+1, %s, %s, %d, 'pending' "+
			"WHERE NOT EXISTS (SELECT 1 FROM queue WHERE url=%s);",
		sqlLiteral(url), sqlLiteral(entity), priority, sqlLiteral(url))
	return s.exec(ctx, sql)
}

// NextBatch claims up to n pending queue items, marking them in-progress.
func (s *Store) NextBatch(ctx context.Context, n int) ([]QueueItem, error) {
	rows, err := s.Query(ctx, fmt.Sprintf(
		"SELECT id, url, entity, priority, status FROM queue WHERE status='pending' ORDER BY priority DESC, id ASC LIMIT %d;", n))
	if err != nil {
		return nil, err
	}
	var items []QueueItem
	for _, r := range rows {
		it := QueueItem{
			ID:       asInt64(r["id"]),
			URL:      asString(r["url"]),
			Entity:   asString(r["entity"]),
			Priority: int(asInt64(r["priority"])),
			Status:   asString(r["status"]),
		}
		items = append(items, it)
		_ = s.exec(ctx, fmt.Sprintf("UPDATE queue SET status='active' WHERE id=%d;", it.ID))
	}
	return items, nil
}

// MarkStatus updates the status of one queue item.
func (s *Store) MarkStatus(ctx context.Context, id int64, status string) error {
	return s.exec(ctx, fmt.Sprintf("UPDATE queue SET status=%s WHERE id=%d;", sqlLiteral(status), id))
}

// PendingCount returns the number of pending queue items.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	rows, err := s.Query(ctx, "SELECT COUNT(*) AS n FROM queue WHERE status='pending';")
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	return int(asInt64(rows[0]["n"])), nil
}

// Stats returns row counts for every table.
func (s *Store) Stats(ctx context.Context) ([]map[string]any, error) {
	tables := []string{"products", "reviews", "qa", "offers", "bestsellers", "categories", "brands", "sellers", "authors", "queue"}
	var out []map[string]any
	for _, t := range tables {
		rows, err := s.Query(ctx, "SELECT COUNT(*) AS n FROM "+t+";")
		if err != nil {
			return nil, err
		}
		n := int64(0)
		if len(rows) > 0 {
			n = asInt64(rows[0]["n"])
		}
		out = append(out, map[string]any{"table": t, "rows": n})
	}
	return out, nil
}

// Vacuum compacts the database.
func (s *Store) Vacuum(ctx context.Context) error { return s.exec(ctx, "VACUUM;") }

func asInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sqlLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		return fmt.Sprintf("%g", x)
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(x), "'", "''") + "'"
	}
}
