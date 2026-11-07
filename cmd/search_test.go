package cmd

import (
	"strings"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestRunSearchUnknownSource(t *testing.T) {
	err := runSearch(nil, []string{"bogus", "x"})
	if err == nil || !strings.Contains(err.Error(), "'cellxgene'") {
		t.Fatalf("got %v", err)
	}
}

func TestPrintSearchTableCellxgene(t *testing.T) {
	rs := []downloaders.SearchResult{{Accession: "a", Title: strings.Repeat("t", 200), FileSize: 1}, {Accession: "b"}}
	if err := printSearchTable(rs, "cellxgene"); err != nil {
		t.Fatal(err)
	}
}
