package arctic

import (
	"fmt"
	"sort"
	"time"
)

// The bulk dumps come in two distribution shapes. Everything from 2005-12
// through 2023-12 lives in one bundle torrent; 2024-01 onward is one torrent per
// month. A month after 2023-12 that is absent from the monthly map is reported
// as "not yet published" rather than an error, so the catalog can lag the
// present without breaking.

// bundleInfoHash is the torrent carrying 2005-12 .. 2023-12, with files at
// reddit/comments/RC_YYYY-MM.zst and reddit/submissions/RS_YYYY-MM.zst.
const bundleInfoHash = "9c263fc85366c1ef8f5bb9da0203f4c8c8db75f4"

// bundleLastYear and bundleLastMonth mark the last month inside the bundle. A
// later month resolves to a monthly torrent instead.
const (
	bundleLastYear  = 2023
	bundleLastMonth = 12
)

// catalogStartYear and catalogStartMonth mark the first month with data.
const (
	catalogStartYear  = 2005
	catalogStartMonth = 12
)

// arcticTrackers is the announce list every dump torrent uses.
var arcticTrackers = []string{
	"https://academictorrents.com/announce.php",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"udp://open.stealth.si:80/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// monthlyInfoHashes maps "YYYY-MM" to the info hash of that month's torrent, for
// the months after the bundle ends. New months are added by appending one line.
var monthlyInfoHashes = map[string]string{
	// 2024
	"2024-01": "ac88546145ca3227e2b90e51ab477c4527dd8b90",
	"2024-02": "5969ae3e21bb481fea63bf649ec933c222c1f824",
	"2024-03": "deef710de36929e0aa77200fddda73c86142372c",
	"2024-04": "ad4617a3e9c1f52405197fc088b28a8018e12a7a",
	"2024-05": "4f60634d96d35158842cd58b495dc3b444d78b0d",
	"2024-06": "dcdecc93ca9a9d758c045345112771cef5b4989a",
	"2024-07": "6e5300446bd9b328d0b812cdb3022891e086d9ec",
	"2024-08": "8c2d4b00ce8ff9d45e335bed106fe9046c60adb0",
	"2024-09": "43a6e113d6ecacf38e58ecc6caa28d68892dd8af",
	"2024-10": "507dfcda29de9936dd77ed4f34c6442dc675c98f",
	"2024-11": "a1b490117808d9541ab9e3e67a3447e2f4f48f01",
	"2024-12": "eb2017da9f63a49460dde21a4ebe3b7c517f3ad9",
	// 2025
	"2025-01": "4fd14d4c3d792e0b1c5cf6b1d9516c48ba6c4a24",
	"2025-02": "2f873e0b15da5ee29b63e586c0ab1dedd3508870",
	"2025-03": "69d5e046e15c02182430879f50d62b18fe1404fb",
	"2025-04": "552f34df5b830d18f98b69541e7e84f2658346b9",
	"2025-05": "186a0f85a52ff4f1b08677cd312423ace9b34976",
	"2025-06": "bec5590bd3bc6c0f2d868f36ec92bec1aff4480e",
	"2025-07": "b6a7ccf72368a7d39c018c423e01bc15aa551122",
	"2025-08": "c71a97c1f7f676c56963c4e15a81f20afb0109be",
	"2025-09": "a92ce24b4180e4aa9295353f4d26f050031e3058",
	"2025-10": "cb4fa22ea76ea0a2bb38885b27323c94a5d9d16c",
	"2025-11": "2d056b22743718ac81915f25b094b6226668663f",
	"2025-12": "481bf2eac43172ae724fd6c75dbcb8e27de77734",
	// 2026
	"2026-01": "8412b89151101d88c915334c45d9c223169a1a60",
	"2026-02": "c5ba00048236b60f819dbf010e9034d24fc291fb",
	"2026-03": "668087bb8c8c9c763b27a1a4c5e7fcb6add25f2c",
	"2026-04": "85d017ddd06920534187e7d45f21c7cec90c9bca",
	"2026-05": "55199eff9368cde1f5c1262dd7c1af09f7503ea5",
	"2026-06": "3bac8bd352bbb74bbb23df4273cf3da5d66ee5a5",
}

// Per-subreddit bundle: the top forty thousand communities repackaged by
// subreddit over 2005-06 .. 2023-12, files at
// reddit/subreddits23/{name}_{comments,submissions}.zst.
const (
	SubredditTorrentInfoHash = "56aa49f9653ba545f48df2e33679f014d2829c10"
	SubredditTorrentURL      = "https://academictorrents.com/download/56aa49f9653ba545f48df2e33679f014d2829c10.torrent"
	SubredditTorrentSubdir   = "subreddits23"
)

// MinEpoch is the earliest instant any source serves: 2005-01-01. Requests
// before this are clamped.
const MinEpoch int64 = 1104537600

// Month is a year and month in the catalog.
type Month struct {
	Year  int
	Month int
}

// String renders a month as YYYY-MM.
func (m Month) String() string { return fmt.Sprintf("%04d-%02d", m.Year, m.Month) }

// Before reports whether m comes strictly before other.
func (m Month) Before(other Month) bool {
	if m.Year != other.Year {
		return m.Year < other.Year
	}
	return m.Month < other.Month
}

// After reports whether m comes strictly after other.
func (m Month) After(other Month) bool { return other.Before(m) }

// Next returns the month after m.
func (m Month) Next() Month {
	if m.Month == 12 {
		return Month{Year: m.Year + 1, Month: 1}
	}
	return Month{Year: m.Year, Month: m.Month + 1}
}

// ParseMonth parses a "YYYY-MM" string into a Month.
func ParseMonth(s string) (Month, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return Month{}, fmt.Errorf("month %q: want YYYY-MM", s)
	}
	return Month{Year: t.Year(), Month: int(t.Month())}, nil
}

