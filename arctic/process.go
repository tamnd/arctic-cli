package arctic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// zstdDecoderSem guards the 2 GB-window decoder. The bulk dumps are compressed
// with a 2 GB window, so each decoder allocates a buffer that large. Allowing
// only one at a time keeps a small machine from doubling its resident set and
// getting OOM-killed when two months decode at once.
var zstdDecoderSem sync.Mutex

// ProcessResult summarizes what one .zst file produced.
type ProcessResult struct {
	Shards       int
	Records      int64
	SkippedLines int64
	Bytes        int64
}

// ShardPathFunc names a shard's output path given its sequence number. ProcessFile
// uses it for the publish layout; when nil, shards are written as NNN.parquet
// under outDir.
type ShardPathFunc func(n int) string

// ShardDone reports a shard the processor just finished writing. The publish
// path uses it to commit shards to the hub as they land instead of waiting for
// the whole month.
type ShardDone struct {
	N       int    // absolute shard index within the month
	Path    string // where the shard was written
	Records int64  // rows in this shard
	Bytes   int64  // Parquet size on disk
}

// ProcessConfig tunes a streaming process run.
type ProcessConfig struct {
	// StartShard skips emitting shards with an index below it. A resumed month
	// whose earlier shards are already committed fast-forwards past them without
	// rewriting or re-uploading.
	StartShard int
	// OnProgress, when set, reports the running count of decoded lines.
	OnProgress func(done int64)
	// OnShard, when set, is called after each shard at or above StartShard is
	// written and validated. Returning an error aborts the file.
	OnShard func(ShardDone) error
}

// ProcessFile decodes the .zst at zstPath, parses each line into the type's
// struct, groups lines into chunks of cfg.ChunkLines, and writes one Parquet
// shard per chunk. Unparseable lines are counted and skipped, never fatal. The
// cb callback, when set, reports the running count of decoded lines.
func ProcessFile(ctx context.Context, cfg Config, zstPath string, t Type, outDir string,
	cb func(done int64)) (ProcessResult, error) {

	return processFile(ctx, cfg, zstPath, t, func(n int) string {
		return filepath.Join(outDir, fmt.Sprintf("%03d.parquet", n))
	}, outDir, ProcessConfig{OnProgress: cb})
}

// ProcessFileTo is like ProcessFile but names shards through pathFn, which the
// publish path uses to lay shards out at data/{type}/YYYY/MM/NNN.parquet. The
// directory of each shard path is created as needed.
func ProcessFileTo(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	cb func(done int64)) (ProcessResult, error) {

	return processFile(ctx, cfg, zstPath, t, pathFn, "", ProcessConfig{OnProgress: cb})
}

// ProcessFileStream is ProcessFileTo with per-shard callbacks and mid-month
// resume, so a caller can commit shards as they are produced and pick up after
// an interrupted run. The returned ProcessResult counts only the shards this
// call produced (those at or above pc.StartShard).
func ProcessFileStream(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	pc ProcessConfig) (ProcessResult, error) {

	return processFile(ctx, cfg, zstPath, t, pathFn, "", pc)
}

