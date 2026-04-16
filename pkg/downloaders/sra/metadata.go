package sra

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const enaFilereportURL = "https://www.ebi.ac.uk/ena/portal/api/filereport"

// RunInfo holds ENA metadata for one SRA run.
type RunInfo struct {
	RunAccession        string
	ExperimentAccession string
	SampleAccession     string
	Layout              string // PAIRED or SINGLE
	Files               []ENAFile
}

// ENAFile holds metadata for one FASTQ file from ENA.
type ENAFile struct {
	FTPPath string
	MD5     string
	Bytes   int64
	Name    string // base filename
}

// HTTPSURL converts the ENA FTP path to an HTTPS URL.
func (f ENAFile) HTTPSURL() string {
	return "https://" + f.FTPPath
}

// fetchRunInfo retrieves run-level file metadata from the ENA filereport API.
// Accepts PRJNA*, SRR*, ERR*, DRR*, SRX*, ERS*, etc.
func (d *SRADownloader) fetchRunInfo(ctx context.Context, accession string) ([]RunInfo, error) {
	fields := strings.Join([]string{
		"run_accession",
		"experiment_accession",
		"sample_accession",
		"library_layout",
		"fastq_ftp",
		"fastq_md5",
		"fastq_bytes",
	}, ",")

	url := fmt.Sprintf("%s?accession=%s&result=read_run&fields=%s", enaFilereportURL, accession, fields)

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ENA filereport request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ENA filereport HTTP %d for %s", resp.StatusCode, accession)
	}

	r := csv.NewReader(resp.Body)
	r.Comma = '\t'
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse ENA TSV: %w", err)
	}

	if len(records) < 2 {
		// Header-only or empty — no runs found.
		return nil, nil
	}

	// Build column index from header row.
	header := records[0]
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[h] = i
	}

	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	var runs []RunInfo
	for _, row := range records[1:] {
		run := RunInfo{
			RunAccession:        get(row, "run_accession"),
			ExperimentAccession: get(row, "experiment_accession"),
			SampleAccession:     get(row, "sample_accession"),
			Layout:              get(row, "library_layout"),
		}
		if run.RunAccession == "" {
			continue
		}

		ftpPaths := splitField(get(row, "fastq_ftp"))
		md5s := splitField(get(row, "fastq_md5"))
		bytesRaw := splitField(get(row, "fastq_bytes"))

		for i, ftp := range ftpPaths {
			if ftp == "" {
				continue
			}
			ef := ENAFile{
				FTPPath: ftp,
				Name:    basename(ftp),
			}
			if i < len(md5s) {
				ef.MD5 = md5s[i]
			}
			if i < len(bytesRaw) {
				if n, err := strconv.ParseInt(bytesRaw[i], 10, 64); err == nil {
					ef.Bytes = n
				}
			}
			run.Files = append(run.Files, ef)
		}
		runs = append(runs, run)
	}

	return runs, nil
}

func splitField(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ";")
}

func basename(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
