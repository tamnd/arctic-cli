package cli

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/spf13/cobra"
	"github.com/tamnd/arctic-cli/arctic"
)

// ---- query -------------------------------------------------------------------

func (a *App) queryCmd() *cobra.Command {
	var author, afterS, beforeS, contains, kind string
	var minScore int64
	var asUser bool
	cmd := &cobra.Command{
		Use:   "query <subreddit|user>",
		Short: "Filter records from an imported entity",
		Long: "query scans the Parquet shards of an entity you have already pulled or\n" +
			"fetched. Disambiguate accounts with a u/ prefix or --user; everything else\n" +
			"is read as a subreddit.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entKind, name := a.entityKind(args[0], asUser)
			types, err := parseTypes(kind)
			if err != nil {
				return codeError(exitUsage, err)
			}
			after, err := parseEpoch(afterS)
			if err != nil {
				return codeError(exitUsage, err)
			}
			before, err := parseEpoch(beforeS)
			if err != nil {
				return codeError(exitUsage, err)
			}
			f := filter{
				author:   normalizeMaybe(author),
				after:    after,
				before:   before,
				minScore: minScore,
				contains: contains,
				limit:    a.limit,
			}
			var comments []arctic.Comment
			var submissions []arctic.Submission
			for _, t := range types {
				dir := shardDir(a.cfg, entKind, name, t)
				switch t {
				case arctic.TypeComments:
					rows, serr := scanComments(dir, f)
					if serr != nil {
						return codeError(exitError, serr)
					}
					comments = append(comments, rows...)
				case arctic.TypeSubmissions:
					rows, serr := scanSubmissions(dir, f)
					if serr != nil {
						return codeError(exitError, serr)
					}
					submissions = append(submissions, rows...)
				}
			}
			total := len(comments) + len(submissions)
			// When a single type is asked for, render that slice directly so the
			// columns match. Otherwise render comments then submissions.
			if len(types) == 1 && types[0] == arctic.TypeSubmissions {
				return a.renderOrEmpty(submissions, total)
			}
			if len(submissions) == 0 {
				return a.renderOrEmpty(comments, total)
			}
			if len(comments) == 0 {
				return a.renderOrEmpty(submissions, total)
			}
			if err := a.render(comments); err != nil {
				return codeError(exitError, err)
			}
			return a.renderOrEmpty(submissions, total)
		},
	}
	cmd.Flags().StringVar(&author, "author", "", "filter by author")
	cmd.Flags().StringVar(&afterS, "after", "", "earliest date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&beforeS, "before", "", "latest date (YYYY-MM-DD)")
	cmd.Flags().Int64Var(&minScore, "min-score", 0, "minimum score")
	cmd.Flags().StringVar(&contains, "contains", "", "case-insensitive substring in body/title/selftext")
	cmd.Flags().StringVar(&kind, "kind", "both", "comments|submissions|both")
	cmd.Flags().BoolVar(&asUser, "user", false, "read the argument as a username")
	return cmd
}

// entityKind resolves an argument and the --user flag to (kind, name).
func (a *App) entityKind(arg string, asUser bool) (string, string) {
	if asUser {
		return "user", normalizeName(arg)
	}
	lower := arg
	for _, p := range []string{"u/", "/u/", "user/", "/user/"} {
		if len(lower) >= len(p) && lower[:len(p)] == p {
			return "user", normalizeName(arg)
		}
	}
	return "subreddit", normalizeName(arg)
}

func normalizeMaybe(s string) string {
	if s == "" {
		return ""
	}
	return normalizeName(s)
}

// ---- catalog -----------------------------------------------------------------

func (a *App) catalogCmd() *cobra.Command {
	var sizes bool
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "List the months the bulk torrent catalog covers",
		RunE: func(cmd *cobra.Command, args []string) error {
			months := arctic.MonthRange(arctic.CatalogStart(), arctic.CatalogEnd())
			var sizeMap map[string]int64
			if sizes {
				m, err := arctic.FetchZstSizes(cmd.Context(), a.cfg)
				if err != nil {
					return mapErr(err)
				}
				sizeMap = m
			}
			rows := make([]catalogRow, 0, len(months))
			for _, m := range months {
				r := catalogRow{
					Month:    m.String(),
					InBundle: arctic.InBundle(m),
				}
				if sizeMap != nil {
					r.CommentsBytes = sizeMap[a.cfg.ZstPath(m, arctic.TypeComments)]
					r.SubmissionsBytes = sizeMap[a.cfg.ZstPath(m, arctic.TypeSubmissions)]
				}
				rows = append(rows, r)
			}
			return a.render(rows)
		},
	}
	cmd.Flags().BoolVar(&sizes, "sizes", false, "fetch per-file sizes from the catalog (network)")
	return cmd
}

type catalogRow struct {
	Month            string `json:"month"`
	InBundle         bool   `json:"in_bundle"`
	CommentsBytes    int64  `json:"comments_bytes,omitempty"`
	SubmissionsBytes int64  `json:"submissions_bytes,omitempty"`
}

// ---- stats -------------------------------------------------------------------

func (a *App) statsCmd() *cobra.Command {
	var by string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize the local index of imported shards",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch by {
			case "month", "type", "subreddit":
			default:
				return codeError(exitUsage, fmt.Errorf("--by must be month, type, or subreddit"))
			}
			idx, err := arctic.OpenIndex(a.cfg.IndexPath())
			if err != nil {
				return codeError(exitError, err)
			}
			defer func() { _ = idx.Close() }()
			rows, err := idx.Stats(by)
			if err != nil {
				return codeError(exitError, err)
			}
			return a.renderOrEmpty(rows, len(rows))
		},
	}
	cmd.Flags().StringVar(&by, "by", "month", "group by: month|type|subreddit")
	return cmd
}

