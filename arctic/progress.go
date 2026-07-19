package arctic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ShardProgress records how much of a (month, type) has been committed to the
// hub. A publish updates it after every batch of shards, so a restart resumes
// mid-month instead of reprocessing from shard zero. It stays local; the hub
// only ever sees finished shards, the ledger, and the README.
type ShardProgress struct {
	Engine  string `json:"engine"` // shard boundaries differ per engine, so resume only within one
	Shards  int    `json:"shards"` // shards committed so far (also the next shard's index)
	Records int64  `json:"records"`
	Bytes   int64  `json:"bytes"`
}

// ProgressPath is where in-flight month progress persists between runs.
func ProgressPath(cfg Config) string {
	return filepath.Join(cfg.RepoRoot, "publish-progress.json")
}

// LoadProgress reads the progress ledger, returning an empty map when none
// exists yet.
func LoadProgress(path string) (map[string]ShardProgress, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]ShardProgress{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]ShardProgress
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]ShardProgress{}
	}
	return m, nil
}

// SaveProgress writes the progress ledger as indented JSON, replacing it
// atomically so a crash mid-write cannot leave a torn file.
func SaveProgress(path string, m map[string]ShardProgress) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
