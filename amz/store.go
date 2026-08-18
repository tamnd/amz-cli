package amz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go, no cgo, no external binary
)

// Store is the local SQLite database.
//
// It is pure Go. The v0.2 store shelled out to a `duckdb` binary, which made the
// README's first line ("one pure-Go binary") false and made `amz db query` fail
// on a machine that had everything else it needed. There is no external process
// here and `TestNoExternalBinaries` fails if one comes back.
//
// SQLite serialises writers itself, so there is no mutex in this type. The
// journal is WAL and busy_timeout is set, which between them mean a concurrent
// reader never blocks a write and a concurrent write waits rather than failing.
type Store struct {
	db   *sql.DB
	path string
	// fts records whether the FTS5 module was available when the file was
	// created. `amz find` reads it so a missing index is reported as a missing
	// index rather than as no results.
	fts bool
}

// ErrOldSchema is returned when the file on disk was written by a build with a
// different schema, including the DuckDB era.
var ErrOldSchema = errors.New("this database was written by an older build")

// ErrNotADatabase is returned when the path exists and is not SQLite.
//
// The overwhelmingly likely cause is a v0.2 DuckDB file sitting at the default
// path, so the message says so rather than leaving somebody to work out what
// "file is not a database" means about a file amz itself wrote.
var ErrNotADatabase = errors.New("not a sqlite database")

// OpenStore opens or creates the database and ensures the schema exists.
func OpenStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// The pragmas go in the DSN so they apply to every connection in the pool
	// rather than to whichever one happened to run a SET.
	//
	// _txlock=immediate is not decoration. Every write here is a transaction that
	// reads first and then writes, which under SQLite's default deferred locking
	// takes a read lock, discovers another writer has moved the database under
	// it, and fails with SQLITE_BUSY_SNAPSHOT. busy_timeout does not help with
	// that one, because there is nothing to wait for: the snapshot this
	// transaction read is already stale and retrying the upgrade cannot fix it.
	// Taking the write lock up front turns the same contention into a wait,
	// which busy_timeout does cover. Sixteen concurrent writers used to lose
	// fourteen of their rows here and report success on none of them.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
	ctx := context.Background()
	if err := s.check(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// FTS5 is a build option. Without it the store still works and only `amz
	// find` is affected, so the failure is recorded rather than fatal.
	if _, err := db.ExecContext(ctx, ftsSQL); err == nil {
		s.fts = true
	}
	if err := s.setMeta(ctx, "schema_version", fmt.Sprint(SchemaVersion)); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.setMeta(ctx, "fts", fmt.Sprint(s.fts)); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// check refuses a file this build cannot read, before it writes to it.
//
// An empty or missing path is new and gets the current schema. A file that is
// not SQLite at all is almost certainly the v0.2 DuckDB store, and the caller is
// told to keep it and start a new one rather than being handed a driver error.
// A SQLite file with an older schema_version is refused for the same reason: a
// version 1 database collapsed two storefronts into one row and the row that
// lost is already gone, so there is nothing for a migration to work from.
func (s *Store) check(ctx context.Context) error {
	fi, err := os.Stat(s.path)
	if err != nil || fi.Size() == 0 {
		return nil
	}
	var n int
	row := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type IN ('table','view')")
	if err := row.Scan(&n); err != nil {
		return fmt.Errorf("%s: %w: if this is a v0.2 database it is a duckdb file, which this build has no reader for. keep it and point --db at a new path", s.path, ErrNotADatabase)
	}
	if n == 0 {
		return nil
	}
	var v string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key='schema_version'").Scan(&v); err != nil {
		// Tables but no meta row is a shape this build never wrote.
		return fmt.Errorf("%s: %w: it has tables and no schema version, so amz cannot tell what is in it", s.path, ErrOldSchema)
	}
	if v != fmt.Sprint(SchemaVersion) {
		return fmt.Errorf("%s: %w: schema version %s, this build writes %d. there is no migration: keep the file and point --db at a new path",
			s.path, ErrOldSchema, v, SchemaVersion)
	}
	return nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// HasFTS reports whether the full text index exists.
func (s *Store) HasFTS() bool { return s.fts }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) setMeta(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v)
	return err
}

