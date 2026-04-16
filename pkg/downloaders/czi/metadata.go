// Package czi downloads datasets from the CZI Virtual Cell Platform (VCP).
//
// API base: https://cosmic-shepherd.prod-vcp.prod.czi.team/v1/data
// Public endpoints require no authentication.
// Authenticated endpoints require a JWT in the VCP_TOKEN environment variable.
//
// Files are stored as S3 URIs; the downloader converts them to plain HTTPS
// for public buckets and falls back to a 403 error message for private ones.
package czi

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
	apiBase     = "https://cosmic-shepherd.prod-vcp.prod.czi.team/v1/data"
	datasetPage = "https://virtualcellmodels.cziscience.com/dataset"
)

// DataItem is a search result from the VCP search endpoint.
type DataItem struct {
	InternalID  string     `json:"internal_id"`
	ExternalID  string     `json:"external_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Domain      string     `json:"domain"`
	Version     string     `json:"version"`
	License     string     `json:"license"`
	URL         string     `json:"url"`
	Assay       []string   `json:"assay"`
	Organism    []string   `json:"organism"`
	Tissue      []string   `json:"tissue"`
	Disease     []string   `json:"disease"`
	CellType    []string   `json:"cell_type"`
	Tags        []string   `json:"tags"`
	Locations   []Location `json:"locations"`
}

// Location holds a file URL and optional size.
type Location struct {
	URL         string `json:"url"`
	ContentSize int64  `json:"contentSize"`
}

// SearchResponse wraps the VCP search results.
type SearchResponse struct {
	Data   []DataItem `json:"data"`
	Total  int        `json:"total"`
	Cursor string     `json:"cursor"`
}

// DatasetRecord is the full record returned by /public/dataset/{id}.
type DatasetRecord struct {
	InternalID string     `json:"internal_id"`
	Label      string     `json:"label"`
	Domain     string     `json:"domain"`
	Version    string     `json:"version"`
	Tags       []string   `json:"tags"`
	Locations  []Location `json:"locations"`
	MD         *CroissantMD `json:"md"`
}

// CroissantMD holds Croissant Lite metadata for a dataset.
type CroissantMD struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	License     string            `json:"license"`
	Version     string            `json:"version"`
	DatePublished string          `json:"datePublished"`
	Distribution []CroissantFile  `json:"distribution"`
}

// CroissantFile is one entry in the Croissant distribution list.
type CroissantFile struct {
	ID             string `json:"@id"`
	Name           string `json:"name"`
	ContentURL     string `json:"contentUrl"`
	EncodingFormat string `json:"encodingFormat"`
	ContentSize    string `json:"contentSize"`
	MD5            string `json:"md5"`
}

// s3ToHTTPS converts an S3 URI to a virtual-hosted-style HTTPS URL.
// s3://bucket/key → https://bucket.s3.amazonaws.com/key
func s3ToHTTPS(s3URI string) string {
	rest := strings.TrimPrefix(s3URI, "s3://")
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return ""
	}
	bucket := rest[:idx]
	key := rest[idx+1:]
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucket, key)
}

// client holds an HTTP client and optional auth token.
type client struct {
	http     *http.Client
	token    string // JWT bearer token; empty = public endpoints
}

func newClient(token string, timeout time.Duration) *client {
	return &client{
		http:  &http.Client{Timeout: timeout},
		token: token,
	}
}

func (c *client) get(ctx context.Context, rawURL string, params url.Values) ([]byte, error) {
	if len(params) > 0 {
		rawURL = rawURL + "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from VCP API: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// endpoint returns the right path prefix depending on whether we have a token.
func (c *client) endpoint(path string) string {
	if c.token != "" {
		return apiBase + "/" + path
	}
	return apiBase + "/public/" + path
}

// search queries the VCP dataset search API.
func (c *client) search(ctx context.Context, query string, limit int, cursor string) (*SearchResponse, error) {
	params := url.Values{
		"query":       {query + " AND latest_version:true"},
		"limit":       {fmt.Sprintf("%d", limit)},
		"use_cursor":  {"true"},
		"download":    {"true"},
		"scout":       {boolStr(cursor == "")},
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	body, err := c.get(ctx, c.endpoint("search"), params)
	if err != nil {
		return nil, err
	}

	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}
	return &resp, nil
}

// getDataset retrieves full metadata for a single dataset ID.
func (c *client) getDataset(ctx context.Context, id string, withDownload bool) (*DatasetRecord, error) {
	params := url.Values{"download": {boolStr(withDownload)}}
	body, err := c.get(ctx, c.endpoint("dataset")+"/"+id, params)
	if err != nil {
		return nil, err
	}

	var rec DatasetRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, fmt.Errorf("parse dataset response: %w", err)
	}
	return &rec, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
