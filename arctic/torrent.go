package arctic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// DownloadProgress is one progress sample streamed to a DownloadCallback while a
// torrent file is fetched.
type DownloadProgress struct {
	Phase      string // "metadata" | "downloading" | "done"
	BytesDone  int64
	BytesTotal int64
	SpeedBps   float64
	Peers      int
	Elapsed    time.Duration
	Message    string
}

// DownloadCallback receives progress samples. It may be nil.
type DownloadCallback func(DownloadProgress)

// ErrCorruption marks a downloaded file that failed integrity checks. The caller
// should delete it and pull again from scratch.
type ErrCorruption struct{ Msg string }

func (e *ErrCorruption) Error() string { return e.Msg }

// ErrTransient marks a timeout or network failure. The partial data on disk is
// fine to resume from.
type ErrTransient struct{ Msg string }

func (e *ErrTransient) Error() string { return e.Msg }

// IsCorruption reports whether err is or wraps an ErrCorruption.
func IsCorruption(err error) bool {
	var ce *ErrCorruption
	return errors.As(err, &ce)
}

// IsTransient reports whether err is or wraps an ErrTransient.
func IsTransient(err error) bool {
	var te *ErrTransient
	return errors.As(err, &te)
}

// stallTimeouts widen across attempts so a slow swarm gets more rope each retry
// before we drop and re-add the torrent.
var stallTimeouts = []time.Duration{
	5 * time.Minute,
	8 * time.Minute,
	15 * time.Minute,
	20 * time.Minute,
	30 * time.Minute,
}

// magnetURI builds a magnet link from an info hash and an announce list. Adding
// the trackers up front means the client can find peers without a separate
// AddTrackers call.
func magnetURI(infoHash string, trackers []string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(infoHash)
	for _, tr := range trackers {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tr))
	}
	return b.String()
}

// newTorrentClient builds a client that stores data under dataDir. The default
// storage is the file backend, which is pure Go; we never reach for the SQLite
// piece store, so the whole package stays cgo-free.
func newTorrentClient(dataDir string) (*torrent.Client, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.Seed = false
	cfg.NoUpload = false // a little tit-for-tat keeps peers from choking us
	cfg.DisableIPv6 = false
	return torrent.NewClient(cfg)
}

// findFile locates a file inside a torrent by its in-torrent path, matching on
// either the full path or the base name so callers can pass either form.
func findFile(t *torrent.Torrent, pathInTorrent string) *torrent.File {
	want := filepath.ToSlash(pathInTorrent)
	base := filepath.Base(want)
	for _, f := range t.Files() {
		p := filepath.ToSlash(f.Path())
		if p == want || filepath.Base(p) == base {
			return f
		}
	}
	return nil
}

// selectOnly downloads f and deselects every other file in the torrent so we
// never pull data we did not ask for.
func selectOnly(t *torrent.Torrent, f *torrent.File) {
	for _, other := range t.Files() {
		if other == f {
			continue
		}
		other.SetPriority(torrent.PiecePriorityNone)
	}
	f.Download()
}

// DownloadFile fetches one file out of a torrent to destPath. It adds the magnet
// with the given trackers, waits for metadata, selects only the wanted file,
// streams progress, and re-adds the torrent on a stall with a widening timeout.
// On completion it runs QuickValidateZst and reports corruption versus transient
// failure so the caller can decide whether to delete or resume.
func DownloadFile(ctx context.Context, infoHash, pathInTorrent, destPath string,
	trackers []string, cb DownloadCallback) error {

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	start := time.Now()
	var lastErr error
	for attempt := 0; attempt < len(stallTimeouts); attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		stall := stallTimeouts[attempt]
		err := downloadAttempt(ctx, infoHash, pathInTorrent, destPath, trackers, stall, start, cb)
		if err == nil {
			if verr := QuickValidateZst(destPath); verr != nil {
				_ = os.Remove(destPath)
				return &ErrCorruption{Msg: fmt.Sprintf("validate %s: %v", filepath.Base(destPath), verr)}
			}
			if cb != nil {
				cb(DownloadProgress{Phase: "done", Elapsed: time.Since(start)})
			}
			return nil
		}
		if IsCorruption(err) || ctx.Err() != nil {
			return err
		}
		lastErr = err
		logf("download %s: attempt %d stalled (%v), retrying", filepath.Base(destPath), attempt+1, err)
	}
	if lastErr == nil {
		lastErr = &ErrTransient{Msg: "download did not complete"}
	}
	return lastErr
}