// Meta reads one meta value.
func (s *Store) Meta(ctx context.Context, k string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key=?", k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// ErrUnscoped is returned when a record that belongs to one storefront arrives
// without saying which.
//
// The store rejects it rather than filing it under an empty marketplace, because
// an empty marketplace is its own key: the record would come back from a query
// for neither the US nor the UK, and the next crawl of either storefront would
// leave it there as a permanent orphan that nothing overwrites.
var ErrUnscoped = errors.New("record has no marketplace, so it cannot be stored under one")

const stamp = "2006-01-02T15:04:05Z07:00"

func at(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(stamp)
}

// PutProduct writes a product, merging it with what is already stored.
//
// The merge is the whole reason this is not an upsert. See Merge: a light read
// carries no A+ content and never did, so its absence must not delete the full
// read's. The price and rank rows it appends are never merged and never
// rewritten, so a merge bug cannot reach the history.
func (s *Store) PutProduct(ctx context.Context, p Product) error {
	return s.PutProductVia(ctx, p, "")
}

// PutProductVia is PutProduct with the surface named explicitly.
//
// The surface is what the merge consults. A caller that does not know it passes
// "", which Merge reads as "carries everything", because a record from an
// unknown surface asserting absence is the one case where guessing wrong deletes
// data.
func (s *Store) PutProductVia(ctx context.Context, p Product, opID string) error {
	if opID == "" {
		if n := len(p.Envelope.Surfaces); n > 0 {
			opID = p.Envelope.Surfaces[n-1]
		}
	}
	return s.PutProductWith(ctx, p, OpByID(opID))
}

// PutProductWith is PutProductVia with the surface already resolved, and
// possibly narrowed.
//
// `amz crawl` without --with-text calls it with op.Without("description",
// "reviews"), because that crawl read those fields and then dropped them, and a
// read that dropped a field is not evidence the field is gone. See Op.Without.
func (s *Store) PutProductWith(ctx context.Context, p Product, op *Op) error {
	if p.Marketplace == "" {
		return fmt.Errorf("product %s: %w", p.ASIN, ErrUnscoped)
	}
	if p.ASIN == "" {
		return errors.New("product has no ASIN, so it has no key")
	}
	opID := ""
	if op != nil {
		opID = op.ID
		if opID == "" {
			opID = op.Name
		}
	}
	if opID == "" {
		if n := len(p.Envelope.Surfaces); n > 0 {
			opID = p.Envelope.Surfaces[n-1]
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var prevJSON, prevOps string
	err = tx.QueryRowContext(ctx,
		"SELECT json, ops FROM product WHERE marketplace=? AND asin=?", p.Marketplace, p.ASIN).
		Scan(&prevJSON, &prevOps)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	merged := p
	if prevJSON != "" {
		var prev Product
		if uerr := json.Unmarshal([]byte(prevJSON), &prev); uerr == nil {
			merged = Merge(prev, p, op)
		}
	}

	uri := ""
	if r := merged.Ref(); r != nil {
		uri = r.URI
	}
	brand := ""
	if merged.Brand != nil {
		brand = merged.Brand.Name
	}
	avail := ""
	if merged.Offer != nil {
		avail = merged.Offer.Availability
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO product (marketplace, asin, uri, title, brand, rating, ratings_count, availability, ops, json, fetched_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(marketplace, asin) DO UPDATE SET
  uri=excluded.uri, title=excluded.title, brand=excluded.brand, rating=excluded.rating,
  ratings_count=excluded.ratings_count, availability=excluded.availability,
  ops=excluded.ops, json=excluded.json, fetched_at=excluded.fetched_at`,
		merged.Marketplace, merged.ASIN, uri, merged.Title, brand,
		nullFloat(merged.Rating), nullInt(merged.RatingsCount), avail,
		mergeOps(prevOps, opID), jsonOf(merged), at(merged.FetchedAt)); err != nil {
		return err
	}

	// The series rows come off the incoming record and not the merged one,
	// because they are observations and the merge is a view. Appending the
	// merged price would file the previous crawl's number under this crawl's
	// timestamp the first time a light read arrived with no price at all.
	obs := at(p.FetchedAt)
	if obs == "" {
		obs = at(time.Now())
	}
	if p.Offer != nil {
		seller := ""
		if p.Offer.SoldBy != nil {
			seller = p.Offer.SoldBy.ID
		}
		for _, m := range []struct {
			kind string
			v    *Money
		}{{"current", p.Offer.Price}, {"list", p.Offer.ListPrice}} {
			if m.v == nil {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO price (marketplace, asin, amount, currency, kind, seller_id, observed_at) VALUES (?,?,?,?,?,?,?)",
				p.Marketplace, p.ASIN, m.v.Value, m.v.Currency, m.kind, seller, obs); err != nil {
				return err
			}
		}
	}
	for _, r := range p.Ranks {
		node := ""
		if r.Node != nil {
			node = r.Node.ID
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO "rank" (marketplace, asin, node_id, category, "rank", observed_at) VALUES (?,?,?,?,?,?)`,
			p.Marketplace, p.ASIN, node, r.Category, r.Rank, obs); err != nil {
			return err
		}
	}

	if s.fts {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM product_fts WHERE asin=? AND marketplace=?", merged.ASIN, merged.Marketplace); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO product_fts (asin, marketplace, title, brand, description, bullets) VALUES (?,?,?,?,?,?)",
			merged.ASIN, merged.Marketplace, merged.Title, brand,
			merged.Description, strings.Join(merged.Bullets, "\n")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// mergeOps keeps the set of surfaces this row has been read from, in order.
//
// It is a set and not the latest, because "this row has been read fully once and
// lightly four times" is the question the merge policy answers and a single
// latest value cannot answer it.
func mergeOps(prev, add string) string {
	if add == "" {
		return prev
	}
	for _, p := range strings.Split(prev, ",") {
		if p == add {
			return prev
		}
	}
	if prev == "" {
		return add
	}
	return prev + "," + add
}

// GetProduct reads one product back, exactly as it was stored.
func (s *Store) GetProduct(ctx context.Context, marketplace, asin string) (Product, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		"SELECT json FROM product WHERE marketplace=? AND asin=?", marketplace, asin).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, sql.ErrNoRows
	}
	if err != nil {
		return Product{}, err
	}
	var p Product
	return p, json.Unmarshal([]byte(raw), &p)
}

// LookupJSON returns the stored record for a URI or ASIN verbatim.
//
// Verbatim matters. The json column is the full record and the typed columns are
// an index over it, so a lookup that rebuilt the record from the columns would
// return less than was stored and would silently improve every time a column was
// added. This returns the bytes.
func (s *Store) LookupJSON(ctx context.Context, marketplace, key string) (string, error) {
	if asin := ExtractASIN(key); asin != "" {
		key = asin
	}
	var raw string
	err := s.db.QueryRowContext(ctx,
		"SELECT json FROM product WHERE asin=? AND (?='' OR marketplace=?) ORDER BY fetched_at DESC LIMIT 1",
		key, marketplace, marketplace).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		// Not a product. Every other table is keyed on its own id space and a
		// lookup that only knew about products would be wrong about categories.
		for _, q := range []string{
			"SELECT json FROM category WHERE node_id=? AND (?='' OR marketplace=?) LIMIT 1",
			"SELECT json FROM seller WHERE seller_id=? AND ?=? LIMIT 1",
			"SELECT json FROM brand WHERE slug=? AND ?=? LIMIT 1",
			"SELECT json FROM author WHERE slug=? AND ?=? LIMIT 1",
			"SELECT json FROM review WHERE review_id=? AND ?=? LIMIT 1",
		} {
			a, b := marketplace, marketplace
			if !strings.Contains(q, "marketplace=?") {
				a, b = "", ""
			}
			if err := s.db.QueryRowContext(ctx, q, key, a, b).Scan(&raw); err == nil {
				return raw, nil
			}
		}
		return "", sql.ErrNoRows
	}
	return raw, err
}

// PutReview upserts a review.
//
// A review id is global, so it stays the primary key. The marketplace is carried
// alongside because the product it is about is scoped, and a join back to
// product needs both halves of that key.
func (s *Store) PutReview(ctx context.Context, r Review) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO review (review_id, marketplace, asin, rating, json, fetched_at) VALUES (?,?,?,?,?,?)
ON CONFLICT(review_id) DO UPDATE SET marketplace=excluded.marketplace, asin=excluded.asin,
  rating=excluded.rating, json=excluded.json, fetched_at=excluded.fetched_at`,
		r.ReviewID, r.Marketplace, r.ASIN, r.Rating, jsonOf(r), at(r.FetchedAt))
	return err
}

// PutQA upserts a question and its answers.
func (s *Store) PutQA(ctx context.Context, q QA) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO qa (qa_id, marketplace, asin, json, fetched_at) VALUES (?,?,?,?,?)
ON CONFLICT(qa_id) DO UPDATE SET marketplace=excluded.marketplace, asin=excluded.asin,
  json=excluded.json, fetched_at=excluded.fetched_at`,
		q.QAID, q.Marketplace, q.ASIN, jsonOf(q), at(q.FetchedAt))
	return err
}

