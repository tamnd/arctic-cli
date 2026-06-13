package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/tamnd/arctic-cli/arctic"
)

// Build metadata, injected via -ldflags by the Makefile/goreleaser.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// App holds shared state threaded through every command.
type App struct {
	cfg arctic.Config

	// global output flags
	output   string
	fields   []string
	noHeader bool
	template string
	color    string
	limit    int
	quiet    bool

	// networking and storage flags
	workers   int
	engine    string
	timeout   time.Duration
	userAgent string
}

// exit codes (see spec section 9.5).
const (
	exitError   = 1
	exitUsage   = 2
	exitNoData  = 3
	exitPartial = 4
	exitBlocked = 5
	exitStall   = 75
)

// ExitError carries a process exit code up to main.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

func codeError(code int, err error) error { return &ExitError{Code: code, Err: err} }

// NewRootCmd builds the full command tree.
func NewRootCmd() *cobra.Command {
	app := &App{cfg: arctic.DefaultConfig()}

	root := &cobra.Command{
		Use:   "arctic",
		Short: "Acquire, process, and query the public Reddit archive",
		Long: "arctic works with the bulk Reddit archive: it pulls the monthly dumps\n" +
			"from the public torrent catalog and the Arctic Shift backfill API,\n" +
			"decompresses the zstd JSONL, writes Parquet, keeps a local index, and can\n" +
			"publish the shards to a Hugging Face dataset.\n\n" +
			"arctic is an independent tool and is not affiliated with, endorsed by, or\n" +
			"sponsored by Reddit, Inc. It moves only public archive data.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return app.setup(cmd)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&app.output, "output", "o", "auto", "output: table|json|jsonl|csv|tsv|url|raw (auto = table on a TTY, jsonl when piped)")
	pf.StringSliceVar(&app.fields, "fields", nil, "comma-separated columns to include")
	pf.BoolVar(&app.noHeader, "no-header", false, "omit the header row in table/csv/tsv")
	pf.StringVar(&app.template, "template", "", "Go text/template applied per record")
	pf.StringVar(&app.color, "color", "auto", "color: auto|always|never")
	pf.IntVarP(&app.limit, "limit", "n", 0, "limit number of records (0 = no limit)")
	pf.BoolVarP(&app.quiet, "quiet", "q", false, "suppress progress on stderr")

	pf.IntVarP(&app.workers, "workers", "j", 0, "concurrent workers (0 = use the hardware budget)")
	pf.StringVar(&app.cfg.DataDir, "data-dir", app.cfg.DataDir, "root for per-entity imports and the index")
	pf.StringVar(&app.cfg.RawDir, "raw-dir", app.cfg.RawDir, "where downloaded .zst files land")
	pf.StringVar(&app.cfg.WorkDir, "work-dir", app.cfg.WorkDir, "scratch for conversion")
	pf.StringVar(&app.engine, "engine", string(app.cfg.Engine), "conversion engine: go|duckdb")
	pf.IntVar(&app.cfg.ChunkLines, "chunk-lines", app.cfg.ChunkLines, "JSONL lines per Parquet shard")
	pf.DurationVar(&app.timeout, "timeout", 60*time.Second, "per-request timeout for the API path")
	pf.StringVar(&app.userAgent, "user-agent", "", "override the API request User-Agent")

	root.AddCommand(
		app.pullCmd(),
		app.subCmd(),
		app.userCmd(),
		app.processCmd(),
		app.queryCmd(),
		app.catalogCmd(),
		app.statsCmd(),
		app.publishCmd(),
		app.infoCmd(),
		app.versionCmd(),
	)
	return root
}

// setup resolves output defaults and the conversion engine.
func (a *App) setup(cmd *cobra.Command) error {
	// When --data-dir moves the root but the sub-directory flags are left at
	// their defaults, re-root them under the new data dir so a single
	// --data-dir keeps everything together. An explicit --raw-dir/--work-dir
	// still wins.
	if f := cmd.Flags(); f.Changed("data-dir") {
		if !f.Changed("raw-dir") {
			a.cfg.RawDir = filepath.Join(a.cfg.DataDir, "raw")
		}
		if !f.Changed("work-dir") {
			a.cfg.WorkDir = filepath.Join(a.cfg.DataDir, "work")
		}
		if !f.Changed("repo-root") {
			a.cfg.RepoRoot = filepath.Join(a.cfg.DataDir, "repo")
		}
	}
	if a.output == "" || a.output == "auto" {
		if isatty.IsTerminal(os.Stdout.Fd()) {
			a.output = string(FormatTable)
		} else {
			a.output = string(FormatJSONL)
		}
	}
	if !Format(a.output).Valid() {
		return codeError(exitUsage, fmt.Errorf("unknown output format %q", a.output))
	}
	switch arctic.Engine(a.engine) {
	case arctic.EngineGo:
	case arctic.EngineDuckDB:
		if !arctic.HasDuckDB {
			return codeError(exitUsage, fmt.Errorf("engine %q needs a binary built with -tags duckdb", a.engine))
		}
	default:
		return codeError(exitUsage, fmt.Errorf("unknown engine %q (want go or duckdb)", a.engine))
	}
	a.cfg.Engine = arctic.Engine(a.engine)
	return nil
}

// budget returns the computed hardware budget, with the --workers override
// applied to the convert and download caps when set.
func (a *App) budget() arctic.Budget {
	b := arctic.ComputeBudget(arctic.DetectHardware(a.cfg.WorkDir))
	if a.workers > 0 {
		b.MaxConvertWorkers = a.workers
	}
	return b
}

// fetchWorkers returns the worker count for the API path.
func (a *App) fetchWorkers() int {
	if a.workers > 0 {
		return a.workers
	}
	return 8
}

// render writes records using the resolved global flags.
func (a *App) render(records any) error {
	r := NewRenderer(os.Stdout, Format(a.output), a.fields, a.noHeader, a.template)
	return r.Render(records)
}

// renderOrEmpty renders records, mapping an empty result to exit code 3.
func (a *App) renderOrEmpty(records any, n int) error {
	if err := a.render(records); err != nil {
		return err
	}
	if n == 0 {
		return codeError(exitNoData, nil)
	}
	return nil
}

// progressf prints a progress line to stderr unless --quiet.
func (a *App) progressf(format string, args ...any) {
	if a.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