// ---- info --------------------------------------------------------------------

func (a *App) infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Report detected hardware, the work budget, and storage paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			hw := arctic.DetectHardware(a.cfg.WorkDir)
			b := a.budget()
			row := infoRow{
				OS:                hw.OS,
				Hostname:          hw.Hostname,
				CPUs:              hw.CPUs,
				RAMTotalGB:        hw.RAMTotalGB,
				RAMAvailableGB:    hw.RAMAvailableGB,
				DiskFreeGB:        hw.DiskFreeGB,
				MaxDownloads:      b.MaxDownloads,
				MaxProcess:        b.MaxProcess,
				MaxConvertWorkers: b.MaxConvertWorkers,
				DuckDBMemoryMB:    b.DuckDBMemoryMB,
				Sequential:        b.Sequential,
				Engine:            string(a.cfg.Engine),
				DuckDBAvailable:   arctic.HasDuckDB,
				DataDir:           a.cfg.DataDir,
				RawDir:            a.cfg.RawDir,
				WorkDir:           a.cfg.WorkDir,
				IndexPath:         a.cfg.IndexPath(),
			}
			return a.render([]infoRow{row})
		},
	}
}

type infoRow struct {
	OS                string  `json:"os"`
	Hostname          string  `json:"hostname"`
	CPUs              int     `json:"cpus"`
	RAMTotalGB        float64 `json:"ram_total_gb"`
	RAMAvailableGB    float64 `json:"ram_available_gb"`
	DiskFreeGB        float64 `json:"disk_free_gb"`
	MaxDownloads      int     `json:"max_downloads"`
	MaxProcess        int     `json:"max_process"`
	MaxConvertWorkers int     `json:"max_convert_workers"`
	DuckDBMemoryMB    int     `json:"duckdb_memory_mb"`
	Sequential        bool    `json:"sequential"`
	Engine            string  `json:"engine"`
	DuckDBAvailable   bool    `json:"duckdb_available"`
	DataDir           string  `json:"data_dir"`
	RawDir            string  `json:"raw_dir"`
	WorkDir           string  `json:"work_dir"`
	IndexPath         string  `json:"index_path"`
}

// ---- entity info (sub info / user info) --------------------------------------

func (a *App) entityInfoCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Report what is imported locally for one " + kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := normalizeName(args[0])
			rows := make([]entityInfoRow, 0, 2)
			var any bool
			for _, t := range []arctic.Type{arctic.TypeComments, arctic.TypeSubmissions} {
				dir := shardDir(a.cfg, kind, name, t)
				shards, rec, bytes, first, last, err := shardStats(dir)
				if err != nil {
					return codeError(exitError, err)
				}
				if shards == 0 {
					continue
				}
				any = true
				rows = append(rows, entityInfoRow{
					Name:    name,
					Type:    string(t),
					Shards:  shards,
					Records: rec,
					Bytes:   bytes,
					First:   epochStr(first),
					Last:    epochStr(last),
				})
			}
			if !any {
				return codeError(exitNoData, fmt.Errorf("nothing imported for %s %q", kind, name))
			}
			return a.render(rows)
		},
	}
}

type entityInfoRow struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Shards  int    `json:"shards"`
	Records int64  `json:"records"`
	Bytes   int64  `json:"bytes"`
	First   string `json:"first"`
	Last    string `json:"last"`
}

func epochStr(e int64) string {
	if e <= 0 {
		return ""
	}
	return time.Unix(e, 0).UTC().Format("2006-01-02")
}

// shardStats reports shard count, row count, byte size, and the created_utc span
// across the Parquet shards in dir. Row counts come from Parquet footers, so it
// does not read the row data.
func shardStats(dir string) (shards int, records, size, first, last int64, err error) {
	paths, err := listShards(dir)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	for _, path := range paths {
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return 0, 0, 0, 0, 0, rerr
		}
		pf, perr := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
		if perr != nil {
			return 0, 0, 0, 0, 0, perr
		}
		shards++
		records += pf.NumRows()
		size += int64(len(data))
	}
	if shards == 0 {
		return 0, 0, 0, 0, 0, nil
	}
	lo, hi, serr := shardSpan(paths)
	if serr != nil {
		return 0, 0, 0, 0, 0, serr
	}
	return shards, records, size, lo, hi, nil
}

// shardSpan reads the created_utc range from the first and last shard, which are
// time-ordered by the dump itself.
func shardSpan(paths []string) (lo, hi int64, err error) {
	read := func(path string) (mn, mx int64, e error) {
		data, e := os.ReadFile(path)
		if e != nil {
			return 0, 0, e
		}
		rows, e := parquet.Read[spanRow](bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return 0, 0, e
		}
		for i, r := range rows {
			if i == 0 || r.CreatedUTC < mn {
				mn = r.CreatedUTC
			}
			if r.CreatedUTC > mx {
				mx = r.CreatedUTC
			}
		}
		return mn, mx, nil
	}
	lo, _, err = read(paths[0])
	if err != nil {
		return 0, 0, err
	}
	_, hi, err = read(paths[len(paths)-1])
	if err != nil {
		return 0, 0, err
	}
	return lo, hi, nil
}

type spanRow struct {
	CreatedUTC int64 `parquet:"created_utc"`
}

// ---- version -----------------------------------------------------------------

func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit, and build date",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.render([]versionRow{{Version: Version, Commit: Commit, Date: Date}})
		},
	}
}

type versionRow struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}
