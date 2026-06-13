// Package arctic acquires, processes, and publishes the public Reddit archive.
// It pulls the monthly bulk dumps from the public torrent catalog, decompresses
// the zstd JSONL, writes Parquet with a stable schema, keeps a local index, and
// can publish the shards to a Hugging Face dataset repository. It carries no CLI
// knowledge and depends only on the standard library, golang.org/x, and a set of
// pure-Go libraries for BitTorrent, zstd, Parquet, and SQLite.
package arctic

import (
	"time"

	"github.com/tidwall/gjson"
)

// Comment is one Reddit comment as it lands in a Parquet shard. The columns are
// the queryable common case: identity, placement in time and community, the
// text, and the fields people filter on. created_utc is the raw epoch the source
// stores; created_at is the same instant as a timestamp so a reader can show a
// human time without converting.
type Comment struct {
	ID              string    `parquet:"id" json:"id"`
	Author          string    `parquet:"author" json:"author"`
	Subreddit       string    `parquet:"subreddit" json:"subreddit"`
	Body            string    `parquet:"body" json:"body"`
	Score           int64     `parquet:"score" json:"score"`
	CreatedUTC      int64     `parquet:"created_utc" json:"created_utc"`
	CreatedAt       time.Time `parquet:"created_at,timestamp" json:"created_at"`
	BodyLength      int32     `parquet:"body_length" json:"body_length"`
	LinkID          string    `parquet:"link_id" json:"link_id"`
	ParentID        string    `parquet:"parent_id" json:"parent_id"`
	Distinguished   string    `parquet:"distinguished" json:"distinguished"`
	AuthorFlairText string    `parquet:"author_flair_text" json:"author_flair_text"`
}

// Submission is one Reddit submission (a link or self post) as it lands in a
// Parquet shard.
type Submission struct {
	ID              string    `parquet:"id" json:"id"`
	Author          string    `parquet:"author" json:"author"`
	Subreddit       string    `parquet:"subreddit" json:"subreddit"`
	Title           string    `parquet:"title" json:"title"`
	Selftext        string    `parquet:"selftext" json:"selftext"`
	Score           int64     `parquet:"score" json:"score"`
	CreatedUTC      int64     `parquet:"created_utc" json:"created_utc"`
	CreatedAt       time.Time `parquet:"created_at,timestamp" json:"created_at"`
	TitleLength     int32     `parquet:"title_length" json:"title_length"`
	NumComments     int64     `parquet:"num_comments" json:"num_comments"`
	URL             string    `parquet:"url" json:"url"`
	Over18          bool      `parquet:"over_18" json:"over_18"`
	LinkFlairText   string    `parquet:"link_flair_text" json:"link_flair_text"`
	AuthorFlairText string    `parquet:"author_flair_text" json:"author_flair_text"`
}

// Type names the two record kinds. It is the value of the --type flag and the
// directory segment in the published layout.
type Type string

const (
	TypeComments    Type = "comments"
	TypeSubmissions Type = "submissions"
)

// Valid reports whether t is one of the known types.
func (t Type) Valid() bool { return t == TypeComments || t == TypeSubmissions }

// Prefix returns the dump file prefix for the type: RC for comments, RS for
// submissions.
func (t Type) Prefix() string {
	if t == TypeComments {
		return "RC"
	}
	return "RS"
}

// CommentFromJSON parses one dump line into a Comment. It returns ok=false when
// the line is not valid JSON, so the caller can count and skip it rather than
// abort the file. Missing scalars coerce to their zero value, matching the
// dumps' habit of omitting fields rather than nulling them.
func CommentFromJSON(line []byte) (Comment, bool) {
	if !gjson.ValidBytes(line) {
		return Comment{}, false
	}
	r := gjson.ParseBytes(line)
	body := r.Get("body").String()
	c := Comment{
		ID:              r.Get("id").String(),
		Author:          r.Get("author").String(),
		Subreddit:       r.Get("subreddit").String(),
		Body:            body,
		Score:           r.Get("score").Int(),
		CreatedUTC:      createdUTC(r),
		BodyLength:      int32(len([]rune(body))),
		LinkID:          r.Get("link_id").String(),
		ParentID:        r.Get("parent_id").String(),
		Distinguished:   r.Get("distinguished").String(),
		AuthorFlairText: r.Get("author_flair_text").String(),
	}
	c.CreatedAt = time.Unix(c.CreatedUTC, 0).UTC()
	return c, true
}

// SubmissionFromJSON parses one dump line into a Submission, with the same
// skip-on-bad-line contract as CommentFromJSON.
func SubmissionFromJSON(line []byte) (Submission, bool) {
	if !gjson.ValidBytes(line) {
		return Submission{}, false
	}
	r := gjson.ParseBytes(line)
	title := r.Get("title").String()
	s := Submission{
		ID:              r.Get("id").String(),
		Author:          r.Get("author").String(),
		Subreddit:       r.Get("subreddit").String(),
		Title:           title,
		Selftext:        r.Get("selftext").String(),
		Score:           r.Get("score").Int(),
		CreatedUTC:      createdUTC(r),
		TitleLength:     int32(len([]rune(title))),
		NumComments:     r.Get("num_comments").Int(),
		URL:             r.Get("url").String(),
		Over18:          r.Get("over_18").Bool(),
		LinkFlairText:   r.Get("link_flair_text").String(),
		AuthorFlairText: r.Get("author_flair_text").String(),
	}
	s.CreatedAt = time.Unix(s.CreatedUTC, 0).UTC()
	return s, true
}

// createdUTC reads created_utc, which the dumps store as either a number or a
// quoted number depending on the era.
func createdUTC(r gjson.Result) int64 {
	v := r.Get("created_utc")
	if v.Type == gjson.String {
		return v.Int()
	}
	return v.Int()
}
