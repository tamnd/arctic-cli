package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/spf13/cobra"
	"github.com/tamnd/arctic-cli/arctic"
	"github.com/tamnd/arctic-cli/shift"
)

// ---- pull --------------------------------------------------------------------

func (a *App) pullCmd() *cobra.Command {
	var fromS, toS, kind string
	var process bool
	cmd := &cobra.Command{
		Use:   "pull [months...]",
		Short: "Download monthly bulk dumps from the torrent catalog",
		Long: "pull fetches the monthly RC_/RS_ dumps from the public torrent catalog.\n" +
			"Name months as arguments (2024-01, or a 2024-01..2024-03 range), or set\n" +
			"--from and --to. With --process each completed file is converted to Parquet.",
		RunE: func(cmd *cobra.Command, args []string) error {
			types, err := parseTypes(kind)
			if err != nil {
				return codeError(exitUsage, err)
			}
			months, err := resolveMonths(args, fromS, toS)
			if err != nil {
				return codeError(exitUsage, err)
			}
			ctx := cmd.Context()
			var failures, notPublished, ok int
			for _, m := range months {
				for _, t := range types {
					path, derr := arctic.DownloadMonth(ctx, a.cfg, m, t, a.downloadProgress(m, t))
					if derr != nil {
						if isNotPublished(derr) {
							a.progressf("%s %s: not yet published", m, t)
							notPublished++
							continue
						}
						a.progressf("%s %s: %v", m, t, derr)
						failures++
						continue
					}
					ok++
					a.progressf("%s %s: %s", m, t, path)
					if process {
						out := filepath.Join(a.cfg.WorkDir, m.String(), string(t))
						res, perr := arctic.ProcessFile(ctx, a.cfg, path, t, out, nil)
						if perr != nil {
							a.progressf("%s %s: process: %v", m, t, perr)
							failures++
							continue
						}
						if rerr := recordShards(a.cfg, t, m.Year, m.Month, "", out); rerr != nil {
							a.progressf("%s %s: indexed shards but: %v", m, t, rerr)
						}
						a.progressf("%s %s: %d shards, %d records (%d skipped)", m, t, res.Shards, res.Records, res.SkippedLines)
					}
				}
			}
			switch {
			case ok == 0 && failures == 0:
				return codeError(exitNoData, fmt.Errorf("no months published in range"))
			case failures > 0:
				return codeError(exitPartial, fmt.Errorf("%d of %d targets failed", failures, ok+failures+notPublished))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromS, "from", arctic.CatalogStart().String(), "first month (YYYY-MM)")
	cmd.Flags().StringVar(&toS, "to", arctic.CatalogEnd().String(), "last month (YYYY-MM)")
	cmd.Flags().StringVar(&kind, "type", "both", "comments|submissions|both")
	cmd.Flags().BoolVar(&process, "process", false, "convert each completed file to Parquet")
	return cmd
}

// resolveMonths turns the args (or the from/to flags) into a month list.
func resolveMonths(args []string, fromS, toS string) ([]arctic.Month, error) {
	if len(args) == 0 {
		from, err := arctic.ParseMonth(fromS)
		if err != nil {
			return nil, err
		}
		to, err := arctic.ParseMonth(toS)
		if err != nil {
			return nil, err
		}
		if to.Before(from) {
			return nil, fmt.Errorf("--to %s is before --from %s", toS, fromS)
		}
		return arctic.MonthRange(from, to), nil
	}
	var out []arctic.Month
	for _, arg := range args {
		if lo, hi, ok := strings.Cut(arg, ".."); ok {
			from, err := arctic.ParseMonth(lo)
			if err != nil {
				return nil, err
			}
			to, err := arctic.ParseMonth(hi)
			if err != nil {
				return nil, err
			}
			out = append(out, arctic.MonthRange(from, to)...)
			continue
		}
		m, err := arctic.ParseMonth(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ---- sub ---------------------------------------------------------------------

func (a *App) subCmd() *cobra.Command {
	var afterS, beforeS, kind string
	var api, noImport bool
	cmd := &cobra.Command{
		Use:   "sub <subreddit>",
		Short: "A community's full history (torrent first, API fallback)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := normalizeName(args[0])
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
			ctx := cmd.Context()
			useTorrent := !api
			if useTorrent {
				inTorrent, terr := arctic.SubredditInTorrent(ctx, name)
				if terr != nil || !inTorrent {
					if terr != nil {
						a.progressf("torrent catalog check failed (%v); using the API", terr)
					} else {
						a.progressf("r/%s is not in the per-subreddit bundle; using the API", name)
					}
					useTorrent = false
				}
			}
			return a.acquireEntity(ctx, "subreddit", name, types, after, before, useTorrent, noImport)
		},
	}
	cmd.Flags().StringVar(&afterS, "after", "", "earliest date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&beforeS, "before", "", "latest date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&kind, "kind", "both", "comments|submissions|both")
	cmd.Flags().BoolVar(&api, "api", false, "force the Arctic Shift API path")
	cmd.Flags().BoolVar(&noImport, "no-import", false, "download only, skip the Parquet import")
	cmd.AddCommand(a.entityInfoCmd("subreddit"))
	return cmd
}

// ---- user --------------------------------------------------------------------

func (a *App) userCmd() *cobra.Command {
	var afterS, beforeS, kind string
	var noImport bool
	cmd := &cobra.Command{
		Use:   "user <username>",
		Short: "An account's full history (Arctic Shift API)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := normalizeName(args[0])
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
			return a.acquireEntity(cmd.Context(), "user", name, types, after, before, false, noImport)
		},
	}
	cmd.Flags().StringVar(&afterS, "after", "", "earliest date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&beforeS, "before", "", "latest date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&kind, "kind", "both", "comments|submissions|both")
	cmd.Flags().BoolVar(&noImport, "no-import", false, "download only, skip the Parquet import")
	cmd.AddCommand(a.entityInfoCmd("user"))
	return cmd
}

// acquireEntity downloads a subreddit or user's history and imports it.
func (a *App) acquireEntity(ctx context.Context, kind, name string, types []arctic.Type, after, before int64, useTorrent, noImport bool) error {
	dir := a.cfg.EntityDir(kind, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return codeError(exitError, err)
	}
	client := a.shiftClient()
	var total int64
	for _, t := range types {
		var zstPath string
		if useTorrent {
			a.progressf("downloading r/%s %s from the torrent bundle", name, t)
			path, err := arctic.DownloadSubreddit(ctx, dir, name, t, a.downloadProgressName(name, t))
			if err != nil {
				return mapErr(err)
			}
			zstPath = path
		} else {
			jsonlPath := filepath.Join(dir, string(t)+".jsonl")
			f, err := os.Create(jsonlPath)
			if err != nil {
				return codeError(exitError, err)
			}
			n, ferr := client.FetchRange(ctx, kind, name, t, clampAfter(after), before, a.fetchWorkers(), f, a.fetchProgress(name))
			cerr := f.Close()
			if ferr != nil {
				return mapErr(ferr)
			}
			if cerr != nil {
				return codeError(exitError, cerr)
			}
			total += n
			a.progressf("%s %s: %d records -> %s", name, t, n, jsonlPath)
			if n == 0 || noImport {
				continue
			}
			zp, zerr := compressJSONL(jsonlPath)
			if zerr != nil {
				return codeError(exitError, zerr)
			}
			zstPath = zp
			defer os.Remove(zp)
		}
		if noImport || zstPath == "" {
			continue
		}
		out := shardDir(a.cfg, kind, name, t)
		res, err := arctic.ProcessFile(ctx, a.cfg, zstPath, t, out, nil)
		if err != nil {
			return mapErr(err)
		}
		if rerr := recordShards(a.cfg, t, 0, 0, name, out); rerr != nil {
			a.progressf("%s %s: indexed shards but: %v", name, t, rerr)
		}
		total += res.Records
		a.progressf("%s %s: %d shards, %d records imported", name, t, res.Shards, res.Records)
	}
	if total == 0 {
		return codeError(exitNoData, fmt.Errorf("no data for %s", name))
	}
	return nil
}

// ---- process -----------------------------------------------------------------

func (a *App) processCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "process <file.zst>...",
		Short: "Convert decompressed JSONL dumps into Parquet",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var rows []processRow
			var failures int
			for _, path := range args {
				t, ok := typeFromFilename(path)
				if !ok {
					a.progressf("%s: cannot tell comments from submissions by name; pass an RC_/RS_ or _comments/_submissions file", path)
					failures++
					continue
				}
				dst := out
				if dst == "" {
					dst = filepath.Join(filepath.Dir(path), strings.TrimSuffix(filepath.Base(path), ".zst"))
				}
				res, err := arctic.ProcessFile(ctx, a.cfg, path, t, dst, nil)
				if err != nil {
					a.progressf("%s: %v", path, err)
					failures++
					continue
				}
				rows = append(rows, processRow{
					File: filepath.Base(path), Type: string(t),
					Shards: res.Shards, Records: res.Records, Skipped: res.SkippedLines, Bytes: res.Bytes,
				})
			}
			if err := a.render(rows); err != nil {
				return codeError(exitError, err)
			}
			if failures > 0 {
				return codeError(exitPartial, fmt.Errorf("%d of %d files failed", failures, len(args)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output directory for the Parquet shards (default: alongside the input)")
	return cmd
}

type processRow struct {
	File    string `json:"file"`
	Type    string `json:"type"`
	Shards  int    `json:"shards"`
	Records int64  `json:"records"`
	Skipped int64  `json:"skipped_lines"`
	Bytes   int64  `json:"bytes"`
}

// ---- helpers -----------------------------------------------------------------

func (a *App) shiftClient() *shift.Client {
	c := shift.NewClient()
	if a.userAgent != "" {
		c.UserAgent = a.userAgent
	}
	if a.timeout > 0 {
		c.HTTP.Timeout = a.timeout
	}
	return c
}

func (a *App) downloadProgress(m arctic.Month, t arctic.Type) arctic.DownloadCallback {
	return func(p arctic.DownloadProgress) {
		if p.Message != "" {
			a.progressf("%s %s: %s", m, t, p.Message)
		}
	}
}

func (a *App) downloadProgressName(name string, t arctic.Type) arctic.DownloadCallback {
	return func(p arctic.DownloadProgress) {
		if p.Message != "" {
			a.progressf("%s %s: %s", name, t, p.Message)
		}
	}
}

func (a *App) fetchProgress(name string) shift.ProgressCallback {
	return func(p shift.Progress) {
		a.progressf("%s %s: %d records", name, p.Type, p.Count)
	}
}

// compressJSONL writes a zstd copy of a JSONL file next to it and returns the
// path. The processor reads .zst uniformly, so the API path compresses its
// output once and feeds the same pipeline the torrent path uses.
func compressJSONL(jsonlPath string) (string, error) {
	in, err := os.Open(jsonlPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	zstPath := strings.TrimSuffix(jsonlPath, ".jsonl") + ".zst"
	out, err := os.Create(zstPath)
	if err != nil {
		return "", err
	}
	enc, err := zstd.NewWriter(out)
	if err != nil {
		out.Close()
		return "", err
	}
	if _, err := enc.ReadFrom(in); err != nil {
		enc.Close()
		out.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		out.Close()
		return "", err
	}
	return zstPath, out.Close()
}

// normalizeName strips r/, /r/, u/, /user/, and a trailing slash from an entity
// argument so "r/golang" and "golang" both resolve to "golang".
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "/")
	for _, p := range []string{"r/", "u/", "user/"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
		}
	}
	return s
}

// typeFromFilename infers comments vs submissions from a dump file name.
func typeFromFilename(path string) (arctic.Type, bool) {
	b := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(b, "rc_"), strings.Contains(b, "_comments"), strings.HasPrefix(b, "comments"):
		return arctic.TypeComments, true
	case strings.HasPrefix(b, "rs_"), strings.Contains(b, "_submissions"), strings.HasPrefix(b, "submissions"):
		return arctic.TypeSubmissions, true
	}
	return "", false
}
