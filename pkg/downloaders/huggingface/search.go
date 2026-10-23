package huggingface

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// searchHit is the subset of /api/datasets?full=true that search shows.
type searchHit struct {
	LastModified time.Time `json:"lastModified"`
	CardData     struct {
		PrettyName string `json:"pretty_name"`
	} `json:"cardData"`
	Gated     any    `json:"gated"`
	ID        string `json:"id"`
	Downloads int    `json:"downloads"`
	Private   bool   `json:"private"`
}

// Search finds Hub datasets whose id matches query, most downloaded first.
// opts.EntryType is a Hub tag filter (e.g. "modality:tabular", "license:mit").
// The Hub has no organism field, so opts.Organism is refused rather than ignored.
func (d *HFDownloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	if opts.Organism != "" {
		return nil, fmt.Errorf("hf search has no organism filter; put it in the query or use --type with a Hub tag")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	q := url.Values{}
	q.Set("search", query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("sort", "downloads")
	q.Set("direction", "-1")
	q.Set("full", "true") // for cardData.pretty_name
	if opts.EntryType != "" {
		q.Set("filter", opts.EntryType)
	}

	var hits []searchHit
	if _, err := d.getJSON(ctx, d.baseURL+"/api/datasets?"+q.Encode(), &hits); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	out := make([]downloaders.SearchResult, 0, len(hits))
	for _, h := range hits {
		r := downloaders.SearchResult{
			Accession: h.ID,
			Title:     h.CardData.PrettyName,
			Downloads: h.Downloads,
		}
		if r.Title == "" {
			r.Title = h.ID
		}
		if (&repoInfo{Gated: h.Gated, Private: h.Private}).restricted() {
			r.EntryType = "gated"
		}
		if !h.LastModified.IsZero() {
			r.Date = h.LastModified.Format("2006-01-02")
		}
		out = append(out, r)
	}
	return out, nil
}
