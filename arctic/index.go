package arctic

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Index is the local catalog of processed shards. It is a pure-Go SQLite
// database in WAL mode, one row per shard, so stats and coverage queries never
// re-read the Parquet.
type Index struct {
	db *sql.DB
}

// StatRow is one rollup line returned by Stats.
type StatRow struct {
	Group  string // month "YYYY-MM", type, or subreddit/user name
	Type   string // empty when grouping by type only
	Shards int64
	Count  int64
	Bytes  int64
}

// OpenIndex opens or creates the index at path and ensures the schema exists.
func OpenIndex(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	idx := &Index{db: db}
	if err := idx.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *Index) migrate() error {
	_, err := idx.db.Exec(`
CREATE TABLE IF NOT EXISTS shards (
    id          INTEGER PRIMARY KEY,
    type        TEXT NOT NULL,
    year        INTEGER NOT NULL DEFAULT 0,
    month       INTEGER NOT NULL DEFAULT 0,
    entity      TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL UNIQUE,
    records     INTEGER NOT NULL DEFAULT 0,
    bytes       INTEGER NOT NULL DEFAULT 0,
    written_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS shards_by_month ON shards(year, month, type);
CREATE INDEX IF NOT EXISTS shards_by_entity ON shards(entity, type);
`)
	return err
}

// RecordShard upserts one processed shard. entity is empty for monthly dumps and
// the subreddit or user name for per-entity imports; for monthly dumps pass year
// and month instead.
func (idx *Index) RecordShard(t Type, year, month int, entity, path string, records, bytes int64) error {
	_, err := idx.db.Exec(`
INSERT INTO shards (type, year, month, entity, path, records, bytes, written_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
    type=excluded.type, year=excluded.year, month=excluded.month,
    entity=excluded.entity, records=excluded.records, bytes=excluded.bytes,
    written_at=excluded.written_at`,
		string(t), year, month, entity, path, records, bytes, nowRFC3339())
	return err
}

// Stats returns rollups grouped by "month", "type", or "subreddit" (which also
// covers user entities). Rows are sorted by group.
func (idx *Index) Stats(by string) ([]StatRow, error) {
	var query string
	switch by {
	case "month":
		query = `
SELECT printf('%04d-%02d', year, month) AS g, type,
       COUNT(*), COALESCE(SUM(records),0), COALESCE(SUM(bytes),0)
FROM shards WHERE year > 0
GROUP BY year, month, type
ORDER BY year, month, type`
	case "type":
		query = `
SELECT type AS g, '' AS type2,
       COUNT(*), COALESCE(SUM(records),0), COALESCE(SUM(bytes),0)
FROM shards
GROUP BY type
ORDER BY type`
	case "subreddit", "entity", "user":
		query = `
SELECT entity AS g, type,
       COUNT(*), COALESCE(SUM(records),0), COALESCE(SUM(bytes),0)
FROM shards WHERE entity != ''
GROUP BY entity, type
ORDER BY entity, type`
	default:
		return nil, fmt.Errorf("unknown rollup %q (want month, type, or subreddit)", by)
	}

	rows, err := idx.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StatRow
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.Group, &r.Type, &r.Shards, &r.Count, &r.Bytes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Close releases the database.
func (idx *Index) Close() error { return idx.db.Close() }
