package arctic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// ErrCommitStall is returned when the pipeline makes no forward progress within
// cfg.MaxCommitStall: neither a processed shard nor a Hugging Face commit. The
// publish leaves the in-flight month uncommitted so a supervisor that restarts
// the process resumes cleanly, and the CLI maps this to exit code 75
// (EX_TEMPFAIL).
var ErrCommitStall = errors.New("arctic: commit stall, no forward progress within max-commit-stall")

// PublishOptions configures a publish run.
type PublishOptions struct {
	From     Month
	To       Month
	Types    []Type
	HFCommit bool // when false, process and stage shards but skip the hub commit
	Private  bool // create the dataset repo private if it does not exist
	Keep     bool // keep local .zst and shards after a commit instead of deleting
}

// Publish runs the resumable download to Parquet to Hugging Face pipeline over a
// month range. It skips months already in the stats ledger, picks pipeline or
// sequential mode from the hardware budget, and returns ErrCommitStall when a
// commit wedges so the caller can exit for a restart.
func Publish(ctx context.Context, cfg Config, opts PublishOptions, cb func(string)) error {
	if cb == nil {
		cb = func(string) {}
	}
	if len(opts.Types) == 0 {
		opts.Types = []Type{TypeComments, TypeSubmissions}
	}
	if opts.From == (Month{}) {
		opts.From = CatalogStart()
	}
	if opts.To == (Month{}) {
		opts.To = CatalogEnd()
	}

	hw := DetectHardware(cfg.WorkDir)
	budget := ComputeBudget(hw)
	cb(fmt.Sprintf("hardware: %s", hw))
	cb(fmt.Sprintf("budget: %s", budget))

	if free := hw.DiskFreeGB; free > 0 && free < float64(cfg.MinFreeGB) {
		return fmt.Errorf("only %.0f GB free, need at least %d GB to start", free, cfg.MinFreeGB)
	}

	statsPath := filepath.Join(cfg.RepoRoot, "stats.csv")
	committed, err := CommittedSet(statsPath)
	if err != nil {
		return fmt.Errorf("read stats ledger: %w", err)
	}

	var hf *HFClient
	if opts.HFCommit {
		token := os.Getenv(EnvHFToken)
		if token == "" {
			return fmt.Errorf("%s is required to commit to hugging face", EnvHFToken)
		}
		hf = NewHFClient(token, cfg.HFRepo)
		if err := hf.CreateDatasetRepo(ctx, opts.Private); err != nil {
			return fmt.Errorf("create dataset repo: %w", err)
		}
	}

	// Build the work list in dependency order, skipping committed months.
	var jobs []publishJob
	skipped := 0
	for _, m := range MonthRange(opts.From, opts.To) {
		for _, t := range opts.Types {
			key := StatsRow{Year: m.Year, Month: m.Month, Type: string(t)}.Key()
			if committed[key] {
				skipped++
				continue
			}
			jobs = append(jobs, publishJob{Month: m, Type: t})
		}
	}
	cb(fmt.Sprintf("plan: %d to do, %d already committed", len(jobs), skipped))
	if len(jobs) == 0 {
		return nil
	}

	p := &publisher{
		cfg:       cfg,
		opts:      opts,
		budget:    budget,
		hf:        hf,
		statsPath: statsPath,
		cb:        cb,
	}
	p.markProgress()

	if budget.Sequential {
		return p.runSequential(ctx, jobs)
	}
	return p.runPipeline(ctx, jobs)
}

type publishJob struct {
	Month Month
	Type  Type

	zstPath string
	result  ProcessResult
	durDown time.Duration
	durProc time.Duration
	durComm time.Duration
}

type publisher struct {
	cfg       Config
	opts      PublishOptions
	budget    Budget
	hf        *HFClient
	statsPath string
	cb        func(string)

	statsMu    sync.Mutex
	commitMu   sync.Mutex
	lastCommit atomic.Int64 // unix nanos of the last forward progress (processed shard or commit)
	stalled    atomic.Bool
}

// markProgress records that the pipeline moved forward, resetting the stall
// clock. A month can take longer to process than MaxCommitStall, so processing
// a shard counts as progress just like a completed commit; otherwise the
// watchdog would cancel a healthy long-running month before it ever commits.
func (p *publisher) markProgress() {
	p.lastCommit.Store(time.Now().UnixNano())
}