func downloadAttempt(ctx context.Context, infoHash, pathInTorrent, destPath string,
	trackers []string, stall time.Duration, start time.Time, cb DownloadCallback) error {

	// anacrolix stores data relative to its DataDir, so point the client at the
	// directory that holds destPath and let it lay the file out by the torrent's
	// own path. We rename into place afterward if the layouts differ.
	dataDir := filepath.Dir(destPath)
	cl, err := newTorrentClient(dataDir)
	if err != nil {
		return &ErrTransient{Msg: err.Error()}
	}
	// Close explicitly rather than deferring so the mmap flush happens before we
	// read the file for validation.
	closed := false
	closeClient := func() {
		if !closed {
			cl.Close()
			closed = true
		}
	}
	defer closeClient()

	if cb != nil {
		cb(DownloadProgress{Phase: "metadata"})
	}

	t, err := cl.AddMagnet(magnetURI(infoHash, trackers))
	if err != nil {
		return &ErrTransient{Msg: fmt.Sprintf("add magnet: %v", err)}
	}

	// Wait for metadata so we can pick the file out of the torrent.
	metaCtx, metaCancel := context.WithTimeout(ctx, stall)
	select {
	case <-t.GotInfo():
	case <-metaCtx.Done():
		metaCancel()
		return &ErrTransient{Msg: "timed out waiting for torrent metadata"}
	}
	metaCancel()

	f := findFile(t, pathInTorrent)
	if f == nil {
		return &ErrCorruption{Msg: fmt.Sprintf("file %q not in torrent", pathInTorrent)}
	}
	selectOnly(t, f)
	total := f.Length()

	// Poll progress and watch for a stall: only real byte movement counts as
	// activity, since the swarm can sit connected at 99 percent with nothing
	// flowing.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastBytes := int64(-1)
	lastMove := time.Now()
	var lastSample time.Time
	var lastSampleBytes int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		done := f.BytesCompleted()
		now := time.Now()
		if done != lastBytes {
			lastBytes = done
			lastMove = now
		}
		if done >= total {
			closeClient() // flush mmap to disk before we hand the path back
			// If the file landed at a torrent-relative path, move it to destPath.
			if err := placeFile(t, f, dataDir, destPath); err != nil {
				return &ErrTransient{Msg: err.Error()}
			}
			return nil
		}
		if now.Sub(lastMove) > stall {
			return &ErrTransient{Msg: fmt.Sprintf("no progress for %s", stall.Round(time.Second))}
		}
		if cb != nil {
			var speed float64
			if !lastSample.IsZero() {
				dt := now.Sub(lastSample).Seconds()
				if dt > 0 {
					speed = float64(done-lastSampleBytes) / dt
				}
			}
			lastSample = now
			lastSampleBytes = done
			peers := t.Stats().ActivePeers
			cb(DownloadProgress{
				Phase:      "downloading",
				BytesDone:  done,
				BytesTotal: total,
				SpeedBps:   speed,
				Peers:      peers,
				Elapsed:    time.Since(start),
				Message:    progressMessage(speed, peers, done, total),
			})
		}
	}
}

