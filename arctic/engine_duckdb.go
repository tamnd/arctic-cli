//go:build duckdb

package arctic

// HasDuckDB reports that this binary carries the DuckDB engine.
const HasDuckDB = true

// DefaultEngine prefers DuckDB when the build carries it, since it is the faster
// converter and gives arctic query real SQL.
func DefaultEngine() Engine { return EngineDuckDB }
