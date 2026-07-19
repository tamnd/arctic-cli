//go:build !duckdb

package arctic

import (
	"context"
	"fmt"
)

// processDuckDB is unreachable in the default build (processFile guards on
// HasDuckDB first), but the symbol has to exist so the package compiles without
// the duckdb tag.
func processDuckDB(_ context.Context, _ Config, _ string, _ Type, _ ShardPathFunc, _ ProcessConfig) (ProcessResult, error) {
	return ProcessResult{}, fmt.Errorf("duckdb engine not built into this binary")
}
