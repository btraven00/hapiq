package geo

import (
	"slices"
	"testing"
)

// Regression: sizes come from the listing, not a hardcoded guess.
func TestParseDirectoryListingSizes(t *testing.T) {
	html := `<a href="/geo/series/GSE147nnn/GSE147405/">Parent Directory</a>    -
<a href="a.csv.gz">a.csv.gz</a>          2020-03-23 12:47   14M
<a href="b.csv.gz">b.csv.gz</a>          2020-03-23 12:47  242K
<a href="c.csv.gz">c.csv.gz</a>          2020-03-23 12:47  5.0M
<a href="d.txt">d.txt</a>                2020-03-23 12:47  512
<a href="e.txt">e.txt</a>`

	want := []listingEntry{
		{"a.csv.gz", 14 << 20},
		{"b.csv.gz", 242 << 10},
		{"c.csv.gz", 5 << 20},
		{"d.txt", 512},
		{"e.txt", -1},
	}
	if got := parseDirectoryListing(html); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
