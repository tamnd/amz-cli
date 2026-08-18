package amz

// The schema, and the three things about it worth reading before the columns.
//
// First, every table whose id space is per marketplace carries the marketplace
// in its primary key. B075F5X8BR on amazon.com and B075F5X8BR on amazon.co.uk
// are two listings with different prices, different sellers and sometimes
// different products, and the v0.2 schema keyed products on the ASIN alone, so a
// crawl of both storefronts kept whichever ran last and reported one row. That
// is the worst kind of wrong: no error, no warning, and a number that looks like
// an answer. Browse nodes are scoped for the same reason, 172282 being
// Electronics on .com and something else on .de.
//
// Sellers, brands and authors are not scoped, and that is deliberate rather than
// an oversight. A merchant id is global across Amazon: A2L77EE7U53NWQ is one
// company everywhere it trades. What varies by storefront is the feedback and
// the storefront page, and those live in the record, which carries the
// marketplace it was measured in. See notes/Spec/3007/04_graph.md section 1.
//
// Second, the full record lives in the json column and the typed columns are an
// index over it, never a subset of it. A field this build does not know about
// still round trips, and a field this build reads is queryable without a JSON
// function in every WHERE clause. `amz lookup` returns the json column verbatim,
// so a record read by v0.3.0 and a record read by v0.9.0 are the same bytes.
//
// Third, price, rank and chart_entry are append only. They have no primary key
// that a second observation can collide with, so nothing in the merge policy can
// reach them and a merge bug cannot destroy history. A price that fell and rose
// again is three rows, and the question "what did this cost in March" has an
// answer that no later crawl can take away.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS product (
  marketplace TEXT NOT NULL,
  asin        TEXT NOT NULL,
  uri         TEXT NOT NULL DEFAULT '',
  title       TEXT,
  brand       TEXT,
  rating      REAL,
  ratings_count INTEGER,
  availability  TEXT,
  -- ops is the surface that produced this row, and read_at is when. Together
  -- they are what the merge policy consults: a light read has no A+ content and
  -- never had, so its absence is not a deletion.
  ops         TEXT NOT NULL DEFAULT '',
  json        TEXT NOT NULL,
  fetched_at  TEXT NOT NULL,
  PRIMARY KEY (marketplace, asin)
);
CREATE INDEX IF NOT EXISTS product_brand_idx ON product (brand);
CREATE INDEX IF NOT EXISTS product_uri_idx   ON product (uri);

