package shift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/tamnd/arctic-cli/arctic"
)

// BaseURL is the public Arctic Shift API endpoint.
const BaseURL = "https://arctic-shift.photon-reddit.com"

// defaultUserAgent identifies the tool to the API operator so a polite client is
// distinguishable from an abusive one. The contact path lets them reach out
// rather than block silently.
const defaultUserAgent = "arctic-cli (+https://github.com/tamnd/arctic-cli)"

// ErrBlocked means the API rate-limited or refused the request and the caller
// should slow down or wait rather than retry hard. The CLI maps it to its
// "blocked" exit code.
var ErrBlocked = errors.New("arctic shift api blocked the request")

// maxRetries bounds the backoff loop on transient failures (connection errors
// and 5xx). A 429 does not consume a retry; it waits on the reset hint instead.
const maxRetries = 10

// Client is a polite HTTP client for the Arctic Shift API. The zero value is not
// usable; build one with NewClient.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string
	// Limiter paces requests so a long backfill stays under the API's tolerance.
	// A nil limiter means no client-side pacing.
	Limiter *rate.Limiter
}

// NewClient returns a Client with a descriptive User-Agent, a 60-second
// per-request timeout, and a limiter that allows roughly two requests per second
// with a small burst. Those defaults keep a multi-month backfill from tripping
// the server's rate limit while still moving at a useful pace.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		BaseURL:   BaseURL,
		UserAgent: defaultUserAgent,
		Limiter:   rate.NewLimiter(rate.Limit(2), 4),
	}
}

// get performs one GET against path with the given query params and returns the
// raw body. It paces on the limiter, retries transient failures with widening
// backoff, and waits out a 429 using the reset header when present. A 429 that
// will not clear, or a refusal, surfaces as ErrBlocked.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	full := c.BaseURL + path
	if len(params) > 0 {
		full += "?" + params.Encode()
	}

	retries := 0
	for {
		if c.Limiter != nil {
			if err := c.Limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		if err != nil {
			return nil, err
		}
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			// A cancelled or expired context is final, not transient.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			retries++
			if retries > maxRetries {
				return nil, fmt.Errorf("get %s: %w", path, err)
			}
			if werr := wait(ctx, backoff(retries)); werr != nil {
				return nil, werr
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			d := rateLimitWait(resp)
			_ = resp.Body.Close()
			retries++
			if retries > maxRetries {
				return nil, ErrBlocked
			}
			if werr := wait(ctx, d); werr != nil {
				return nil, werr
			}
			continue
		}

		// 403 is the API saying no in a way retries will not fix.
		if resp.StatusCode == http.StatusForbidden {
			_ = resp.Body.Close()
			return nil, ErrBlocked
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			retries++
			if retries > maxRetries {
				return nil, fmt.Errorf("get %s: server returned %d", path, resp.StatusCode)
			}
			if werr := wait(ctx, backoff(retries)); werr != nil {
				return nil, werr
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("get %s: status %d: %s", path, resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			retries++
			if retries > maxRetries {
				return nil, fmt.Errorf("get %s: read body: %w", path, err)
			}
			if werr := wait(ctx, backoff(retries)); werr != nil {
				return nil, werr
			}
			continue
		}
		return body, nil
	}
}

// GetMinDate returns the earliest available created_utc epoch for an entity,
// clamped up to arctic.MinEpoch. kind is "subreddit" or "user"; name carries no
// r/ or u/ prefix. A response with no data is reported as an error so the caller
// can treat an unknown entity as no-data.
func (c *Client) GetMinDate(ctx context.Context, kind, name string) (int64, error) {
	params := url.Values{}
	params.Set(entityParam(kind), name)

	body, err := c.get(ctx, "/api/utils/min", params)
	if err != nil {
		return 0, err
	}

	// The endpoint returns an RFC3339 instant under "data". Older shapes used a
	// trailing-millisecond form, so accept both.
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("decode min date: %w", err)
	}
	if result.Data == "" {
		return 0, fmt.Errorf("no data for %s %q", kind, name)
	}

	t, err := parseInstant(result.Data)
	if err != nil {
		return 0, fmt.Errorf("parse min date %q: %w", result.Data, err)
	}

	epoch := t.Unix()
	if epoch < arctic.MinEpoch {
		epoch = arctic.MinEpoch
	}
	return epoch, nil
}

// parseInstant accepts the RFC3339 variants the API has returned over time.
func parseInstant(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

// rateLimitWait reads the API's reset hint from a 429 response and returns how
// long to wait, defaulting to 30 seconds when the header is absent or stale.
func rateLimitWait(resp *http.Response) time.Duration {
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(epoch, 0))
			if d > time.Second {
				return d
			}
			return time.Second
		}
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 30 * time.Second
}

// backoff grows exponentially with the attempt count, capped at 60 seconds.
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// wait sleeps for d but returns early if the context is cancelled.
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// entityParam maps a kind to the query parameter the API expects.
func entityParam(kind string) string {
	if kind == "user" {
		return "author"
	}
	return "subreddit"
}

// endpointFor maps a record type to its search path segment.
func endpointFor(t arctic.Type) string {
	if t == arctic.TypeComments {
		return "comments"
	}
	return "posts"
}

// fieldsFor returns the field projection for a record type. Asking for a subset
// keeps each page small and matches the columns the schema keeps.
func fieldsFor(t arctic.Type) string {
	if t == arctic.TypeComments {
		return commentFields
	}
	return submissionFields
}

// commentFields is the projection the API validates for comment search. The set
// matches the columns the comment schema carries.
const commentFields = "id,author,body,created_utc,score,subreddit,link_id,parent_id,distinguished,author_flair_text"

// submissionFields is the projection for submission search.
const submissionFields = "id,title,selftext,author,created_utc,score,num_comments,subreddit,url,over_18,link_flair_text,author_flair_text"