// CatalogStart is the first month in the catalog.
func CatalogStart() Month { return Month{Year: catalogStartYear, Month: catalogStartMonth} }

// CatalogEnd is the last month the catalog can resolve: the later of the bundle
// end and the newest monthly torrent in the map.
func CatalogEnd() Month {
	end := Month{Year: bundleLastYear, Month: bundleLastMonth}
	for ym := range monthlyInfoHashes {
		m, err := ParseMonth(ym)
		if err == nil && m.After(end) {
			end = m
		}
	}
	return end
}

// MonthRange returns every month from start to end inclusive.
func MonthRange(start, end Month) []Month {
	var out []Month
	for m := start; !m.After(end); m = m.Next() {
		out = append(out, m)
	}
	return out
}

// ErrNotPublished marks a month after the bundle that has no monthly torrent
// yet. It is a no-data condition, not a failure.
type ErrNotPublished struct{ Month Month }

func (e *ErrNotPublished) Error() string {
	return fmt.Sprintf("%s is not yet published", e.Month)
}

// InfoHashFor resolves the torrent info hash that carries a month. Months in the
// bundle range return the bundle hash; later months return their monthly hash,
// or ErrNotPublished when the map has not caught up.
func InfoHashFor(m Month) (string, error) {
	if !m.Before(CatalogStart()) && !m.After(Month{Year: bundleLastYear, Month: bundleLastMonth}) {
		return bundleInfoHash, nil
	}
	if m.Before(CatalogStart()) {
		return "", fmt.Errorf("%s is before the catalog starts (%s)", m, CatalogStart())
	}
	h, ok := monthlyInfoHashes[m.String()]
	if !ok {
		return "", &ErrNotPublished{Month: m}
	}
	return h, nil
}

// InBundle reports whether a month lives in the bundle torrent.
func InBundle(m Month) bool {
	return !m.Before(CatalogStart()) && !m.After(Month{Year: bundleLastYear, Month: bundleLastMonth})
}

// FilePathInTorrent returns the path of a month+type file inside its torrent.
// The bundle nests by type (reddit/comments/RC_YYYY-MM.zst); a monthly torrent
// holds the bare file at its root.
func FilePathInTorrent(m Month, t Type) string {
	name := fmt.Sprintf("%s_%s.zst", t.Prefix(), m)
	if InBundle(m) {
		return fmt.Sprintf("reddit/%s/%s", t, name)
	}
	return name
}

// SubredditFilePathInTorrent returns the path of a subreddit+type file inside
// the per-subreddit bundle.
func SubredditFilePathInTorrent(name string, t Type) string {
	return fmt.Sprintf("reddit/%s/%s_%s.zst", SubredditTorrentSubdir, name, t)
}

// KnownMonthlyHashes returns the monthly "YYYY-MM" keys in sorted order, for
// catalog listings.
func KnownMonthlyHashes() []string {
	keys := make([]string, 0, len(monthlyInfoHashes))
	for k := range monthlyInfoHashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
