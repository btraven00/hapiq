package geo

import (
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestParseSummaryToSearchResult(t *testing.T) {
	summary := DocSum{
		ID: "200123456",
		Items: []Item{
			{Name: "title", Content: "Single-cell RNA-seq of human liver"},
			{Name: "Accession", Content: "GSE123456"},
			{Name: "taxon", Content: "Homo sapiens"},
			{Name: "entryType", Content: "GSE"},
			{Name: "gdsType", Content: "Expression profiling by high throughput sequencing"},
			{Name: "PDAT", Content: "2023/04/15"},
			{Name: "SSInfo", Items: []Item{
				{Name: "samples", Content: "42"},
				{Name: "subsets", Content: "3"},
			}},
		},
	}

	got := parseSummaryToSearchResult(summary)

	want := downloaders.SearchResult{
		Accession:   "GSE123456",
		Title:       "Single-cell RNA-seq of human liver",
		Organism:    "Homo sapiens",
		EntryType:   "GSE",
		DatasetType: "Expression profiling by high throughput sequencing",
		Date:        "2023-04",
		SampleCount: 42,
	}

	if got.Accession != want.Accession {
		t.Errorf("Accession = %q, want %q", got.Accession, want.Accession)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
	if got.Organism != want.Organism {
		t.Errorf("Organism = %q, want %q", got.Organism, want.Organism)
	}
	if got.EntryType != want.EntryType {
		t.Errorf("EntryType = %q, want %q", got.EntryType, want.EntryType)
	}
	if got.DatasetType != want.DatasetType {
		t.Errorf("DatasetType = %q, want %q", got.DatasetType, want.DatasetType)
	}
	if got.Date != want.Date {
		t.Errorf("Date = %q, want %q", got.Date, want.Date)
	}
	if got.SampleCount != want.SampleCount {
		t.Errorf("SampleCount = %d, want %d", got.SampleCount, want.SampleCount)
	}
}

func TestParseSummaryToSearchResult_NSamplesField(t *testing.T) {
	// Some GEO entries use n_samples instead of SSInfo
	summary := DocSum{
		ID: "100000001",
		Items: []Item{
			{Name: "Accession", Content: "GDS1001"},
			{Name: "n_samples", Content: "8"},
			{Name: "PDAT", Content: "2020/06"},
		},
	}

	got := parseSummaryToSearchResult(summary)

	if got.SampleCount != 8 {
		t.Errorf("SampleCount = %d, want 8", got.SampleCount)
	}
	if got.Date != "2020-06" {
		t.Errorf("Date = %q, want %q", got.Date, "2020-06")
	}
}

func TestGEODownloader_SearchImplementsSearcher(t *testing.T) {
	d := NewGEODownloader()
	var _ downloaders.Searcher = d // compile-time interface check
}