-- Append only. No primary key, on purpose.
CREATE TABLE IF NOT EXISTS price (
  marketplace TEXT NOT NULL,
  asin        TEXT NOT NULL,
  amount      REAL,
  currency    TEXT,
  -- kind separates the current price from the list price and from a deal price,
  -- because a row that averaged them would describe a product nobody was sold.
  kind        TEXT NOT NULL DEFAULT 'current',
  seller_id   TEXT,
  observed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS price_asin_idx ON price (marketplace, asin, observed_at);

-- Append only.
CREATE TABLE IF NOT EXISTS rank (
  marketplace TEXT NOT NULL,
  asin        TEXT NOT NULL,
  node_id     TEXT NOT NULL DEFAULT '',
  category    TEXT,
  rank        INTEGER,
  observed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS rank_asin_idx ON rank (marketplace, asin, observed_at);

-- Append only. Chart membership is a fact about a moment: a product that was
-- number 3 last Tuesday still was, whatever it is today.
CREATE TABLE IF NOT EXISTS chart_entry (
  marketplace TEXT NOT NULL,
  list_type   TEXT NOT NULL,
  node_id     TEXT NOT NULL DEFAULT '',
  rank        INTEGER,
  asin        TEXT NOT NULL,
  json        TEXT NOT NULL,
  observed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS chart_entry_list_idx ON chart_entry (marketplace, list_type, node_id, observed_at);
CREATE INDEX IF NOT EXISTS chart_entry_asin_idx ON chart_entry (marketplace, asin);

-- The graph. Every edge carries the surface that asserted it and the time it was
-- read, because on Amazon almost every edge is temporary. sponsored is a first
-- class property and the default traversal excludes sponsored edges.
CREATE TABLE IF NOT EXISTS edge (
  src        TEXT NOT NULL,
  predicate  TEXT NOT NULL,
  dst        TEXT NOT NULL,
  via        TEXT NOT NULL DEFAULT '',
  sponsored  INTEGER NOT NULL DEFAULT 0,
  position   INTEGER,
  json       TEXT NOT NULL DEFAULT '{}',
  observed_at TEXT NOT NULL,
  PRIMARY KEY (src, predicate, dst, via)
);
CREATE INDEX IF NOT EXISTS edge_dst_idx ON edge (dst, predicate);

CREATE TABLE IF NOT EXISTS review (
  review_id   TEXT PRIMARY KEY,
  marketplace TEXT NOT NULL DEFAULT '',
  asin        TEXT NOT NULL DEFAULT '',
  rating      REAL,
  json        TEXT NOT NULL,
  fetched_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS review_asin_idx ON review (marketplace, asin);

CREATE TABLE IF NOT EXISTS qa (
  qa_id       TEXT PRIMARY KEY,
  marketplace TEXT NOT NULL DEFAULT '',
  asin        TEXT NOT NULL DEFAULT '',
  json        TEXT NOT NULL,
  fetched_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS qa_asin_idx ON qa (marketplace, asin);

CREATE TABLE IF NOT EXISTS offer (
  marketplace TEXT NOT NULL,
  asin        TEXT NOT NULL,
  seller_id   TEXT NOT NULL DEFAULT '',
  condition   TEXT,
  json        TEXT NOT NULL,
  fetched_at  TEXT NOT NULL,
  PRIMARY KEY (marketplace, asin, seller_id, condition)
);

CREATE TABLE IF NOT EXISTS category (
  marketplace TEXT NOT NULL,
  node_id     TEXT NOT NULL,
  uri         TEXT NOT NULL DEFAULT '',
  name        TEXT,
  json        TEXT NOT NULL,
  fetched_at  TEXT NOT NULL,
  PRIMARY KEY (marketplace, node_id)
);

CREATE TABLE IF NOT EXISTS brand (
  slug       TEXT PRIMARY KEY,
  uri        TEXT NOT NULL DEFAULT '',
  name       TEXT,
  json       TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS seller (
  seller_id  TEXT PRIMARY KEY,
  uri        TEXT NOT NULL DEFAULT '',
  name       TEXT,
  json       TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS author (
  slug       TEXT PRIMARY KEY,
  uri        TEXT NOT NULL DEFAULT '',
  name       TEXT,
  json       TEXT NOT NULL,
  fetched_at TEXT NOT NULL
);

-- The frontier. url is unique so a seed enqueued twice is one row, which is what
-- makes a killed crawl restartable without duplication. attempts and error are
-- on the row rather than in a log, because the answer to "why did this stall" has
-- to survive the process that stalled.
CREATE TABLE IF NOT EXISTS queue (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  url       TEXT NOT NULL UNIQUE,
  entity    TEXT NOT NULL DEFAULT '',
  priority  INTEGER NOT NULL DEFAULT 0,
  depth     INTEGER NOT NULL DEFAULT 0,
  status    TEXT NOT NULL DEFAULT 'pending',
  attempts  INTEGER NOT NULL DEFAULT 0,
  error     TEXT NOT NULL DEFAULT '',
  claimed_at TEXT NOT NULL DEFAULT '',
  done_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS queue_status_idx ON queue (status, priority DESC, id);
`

// ftsSQL is separate because FTS5 is a compile time option and a build without
// it has to keep working.
//
// The store is still useful without full text search: every other query works
// and only `amz find` is affected. Folding this into schemaSQL would mean a
// missing module took the whole store down, so the failure is caught, recorded
// in meta, and reported by `amz find` as a missing index rather than as an
// empty result set.
const ftsSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS product_fts USING fts5 (
  asin UNINDEXED,
  marketplace UNINDEXED,
  title,
  brand,
  description,
  bullets,
  tokenize = 'unicode61'
);
`

// SchemaVersion is the shape of the tables this build writes.
//
// Version 1 keyed products on the ASIN alone. Version 2 fixed that and was
// written by an external duckdb binary. Version 3 is the same key discipline in
// pure Go SQLite, with price, rank and chart membership split out as append only
// series and the full record kept in a json column.
//
// There is no migration from either. A version 1 database has already lost what
// a migration would need, two storefronts collapsed into one row and the row that
// lost is gone. A version 2 database is a DuckDB file, which is a different
// format that this build has no reader for and will not grow one, because the
// whole point of this milestone is that there is no second engine. Both are
// refused with a message that says to keep the file and start a new one, which is
// honest about the cost and does not pretend a rebuild is a recovery.
const SchemaVersion = 3
