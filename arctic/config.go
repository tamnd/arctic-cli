package arctic

import (
	"os"
	"path/filepath"
	"time"
)

// Engine names a conversion backend.
type Engine string

const (
	// EngineGo is the pure-Go Parquet writer. It is always available and is the
	// only engine on a CGO_ENABLED=0 build.
	EngineGo Engine = "go"
	// EngineDuckDB is the DuckDB-backed engine. It is present only in a binary
	// built with -tags duckdb.
	EngineDuckDB Engine = "duckdb"
)

// Config holds the resolved paths and tunables for acquisition, processing, and
// publishing. The CLI fills it from flags and environment; the library reads it
// and never touches the environment itself.
type Config struct {
	// DataDir is the root for per-entity imports and the local index.
	DataDir string
	// RawDir is where downloaded .zst files land.
	RawDir string
	// WorkDir is scratch for conversion.
	WorkDir string
	// RepoRoot stages the Hugging Face dataset repo during a publish.
	RepoRoot string
	// HFRepo is the Hugging Face dataset id to publish to.
	HFRepo string

	// Engine selects the conversion backend.
	Engine Engine
	// ChunkLines is the number of JSONL lines per Parquet shard.
	ChunkLines int
	// CommitEveryShards commits to the hub every N shards within a month so a
	// big month lands incrementally and a restart resumes mid-month. Zero
	// commits the whole month in one go at the end.
	CommitEveryShards int
	// DuckDBMemoryMB caps the DuckDB engine's memory.
	DuckDBMemoryMB int

	// MinFreeGB is the free-disk floor a publish refuses to start below.
	MinFreeGB int
	// DownloadFloorGB holds back starting a new month's download while free
	// disk is below this, so months process in parallel without the pipeline
	// piling up more .zst files than the disk can hold. Zero disables the gate.
	DownloadFloorGB int
	// MaxCommitStall is how long a Hugging Face commit may make no progress
	// before the publish exits with the restart code.
	MaxCommitStall time.Duration

	// MaxDownloads, MaxProcess, MaxConvertWorkers, and MaxDecodes bound
	// concurrency. Zero means "use the computed budget".
	MaxDownloads      int
	MaxProcess        int
	MaxConvertWorkers int
	MaxDecodes        int
}

// Environment variable names. The CLI reads these as flag defaults.
const (
	EnvDataDir        = "ARCTIC_DATA_DIR"
	EnvRawDir         = "ARCTIC_RAW_DIR"
	EnvWorkDir        = "ARCTIC_WORK_DIR"
	EnvRepoRoot       = "ARCTIC_REPO_ROOT"
	EnvMinFreeGB      = "ARCTIC_MIN_FREE_GB"
	EnvChunkLines     = "ARCTIC_CHUNK_LINES"
	EnvCommitEvery    = "ARCTIC_COMMIT_EVERY"
	EnvDownloadFloor  = "ARCTIC_DOWNLOAD_FLOOR_GB"
	EnvMaxDownloads   = "ARCTIC_MAX_DOWNLOADS"
	EnvMaxProcess     = "ARCTIC_MAX_PROCESS"
	EnvMaxConvert     = "ARCTIC_MAX_CONVERT"
	EnvMaxDecodes     = "ARCTIC_MAX_DECODES"
	EnvEngine         = "ARCTIC_ENGINE"
	EnvHFToken        = "HF_TOKEN"
	DefaultHFRepo     = "open-index/arctic"
	DefaultChunkLines = 500000
	// DefaultCommitEveryShards keeps a big month landing every few shards
	// instead of one commit at the end, so progress is visible and resumable.
	DefaultCommitEveryShards = 8
	DefaultMinFreeGB         = 30
	// DefaultDownloadFloorGB leaves room for a few in-flight months at once so
	// the pipeline can process them in parallel without exhausting the disk.
	DefaultDownloadFloorGB = 40
)

// DefaultDataDir returns the XDG data directory for arctic, honoring
// ARCTIC_DATA_DIR when set.
func DefaultDataDir() string {
	if d := os.Getenv(EnvDataDir); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "arctic")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "arctic")
	}
	return filepath.Join(home, ".local", "share", "arctic")
}

// DefaultConfig returns a Config with the standard paths and tunables. The
// ARCTIC_* environment variables override the defaults; a flag the CLI sets
// later overrides the environment in turn.
func DefaultConfig() Config {
	data := DefaultDataDir()
	c := Config{
		DataDir:           data,
		RawDir:            envOr(EnvRawDir, filepath.Join(data, "raw")),
		WorkDir:           envOr(EnvWorkDir, filepath.Join(data, "work")),
		RepoRoot:          envOr(EnvRepoRoot, filepath.Join(data, "repo")),
		HFRepo:            DefaultHFRepo,
		Engine:            DefaultEngine(),
		ChunkLines:        DefaultChunkLines,
		CommitEveryShards: DefaultCommitEveryShards,
		MinFreeGB:         DefaultMinFreeGB,
		DownloadFloorGB:   DefaultDownloadFloorGB,
		MaxCommitStall:    45 * time.Minute,
	}
	if v := os.Getenv(EnvEngine); v != "" {
		c.Engine = Engine(v)
	}
	if n := envInt(EnvChunkLines); n > 0 {
		c.ChunkLines = n
	}
	if n := envInt(EnvCommitEvery); n > 0 {
		c.CommitEveryShards = n
	}
	if n := envInt(EnvMinFreeGB); n > 0 {
		c.MinFreeGB = n
	}
	if n := envInt(EnvDownloadFloor); n > 0 {
		c.DownloadFloorGB = n
	}
	if n := envInt(EnvMaxDownloads); n > 0 {
		c.MaxDownloads = n
	}
	if n := envInt(EnvMaxProcess); n > 0 {
		c.MaxProcess = n
	}
	if n := envInt(EnvMaxConvert); n > 0 {
		c.MaxConvertWorkers = n
	}
	if n := envInt(EnvMaxDecodes); n > 0 {
		c.MaxDecodes = n
	}
	return c
}

// envOr returns the environment value for key, or def when it is unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt parses the environment value for key as an int, returning 0 when it is
// unset or not a number.
func envInt(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// IndexPath is the local catalog database path.
func (c Config) IndexPath() string { return filepath.Join(c.DataDir, "index.db") }

// EntityDir returns the import directory for a subreddit or user.
func (c Config) EntityDir(kind, name string) string {
	return filepath.Join(c.DataDir, kind, name)
}

// ZstPath returns where a month+type dump file lives once downloaded.
func (c Config) ZstPath(m Month, t Type) string {
	return filepath.Join(c.RawDir, "reddit", string(t),
		t.Prefix()+"_"+m.String()+".zst")
}

// ShardHFPath returns the in-repo path of a shard for the published layout.
func ShardHFPath(t Type, m Month, n int) string {
	return filepath.Join("data", string(t),
		intToStr4(m.Year), intToStr2(m.Month), padShard(n)+".parquet")
}

func intToStr4(n int) string { return pad(n, 4) }
func intToStr2(n int) string { return pad(n, 2) }
func padShard(n int) string  { return pad(n, 3) }

func pad(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