// PutOffer upserts one offer from the offer listing page.
func (s *Store) PutOffer(ctx context.Context, marketplace, asin string, o Offer, fetched time.Time) error {
	if marketplace == "" {
		return fmt.Errorf("offer for %s: %w", asin, ErrUnscoped)
	}
	seller := ""
	if o.SoldBy != nil {
		seller = o.SoldBy.ID
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO offer (marketplace, asin, seller_id, condition, json, fetched_at) VALUES (?,?,?,?,?,?)
ON CONFLICT(marketplace, asin, seller_id, condition) DO UPDATE SET
  json=excluded.json, fetched_at=excluded.fetched_at`,
		marketplace, asin, seller, o.Condition, jsonOf(o), at(fetched))
	if err != nil {
		return err
	}
	if o.Price != nil {
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO price (marketplace, asin, amount, currency, kind, seller_id, observed_at) VALUES (?,?,?,?,?,?,?)",
			marketplace, asin, o.Price.Value, o.Price.Currency, "offer", seller, at(fetched))
	}
	return err
}

// PutOfferListing writes one row from the competing offer list.
//
// It is separate from PutOffer because the two are different records rather than
// two spellings of one. Offer is the buy box, built out of a dozen parsed money
// fields; OfferListing is a row on the offer listing page, flat, with the seller
// named inline. Flattening one into the other would mean deciding which of its
// fields to drop, and the answer would be most of them.
func (s *Store) PutOfferListing(ctx context.Context, l OfferListing) error {
	if l.Marketplace == "" {
		return fmt.Errorf("offer for %s: %w", l.ASIN, ErrUnscoped)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO offer (marketplace, asin, seller_id, condition, json, fetched_at) VALUES (?,?,?,?,?,?)
ON CONFLICT(marketplace, asin, seller_id, condition) DO UPDATE SET
  json=excluded.json, fetched_at=excluded.fetched_at`,
		l.Marketplace, l.ASIN, l.SellerID, l.Condition, jsonOf(l), at(l.FetchedAt)); err != nil {
		return err
	}
	if l.Price == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO price (marketplace, asin, amount, currency, kind, seller_id, observed_at) VALUES (?,?,?,?,?,?,?)",
		l.Marketplace, l.ASIN, l.Price, l.Currency, "offer", l.SellerID, at(l.FetchedAt))
	return err
}