// runSequential takes each job fully through download, process, commit, ledger,
// and cleanup before starting the next. It never holds two big files at once.
func (p *publisher) runSequential(ctx context.Context, jobs []publishJob) error {
	stallCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go p.watchStall(stallCtx, cancel)

	for i := range jobs {
		if err := p.processOne(stallCtx, &jobs[i]); err != nil {
			if p.stalled.Load() {
				return ErrCommitStall
			}
			if stallCtx.Err() != nil && ctx.Err() == nil {
				return ErrCommitStall
			}
			return err
		}
	}
	return nil
}

// runPipeline overlaps the stages: month N+1 downloads while N processes and
// N-1 commits, bounded by the budget. Commits stay serialized through one
// worker since the hub commit API is one-at-a-time per repo.
func (p *publisher) runPipeline(ctx context.Context, jobs []publishJob) error {
	stallCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go p.watchStall(stallCtx, cancel)

	downloadCh := make(chan *publishJob, p.budget.MaxDownloads+1)
	processCh := make(chan *publishJob, p.budget.MaxProcess+1)
	commitCh := make(chan *publishJob, 2)

	g, gctx := errgroup.WithContext(stallCtx)

	// Feed jobs in order.
	g.Go(func() error {
		defer close(downloadCh)
		for i := range jobs {
			select {
			case <-gctx.Done():
				return nil
			case downloadCh <- &jobs[i]:
			}
		}
		return nil
	})

	// Download workers.
	var downWG sync.WaitGroup
	for i := 0; i < p.budget.MaxDownloads; i++ {
		downWG.Add(1)
		g.Go(func() error {
			defer downWG.Done()
			for job := range downloadCh {
				if gctx.Err() != nil {
					continue
				}
				if err := p.download(gctx, job); err != nil {
					p.cb(fmt.Sprintf("skip %s %s: download failed: %v", job.Month, job.Type, err))
					continue
				}
				select {
				case <-gctx.Done():
				case processCh <- job:
				}
			}
			return nil
		})
	}
	g.Go(func() error { downWG.Wait(); close(processCh); return nil })

	// Process workers.
	var procWG sync.WaitGroup
	for i := 0; i < p.budget.MaxProcess; i++ {
		procWG.Add(1)
		g.Go(func() error {
			defer procWG.Done()
			for job := range processCh {
				if gctx.Err() != nil {
					continue
				}
				if err := p.process(gctx, job); err != nil {
					p.cb(fmt.Sprintf("skip %s %s: process failed: %v", job.Month, job.Type, err))
					continue
				}
				select {
				case <-gctx.Done():
				case commitCh <- job:
				}
			}
			return nil
		})
	}
	g.Go(func() error { procWG.Wait(); close(commitCh); return nil })

	// Commit worker (single).
	g.Go(func() error {
		for job := range commitCh {
			if gctx.Err() != nil {
				continue
			}
			if err := p.commit(gctx, job); err != nil {
				var he *hfError
				if errors.As(err, &he) && he.kind == "fatal" {
					return err
				}
				p.cb(fmt.Sprintf("commit %s %s failed: %v", job.Month, job.Type, err))
				continue
			}
		}
		return nil
	})

	err := g.Wait()
	if p.stalled.Load() {
		return ErrCommitStall
	}
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// processOne runs a single job through all stages for the sequential mode.
func (p *publisher) processOne(ctx context.Context, job *publishJob) error {
	if err := p.download(ctx, job); err != nil {
		p.cb(fmt.Sprintf("skip %s %s: download failed: %v", job.Month, job.Type, err))
		return nil
	}
	if err := p.process(ctx, job); err != nil {
		p.cb(fmt.Sprintf("skip %s %s: process failed: %v", job.Month, job.Type, err))
		return nil
	}
	return p.commit(ctx, job)
}

func (p *publisher) download(ctx context.Context, job *publishJob) error {
	start := time.Now()
	p.cb(fmt.Sprintf("download %s %s", job.Month, job.Type))
	dest, err := DownloadMonth(ctx, p.cfg, job.Month, job.Type, func(pr DownloadProgress) {
		if pr.Phase == "downloading" && pr.Message != "" {
			p.cb(fmt.Sprintf("download %s %s: %s", job.Month, job.Type, pr.Message))
		}
	})
	if err != nil {
		// A month not yet published is a no-data condition, not a failure to log.
		var np *ErrNotPublished
		if errors.As(err, &np) {
			return err
		}
		return err
	}
	job.zstPath = dest
	job.durDown = time.Since(start)
	return nil
}

func (p *publisher) process(ctx context.Context, job *publishJob) error {
	start := time.Now()
	p.cb(fmt.Sprintf("process %s %s", job.Month, job.Type))
	pathFn := func(n int) string {
		return filepath.Join(p.cfg.RepoRoot, ShardHFPath(job.Type, job.Month, n))
	}
	res, err := ProcessFileTo(ctx, p.cfg, job.zstPath, job.Type, pathFn, func(int64) {
		p.markProgress()
	})
	if err != nil {
		return err
	}
	job.result = res
	job.durProc = time.Since(start)
	p.cb(fmt.Sprintf("process %s %s: %d shards, %d records", job.Month, job.Type, res.Shards, res.Records))
	return nil
}

func (p *publisher) commit(ctx context.Context, job *publishJob) error {
	start := time.Now()

	if p.opts.HFCommit && p.hf != nil {
		// One commit per repo at a time.
		p.commitMu.Lock()
		err := p.commitShards(ctx, job)
		p.commitMu.Unlock()
		if err != nil {
			return err
		}
		p.markProgress()
	}

	job.durComm = time.Since(start)

	// Record the month in the ledger and regenerate the README.
	if err := p.recordAndREADME(ctx, job); err != nil {
		return err
	}
	p.cb(fmt.Sprintf("committed %s %s", job.Month, job.Type))

	if !p.opts.Keep {
		p.cleanup(job)
	}
	return nil
}

func (p *publisher) commitShards(ctx context.Context, job *publishJob) error {
	var ops []HFOp
	for n := 0; n < job.result.Shards; n++ {
		rel := ShardHFPath(job.Type, job.Month, n)
		ops = append(ops, HFOp{
			LocalPath:  filepath.Join(p.cfg.RepoRoot, rel),
			PathInRepo: filepath.ToSlash(rel),
		})
	}
	if len(ops) == 0 {
		return nil
	}
	return p.hf.UploadFiles(ctx, ops)
}

func (p *publisher) recordAndREADME(ctx context.Context, job *publishJob) error {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()

	row := StatsRow{
		Year:         job.Month.Year,
		Month:        job.Month.Month,
		Type:         string(job.Type),
		Shards:       job.result.Shards,
		Count:        job.result.Records,
		SizeBytes:    job.result.Bytes,
		DurDownloadS: job.durDown.Seconds(),
		DurProcessS:  job.durProc.Seconds(),
		DurCommitS:   job.durComm.Seconds(),
		CommittedAt:  nowRFC3339(),
	}
	if fi, err := os.Stat(job.zstPath); err == nil {
		row.ZstBytes = fi.Size()
	}
	if err := AppendStats(p.statsPath, row); err != nil {
		return fmt.Errorf("append stats: %w", err)
	}

	rows, err := ReadStats(p.statsPath)
	if err != nil {
		return err
	}
	readme := GenerateREADME(p.cfg, rows)
	readmePath := filepath.Join(p.cfg.RepoRoot, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
		return err
	}
	// Push the ledger and README alongside the shards when committing.
	if p.opts.HFCommit && p.hf != nil {
		p.commitMu.Lock()
		err := p.hf.UploadFiles(ctx, []HFOp{
			{LocalPath: p.statsPath, PathInRepo: "stats.csv"},
			{LocalPath: readmePath, PathInRepo: "README.md"},
		})
		p.commitMu.Unlock()
		if err != nil {
			return err
		}
		p.markProgress()
	}
	return nil
}

func (p *publisher) cleanup(job *publishJob) {
	if job.zstPath != "" {
		_ = os.Remove(job.zstPath)
	}
	dir := filepath.Dir(filepath.Join(p.cfg.RepoRoot, ShardHFPath(job.Type, job.Month, 0)))
	_ = os.RemoveAll(dir)
}

// idleSince reports how long the pipeline has gone without forward progress.
func (p *publisher) idleSince() time.Duration {
	return time.Since(time.Unix(0, p.lastCommit.Load()))
}

// stalledOut reports whether the idle time has crossed cfg.MaxCommitStall. A
// non-positive MaxCommitStall disables the watchdog.
func (p *publisher) stalledOut() bool {
	maxStall := p.cfg.MaxCommitStall
	return maxStall > 0 && p.idleSince() > maxStall
}

// watchStall cancels the run when the pipeline makes no forward progress within
// cfg.MaxCommitStall, which surfaces as ErrCommitStall. Processing a shard
// counts as progress, so a long month does not trip the watchdog before it can
// commit; only a genuine wedge does.
func (p *publisher) watchStall(ctx context.Context, cancel context.CancelFunc) {
	if p.cfg.MaxCommitStall <= 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.stalledOut() {
				p.cb(fmt.Sprintf("commit stall: no progress for %s, restarting", p.idleSince().Round(time.Second)))
				p.stalled.Store(true)
				cancel()
				return
			}
		}
	}
}
