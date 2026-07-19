//go:build duckdb

package arctic

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/klauspost/compress/zstd"
	_ "github.com/marcboeker/go-duckdb"
)

// processDuckDB decodes the .zst and turns each cfg.ChunkLines-sized run of
// JSONL into a Parquet shard with a single DuckDB COPY. Each chunk is written
// to one temporary JSONL file, converted, and deleted before the next chunk is
// filled, so disk never holds more than one uncompressed chunk plus its shard
// at a time. That matters for the comments dumps, whose uncompressed JSONL runs
// to hundreds of GB and would overrun the disk if staged all at once. read_json
// with ignore_errors mirrors the Go engine's skip-on-bad-line behavior. This
// path pulls a cgo dependency and is never in the default build.
func processDuckDB(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	cb func(done int64)) (ProcessResult, error) {

	chunkLines := cfg.ChunkLines
	if chunkLines <= 0 {
		chunkLines = DefaultChunkLines
	}

	work, err := os.MkdirTemp(cfg.WorkDir, "duckchunk-")
	if err != nil {
		work, err = os.MkdirTemp("", "duckchunk-")
		if err != nil {
			return ProcessResult{}, err
		}
	}
	defer func() { _ = os.RemoveAll(work) }()

	zstdDecoderSem.Lock()
	defer zstdDecoderSem.Unlock()
	f, err := os.Open(zstPath)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("open zst: %w", err)
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(f, zstd.WithDecoderMaxWindow(1<<31))
	if err != nil {
		return ProcessResult{}, fmt.Errorf("zstd reader: %w", err)
	}
	defer dec.Close()

	var res ProcessResult
	shardN := 0

	scanner := bufio.NewScanner(dec)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	var (
		chunkFile   *os.File
		chunkWriter *bufio.Writer
		chunkPath   string
		chunkIdx    int
		lineInChunk int
		lineCount   int64
	)
	openChunk := func() error {
		chunkPath = filepath.Join(work, fmt.Sprintf("chunk_%03d.jsonl", chunkIdx))
		chunkIdx++
		cf, e := os.Create(chunkPath)
		if e != nil {
			return e
		}
		chunkFile = cf
		chunkWriter = bufio.NewWriterSize(cf, 8*1024*1024)
		lineInChunk = 0
		return nil
	}

	// convertChunk finalizes the current chunk file, turns it into one Parquet
	// shard, and removes the JSONL so it never accumulates. An empty chunk (a
	// clean chunk boundary at EOF) is just dropped.
	convertChunk := func() error {
		if err := chunkWriter.Flush(); err != nil {
			return err
		}
		if err := chunkFile.Close(); err != nil {
			return err
		}
		fi, statErr := os.Stat(chunkPath)
		if statErr != nil || fi.Size() == 0 {
			_ = os.Remove(chunkPath)
			return nil
		}
		shardPath := pathFn(shardN)
		if err := os.MkdirAll(filepath.Dir(shardPath), 0o755); err != nil {
			return err
		}
		rows, size, err := duckConvert(ctx, cfg, chunkPath, t, shardPath)
		if err != nil {
			return err
		}
		if err := ValidateParquet(shardPath); err != nil {
			_ = os.Remove(shardPath)
			return fmt.Errorf("validate shard %d: %w", shardN, err)
		}
		res.Shards++
		res.Records += rows
		res.Bytes += size
		shardN++
		_ = os.Remove(chunkPath)
		return nil
	}

	if err := openChunk(); err != nil {
		return res, err
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		line := scanner.Bytes()
		lineCount++
		if cb != nil && lineCount%200000 == 0 {
			cb(lineCount)
		}
		if len(line) == 0 || line[0] != '{' {
			res.SkippedLines++
			continue
		}
		_, _ = chunkWriter.Write(line)
		_ = chunkWriter.WriteByte('\n')
		lineInChunk++
		if lineInChunk >= chunkLines {
			if err := convertChunk(); err != nil {
				return res, err
			}
			if err := openChunk(); err != nil {
				return res, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return res, fmt.Errorf("scan jsonl: %w", err)
	}
	// Convert the trailing partial chunk (a no-op when the last boundary landed
	// exactly at EOF).
	if err := convertChunk(); err != nil {
		return res, err
	}
	if cb != nil {
		cb(lineCount)
	}
	return res, nil
}

func duckConvert(ctx context.Context, cfg Config, chunkPath string, t Type, shardPath string) (int64, int64, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return 0, 0, fmt.Errorf("duckdb open: %w", err)
	}
	defer func() { _ = db.Close() }()

	threads := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("SET threads = %d", threads)); err != nil {
		return 0, 0, fmt.Errorf("duckdb set threads: %w", err)
	}
	if cfg.DuckDBMemoryMB > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET memory_limit='%dMB'", cfg.DuckDBMemoryMB)); err != nil {
			return 0, 0, fmt.Errorf("duckdb set memory_limit: %w", err)
		}
	}

	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	selectCols, readCols := duckComments, duckCommentsRead
	if t == TypeSubmissions {
		selectCols, readCols = duckSubmissions, duckSubmissionsRead
	}
	copySQL := fmt.Sprintf(`COPY (
SELECT %s
FROM read_json('%s', format='newline_delimited', columns=%s,
    maximum_object_size=2097152, ignore_errors=true)
) TO '%s' (FORMAT PARQUET, COMPRESSION ZSTD, ROW_GROUP_SIZE 131072)`,
		selectCols, esc(chunkPath), readCols, esc(shardPath))

	if _, err := db.ExecContext(ctx, copySQL); err != nil {
		_ = os.Remove(shardPath)
		return 0, 0, fmt.Errorf("duckdb copy: %w", err)
	}
	fi, err := os.Stat(shardPath)
	if err != nil {
		return 0, 0, err
	}
	var rows int64
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')", esc(shardPath))).Scan(&rows); err != nil {
		return 0, 0, fmt.Errorf("duckdb count rows: %w", err)
	}
	return rows, fi.Size(), nil
}