// PutChartEntry appends one chart placement.
//
// Append only, because a chart is a fact about a moment. Overwriting last week's
// number three with this week's would answer "what is number three today" and
// destroy the only interesting question a chart supports, which is what moved.
func (s *Store) PutChartEntry(ctx context.Context, e BestsellerEntry) error {
	if e.Marketplace == "" {
		return fmt.Errorf("chart entry %s: %w", e.ASIN, ErrUnscoped)
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO chart_entry (marketplace, list_type, node_id, rank, asin, json, observed_at) VALUES (?,?,?,?,?,?,?)",
		e.Marketplace, e.ListType, e.NodeID, e.Rank, e.ASIN, jsonOf(e), at(e.FetchedAt))
	return err
}

// PutBestseller is the v0.2 name for PutChartEntry.
//
// Deprecated: use PutChartEntry. Kept because the crawl calls it and renaming a
// method and its callers in the same commit as a schema change makes a bisect
// harder than it needs to be.
func (s *Store) PutBestseller(ctx context.Context, e BestsellerEntry) error {
	return s.PutChartEntry(ctx, e)
}

// PutCategory upserts a browse node.
func (s *Store) PutCategory(ctx context.Context, c Category) error {
	mkt := c.Envelope.Marketplace
	if mkt == "" {
		return fmt.Errorf("category %s: %w", c.NodeID, ErrUnscoped)
	}
	uri := ""
	if r := c.Ref(); r != nil {
		uri = r.URI
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO category (marketplace, node_id, uri, name, json, fetched_at) VALUES (?,?,?,?,?,?)
ON CONFLICT(marketplace, node_id) DO UPDATE SET uri=excluded.uri, name=excluded.name,
  json=excluded.json, fetched_at=excluded.fetched_at`,
		mkt, c.NodeID, uri, c.Name, jsonOf(c), at(c.FetchedAt))
	return err
}

// PutEdge writes one graph edge.
//
// The key is (src, predicate, dst, via) rather than the triple alone, because
// the same pair of nodes is asserted by different surfaces and which surface
// said it is part of the claim. A product's rank line and a browse page's link
// both say a product is in a category, and only one of them names a node id.
func (s *Store) PutEdge(ctx context.Context, e Edge) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO edge (src, predicate, dst, via, sponsored, position, json, observed_at) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(src, predicate, dst, via) DO UPDATE SET sponsored=excluded.sponsored,
  position=excluded.position, json=excluded.json, observed_at=excluded.observed_at`,
		e.Src, e.Predicate, e.Dst, e.Via, boolInt(e.Sponsored), e.Position, jsonOf(e), at(e.ObservedAt))
	return err
}

