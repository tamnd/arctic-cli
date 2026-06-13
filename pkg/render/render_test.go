package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

type rec struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Score int       `json:"score"`
	Tags  []string  `json:"tags"`
	When  time.Time `json:"when"`
	URL   string    `json:"url"`
}

func sample() []rec {
	return []rec{
		{ID: "a1", Title: "first", Score: 10, Tags: []string{"x", "y"}, When: time.Unix(0, 0).UTC(), URL: "https://example.com/a1"},
		{ID: "b2", Title: "second", Score: 3, Tags: nil, URL: "https://example.com/b2"},
	}
}

func render(t *testing.T, f Format, fields []string, noHeader bool, tmpl string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := New(&buf, f, fields, noHeader, tmpl).Render(sample()); err != nil {
		t.Fatalf("render %s: %v", f, err)
	}
	return buf.String()
}

func TestJSONL(t *testing.T) {
	out := render(t, FormatJSONL, nil, false, "")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], `"id":"a1"`) {
		t.Errorf("missing id in %q", lines[0])
	}
}

func TestCSVHeaderAndFields(t *testing.T) {
	out := render(t, FormatCSV, []string{"id", "score", "tags"}, false, "")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "id,score,tags" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "a1,10,x;y" {
		t.Errorf("row = %q", lines[1])
	}
}

func TestCSVNoHeader(t *testing.T) {
	out := render(t, FormatCSV, []string{"id"}, true, "")
	if strings.Contains(out, "id\n") && strings.HasPrefix(out, "id") {
		t.Errorf("header should be omitted: %q", out)
	}
}

func TestURL(t *testing.T) {
	out := render(t, FormatURL, nil, false, "")
	if out != "https://example.com/a1\nhttps://example.com/b2\n" {
		t.Errorf("url output = %q", out)
	}
}

func TestTemplate(t *testing.T) {
	// A template reads the lowercase json keys, the same names --fields and the
	// table header use, not the Go struct field names.
	out := render(t, FormatTable, nil, false, "{{.id}}={{.score}}")
	if out != "a1=10\nb2=3\n" {
		t.Errorf("template output = %q", out)
	}
}

func TestTemplateJoinSlice(t *testing.T) {
	out := render(t, FormatTable, nil, false, `{{.id}}:{{join "," .tags}}`)
	// First record has tags x,y; second has none.
	if out != "a1:x,y\nb2:\n" {
		t.Errorf("template join output = %q", out)
	}
}

func TestTableHeaderUppercase(t *testing.T) {
	out := render(t, FormatTable, []string{"id", "title"}, false, "")
	if !strings.HasPrefix(out, "ID") || !strings.Contains(out, "TITLE") {
		t.Errorf("table header = %q", out)
	}
}
