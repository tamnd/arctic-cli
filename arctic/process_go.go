package arctic

import (
	"context"
	"fmt"
	"os"

	parquet "github.com/parquet-go/parquet-go"
	pzstd "github.com/parquet-go/parquet-go/compress/zstd"
)

// rowGroupSize is the Parquet row-group size for every shard. It is the same
// across both engines so the published layout is uniform.
const rowGroupSize = 131072

// goParquetZstdCodec compresses shards at zstd's default level. That is roughly
// three times faster to write than the best-compression level for output only
// a little larger, which matters across a multi-terabyte backfill.
var goParquetZstdCodec = &pzstd.Codec{Level: pzstd.SpeedDefault}

// writeCommentShard writes a batch of comments to shardPath as Parquet with
// zstd compression, then validates the file. It returns the byte size on disk.
func writeCommentShard(ctx context.Context, rows []Comment, shardPath string) (int64, error) {
	return writeShard[Comment](ctx, rows, shardPath)
}

// writeSubmissionShard writes a batch of submissions to shardPath.
func writeSubmissionShard(ctx context.Context, rows []Submission, shardPath string) (int64, error) {
	return writeShard[Submission](ctx, rows, shardPath)
}

func writeShard[T any](ctx context.Context, rows []T, shardPath string) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	f, err := os.Create(shardPath)
	if err != nil {
		return 0, fmt.Errorf("create shard: %w", err)
	}
	// Closed explicitly below; if anything fails we remove the partial file.
	w := parquet.NewGenericWriter[T](f,
		parquet.Compression(goParquetZstdCodec),
		parquet.MaxRowsPerRowGroup(rowGroupSize),
	)

	const batch = 4096
	for i := 0; i < len(rows); i += batch {
		end := i + batch
		if end > len(rows) {
			end = len(rows)
		}
		if _, err := w.Write(rows[i:end]); err != nil {
			w.Close()
			f.Close()
			os.Remove(shardPath)
			return 0, fmt.Errorf("write rows: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		f.Close()
		os.Remove(shardPath)
		return 0, fmt.Errorf("close writer: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(shardPath)
		return 0, fmt.Errorf("close file: %w", err)
	}

	if err := ValidateParquet(shardPath); err != nil {
		os.Remove(shardPath)
		return 0, fmt.Errorf("validate shard: %w", err)
	}
	fi, err := os.Stat(shardPath)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
