package cli

import (
	"fmt"
	"time"

	"github.com/tamnd/arctic-cli/arctic"
)

// parseEpoch turns a date flag into a Unix epoch. It accepts YYYY, YYYY-MM, and
// YYYY-MM-DD, plus a bare epoch in seconds. An empty string returns 0, which the
// callers read as "unbounded".
func parseEpoch(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Unix(), nil
		}
	}
	var n int64
	if _, err := fmt.Sscan(s, &n); err == nil && n > 0 {
		return n, nil
	}
	return 0, fmt.Errorf("date %q: want YYYY, YYYY-MM, YYYY-MM-DD, or an epoch", s)
}

// clampAfter raises an after-epoch to the earliest instant any source serves.
func clampAfter(after int64) int64 {
	if after < arctic.MinEpoch {
		return arctic.MinEpoch
	}
	return after
}

// parseTypes resolves a --kind flag into the list of record types to act on.
func parseTypes(kind string) ([]arctic.Type, error) {
	switch kind {
	case "", "both", "all":
		return []arctic.Type{arctic.TypeComments, arctic.TypeSubmissions}, nil
	case "comments", "comment", "c":
		return []arctic.Type{arctic.TypeComments}, nil
	case "submissions", "submission", "posts", "s":
		return []arctic.Type{arctic.TypeSubmissions}, nil
	default:
		return nil, fmt.Errorf("unknown kind %q (want comments, submissions, or both)", kind)
	}
}