func processFile(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	outDir string, pc ProcessConfig) (ProcessResult, error) {

	if !t.Valid() {
		return ProcessResult{}, fmt.Errorf("unknown type %q", t)
	}
	if cfg.Engine == EngineDuckDB {
		if !HasDuckDB {
			return ProcessResult{}, fmt.Errorf("duckdb engine requested but this binary was not built with -tags duckdb")
		}
		return processDuckDB(ctx, cfg, zstPath, t, pathFn, pc)
	}

	chunkLines := cfg.ChunkLines
	if chunkLines <= 0 {
		chunkLines = DefaultChunkLines
	}
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return ProcessResult{}, fmt.Errorf("mkdir out: %w", err)
		}
	}

	// Hold the decoder semaphore for the scan phase only. Once the file is fully
	// read the 2 GB window is freed and another month may begin decoding.
	zstdDecoderSem.Lock()
	f, err := os.Open(zstPath)
	if err != nil {
		zstdDecoderSem.Unlock()
		return ProcessResult{}, fmt.Errorf("open zst: %w", err)
	}
	dec, err := zstd.NewReader(f, zstd.WithDecoderMaxWindow(1<<31))
	if err != nil {
		_ = f.Close()
		zstdDecoderSem.Unlock()
		return ProcessResult{}, fmt.Errorf("zstd reader: %w", err)
	}

	var res ProcessResult
	shardN := 0

	// flush writes the buffered chunk as shard shardN. When shardN is below
	// pc.StartShard the shard is already committed from an earlier run, so it
	// fast-forwards past the chunk (which was never buffered) without writing.
	var comments []Comment
	var submissions []Submission

	flush := func(rowCount int) error {
		if rowCount == 0 {
			return nil
		}
		if shardN < pc.StartShard {
			comments = comments[:0]
			submissions = submissions[:0]
			shardN++
			return nil
		}
		shardPath := pathFn(shardN)
		if err := os.MkdirAll(filepath.Dir(shardPath), 0o755); err != nil {
			return fmt.Errorf("mkdir shard dir: %w", err)
		}
		var size int64
		var werr error
		if t == TypeComments {
			size, werr = writeCommentShard(ctx, comments, shardPath)
			comments = comments[:0]
		} else {
			size, werr = writeSubmissionShard(ctx, submissions, shardPath)
			submissions = submissions[:0]
		}
		if werr != nil {
			return werr
		}
		res.Shards++
		res.Records += int64(rowCount)
		res.Bytes += size
		if pc.OnShard != nil {
			if err := pc.OnShard(ShardDone{N: shardN, Path: shardPath, Records: int64(rowCount), Bytes: size}); err != nil {
				return err
			}
		}
		shardN++
		return nil
	}

	scanner := bufio.NewScanner(dec)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	var lineCount int64
	if t == TypeComments {
		comments = make([]Comment, 0, chunkLines)
	} else {
		submissions = make([]Submission, 0, chunkLines)
	}

	var loopErr error
	chunkCount := 0
scan:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			loopErr = ctx.Err()
			break scan
		default:
		}
		line := scanner.Bytes()
		lineCount++
		if pc.OnProgress != nil && lineCount%200000 == 0 {
			pc.OnProgress(lineCount)
		}
		if len(line) == 0 || line[0] != '{' {
			res.SkippedLines++
			continue
		}
		// While fast-forwarding past already-committed shards, parse to keep the
		// chunk boundaries identical but do not buffer the rows.
		buffering := shardN >= pc.StartShard
		if t == TypeComments {
			c, ok := CommentFromJSON(line)
			if !ok {
				res.SkippedLines++
				continue
			}
			if buffering {
				comments = append(comments, c)
			}
			chunkCount++
		} else {
			s, ok := SubmissionFromJSON(line)
			if !ok {
				res.SkippedLines++
				continue
			}
			if buffering {
				submissions = append(submissions, s)
			}
			chunkCount++
		}
		if chunkCount >= chunkLines {
			if err := flush(chunkCount); err != nil {
				loopErr = err
				break
			}
			chunkCount = 0
		}
	}
	if loopErr == nil {
		if err := scanner.Err(); err != nil && err != io.EOF {
			loopErr = fmt.Errorf("scan jsonl: %w", err)
		}
	}

	// Free the 2 GB window before flushing the final shard so the heap is back
	// down while the parquet writer runs.
	dec.Close()
	_ = f.Close()
	runtime.GC()
	debug.FreeOSMemory()
	zstdDecoderSem.Unlock()

	if loopErr != nil {
		return res, loopErr
	}
	if err := flush(chunkCount); err != nil {
		return res, err
	}
	if pc.OnProgress != nil {
		pc.OnProgress(lineCount)
	}
	return res, nil
}

// ValidateParquet checks that a shard is non-empty and carries the PAR1 magic at
// head and tail, which catches a truncated write before a consumer ever opens
// the file.
func ValidateParquet(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if fi.Size() < 12 {
		return fmt.Errorf("file too small (%d bytes)", fi.Size())
	}

	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	if string(head[:]) != "PAR1" {
		return fmt.Errorf("bad header magic %q", head)
	}

	if _, err := f.Seek(-4, io.SeekEnd); err != nil {
		return fmt.Errorf("seek tail: %w", err)
	}
	var tail [4]byte
	if _, err := io.ReadFull(f, tail[:]); err != nil {
		return fmt.Errorf("read tail: %w", err)
	}
	if string(tail[:]) != "PAR1" {
		return fmt.Errorf("bad tail magic %q (truncated?)", tail)
	}
	return nil
}
