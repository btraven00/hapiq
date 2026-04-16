package geo

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
)

// ELinkResultGDS parses the eLinkResult XML for gds/sra links.
type ELinkResultGDS struct {
	XMLName  xml.Name      `xml:"eLinkResult"`
	LinkSets []ELinkSetGDS `xml:"LinkSet"`
}

// ELinkSetGDS holds the linked IDs from a single ELink call.
type ELinkSetGDS struct {
	DbFrom   string            `xml:"DbFrom"`
	IDList   []string          `xml:"IdList>Id"`
	LinkDBs  []ELinkDBGDS      `xml:"LinkSetDb"`
}

// ELinkDBGDS holds links to a specific target database.
type ELinkDBGDS struct {
	DbTo     string   `xml:"DbTo"`
	LinkName string   `xml:"LinkName"`
	Links    []string `xml:"Link>Id"`
}

// ResolveBioProject resolves a BioProject accession (PRJNA*) to its linked GEO
// series UIDs using a two-step ESearch+ELink call.
// Returns the list of GDS UIDs (e.g. "200133344" for GSE133344).
func (d *GEODownloader) ResolveBioProject(ctx context.Context, prjna string) ([]string, error) {
	// Step 1: ESearch bioproject → numeric UID
	bioprojectUID, err := d.searchDB(ctx, "bioproject", prjna)
	if err != nil {
		return nil, fmt.Errorf("BioProject lookup failed for %s: %w", prjna, err)
	}

	// Step 2: ELink bioproject → gds
	return d.elink(ctx, "bioproject", "gds", bioprojectUID)
}

// ResolveGSEToSRARuns resolves a GDS UID to its linked SRA experiment UIDs,
// then fetches the SRR run accessions from ESummary.
func (d *GEODownloader) ResolveGSEToSRARuns(ctx context.Context, gdsUID string) ([]SRARun, error) {
	sraUIDs, err := d.elink(ctx, "gds", "sra", gdsUID)
	if err != nil || len(sraUIDs) == 0 {
		return nil, err
	}

	return d.fetchSRARuns(ctx, sraUIDs)
}

// SRARun holds the key fields for an SRA run.
type SRARun struct {
	RunAccession string // SRR*
	Experiment   string // SRX*
	Sample       string // SRS*
	BioSample    string // SAMN*
	GSM          string // GSM* (if linked)
	Spots        int64
	Bases        int64
	Layout       string // PAIRED or SINGLE
}

// SRARunSummary is the XML structure returned by ESummary for the sra database.
type SRARunSummary struct {
	XMLName xml.Name       `xml:"eSummaryResult"`
	DocSums []SRADocSum    `xml:"DocSum"`
}

// SRADocSum holds run fields from an SRA ESummary result.
type SRADocSum struct {
	ID    string    `xml:"Id"`
	Items []SRAItem `xml:"Item"`
}

// SRAItem holds a single field from the SRA ESummary.
type SRAItem struct {
	Name    string    `xml:"Name,attr"`
	Content string    `xml:",chardata"`
	Items   []SRAItem `xml:"Item,omitempty"`
}

// searchDB searches a given NCBI database and returns the first UID.
func (d *GEODownloader) searchDB(ctx context.Context, db, term string) (string, error) {
	params := url.Values{}
	params.Set("db", db)
	params.Set("term", term)
	params.Set("retmax", "1")
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	searchURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?" + params.Encode()

	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, searchURL)
	if err != nil {
		return "", err
	}

	var resp ESearchResponse
	if err := xml.Unmarshal(content, &resp); err != nil {
		return "", fmt.Errorf("parse esearch: %w", err)
	}

	if len(resp.IdList.IDs) == 0 {
		return "", fmt.Errorf("no records found for %q in database %q", term, db)
	}

	return resp.IdList.IDs[0], nil
}

// elink calls ELink for dbfrom→dbto and returns the linked IDs.
func (d *GEODownloader) elink(ctx context.Context, dbfrom, dbto, id string) ([]string, error) {
	params := url.Values{}
	params.Set("dbfrom", dbfrom)
	params.Set("db", dbto)
	params.Set("id", id)
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	linkURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/elink.fcgi?" + params.Encode()

	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, linkURL)
	if err != nil {
		return nil, err
	}

	var result ELinkResultGDS
	if err := xml.Unmarshal(content, &result); err != nil {
		return nil, fmt.Errorf("parse elink: %w", err)
	}

	for _, ls := range result.LinkSets {
		for _, ldb := range ls.LinkDBs {
			if ldb.DbTo == dbto && len(ldb.Links) > 0 {
				return ldb.Links, nil
			}
		}
	}

	return nil, nil
}

