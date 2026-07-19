package arctic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// monthlyReleasesAPI lists the arctic_shift monthly dump releases. One page of
// 100 covers roughly eight years of monthly releases, so the compiled-in map
// carries the older tail and a single request keeps the recent months current.
const monthlyReleasesAPI = "https://api.github.com/repos/ArthurHeitmann/arctic_shift/releases?per_page=100"

var (
	monthRe    = regexp.MustCompile(`(20\d{2})[-_](0[1-9]|1[0-2])`)
	atHashRe   = regexp.MustCompile(`academictorrents\.com/(?:details|download)/([0-9a-fA-F]{40})`)
	btihHashRe = regexp.MustCompile(`(?i)urn:btih:([0-9a-fA-F]{40})`)
)

// ghRelease is the slice of the GitHub releases payload we read.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
}

// MonthlyCachePath is where refreshed month hashes persist between runs.
func MonthlyCachePath(cfg Config) string {
	return filepath.Join(cfg.DataDir, "catalog-monthly.json")
}

// FetchMonthlyHashes pulls the latest monthly dump releases and returns a
// month ("YYYY-MM") to info-hash map for the comment/submission dumps.
func FetchMonthlyHashes(ctx context.Context, client *http.Client) (map[string]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, monthlyReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arctic-cli")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return parseReleases(data)
}

// parseReleases extracts month to info-hash pairs from the releases payload. A
// release qualifies only when it names a month, carries an Academic Torrents (or
// magnet) hash, and its body references that month's RC_/RS_ dump files, so
// subreddit-metadata and tooling releases are skipped.
func parseReleases(data []byte) (map[string]string, error) {
	var rels []ghRelease
	if err := json.Unmarshal(data, &rels); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	out := make(map[string]string)
	for _, r := range rels {
		ym := monthFrom(r.TagName)
		if ym == "" {
			ym = monthFrom(r.Name)
		}
		if ym == "" {
			continue
		}
		if !strings.Contains(r.Body, "RC_"+ym) && !strings.Contains(r.Body, "RS_"+ym) {
			continue
		}
		h := hashFrom(r.Body)
		if h == "" {
			continue
		}
		out[ym] = strings.ToLower(h)
	}
	return out, nil
}

// monthFrom pulls the first YYYY-MM (accepting a "_" separator) out of s.
func monthFrom(s string) string {
	m := monthRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + "-" + m[2]
}

// hashFrom pulls a torrent info hash from an Academic Torrents URL or magnet.
func hashFrom(s string) string {
	if m := atHashRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := btihHashRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// LoadMonthlyCache reads the refreshed month hashes, returning nil when no cache
// exists yet.
func LoadMonthlyCache(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// SaveMonthlyCache writes the month hashes as sorted, indented JSON.
func SaveMonthlyCache(path string, m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