// placeFile moves the torrent's on-disk file to destPath when the client laid it
// out under a torrent-relative subdirectory.
func placeFile(t *torrent.Torrent, f *torrent.File, dataDir, destPath string) error {
	laid := filepath.Join(dataDir, filepath.FromSlash(f.Path()))
	if laid == destPath {
		return nil
	}
	if _, err := os.Stat(laid); err != nil {
		// Some torrents are single-file at the root; the client may already have
		// written destPath directly.
		if _, derr := os.Stat(destPath); derr == nil {
			return nil
		}
		return fmt.Errorf("expected file at %s: %w", laid, err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.Rename(laid, destPath)
}

func progressMessage(speed float64, peers int, done, total int64) string {
	if speed <= 0 {
		return fmt.Sprintf("%d peers, connecting", peers)
	}
	msg := fmt.Sprintf("%.1f MB/s, %d peers", speed/1e6, peers)
	if total > 0 && done < total {
		remain := float64(total-done) / speed
		msg += fmt.Sprintf(", ETA %s", (time.Duration(remain) * time.Second).Round(time.Second))
	}
	return msg
}

// DownloadMonth resolves a month and type to its torrent and in-torrent path,
// then fetches the .zst into cfg.ZstPath. It returns the destination path.
func DownloadMonth(ctx context.Context, cfg Config, m Month, t Type, cb DownloadCallback) (string, error) {
	infoHash, err := InfoHashFor(m)
	if err != nil {
		return "", err
	}
	dest := cfg.ZstPath(m, t)
	if _, statErr := os.Stat(dest); statErr == nil {
		if QuickValidateZst(dest) == nil {
			return dest, nil // already have a good copy
		}
		_ = os.Remove(dest)
	}
	pathInTorrent := FilePathInTorrent(m, t)
	if err := DownloadFile(ctx, infoHash, pathInTorrent, dest, arcticTrackers, cb); err != nil {
		return "", err
	}
	return dest, nil
}

// DownloadSubreddit fetches a single subreddit's comments or submissions file
// from the per-subreddit bundle torrent into destDir.
func DownloadSubreddit(ctx context.Context, destDir, name string, t Type, cb DownloadCallback) (string, error) {
	pathInTorrent := SubredditFilePathInTorrent(name, t)
	dest := filepath.Join(destDir, fmt.Sprintf("%s_%s.zst", name, t))
	if _, statErr := os.Stat(dest); statErr == nil {
		if QuickValidateZst(dest) == nil {
			return dest, nil
		}
		_ = os.Remove(dest)
	}
	if err := DownloadFile(ctx, SubredditTorrentInfoHash, pathInTorrent, dest, arcticTrackers, cb); err != nil {
		return "", err
	}
	return dest, nil
}

// SubredditInTorrent reports whether name has a file in the per-subreddit
// bundle, so a caller can pick the torrent path over the API without a failed
// download. It reads the torrent's file manifest once and caches it.
func SubredditInTorrent(ctx context.Context, name string) (bool, error) {
	names, err := subredditPresence(ctx)
	if err != nil {
		return false, err
	}
	_, ok := names[strings.ToLower(name)]
	return ok, nil
}

var (
	subredditPresenceOnce  sync.Once
	subredditPresenceNames map[string]struct{}
	subredditPresenceErr   error
)

// subredditPresence loads the set of subreddit names present in the bundle
// torrent's manifest. The bundle has tens of thousands of files, so we read the
// manifest once and reuse it.
func subredditPresence(ctx context.Context) (map[string]struct{}, error) {
	subredditPresenceOnce.Do(func() {
		dir, err := os.MkdirTemp("", "arctic-subs-")
		if err != nil {
			subredditPresenceErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		files, err := torrentFileList(ctx, SubredditTorrentInfoHash, dir, 5*time.Minute)
		if err != nil {
			subredditPresenceErr = err
			return
		}
		names := make(map[string]struct{}, len(files)/2)
		for _, p := range files {
			base := filepath.Base(p)
			if !strings.HasSuffix(base, ".zst") {
				continue
			}
			stem := strings.TrimSuffix(base, ".zst")
			for _, suffix := range []string{"_comments", "_submissions"} {
				if strings.HasSuffix(stem, suffix) {
					names[strings.ToLower(strings.TrimSuffix(stem, suffix))] = struct{}{}
				}
			}
		}
		subredditPresenceNames = names
	})
	return subredditPresenceNames, subredditPresenceErr
}

// torrentFileList opens a torrent for metadata only and returns its file paths.
// No data is downloaded; every file is deselected.
func torrentFileList(ctx context.Context, infoHash, dataDir string, timeout time.Duration) ([]string, error) {
	cl, err := newTorrentClient(dataDir)
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	t, err := cl.AddMagnet(magnetURI(infoHash, arcticTrackers))
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	metaCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-t.GotInfo():
	case <-metaCtx.Done():
		return nil, fmt.Errorf("timed out waiting for metadata")
	}
	files := t.Files()
	paths := make([]string, 0, len(files))
	for _, f := range files {
		f.SetPriority(torrent.PiecePriorityNone)
		paths = append(paths, f.Path())
	}
	return paths, nil
}

// FetchZstSizes queries the catalog torrents for each month's .zst file size,
// downloading no data, and caches the result to zst_sizes.json under DataDir.
// The keys are "type/YYYY-MM".
func FetchZstSizes(ctx context.Context, cfg Config) (map[string]int64, error) {
	cachePath := filepath.Join(cfg.DataDir, "zst_sizes.json")
	sizes := make(map[string]int64, 512)

	dir, err := os.MkdirTemp(cfg.DataDir, "arctic-sizes-")
	if err != nil {
		dir, err = os.MkdirTemp("", "arctic-sizes-")
		if err != nil {
			return nil, err
		}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	collect := func(infoHash string, timeout time.Duration) {
		sub := filepath.Join(dir, infoHash)
		files, lerr := torrentFileListWithLengths(ctx, infoHash, sub, timeout)
		if lerr != nil {
			logf("catalog: %s: %v", infoHash, lerr)
			return
		}
		for _, fl := range files {
			typ, ym := parseTorrentFilePath(fl.path)
			if typ != "" && ym != "" {
				sizes[typ+"/"+ym] = fl.length
			}
		}
	}

	collect(bundleInfoHash, 5*time.Minute)
	for _, ym := range KnownMonthlyHashes() {
		if ctx.Err() != nil {
			break
		}
		collect(monthlyInfoHashes[ym], 2*time.Minute)
	}

	if err := saveJSON(cachePath, sizes); err != nil {
		return sizes, fmt.Errorf("save catalog: %w", err)
	}
	return sizes, nil
}

type torrentFileLength struct {
	path   string
	length int64
}

func torrentFileListWithLengths(ctx context.Context, infoHash, dataDir string, timeout time.Duration) ([]torrentFileLength, error) {
	cl, err := newTorrentClient(dataDir)
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	t, err := cl.AddMagnet(magnetURI(infoHash, arcticTrackers))
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	metaCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-t.GotInfo():
	case <-metaCtx.Done():
		return nil, fmt.Errorf("timed out waiting for metadata")
	}
	files := t.Files()
	out := make([]torrentFileLength, 0, len(files))
	for _, f := range files {
		f.SetPriority(torrent.PiecePriorityNone)
		out = append(out, torrentFileLength{path: f.Path(), length: f.Length()})
	}
	return out, nil
}

// parseTorrentFilePath pulls (type, "YYYY-MM") out of a torrent file path,
// handling both the bundle layout (reddit/comments/RC_2005-12.zst) and the
// monthly layout (RC_2024-01.zst at the root).
func parseTorrentFilePath(path string) (typ, ym string) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".zst") {
		return "", ""
	}
	stem := strings.TrimSuffix(base, ".zst")
	if len(stem) < 10 || stem[2] != '_' {
		return "", ""
	}
	ym = stem[3:]
	if len(ym) != 7 {
		return "", ""
	}
	switch stem[:2] {
	case "RC":
		typ = "comments"
	case "RS":
		typ = "submissions"
	default:
		return "", ""
	}
	return typ, ym
}

