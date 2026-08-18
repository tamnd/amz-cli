package amz

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/tamnd/amz-cli/pkg/graph"
)

// The graph half of the store: writing edges in bulk, reading them back, and
// loading enough of them to walk.
//
// It is a separate file from store.go because the two answer different
// questions. store.go is about records, which merge and have a current state.
// This is about claims, which accumulate and are only ever true of a moment.

// PutEdges writes a batch of edges in one transaction.
//
// One transaction rather than one per edge, because a detail page yields eighty
// of them and eighty transactions against SQLite is eighty fsyncs. An edge that
// fails validation stops the batch: an edge with no timestamp or an unknown
// predicate is a bug in the caller, and writing the other seventy-nine and
// returning success would hide it until somebody queried the graph and found a
// hole in it.
func (s *Store) PutEdges(ctx context.Context, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO edge (src, predicate, dst, via, sponsored, position, json, observed_at) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(src, predicate, dst, via) DO UPDATE SET sponsored=excluded.sponsored,
  position=excluded.position, json=excluded.json, observed_at=excluded.observed_at`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range edges {
		if err := e.Valid(); err != nil {
			return fmt.Errorf("refusing the batch: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, e.Src, e.Predicate, e.Dst, e.Via,
			boolInt(e.Sponsored), e.Position, jsonOf(e), at(e.ObservedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EdgeQuery is what to read back out of the edge table.
type EdgeQuery struct {
	// Src and Dst filter by endpoint. Both empty reads the whole table.
	Src, Dst string
	// Predicates limits to these. Empty means all sixteen.
	Predicates []string
	// IncludeSponsored puts paid placements back in.
	IncludeSponsored bool
	// Limit caps the rows. Zero means no cap.
	Limit int
}

// Edges reads edges back.
//
// The row is rebuilt from the json column and not from the typed columns, for
// the same reason a product is: the typed columns are an index over the record
// and the record is the json. Props in particular exists only there, so a read
// off the columns would return every edge stripped of what it carried.
func (s *Store) Edges(ctx context.Context, q EdgeQuery) ([]Edge, error) {
	sqlText := "SELECT json FROM edge WHERE 1=1"
	var args []any
	if q.Src != "" {
		sqlText += " AND src=?"
		args = append(args, q.Src)
	}
	if q.Dst != "" {
		sqlText += " AND dst=?"
		args = append(args, q.Dst)
	}
	if !q.IncludeSponsored {
		sqlText += " AND sponsored=0"
	}
	if len(q.Predicates) > 0 {
		sqlText += " AND predicate IN (" + placeholders(len(q.Predicates)) + ")"
		for _, p := range q.Predicates {
			args = append(args, p)
		}
	}
	sqlText += " ORDER BY src, predicate, dst, via"
	if q.Limit > 0 {
		sqlText += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Edge
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e Edge
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LoadGraph reads the whole edge table into memory for a traversal.
//
// In memory, deliberately. A traversal is a handful of hops over a store that
// holds tens of thousands of edges at the sizes this tool crawls at, and doing
// it in SQL would mean a recursive CTE that has to re-express the sponsored
// rule and the symmetric rule in a second language. If somebody ever crawls
// enough of Amazon for this to matter, the fix is a bounded load around the
// start node rather than a query language.
func (s *Store) LoadGraph(ctx context.Context, q EdgeQuery) (*graph.Graph, error) {
	edges, err := s.Edges(ctx, q)
	if err != nil {
		return nil, err
	}
	g := graph.New()
	for _, e := range edges {
		// A stored edge that no longer validates is skipped rather than
		// refused. The batch write above is where invalid edges are stopped;
		// here the data is already on disk and failing the whole read because
		// one row predates a vocabulary change helps nobody.
		_ = g.Add(e)
	}
	return g, nil
}

// EachProduct calls fn for every stored product, oldest first.
//
// A callback rather than a slice, because an export of a large store should not
// need the whole store resident to write the first line of output.
func (s *Store) EachProduct(ctx context.Context, marketplace string, fn func(Product) error) error {
	rows, err := s.db.QueryContext(ctx,
		"SELECT json FROM product WHERE (?='' OR marketplace=?) ORDER BY marketplace, asin",
		marketplace, marketplace)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var p Product
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

// EachReview calls fn for every stored review.
func (s *Store) EachReview(ctx context.Context, marketplace string, fn func(Review) error) error {
	rows, err := s.db.QueryContext(ctx,
		"SELECT json FROM review WHERE (?='' OR marketplace=?) ORDER BY marketplace, asin, review_id",
		marketplace, marketplace)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var r Review
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

// CountEdges is how many edges are stored, for the size warning on a traversal
// and for `amz db stats`.
func (s *Store) CountEdges(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM edge").Scan(&n)
	if err != nil && !errorsIsNoRows(err) {
		return 0, err
	}
	return n, nil
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
