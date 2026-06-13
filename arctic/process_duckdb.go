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

// processDuckDB decodes the .zst, writes chunks of cfg.ChunkLines to temporary
// JSONL files, and turns each chunk into a Parquet shard with a single DuckDB
// COPY. read_json with ignore_errors mirrors the Go engine's skip-on-bad-line
// behavior. This path pulls a cgo dependency and is never in the default build.
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
	defer os.RemoveAll(work)

	zstdDecoderSem.Lock()
	f, err := os.Open(zstPath)
	if err != nil {
		zstdDecoderSem.Unlock()
		return ProcessResult{}, fmt.Errorf("open zst: %w", err)
	}
	dec, err := zstd.NewReader(f, zstd.WithDecoderMaxWindow(1<<31))
	if err != nil {
		f.Close()
		zstdDecoderSem.Unlock()
		return ProcessResult{}, fmt.Errorf("zstd reader: %w", err)
	}

	var res ProcessResult
	shardN := 0
	var chunkPaths []string

	scanner := bufio.NewScanner(dec)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	var (
		chunkFile   *os.File
		chunkWriter *bufio.Writer
		lineInChunk int
		lineCount   int64
	)
	openChunk := func() error {
		p := filepath.Join(work, fmt.Sprintf("chunk_%03d.jsonl", len(chunkPaths)))
		cf, e := os.Create(p)
		if e != nil {
			return e
		}
		chunkPaths = append(chunkPaths, p)
		chunkFile = cf
		chunkWriter = bufio.NewWriterSize(cf, 8*1024*1024)
		return nil
	}
	if err := openChunk(); err != nil {
		dec.Close()
		f.Close()
		zstdDecoderSem.Unlock()
		return res, err
	}

	flush := func() error {
		if lineInChunk == 0 {
			return nil
		}
		if err := chunkWriter.Flush(); err != nil {
			return err
		}
		chunkFile.Close()
		lineInChunk = 0
		return openChunk()
	}

	var loopErr error
	for scanner.Scan() {
		if ctx.Err() != nil {
			loopErr = ctx.Err()
			break
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
		chunkWriter.Write(line)
		chunkWriter.WriteByte('\n')
		lineInChunk++
		if lineInChunk >= chunkLines {
			if err := flush(); err != nil {
				loopErr = err
				break
			}
		}
	}
	if loopErr == nil {
		if err := scanner.Err(); err != nil {
			loopErr = fmt.Errorf("scan jsonl: %w", err)
		} else {
			chunkWriter.Flush()
			chunkFile.Close()
		}
	}
	dec.Close()
	f.Close()
	zstdDecoderSem.Unlock()
	if loopErr != nil {
		return res, loopErr
	}

	for _, cp := range chunkPaths {
		fi, statErr := os.Stat(cp)
		if statErr != nil || fi.Size() == 0 {
			continue
		}
		shardPath := pathFn(shardN)
		if err := os.MkdirAll(filepath.Dir(shardPath), 0o755); err != nil {
			return res, err
		}
		rows, size, err := duckConvert(ctx, cfg, cp, t, shardPath)
		if err != nil {
			return res, err
		}
		if err := ValidateParquet(shardPath); err != nil {
			os.Remove(shardPath)
			return res, fmt.Errorf("validate shard %d: %w", shardN, err)
		}
		res.Shards++
		res.Records += rows
		res.Bytes += size
		shardN++
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
	defer db.Close()

	threads := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}
	db.ExecContext(ctx, fmt.Sprintf("SET threads = %d", threads))
	if cfg.DuckDBMemoryMB > 0 {
		db.ExecContext(ctx, fmt.Sprintf("SET memory_limit='%dMB'", cfg.DuckDBMemoryMB))
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
		os.Remove(shardPath)
		return 0, 0, fmt.Errorf("duckdb copy: %w", err)
	}
	fi, err := os.Stat(shardPath)
	if err != nil {
		return 0, 0, err
	}
	var rows int64
	db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM read_parquet('%s')", esc(shardPath))).Scan(&rows)
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