// Observation is one point in a price or rank series.
type Observation struct {
	At       string  `json:"at"`
	Kind     string  `json:"kind,omitempty"`
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
	Rank     int     `json:"rank,omitempty"`
	Category string  `json:"category,omitempty"`
	NodeID   string  `json:"node_id,omitempty"`
	Seller   string  `json:"seller_id,omitempty"`
}

// PriceSeries returns every price observed for one ASIN, oldest first.
func (s *Store) PriceSeries(ctx context.Context, marketplace, asin string) ([]Observation, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT observed_at, kind, amount, currency, seller_id FROM price WHERE asin=? AND (?='' OR marketplace=?) ORDER BY observed_at",
		asin, marketplace, marketplace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Observation
	for rows.Next() {
		var o Observation
		if err := rows.Scan(&o.At, &o.Kind, &o.Amount, &o.Currency, &o.Seller); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RankSeries returns every rank observed for one ASIN, oldest first.
func (s *Store) RankSeries(ctx context.Context, marketplace, asin string) ([]Observation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT observed_at, "rank", category, node_id FROM "rank" WHERE asin=? AND (?='' OR marketplace=?) ORDER BY observed_at`,
		asin, marketplace, marketplace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Observation
	for rows.Next() {
		var o Observation
		if err := rows.Scan(&o.At, &o.Rank, &o.Category, &o.NodeID); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// FTSHit is one full text search result.
type FTSHit struct {
	ASIN        string `json:"asin"`
	Marketplace string `json:"marketplace"`
	Title       string `json:"title,omitempty"`
	Brand       string `json:"brand,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
}

// ErrNoFTS is returned when the build that created the file had no FTS5.
var ErrNoFTS = errors.New("this database has no full text index, because the build that created it had no fts5")

// Find runs a full text query over the stored products.
func (s *Store) Find(ctx context.Context, query string, limit int) ([]FTSHit, error) {
	if !s.fts {
		return nil, ErrNoFTS
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT asin, marketplace, title, brand, snippet(product_fts, 2, '[', ']', '...', 12)
FROM product_fts WHERE product_fts MATCH ? ORDER BY rank LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FTSHit
	for rows.Next() {
		var h FTSHit
		if err := rows.Scan(&h.ASIN, &h.Marketplace, &h.Title, &h.Brand, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ErrNotReadOnly is returned when `amz query` is handed something that writes.
//
// The store is a record of what was read from Amazon, and it took hours of
// somebody's bandwidth and Amazon's to fill. A typo in an interactive SQL prompt
// should not be able to empty it, so the query command opens a read only view of
// the same file and this check is the message that explains why.
var ErrNotReadOnly = errors.New("amz query is read only")

// Query runs a read only SQL query and returns rows as JSON objects.
func (s *Store) Query(ctx context.Context, query string) ([]map[string]any, error) {
	if w := writeVerb(query); w != "" {
		return nil, fmt.Errorf("%w, and this starts with %s. use sqlite3 on %s if you mean it", ErrNotReadOnly, w, s.path)
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = normalizeCell(cells[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// writeVerb names the statement that would write, or "" for a read.
//
// It is a keyword check and not a parser, which means it is conservative in the
// direction that matters: it can refuse a read that mentions DELETE in a string
// literal, and the answer to that is to use sqlite3. It cannot let a write
// through, which is the failure that costs a crawl.
func writeVerb(q string) string {
	for _, line := range strings.Split(q, ";") {
		f := strings.Fields(strings.ToUpper(line))
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "REPLACE", "TRUNCATE", "VACUUM", "PRAGMA", "ATTACH", "DETACH", "REINDEX":
			return f[0]
		}
	}
	return ""
}

// normalizeCell turns driver values into things json.Marshal writes plainly.
func normalizeCell(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(stamp)
	default:
		return v
	}
}

// Enqueue adds one frontier item, ignoring a URL that is already there.
//
// The uniqueness is on the URL in the schema rather than checked here, because
// two crawls of overlapping seed sets is the normal case and a check-then-insert
// would race. This is what makes a killed crawl restartable: re-seeding is free.
func (s *Store) Enqueue(ctx context.Context, url, entity string, priority int) error {
	return s.EnqueueDepth(ctx, url, entity, priority, 0)
}

// EnqueueDepth is Enqueue with the rail depth this URL was reached at.
func (s *Store) EnqueueDepth(ctx context.Context, url, entity string, priority, depth int) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO queue (url, entity, priority, depth) VALUES (?,?,?,?) ON CONFLICT(url) DO NOTHING",
		url, entity, priority, depth)
	return err
}

// NextBatch claims up to n pending items and marks them active.
//
// The claim is one UPDATE with a RETURNING clause rather than a SELECT followed
// by an UPDATE, so two crawls against the same store cannot hand the same URL to
// both. That is also what makes a kill safe: an item that was active when the
// process died is visible as active and is recovered by Recover below, rather
// than being lost or silently re-fetched.
func (s *Store) NextBatch(ctx context.Context, n int) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
UPDATE queue SET status='active', claimed_at=?, attempts=attempts+1
WHERE id IN (SELECT id FROM queue WHERE status='pending' ORDER BY priority DESC, id LIMIT ?)
RETURNING id, url, entity, priority, depth, status, attempts`, at(time.Now()), n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []QueueItem
	for rows.Next() {
		var it QueueItem
		if err := rows.Scan(&it.ID, &it.URL, &it.Entity, &it.Priority, &it.Depth, &it.Status, &it.Attempts); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Recover returns items left active by a crawl that died, putting them back.
//
// It returns the count so the crawl can say what it picked up rather than
// silently repairing. An item that has been attempted too many times is parked as
// 'stuck' instead, because an infinite retry on a page that always fails is how a
// resumable crawl becomes a crawl that never finishes.
func (s *Store) Recover(ctx context.Context, maxAttempts int) (int, error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE queue SET status='stuck' WHERE status='active' AND attempts>=?", maxAttempts); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE queue SET status='pending' WHERE status='active'")
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// MarkStatus updates one queue item, recording why if it failed.
func (s *Store) MarkStatus(ctx context.Context, id int64, status string, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	done := ""
	if status == "done" || status == "error" || status == "skipped" {
		done = at(time.Now())
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE queue SET status=?, error=?, done_at=? WHERE id=?", status, msg, done, id)
	return err
}

// QueueCounts returns the number of items in each status.
func (s *Store) QueueCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT status, count(*) FROM queue GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// PendingCount returns the number of pending queue items.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	c, err := s.QueueCounts(ctx)
	return c["pending"], err
}

// storeTables is the list Stats walks. It is written out rather than read from
// sqlite_master so a new table has to be added here deliberately, and so
// product_fts and the FTS5 shadow tables do not show up as rows anybody counts.
var storeTables = []string{
	"product", "price", "rank", "chart_entry", "edge",
	"review", "qa", "offer", "category", "brand", "seller", "author", "queue",
}

// Stats returns row counts per table.
func (s *Store) Stats(ctx context.Context) ([]map[string]any, error) {
	var out []map[string]any
	for _, t := range storeTables {
		var n int64
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM "`+t+`"`).Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"table": t, "rows": n})
	}
	return out, nil
}

// Vacuum compacts the database.
func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
