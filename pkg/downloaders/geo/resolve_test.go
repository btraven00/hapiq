package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockNCBITransport redirects both eutils.ncbi.nlm.nih.gov and
// www.ncbi.nlm.nih.gov calls to the same test server.
type mockNCBITransport struct {
	server *httptest.Server
}

func (t *mockNCBITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	if strings.Contains(host, "eutils.ncbi.nlm.nih.gov") ||
		strings.Contains(host, "www.ncbi.nlm.nih.gov") {
		mockHost := strings.TrimPrefix(t.server.URL, "http://")
		req.URL.Scheme = "http"
		req.URL.Host = mockHost
	}
	return http.DefaultTransport.RoundTrip(req)
}

func newTestDownloaderWithServer(server *httptest.Server) *GEODownloader {
	return NewGEODownloader(WithHTTPClient(&http.Client{
		Transport: &mockNCBITransport{server: server},
		Timeout:   10 * time.Second,
	}))
}

// geoTextPageForGSE133344 is a minimal !Series text snippet that matches
// the real GEO page format for GSE133344.
const geoTextPageForGSE133344 = `
^SERIES = GSE133344
!Series_title = Exploring genetic interaction manifolds constructed from rich single-cell phenotypes
!Series_geo_accession = GSE133344
!Series_status = Public on Aug 05 2019
!Series_submission_date = Jul 31 2019
!Series_relation = BioProject: https://www.ncbi.nlm.nih.gov/bioproject/PRJNA556011
!Series_relation = SRA: https://www.ncbi.nlm.nih.gov/sra?term=SRP212114
`

func TestExtractSRPFromGEOPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(geoTextPageForGSE133344))
	}))
	defer server.Close()

	d := newTestDownloaderWithServer(server)
	srp, err := d.extractSRPFromGEOPage(context.Background(), "GSE133344")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srp != "SRP212114" {
		t.Fatalf("expected SRP212114, got %q", srp)
	}
}