// QuickValidateZst is a fast sanity check on a .zst file. It verifies the zstd
// magic at the head, that the final bytes are not zero-padded (which is what a
// missing boundary piece leaves behind), and that sampled interior regions are
// not zero-filled holes from incomplete pieces.
func QuickValidateZst(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	size := fi.Size()
	if size < 8 {
		return fmt.Errorf("file too small (%d bytes)", size)
	}

	// zstd regular-frame magic 0xFD2FB528, little-endian on disk.
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("read magic: %w", err)
	}
	if magic[0] != 0x28 || magic[1] != 0xb5 || magic[2] != 0x2f || magic[3] != 0xfd {
		return fmt.Errorf("bad zstd magic %02x%02x%02x%02x", magic[0], magic[1], magic[2], magic[3])
	}

	// A valid zstd stream ends in non-zero data (a checksum or frame header). An
	// all-zero tail means the last piece never landed.
	if _, err := f.Seek(-16, io.SeekEnd); err == nil {
		var tail [16]byte
		if _, err := io.ReadFull(f, tail[:]); err == nil && isAllZero(tail[:]) {
			return fmt.Errorf("zero-padded tail: boundary piece missing")
		}
	}

	// Sample the interior to catch large zero-filled holes.
	const sampleSize = 4096
	if size > sampleSize*4 {
		var sample [sampleSize]byte
		for _, pct := range []int64{25, 50, 75} {
			offset := size * pct / 100
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				continue
			}
			if n, err := io.ReadFull(f, sample[:]); err == nil && n == sampleSize && isAllZero(sample[:]) {
				return fmt.Errorf("zero-filled region at %d%%: incomplete download", pct)
			}
		}
	}
	return nil
}

func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
