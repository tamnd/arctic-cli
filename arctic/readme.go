package arctic

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateREADME builds the dataset card for the published Hugging Face repo. It
// renders the YAML front matter the hub uses for indexing, the stable schema for
// both record types, a coverage summary built from the ledger, and short usage
// notes. It is deliberately compact: a reader can take in the whole card without
// scrolling past boilerplate. rows is the full stats ledger; an empty ledger
// still produces a valid card so the README exists from the first commit.
func GenerateREADME(cfg Config, rows []StatsRow) string {
	var b strings.Builder

	// YAML front matter. The hub reads license, language, tags, and size_category
	// from here. Keep it minimal and honest.
	b.WriteString("---\n")
	b.WriteString("license: cc-by-4.0\n")
	b.WriteString("language:\n  - en\n")
	b.WriteString("pretty_name: Reddit Public Archive (Parquet)\n")
	b.WriteString("tags:\n  - reddit\n  - social-media\n  - text\n  - parquet\n")
	b.WriteString("task_categories:\n  - text-classification\n  - text-generation\n")
	b.WriteString(fmt.Sprintf("size_categories:\n  - %s\n", sizeCategory(totalRecords(rows))))
	b.WriteString("---\n\n")

	b.WriteString("# Reddit Public Archive\n\n")
	b.WriteString("Monthly Reddit comment and submission dumps, decompressed from the public ")
	b.WriteString("archive and rewritten as Parquet with a stable schema. Each month and record ")
	b.WriteString("type is sharded so a reader can pull one month without downloading the whole set.\n\n")

	writeSchemaSection(&b)
	writeLayoutSection(&b)
	writeCoverageSection(&b, rows)
	writeUsageSection(&b, cfg)
	writeProvenanceSection(&b)

	return b.String()
}

func writeSchemaSection(b *strings.Builder) {
	b.WriteString("## Schema\n\n")
	b.WriteString("`created_utc` is the raw epoch second the source stores. `created_at` is the ")
	b.WriteString("same instant as a timestamp so a query can group by day without converting. ")
	b.WriteString("`body_length` and `title_length` count runes, not bytes.\n\n")

	b.WriteString("### comments\n\n")
	b.WriteString("| column | type | notes |\n")
	b.WriteString("| --- | --- | --- |\n")
	b.WriteString("| id | string | comment id |\n")
	b.WriteString("| author | string | account name, `[deleted]` when removed |\n")
	b.WriteString("| subreddit | string | community name without `r/` |\n")
	b.WriteString("| body | string | comment text |\n")
	b.WriteString("| score | int64 | net votes at dump time |\n")
	b.WriteString("| created_utc | int64 | epoch seconds |\n")
	b.WriteString("| created_at | timestamp | created_utc as a time |\n")
	b.WriteString("| body_length | int32 | rune count of body |\n")
	b.WriteString("| link_id | string | parent submission fullname |\n")
	b.WriteString("| parent_id | string | parent comment or submission fullname |\n")
	b.WriteString("| distinguished | string | moderator/admin marker, empty when none |\n")
	b.WriteString("| author_flair_text | string | author flair, empty when none |\n\n")

	b.WriteString("### submissions\n\n")
	b.WriteString("| column | type | notes |\n")
	b.WriteString("| --- | --- | --- |\n")
	b.WriteString("| id | string | submission id |\n")
	b.WriteString("| author | string | account name, `[deleted]` when removed |\n")
	b.WriteString("| subreddit | string | community name without `r/` |\n")
	b.WriteString("| title | string | post title |\n")
	b.WriteString("| selftext | string | self-post body, empty for link posts |\n")
	b.WriteString("| score | int64 | net votes at dump time |\n")
	b.WriteString("| created_utc | int64 | epoch seconds |\n")
	b.WriteString("| created_at | timestamp | created_utc as a time |\n")
	b.WriteString("| title_length | int32 | rune count of title |\n")
	b.WriteString("| num_comments | int64 | comment count at dump time |\n")
	b.WriteString("| url | string | linked url or permalink |\n")
	b.WriteString("| over_18 | bool | NSFW marker |\n")
	b.WriteString("| link_flair_text | string | post flair, empty when none |\n")
	b.WriteString("| author_flair_text | string | author flair, empty when none |\n\n")
}

func writeLayoutSection(b *strings.Builder) {
	b.WriteString("## Layout\n\n")
	b.WriteString("Shards live under `data/{type}/{year}/{month}/{NNN}.parquet`, for example ")
	b.WriteString("`data/comments/2011/03/000.parquet`. Files are zstd-compressed Parquet with ")
	b.WriteString("a row group of 131072 rows. Read a glob to load a month, year, or the whole set.\n\n")
}