// fetchSRARuns fetches SRA run details for the given SRA UIDs.
// NCBI limits esummary to ~500 IDs per call; we batch if needed.
func (d *GEODownloader) fetchSRARuns(ctx context.Context, uids []string) ([]SRARun, error) {
	const batchSize = 200

	var runs []SRARun

	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}

		batch, err := d.fetchSRARunsBatch(ctx, uids[i:end])
		if err != nil {
			return runs, err
		}

		runs = append(runs, batch...)
	}

	return runs, nil
}

// fetchSRARunsBatch fetches one batch of SRA UIDs.
func (d *GEODownloader) fetchSRARunsBatch(ctx context.Context, uids []string) ([]SRARun, error) {
	params := url.Values{}
	params.Set("db", "sra")
	params.Set("id", strings.Join(uids, ","))
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	summaryURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?" + params.Encode()

	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, summaryURL)
	if err != nil {
		return nil, err
	}

	var resp SRARunSummary
	if err := xml.Unmarshal(content, &resp); err != nil {
		return nil, fmt.Errorf("parse sra esummary: %w", err)
	}

	var runs []SRARun

	for _, doc := range resp.DocSums {
		run := parseSRADocSum(doc)
		if run.RunAccession != "" {
			runs = append(runs, run)
		}
	}

	return runs, nil
}

// parseSRADocSum extracts the key fields from an SRA ESummary DocSum.
// The SRA ESummary XML is non-standard: run/experiment info is packed
// into a single "Runs" item as an XML-in-XML attribute string.
func parseSRADocSum(doc SRADocSum) SRARun {
	run := SRARun{}

	for _, item := range doc.Items {
		switch item.Name {
		case "Runs":
			// Looks like: <Run acc="SRR9602561" total_spots="..." total_bases="..." ... />
			// Parse inline XML attributes with a simple regex-free approach.
			parseRunsField(item.Content, &run)
		case "ExpXml":
			// Contains experiment + sample + library layout info
			parseExpXML(item.Content, &run)
		}
	}

	return run
}

// parseRunsField extracts the SRR accession from the Runs XML field.
func parseRunsField(content string, run *SRARun) {
	// Find Run acc="SRRxxx"
	const accAttr = `acc="`
	idx := strings.Index(content, accAttr)
	if idx < 0 {
		return
	}

	start := idx + len(accAttr)
	end := strings.Index(content[start:], `"`)
	if end < 0 {
		return
	}

	run.RunAccession = content[start : start+end]
}

// parseExpXML extracts experiment, sample, and layout from the ExpXml field.
func parseExpXML(content string, run *SRARun) {
	// Extract Experiment accession
	if acc := extractAttr(content, "Experiment", "acc"); acc != "" {
		run.Experiment = acc
	}
	// Extract Sample accession
	if acc := extractAttr(content, "Sample", "acc"); acc != "" {
		run.Sample = acc
	}
	// Extract library layout
	if strings.Contains(content, "<PAIRED") {
		run.Layout = "PAIRED"
	} else if strings.Contains(content, "<SINGLE") {
		run.Layout = "SINGLE"
	}
}

// extractAttr is a minimal XML attribute extractor for a known element name.
func extractAttr(content, element, attr string) string {
	tag := "<" + element + " "
	idx := strings.Index(content, tag)
	if idx < 0 {
		return ""
	}

	sub := content[idx+len(tag):]
	attrKey := attr + `="`
	aidx := strings.Index(sub, attrKey)
	if aidx < 0 {
		return ""
	}

	start := aidx + len(attrKey)
	end := strings.Index(sub[start:], `"`)
	if end < 0 {
		return ""
	}

	return sub[start : start+end]
}

// IsBioProjectAccession reports whether id looks like a BioProject accession.
func IsBioProjectAccession(id string) bool {
	up := strings.ToUpper(strings.TrimSpace(id))
	// PRJNA*, PRJNB*, PRJDB*, PRJEB* are the common prefixes
	return strings.HasPrefix(up, "PRJ") && len(up) > 5
}
