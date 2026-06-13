//go:build !duckdb

package arctic

// HasDuckDB reports whether this binary carries the DuckDB engine. The default
// build does not.
const HasDuckDB = false

// DefaultEngine is the engine a fresh Config uses. Without the duckdb build tag
// the pure-Go writer is the only option.
func DefaultEngine() Engine { return EngineGo }
