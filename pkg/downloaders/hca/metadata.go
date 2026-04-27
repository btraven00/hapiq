// Package hca downloads count matrices and processed files from the
// Human Cell Atlas Data Portal via the Azul service API.
//
// Datasets are referenced by HCA project UUID (36-char), e.g.
//
//	cc95ff89-2e68-4a08-a234-480eca21ce79
//
// The downloader walks both DCP-generated `matrices` and
// `contributedAnalyses` trees, collecting every file entry and its
// pre-built download URL.
package hca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const azulProject = "https://service.azul.data.humancellatlas.org/index/projects/"

// File describes one matrix or processed-output file inside an HCA project.
// Fields mirror the Azul JSON for each file leaf.
type File struct {
	Name               string   `json:"name"`
	Format             string   `json:"format"`
	UUID               string   `json:"uuid"`
	Version            string   `json:"version"`
	SHA256             string   `json:"sha256"`
	AzulURL            string   `json:"azul_url"`
	DRSURI             string   `json:"drs_uri"`
	FileSource         string   `json:"fileSource"`
	ContentDescription []string `json:"contentDescription"`
	Size               int64    `json:"size"`
	IsIntermediate     bool     `json:"isIntermediate"`
}

// Project is the trimmed Azul project payload we care about.
type Project struct {
	ProjectID         string                  `json:"projectId"`
	ProjectTitle      string                  `json:"projectTitle"`
	ProjectShortname  string                  `json:"projectShortname"`
	Description       string                  `json:"projectDescription"`
	EstimatedCells    *int64                  `json:"estimatedCellCount"`
	Accessions        []ProjectAccession      `json:"accessions"`
	Matrices          map[string]any          `json:"matrices"`
	ContributedAnalyses map[string]any        `json:"contributedAnalyses"`
	ContributorMatrices map[string]any        `json:"contributorMatrices"`
}

// ProjectAccession is an external repository reference (BioStudies, INSDC, …).
type ProjectAccession struct {
	Namespace string `json:"namespace"`
	Accession string `json:"accession"`
}

// projectsResponse wraps the Azul `index/projects/<uuid>` response.
type projectsResponse struct {
	Projects []Project `json:"projects"`
}

// fetchProject retrieves the project document for an HCA UUID.
func fetchProject(ctx context.Context, client *http.Client, uuid string) (*Project, error) {
	u := azulProject + uuid
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hca: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("hca: project %q not found", uuid)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hca: HTTP %d for %s", resp.StatusCode, u)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hca: read body: %w", err)
	}

	// The Azul endpoint may return a single project or a list under "projects".
	// Try the wrapped form first; fall back to a single-project document.
	var wrap projectsResponse
	if err := json.Unmarshal(body, &wrap); err == nil && len(wrap.Projects) > 0 {
		p := wrap.Projects[0]
		return &p, nil
	}
	var p Project
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("hca: parse project: %w", err)
	}
	return &p, nil
}

// collectFiles walks the heterogeneous Azul matrix tree and returns every
// file leaf encountered. Each tree branches via nested maps keyed on facet
// values (genusSpecies → libraryConstructionApproach → developmentStage → …)
// and terminates in a list of file objects.
func collectFiles(node any, out *[]File) {
	switch v := node.(type) {
	case map[string]any:
		// A file leaf is itself a map with a "name" field. The Azul matrix
		// tree never uses "name" as a facet key, so this disambiguates.
		if _, ok := v["name"].(string); ok {
			if f, err := decodeFile(v); err == nil {
				*out = append(*out, f)
				return
			}
		}
		for _, child := range v {
			collectFiles(child, out)
		}
	case []any:
		for _, item := range v {
			collectFiles(item, out)
		}
	}
}

func decodeFile(m map[string]any) (File, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

// allFiles flattens both DCP-generated and contributed matrix trees.
func allFiles(p *Project) []File {
	var out []File
	collectFiles(map[string]any(p.Matrices), &out)
	collectFiles(map[string]any(p.ContributedAnalyses), &out)
	collectFiles(map[string]any(p.ContributorMatrices), &out)
	return out
}

// IsCountMatrix reports whether a file looks like a raw/processed count matrix.
// Used as a default heuristic in the downloader and exposed for callers.
func (f *File) IsCountMatrix() bool {
	for _, d := range f.ContentDescription {
		if strings.Contains(strings.ToLower(d), "count matrix") {
			return true
		}
	}
	switch strings.ToLower(f.Format) {
	case "loom", "h5", "h5ad", "mtx":
		return true
	}
	return false
}

// newHTTPClient builds a client that follows the Azul → S3 redirect chain.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}
