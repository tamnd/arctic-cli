package arctic

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// StatsRow is one committed (month, type) entry in the publish ledger. The
// durations record how long each stage took so the README can show throughput.
type StatsRow struct {
	Year         int
	Month        int
	Type         string
	Shards       int
	Count        int64
	SizeBytes    int64 // total Parquet size across the month's shards
	ZstBytes     int64 // size of the source .zst, 0 when not recorded
	DurDownloadS float64
	DurProcessS  float64
	DurCommitS   float64
	CommittedAt  string // RFC3339; empty means not yet committed
}

// Key identifies a row as "YYYY-MM-type", the same key CommittedSet uses for
// resume.
func (r StatsRow) Key() string {
	return fmt.Sprintf("%04d-%02d-%s", r.Year, r.Month, r.Type)
}

var statsHeader = []string{
	"year", "month", "type", "shards", "count", "size_bytes", "zst_bytes",
	"dur_download_s", "dur_process_s", "dur_commit_s", "committed_at",
}

// ReadStats reads the stats.csv ledger. A missing file returns no rows and no
// error so a fresh run starts clean.
func ReadStats(path string) ([]StatsRow, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	if _, err := r.Read(); err != nil { // header
		return nil, nil
	}
	var rows []StatsRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 11 {
			continue
		}
		var row StatsRow
		row.Year, _ = strconv.Atoi(rec[0])
		row.Month, _ = strconv.Atoi(rec[1])
		row.Type = rec[2]
		row.Shards, _ = strconv.Atoi(rec[3])
		row.Count, _ = strconv.ParseInt(rec[4], 10, 64)
		row.SizeBytes, _ = strconv.ParseInt(rec[5], 10, 64)
		row.ZstBytes, _ = strconv.ParseInt(rec[6], 10, 64)
		row.DurDownloadS, _ = strconv.ParseFloat(rec[7], 64)
		row.DurProcessS, _ = strconv.ParseFloat(rec[8], 64)
		row.DurCommitS, _ = strconv.ParseFloat(rec[9], 64)
		row.CommittedAt = rec[10]
		rows = append(rows, row)
	}
	return rows, nil
}

// WriteStats writes the full ledger, deduplicating by key (last write wins) and
// sorting by month then type. The write is atomic via a temp file and rename.
func WriteStats(path string, rows []StatsRow) error {
	byKey := make(map[string]StatsRow, len(rows))
	for _, r := range rows {
		byKey[r.Key()] = r
	}
	merged := make([]StatsRow, 0, len(byKey))
	for _, r := range byKey {
		merged = append(merged, r)
	}
	sort.Slice(merged, func(i, j int) bool {
		a, b := merged[i], merged[j]
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		if a.Month != b.Month {
			return a.Month < b.Month
		}
		return a.Type < b.Type
	})

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stats-*.csv")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath)
	}()

	w := csv.NewWriter(tmp)
	w.Write(statsHeader)
	for _, r := range merged {
		w.Write(statsRecord(r))
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// AppendStats merges one row into the ledger at path, replacing any existing row
// with the same key.
func AppendStats(path string, row StatsRow) error {
	rows, err := ReadStats(path)
	if err != nil {
		return err
	}
	rows = append(rows, row)
	return WriteStats(path, rows)
}

func statsRecord(r StatsRow) []string {
	return []string{
		strconv.Itoa(r.Year),
		strconv.Itoa(r.Month),
		r.Type,
		strconv.Itoa(r.Shards),
		strconv.FormatInt(r.Count, 10),
		strconv.FormatInt(r.SizeBytes, 10),
		strconv.FormatInt(r.ZstBytes, 10),
		strconv.FormatFloat(r.DurDownloadS, 'f', 2, 64),
		strconv.FormatFloat(r.DurProcessS, 'f', 2, 64),
		strconv.FormatFloat(r.DurCommitS, 'f', 2, 64),
		r.CommittedAt,
	}
}

// CommittedSet reads the ledger and returns the set of committed keys
// ("YYYY-MM-type"), used to skip work that already landed on a resumed run. A
// row counts as committed once it carries a CommittedAt timestamp.
func CommittedSet(path string) (map[string]bool, error) {
	rows, err := ReadStats(path)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.CommittedAt != "" {
			set[r.Key()] = true
		}
	}
	return set, nil
}

// nowRFC3339 is the timestamp format the ledger stores CommittedAt in.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
