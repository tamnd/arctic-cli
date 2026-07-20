package arctic

import (
	"fmt"
	"strconv"
	"strings"
)

// commitMessage builds a clear summary and description for a hub commit from the
// files it carries. The dataset history then shows, at a glance, how many shards
// landed, which month and type they belong to, their shard range, and the total
// size, instead of a generic "arctic shard commit" on every commit.
func commitMessage(files []preparedFile) (summary, description string) {
	var adds, dels []preparedFile
	var total int64
	for _, f := range files {
		if f.op.Delete {
			dels = append(dels, f)
			continue
		}
		adds = append(adds, f)
		total += f.size
	}

	scope := commitScope(files)

	var s strings.Builder
	switch {
	case len(adds) > 0 && len(dels) == 0:
		s.WriteString("Add ")
	case len(adds) == 0 && len(dels) > 0:
		s.WriteString("Remove ")
	default:
		s.WriteString("Update ")
	}
	if scope != "" {
		s.WriteString(scope)
		s.WriteString(" ")
	}
	if rng := shardRange(adds); rng != "" {
		s.WriteString(rng)
		s.WriteString(" ")
	}
	var counts []string
	if len(adds) > 0 {
		counts = append(counts, fmt.Sprintf("%s, %s", plural(len(adds), "file"), humanBytes(total)))
	}
	if len(dels) > 0 {
		counts = append(counts, fmt.Sprintf("%d removed", len(dels)))
	}
	s.WriteString("(" + strings.Join(counts, ", ") + ")")
	summary = s.String()

	var d strings.Builder
	if scope != "" {
		fmt.Fprintf(&d, "%s\n\n", scope)
	}
	if len(adds) > 0 {
		fmt.Fprintf(&d, "Added %s (%s):\n", plural(len(adds), "file"), humanBytes(total))
		for _, f := range adds {
			fmt.Fprintf(&d, "- %s (%s)\n", f.op.PathInRepo, humanBytes(f.size))
		}
	}
	if len(dels) > 0 {
		if len(adds) > 0 {
			d.WriteString("\n")
		}
		fmt.Fprintf(&d, "Removed %s:\n", plural(len(dels), "file"))
		for _, f := range dels {
			fmt.Fprintf(&d, "- %s\n", f.op.PathInRepo)
		}
	}
	description = strings.TrimRight(d.String(), "\n")
	return summary, description
}

// commitScope returns "type YYYY-MM" when every file in the commit belongs to
// the same type and month, or "" when the commit spans more than one. The shard
// layout is data/{type}/{YYYY}/{MM}/{NNN}.parquet.
func commitScope(files []preparedFile) string {
	scope := ""
	for _, f := range files {
		t, ym, ok := shardScope(f.op.PathInRepo)
		if !ok {
			return ""
		}
		s := t + " " + ym
		if scope == "" {
			scope = s
		} else if scope != s {
			return ""
		}
	}
	return scope
}

// shardScope pulls the type and YYYY-MM out of a shard path, reporting false for
// anything that is not a shard (README, stats.csv, or an unexpected layout).
func shardScope(path string) (typ, yearMonth string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "data" || !strings.HasSuffix(parts[4], ".parquet") {
		return "", "", false
	}
	return parts[1], parts[2] + "-" + parts[3], true
}

// shardRange renders the shard numbers as "shard 061" or "shards 061-064" when
// they form one contiguous run, and "" otherwise (a gap, or non-shard files).
func shardRange(files []preparedFile) string {
	if len(files) == 0 {
		return ""
	}
	nums := make([]int, 0, len(files))
	for _, f := range files {
		parts := strings.Split(f.op.PathInRepo, "/")
		if len(parts) != 5 || !strings.HasSuffix(parts[4], ".parquet") {
			return ""
		}
		n, err := strconv.Atoi(strings.TrimSuffix(parts[4], ".parquet"))
		if err != nil {
			return ""
		}
		nums = append(nums, n)
	}
	lo, hi := nums[0], nums[0]
	for _, n := range nums[1:] {
		if n < lo {
			lo = n
		}
		if n > hi {
			hi = n
		}
	}
	if hi-lo+1 != len(nums) {
		return "" // a gap: not a clean range, so skip it rather than mislead
	}
	if lo == hi {
		return "shard " + pad(lo, 3)
	}
	return "shards " + pad(lo, 3) + "-" + pad(hi, 3)
}

// humanBytes renders a byte count as B, KB, MB, GB, or TB with one decimal
// place above the kilobyte, matching how a person reads a file size.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	val := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	i := -1
	for val >= unit && i < len(units)-1 {
		val /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", val, units[i])
}

// plural renders "1 file" or "3 files".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
