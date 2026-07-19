package arctic

import (
	"path/filepath"
	"reflect"
	"testing"
)

const sampleReleases = `[
  {
    "tag_name": "2026_07",
    "name": "Reddit dump 2026-07",
    "body": "Monthly dump with RC_2026-07.zst and RS_2026-07.zst.\nTorrent: https://academictorrents.com/details/AAAA1111bbbb2222cccc3333dddd4444eeee5555"
  },
  {
    "tag_name": "subreddits_2026_07",
    "name": "Subreddit metadata 2026-07",
    "body": "Per-subreddit metadata, files subreddits_2026-07.zst.\nhttps://academictorrents.com/details/9999999999999999999999999999999999999999"
  },
  {
    "tag_name": "2026_08",
    "name": "Reddit dump 2026-08",
    "body": "RS_2026-08.zst and RC_2026-08.zst\nmagnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678&dn=x"
  },
  {
    "tag_name": "tooling-v1.2",
    "name": "Tooling release",
    "body": "No month, no torrent here."
  }
]`

func TestParseReleasesMonthlyOnly(t *testing.T) {
	got, err := parseReleases([]byte(sampleReleases))
	if err != nil {
		t.Fatalf("parseReleases: %v", err)
	}
	want := map[string]string{
		"2026-07": "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"2026-08": "1234567890abcdef1234567890abcdef12345678",
	}
	// want has exactly the two dump months: the subreddit-metadata release (which
	// also names a month and a hash but carries no RC_/RS_ files) and the tooling
	// release are both excluded.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseReleases = %v, want %v", got, want)
	}
}

func TestRegisterMonthlyHashesResolves(t *testing.T) {
	ym := "2026-11"
	if _, err := InfoHashFor(Month{Year: 2026, Month: 11}); err == nil {
		t.Fatal("2026-11 unexpectedly known before refresh")
	}
	RegisterMonthlyHashes(map[string]string{ym: "abcabcabcabcabcabcabcabcabcabcabcabcabca"})
	defer delete(monthlyOverride, ym)

	h, err := InfoHashFor(Month{Year: 2026, Month: 11})
	if err != nil {
		t.Fatalf("InfoHashFor after register: %v", err)
	}
	if h != "abcabcabcabcabcabcabcabcabcabcabcabcabca" {
		t.Fatalf("hash = %q", h)
	}
	if end := CatalogEnd(); end.String() != "2026-11" {
		t.Fatalf("CatalogEnd = %s, want 2026-11", end)
	}
}

func TestRegisterMonthlyHashesRejectsJunk(t *testing.T) {
	RegisterMonthlyHashes(map[string]string{
		"not-a-month": "abcabcabcabcabcabcabcabcabcabcabcabcabca",
		"2027-01":     "tooshort",
	})
	if _, ok := monthlyOverride["not-a-month"]; ok {
		t.Fatal("accepted a malformed month")
	}
	if _, ok := monthlyOverride["2027-01"]; ok {
		t.Fatal("accepted a malformed hash")
	}
}

func TestMonthlyCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog-monthly.json")
	if m, err := LoadMonthlyCache(path); err != nil || m != nil {
		t.Fatalf("missing cache should be (nil,nil), got (%v,%v)", m, err)
	}
	want := map[string]string{"2026-07": "aaaa1111bbbb2222cccc3333dddd4444eeee5555"}
	if err := SaveMonthlyCache(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadMonthlyCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}