const duckCommentsRead = `{
    id: 'VARCHAR', author: 'VARCHAR', subreddit: 'VARCHAR',
    body: 'VARCHAR', score: 'VARCHAR', created_utc: 'VARCHAR',
    link_id: 'VARCHAR', parent_id: 'VARCHAR',
    distinguished: 'VARCHAR', author_flair_text: 'VARCHAR'
}`

const duckSubmissionsRead = `{
    id: 'VARCHAR', author: 'VARCHAR', subreddit: 'VARCHAR',
    title: 'VARCHAR', selftext: 'VARCHAR', score: 'VARCHAR',
    created_utc: 'VARCHAR', num_comments: 'VARCHAR',
    url: 'VARCHAR', over_18: 'VARCHAR',
    link_flair_text: 'VARCHAR', author_flair_text: 'VARCHAR'
}`

const duckComments = `
    TRY_CAST(id AS VARCHAR) AS id,
    TRY_CAST(author AS VARCHAR) AS author,
    TRY_CAST(subreddit AS VARCHAR) AS subreddit,
    TRY_CAST(body AS VARCHAR) AS body,
    COALESCE(TRY_CAST(score AS BIGINT), 0) AS score,
    COALESCE(TRY_CAST(created_utc AS BIGINT), 0) AS created_utc,
    CASE WHEN created_utc IS NOT NULL THEN epoch_ms(CAST(created_utc AS BIGINT) * 1000) ELSE NULL END AS created_at,
    CAST(LENGTH(COALESCE(CAST(body AS VARCHAR), '')) AS INTEGER) AS body_length,
    TRY_CAST(link_id AS VARCHAR) AS link_id,
    TRY_CAST(parent_id AS VARCHAR) AS parent_id,
    TRY_CAST(distinguished AS VARCHAR) AS distinguished,
    TRY_CAST(author_flair_text AS VARCHAR) AS author_flair_text`

const duckSubmissions = `
    TRY_CAST(id AS VARCHAR) AS id,
    TRY_CAST(author AS VARCHAR) AS author,
    TRY_CAST(subreddit AS VARCHAR) AS subreddit,
    TRY_CAST(title AS VARCHAR) AS title,
    TRY_CAST(selftext AS VARCHAR) AS selftext,
    COALESCE(TRY_CAST(score AS BIGINT), 0) AS score,
    COALESCE(TRY_CAST(created_utc AS BIGINT), 0) AS created_utc,
    CASE WHEN created_utc IS NOT NULL THEN epoch_ms(CAST(created_utc AS BIGINT) * 1000) ELSE NULL END AS created_at,
    CAST(LENGTH(COALESCE(CAST(title AS VARCHAR), '')) AS INTEGER) AS title_length,
    COALESCE(TRY_CAST(num_comments AS BIGINT), 0) AS num_comments,
    TRY_CAST(url AS VARCHAR) AS url,
    COALESCE(TRY_CAST(over_18 AS BOOLEAN), false) AS over_18,
    TRY_CAST(link_flair_text AS VARCHAR) AS link_flair_text,
    TRY_CAST(author_flair_text AS VARCHAR) AS author_flair_text`
