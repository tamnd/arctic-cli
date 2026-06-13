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

// ProcessFile decodes the .zst at zstPath, parses each line into the type's
// struct, groups lines into chunks of cfg.ChunkLines, and writes one Parquet
// shard per chunk. Unparseable lines are counted and skipped, never fatal. The
// cb callback, when set, reports the running count of decoded lines.
func ProcessFile(ctx context.Context, cfg Config, zstPath string, t Type, outDir string,
	cb func(done int64)) (ProcessResult, error) {

	return processFile(ctx, cfg, zstPath, t, func(n int) string {
		return filepath.Join(outDir, fmt.Sprintf("%03d.parquet", n))
	}, outDir, cb)
}

// ProcessFileTo is like ProcessFile but names shards through pathFn, which the
// publish path uses to lay shards out at data/{type}/YYYY/MM/NNN.parquet. The
// directory of each shard path is created as needed.
func ProcessFileTo(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	cb func(done int64)) (ProcessResult, error) {

	return processFile(ctx, cfg, zstPath, t, pathFn, "", cb)
}

func processFile(ctx context.Context, cfg Config, zstPath string, t Type, pathFn ShardPathFunc,
	outDir string, cb func(done int64)) (ProcessResult, error) {

	if !t.Valid() {
		return ProcessResult{}, fmt.Errorf("unknown type %q", t)
	}
	if cfg.Engine == EngineDuckDB {
		if !HasDuckDB {
			return ProcessResult{}, fmt.Errorf("duckdb engine requested but this binary was not built with -tags duckdb")
		}
		return processDuckDB(ctx, cfg, zstPath, t, pathFn, cb)
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

	// flush writes the current chunk as a shard and resets the buffer.
	var comments []Comment
	var submissions []Submission

	flush := func() error {
		var rowCount int
		if t == TypeComments {
			rowCount = len(comments)
		} else {
			rowCount = len(submissions)
		}
		if rowCount == 0 {
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
		if cb != nil && lineCount%200000 == 0 {
			cb(lineCount)
		}
		if len(line) == 0 || line[0] != '{' {
			res.SkippedLines++
			continue
		}
		if t == TypeComments {
			c, ok := CommentFromJSON(line)
			if !ok {
				res.SkippedLines++
				continue
			}
			comments = append(comments, c)
			chunkCount++
		} else {
			s, ok := SubmissionFromJSON(line)
			if !ok {
				res.SkippedLines++
				continue
			}
			submissions = append(submissions, s)
			chunkCount++
		}
		if chunkCount >= chunkLines {
			if err := flush(); err != nil {
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
	if err := flush(); err != nil {
		return res, err
	}
	if cb != nil {
		cb(lineCount)
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