func TestExtractSRPFromGEOPage_NoSRARelation(t *testing.T) {
	body := `
^SERIES = GSE999999
!Series_title = No SRA series
!Series_relation = BioProject: https://www.ncbi.nlm.nih.gov/bioproject/PRJNA999
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer server.Close()

	d := newTestDownloaderWithServer(server)
	srp, err := d.extractSRPFromGEOPage(context.Background(), "GSE999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srp != "" {
		t.Fatalf("expected empty SRP for dataset without SRA relation, got %q", srp)
	}
}

func TestSearchSRAByStudy(t *testing.T) {
	sraSearchResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSearchResult>
	<Count>3</Count>
	<RetMax>3</RetMax>
	<RetStart>0</RetStart>
	<IdList>
		<Id>7654321</Id>
		<Id>7654322</Id>
		<Id>7654323</Id>
	</IdList>
</eSearchResult>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sraSearchResponse))
	}))
	defer server.Close()

	d := newTestDownloaderWithServer(server)
	uids, err := d.searchSRAByStudy(context.Background(), "SRP212114")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(uids) != 3 {
		t.Fatalf("expected 3 UIDs, got %d", len(uids))
	}
	if uids[0] != "7654321" {
		t.Fatalf("expected first UID 7654321, got %q", uids[0])
	}
}

// TestResolveGSEToSRARuns_FallbackPath verifies that when ELink returns nothing,
// the resolver falls back to GEO page → SRP → SRA search.
func TestResolveGSEToSRARuns_FallbackPath(t *testing.T) {
	// ELink returns empty result (no gds→sra links).
	elinkEmptyResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eLinkResult>
	<LinkSet>
		<DbFrom>gds</DbFrom>
	</LinkSet>
</eLinkResult>`

	// SRA ESearch response for SRP212114[accession].
	sraSearchResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSearchResult>
	<Count>1</Count>
	<RetMax>1</RetMax>
	<IdList>
		<Id>9988776</Id>
	</IdList>
</eSearchResult>`

	// SRA ESummary with one run record (SRR9602561).
	sraSummaryResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSummaryResult>
	<DocSum>
		<Id>9988776</Id>
		<Item Name="Runs" Type="String"><![CDATA[<Run acc="SRR9602561" total_spots="123" total_bases="456"/>]]></Item>
		<Item Name="ExpXml" Type="String"><![CDATA[<Experiment acc="SRX6367795"/><Sample acc="SRS5047891"/><PAIRED/>]]></Item>
	</DocSum>
</eSummaryResult>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		path := r.URL.Path
		query := r.URL.RawQuery
		switch {
		case strings.Contains(path, "elink") || strings.Contains(query, "elink"):
			w.Write([]byte(elinkEmptyResponse))
		case strings.Contains(path, "esummary") || strings.Contains(query, "esummary"):
			w.Write([]byte(sraSummaryResponse))
		case strings.Contains(path, "esearch") || strings.Contains(query, "esearch"):
			// Could be called twice: once for SRA search, once as part of metadata.
			w.Write([]byte(sraSearchResponse))
		default:
			// GEO text page
			w.Write([]byte(geoTextPageForGSE133344))
		}
	}))
	defer server.Close()

	d := newTestDownloaderWithServer(server)
	runs, err := d.ResolveGSEToSRARuns(context.Background(), "200133344", "GSE133344")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one SRA run via fallback path")
	}
	if runs[0].RunAccession != "SRR9602561" {
		t.Fatalf("expected SRR9602561, got %q", runs[0].RunAccession)
	}
}

// TestResolveGSEToSRARuns_ELinkPath verifies the primary ELink gds→sra path.
func TestResolveGSEToSRARuns_ELinkPath(t *testing.T) {
	elinkResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eLinkResult>
	<LinkSet>
		<DbFrom>gds</DbFrom>
		<LinkSetDb>
			<DbTo>sra</DbTo>
			<LinkName>gds_sra</LinkName>
			<Link><Id>9988776</Id></Link>
		</LinkSetDb>
	</LinkSet>
</eLinkResult>`

	sraSummaryResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSummaryResult>
	<DocSum>
		<Id>9988776</Id>
		<Item Name="Runs" Type="String"><![CDATA[<Run acc="SRR9602561" total_spots="123" total_bases="456"/>]]></Item>
		<Item Name="ExpXml" Type="String"><![CDATA[<Experiment acc="SRX6367795"/><Sample acc="SRS5047891"/><PAIRED/>]]></Item>
	</DocSum>
</eSummaryResult>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		path := r.URL.Path
		query := r.URL.RawQuery
		switch {
		case strings.Contains(path, "elink") || strings.Contains(query, "elink"):
			w.Write([]byte(elinkResponse))
		default:
			w.Write([]byte(sraSummaryResponse))
		}
	}))
	defer server.Close()

	d := newTestDownloaderWithServer(server)
	runs, err := d.ResolveGSEToSRARuns(context.Background(), "200133344", "GSE133344")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected one SRA run from ELink path")
	}
	if runs[0].RunAccession != "SRR9602561" {
		t.Fatalf("expected SRR9602561, got %q", runs[0].RunAccession)
	}
}

// TestResolveGSEToSRARuns_Integration_GSE133344 hits real NCBI endpoints.
// Run with: go test ./pkg/downloaders/geo/ -run Integration -v
// Skipped in short mode (go test -short).
func TestResolveGSEToSRARuns_Integration_GSE133344(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	d := NewGEODownloader(WithTimeout(30 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: get GDS UID for GSE133344.
	uid, err := d.searchGEORecord(ctx, "GSE133344")
	if err != nil {
		t.Fatalf("searchGEORecord: %v", err)
	}
	if uid == "" {
		t.Fatal("expected non-empty GDS UID for GSE133344")
	}
	t.Logf("GDS UID for GSE133344: %s", uid)

	// Step 2: resolve to SRA runs (exercises both ELink and fallback paths).
	runs, err := d.ResolveGSEToSRARuns(ctx, uid, "GSE133344")
	if err != nil {
		t.Fatalf("ResolveGSEToSRARuns: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected SRA runs for GSE133344 — known to have data in SRA under SRP212114")
	}
	t.Logf("Resolved %d SRA runs for GSE133344", len(runs))

	// Spot-check: the first run should have an SRR accession.
	if runs[0].RunAccession == "" {
		t.Fatal("first run has empty RunAccession")
	}
	t.Logf("First run: %s (experiment %s)", runs[0].RunAccession, runs[0].Experiment)
}