func writeCoverageSection(b *strings.Builder, rows []StatsRow) {
	b.WriteString("## Coverage\n\n")
	committed := committedRows(rows)
	if len(committed) == 0 {
		b.WriteString("No months have been published yet.\n\n")
		return
	}

	var comments, submissions StatsRow
	var months = map[string]bool{}
	for _, r := range committed {
		months[fmt.Sprintf("%04d-%02d", r.Year, r.Month)] = true
		switch r.Type {
		case string(TypeComments):
			comments.Count += r.Count
			comments.SizeBytes += r.SizeBytes
			comments.Shards += r.Shards
		case string(TypeSubmissions):
			submissions.Count += r.Count
			submissions.SizeBytes += r.SizeBytes
			submissions.Shards += r.Shards
		}
	}
	first, last := monthBounds(committed)

	b.WriteString(fmt.Sprintf("Months covered: **%d** (%s to %s).\n\n", len(months), first, last))
	b.WriteString("| type | months | shards | records | parquet size |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	b.WriteString(fmt.Sprintf("| comments | %d | %d | %s | %s |\n",
		countMonths(committed, string(TypeComments)), comments.Shards,
		humanCount(comments.Count), humanBytes(comments.SizeBytes)))
	b.WriteString(fmt.Sprintf("| submissions | %d | %d | %s | %s |\n",
		countMonths(committed, string(TypeSubmissions)), submissions.Shards,
		humanCount(submissions.Count), humanBytes(submissions.SizeBytes)))
	b.WriteString(fmt.Sprintf("| **total** | %d | %d | %s | %s |\n\n",
		len(months), comments.Shards+submissions.Shards,
		humanCount(comments.Count+submissions.Count),
		humanBytes(comments.SizeBytes+submissions.SizeBytes)))
}

func writeUsageSection(b *strings.Builder, cfg Config) {
	repo := cfg.HFRepo
	if repo == "" {
		repo = DefaultHFRepo
	}
	b.WriteString("## Usage\n\n")

	b.WriteString("DuckDB, straight off the hub:\n\n")
	b.WriteString("```sql\n")
	b.WriteString(fmt.Sprintf("SELECT subreddit, count(*) AS comments\n"+
		"FROM 'hf://datasets/%s/data/comments/2011/*/*.parquet'\n"+
		"GROUP BY subreddit ORDER BY comments DESC LIMIT 10;\n", repo))
	b.WriteString("```\n\n")

	b.WriteString("pandas:\n\n")
	b.WriteString("```python\n")
	b.WriteString("import pandas as pd\n")
	b.WriteString(fmt.Sprintf("df = pd.read_parquet(\n"+
		"    \"hf://datasets/%s/data/submissions/2011/03/000.parquet\"\n)\n", repo))
	b.WriteString("```\n\n")

	b.WriteString("datasets:\n\n")
	b.WriteString("```python\n")
	b.WriteString("from datasets import load_dataset\n")
	b.WriteString(fmt.Sprintf("ds = load_dataset(\n"+
		"    \"%s\",\n"+
		"    data_files=\"data/comments/2011/03/*.parquet\",\n"+
		"    split=\"train\",\n)\n", repo))
	b.WriteString("```\n\n")
}

func writeProvenanceSection(b *strings.Builder) {
	b.WriteString("## Source and license\n\n")
	b.WriteString("The records come from the public Reddit archive distributed over Academic ")
	b.WriteString("Torrents and are republished here on the Hugging Face Hub as Parquet. The ")
	b.WriteString("content is user-generated and remains subject to its original terms; this card ")
	b.WriteString("covers the Parquet packaging only, released under CC BY 4.0. Treat author names ")
	b.WriteString("and text as personal data and follow the relevant takedown and privacy rules ")
	b.WriteString("when you use it.\n")
}

// committedRows returns only the rows that carry a commit timestamp.
func committedRows(rows []StatsRow) []StatsRow {
	var out []StatsRow
	for _, r := range rows {
		if r.CommittedAt != "" {
			out = append(out, r)
		}
	}
	return out
}

func totalRecords(rows []StatsRow) int64 {
	var n int64
	for _, r := range committedRows(rows) {
		n += r.Count
	}
	return n
}

func countMonths(rows []StatsRow, typ string) int {
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Type == typ {
			seen[fmt.Sprintf("%04d-%02d", r.Year, r.Month)] = true
		}
	}
	return len(seen)
}

func monthBounds(rows []StatsRow) (first, last string) {
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, fmt.Sprintf("%04d-%02d", r.Year, r.Month))
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", ""
	}
	return keys[0], keys[len(keys)-1]
}

// sizeCategory maps a record count to the hub's size_categories bucket.
func sizeCategory(n int64) string {
	switch {
	case n >= 1_000_000_000_000:
		return "n>1T"
	case n >= 100_000_000_000:
		return "100B<n<1T"
	case n >= 10_000_000_000:
		return "10B<n<100B"
	case n >= 1_000_000_000:
		return "1B<n<10B"
	case n >= 100_000_000:
		return "100M<n<1B"
	case n >= 10_000_000:
		return "10M<n<100M"
	case n >= 1_000_000:
		return "1M<n<10M"
	case n >= 100_000:
		return "100K<n<1M"
	default:
		return "n<100K"
	}
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
