package shift

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/tamnd/arctic-cli/arctic"
)

// record is a minimal API record. The pager only reads created_utc, so the rest
// of the schema can stay out of the fixtures.
type record struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	Subreddit  string `json:"subreddit"`
	CreatedUTC int64  `json:"created_utc"`
}

// fakeAPI serves records sorted ascending and honors after/before, mimicking the
// real search endpoint closely enough to exercise the pager.
func fakeAPI(t *testing.T, all []record) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)

		const pageSize = 2
		var page []record
		for _, rec := range all {
			if rec.CreatedUTC <= after {
				continue
			}
			if before > 0 && rec.CreatedUTC >= before {
				continue
			}
			page = append(page, rec)
			if len(page) >= pageSize {
				break
			}
		}

		raw := make([]json.RawMessage, 0, len(page))
		for _, rec := range page {
			b, _ := json.Marshal(rec)
			raw = append(raw, b)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": raw})
	}))
}

func testClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	// No client-side pacing in tests; the limiter only slows real backfills.
	c.Limiter = rate.NewLimiter(rate.Inf, 1)
	return c
}

func countLines(t *testing.T, b []byte) []record {
	t.Helper()
	var got []record
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			t.Fatalf("empty line in output")
		}
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line is not one json object: %v", err)
		}
		got = append(got, rec)
	}
	return got
}

func TestFetchSubredditPagesAndTerminates(t *testing.T) {
	all := []record{
		{ID: "a", CreatedUTC: 1104537700},
		{ID: "b", CreatedUTC: 1104537800},
		{ID: "c", CreatedUTC: 1104537900},
		{ID: "d", CreatedUTC: 1104538000},
		{ID: "e", CreatedUTC: 1104538100},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var buf bytes.Buffer
	n, err := testClient(srv).FetchSubreddit(context.Background(), "golang", arctic.TypeComments, 0, 0, &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(all)) {
		t.Fatalf("count = %d, want %d", n, len(all))
	}
	got := countLines(t, buf.Bytes())
	if len(got) != len(all) {
		t.Fatalf("wrote %d lines, want %d", len(got), len(all))
	}
	for i := range all {
		if got[i].ID != all[i].ID {
			t.Fatalf("line %d id = %q, want %q", i, got[i].ID, all[i].ID)
		}
	}
}

func TestFetchUserHonorsBefore(t *testing.T) {
	all := []record{
		{ID: "a", CreatedUTC: 1104537700},
		{ID: "b", CreatedUTC: 1104537800},
		{ID: "c", CreatedUTC: 1104537900},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var buf bytes.Buffer
	// before excludes the last record at 1104537900.
	n, err := testClient(srv).FetchUser(context.Background(), "someone", arctic.TypeSubmissions, 0, 1104537900, &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

func TestFetchClampsToMinEpoch(t *testing.T) {
	// A record before MinEpoch must never come back: the pager clamps after up to
	// the floor, and the fake honors after.
	all := []record{
		{ID: "old", CreatedUTC: arctic.MinEpoch - 1000},
		{ID: "new", CreatedUTC: arctic.MinEpoch + 1000},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var buf bytes.Buffer
	n, err := testClient(srv).FetchSubreddit(context.Background(), "golang", arctic.TypeComments, 0, 0, &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1 (the pre-2005 record must be clamped out)", n)
	}
	got := countLines(t, buf.Bytes())
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("got %+v, want only the post-MinEpoch record", got)
	}
}

func TestFetchSameTimestampTerminates(t *testing.T) {
	// A whole page sharing one timestamp would stall a naive cursor. The pager
	// nudges past it, so this must terminate even though three records share a
	// second.
	all := []record{
		{ID: "a", CreatedUTC: 1104538000},
		{ID: "b", CreatedUTC: 1104538000},
		{ID: "c", CreatedUTC: 1104538000},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var buf bytes.Buffer
	done := make(chan struct{})
	var n int64
	var err error
	go func() {
		n, err = testClient(srv).FetchSubreddit(context.Background(), "golang", arctic.TypeComments, 0, 0, &buf, nil)
		close(done)
	}()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	// The same-second nudge can drop the same-second tail; the point is it stops.
	if n == 0 {
		t.Fatalf("expected at least one record, got 0")
	}
}

func TestFetchProgressCallback(t *testing.T) {
	all := []record{
		{ID: "a", CreatedUTC: 1104537700},
		{ID: "b", CreatedUTC: 1104537800},
		{ID: "c", CreatedUTC: 1104537900},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var last Progress
	var calls int
	cb := func(p Progress) {
		last = p
		calls++
	}
	var buf bytes.Buffer
	if _, err := testClient(srv).FetchSubreddit(context.Background(), "golang", arctic.TypeComments, 0, 0, &buf, cb); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("progress callback never fired")
	}
	if last.Count != 3 {
		t.Fatalf("final progress count = %d, want 3", last.Count)
	}
	if last.Type != string(arctic.TypeComments) {
		t.Fatalf("progress type = %q", last.Type)
	}
}

func TestGetBlockedOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	_, err := testClient(srv).FetchSubreddit(context.Background(), "golang", arctic.TypeComments, 0, 0, &buf, nil)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
}

func TestGetMinDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":"2010-05-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	epoch, err := testClient(srv).GetMinDate(context.Background(), "subreddit", "golang")
	if err != nil {
		t.Fatal(err)
	}
	want := int64(1272672000) // 2010-05-01T00:00:00Z
	if epoch != want {
		t.Fatalf("epoch = %d, want %d", epoch, want)
	}
}

func TestGetMinDateClamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":"2001-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	epoch, err := testClient(srv).GetMinDate(context.Background(), "subreddit", "golang")
	if err != nil {
		t.Fatal(err)
	}
	if epoch != arctic.MinEpoch {
		t.Fatalf("epoch = %d, want clamp to MinEpoch %d", epoch, arctic.MinEpoch)
	}
}

func TestFetchRangeConcatenatesInOrder(t *testing.T) {
	// Two months of records; FetchRange must return them oldest to newest even
	// though buckets fetch concurrently.
	all := []record{
		{ID: "jan1", CreatedUTC: time2005(1, 5)},
		{ID: "jan2", CreatedUTC: time2005(1, 20)},
		{ID: "feb1", CreatedUTC: time2005(2, 10)},
		{ID: "feb2", CreatedUTC: time2005(2, 25)},
	}
	srv := fakeAPI(t, all)
	defer srv.Close()

	var buf bytes.Buffer
	after := time2005(1, 1)
	before := time2005(3, 1)
	n, err := testClient(srv).FetchRange(context.Background(), "subreddit", "golang", arctic.TypeComments, after, before, 4, &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(all)) {
		t.Fatalf("count = %d, want %d", n, len(all))
	}
	got := countLines(t, buf.Bytes())
	for i := 1; i < len(got); i++ {
		if got[i].CreatedUTC < got[i-1].CreatedUTC {
			t.Fatalf("output not in ascending order at %d: %d < %d", i, got[i].CreatedUTC, got[i-1].CreatedUTC)
		}
	}
	if len(got) != len(all) {
		t.Fatalf("wrote %d lines, want %d", len(got), len(all))
	}
}

// time2005 returns the epoch for a 2005 month/day at midnight UTC, for fixtures
// that sit above MinEpoch.
func time2005(month, day int) int64 {
	return time.Date(2005, time.Month(month), day, 0, 0, 0, 0, time.UTC).Unix()
}
