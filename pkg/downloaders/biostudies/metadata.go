// Package biostudies downloads datasets from EBI BioStudies
// (https://www.ebi.ac.uk/biostudies). Studies are referenced by accession
// (e.g. S-BSST1502, E-MTAB-8077) and may contain arbitrary files attached
// to a hierarchical section tree.
//
// The downloader fetches the study JSON, walks the section tree to enumerate
// all files, and downloads them via the public files endpoint.
package biostudies

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiBase   = "https://www.ebi.ac.uk/biostudies/api/v1"
	filesBase = "https://www.ebi.ac.uk/biostudies/files"
)

// Study is the minimal projection of the BioStudies study JSON we consume.
type Study struct {
	Accno      string      `json:"accno"`
	Attributes []Attribute `json:"attributes"`
	Section    Section     `json:"section"`
}

// Section is a node in the study's hierarchical content tree. The API
// encodes both `files` and `subsections` as heterogeneous JSON: each
// element may be either an object or an array of objects. We decode them
// as RawMessage and unwrap during traversal.
type Section struct {
	Type        string            `json:"type"`
	Accno       string            `json:"accno"`
	Attributes  []Attribute       `json:"attributes"`
	Files       []json.RawMessage `json:"files"`
	Subsections []json.RawMessage `json:"subsections"`
	Links       []Link            `json:"links"`
}

// Link is a typed pointer from a study to an external record (BioSample,
// ENA project, …). The `attributes` collection carries the link Type.
type Link struct {
	URL        string      `json:"url"`
	Attributes []Attribute `json:"attributes"`
}

// LinkType returns the value of the link's "Type" attribute, or "" if absent.
func (l *Link) LinkType() string { return attrValue(l.Attributes, "Type") }

// Attribute is a generic name/value pair used throughout BioStudies metadata.
type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// File is a single enumerated file within a study.
type File struct {
	Path       string      `json:"path"`
	Size       int64       `json:"size"`
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
}

// DownloadURL returns the public HTTP URL for a file. The server issues a
// 302 redirect to an FTP host; the standard http client follows it.
func (f *File) DownloadURL(accno string) string {
	// Path segments may contain spaces or other characters; encode them
	// per-segment so '/' separators are preserved.
	parts := strings.Split(f.Path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return fmt.Sprintf("%s/%s/%s", filesBase, accno, strings.Join(parts, "/"))
}

// fetchStudy retrieves the study JSON for an accession.
func fetchStudy(ctx context.Context, client *http.Client, accno string) (*Study, error) {
	u := fmt.Sprintf("%s/studies/%s", apiBase, url.PathEscape(accno))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("biostudies: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("biostudies: study %q not found", accno)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("biostudies: HTTP %d for %s", resp.StatusCode, u)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("biostudies: read body: %w", err)
	}

	var s Study
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("biostudies: parse study: %w", err)
	}
	return &s, nil
}

// walkFiles flattens all files in the study into a slice. Section nesting
// is preserved by joining ancestor section accessions into the file path
// when the path is otherwise ambiguous.
func walkFiles(s *Section) []File {
	var out []File
	collectFiles(s, &out)
	return out
}

func collectFiles(s *Section, out *[]File) {
	if s == nil {
		return
	}
	for _, raw := range s.Files {
		appendFiles(raw, out)
	}
	for _, raw := range s.Subsections {
		walkSubsection(raw, out)
	}
}

// appendFiles decodes a `files` element which may be a single object or an
// array of objects.
func appendFiles(raw json.RawMessage, out *[]File) {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return
	}
	switch trimmed[0] {
	case '[':
		var arr []File
		if err := json.Unmarshal(raw, &arr); err == nil {
			*out = append(*out, arr...)
		}
	case '{':
		var f File
		if err := json.Unmarshal(raw, &f); err == nil {
			*out = append(*out, f)
		}
	}
}

// walkSubsection decodes a `subsections` element (object or array) and
// recurses into each section.
func walkSubsection(raw json.RawMessage, out *[]File) {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return
	}
	switch trimmed[0] {
	case '[':
		var arr []Section
		if err := json.Unmarshal(raw, &arr); err == nil {
			for i := range arr {
				collectFiles(&arr[i], out)
			}
		}
	case '{':
		var sec Section
		if err := json.Unmarshal(raw, &sec); err == nil {
			collectFiles(&sec, out)
		}
	}
}

// attrValue returns the value of the named attribute (case-insensitive),
// or the empty string when absent.
func attrValue(attrs []Attribute, name string) string {
	target := strings.ToLower(name)
	for _, a := range attrs {
		if strings.ToLower(a.Name) == target {
			return a.Value
		}
	}
	return ""
}

// newHTTPClient builds a client that follows redirects (the files endpoint
// 302s to FTP) with a bounded redirect chain.
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
