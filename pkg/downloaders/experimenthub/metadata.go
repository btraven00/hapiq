package experimenthub

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const (
	metadataURL    = "https://experimenthub.bioconductor.org/metadata/experimenthub.sqlite3"
	fetchURLPrefix = "https://experimenthub.bioconductor.org/fetch/"
)

// resourceInfo holds the subset of EH metadata hapiq cares about.
// Note: the upstream rdatapaths table only stores rdatapath / rdataclass /
// dispatchclass — no size or checksum. We surface what's there.
// RDataPath/RDataClass/DispatchClass are those of the first of Paths.
type resourceInfo struct {
	Paths         []rdataPath
	AHID          string
	Title         string
	Description   string
	Species       string
	DataProvider  string
	Maintainer    string
	RDataPath     string
	RDataClass    string
	DispatchClass string
}

// rdataPath is one file of a resource. ID is the rdatapaths row id, which is
// what /fetch/<id> takes; it is not the number in the EH id.
type rdataPath struct {
	Path string
	ID   int64
}

// cachePath returns the on-disk location of the cached metadata sqlite file,
// honoring viper / env overrides via resolveCacheDir.
func cachePath() (string, error) {
	dir := resolveCacheDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFilename), nil
}

// ensureMetadata downloads the metadata sqlite if it is missing or older than
// cacheMaxAge. Returns the local path.
func ensureMetadata(ctx context.Context, client *http.Client, verbose bool) (string, error) {
	path, err := cachePath()
	if err != nil {
		return "", err
	}

	maxAge := resolveMaxAge()
	if fi, err := os.Stat(path); err == nil {
		if time.Since(fi.ModTime()) < maxAge {
			return path, nil
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "ExperimentHub metadata cache is stale (%s old); refreshing\n",
				time.Since(fi.ModTime()).Round(time.Hour))
		}
	} else if verbose {
		fmt.Fprintf(os.Stderr, "Fetching ExperimentHub metadata cache → %s\n", path)
	}

	if err := downloadTo(ctx, client, metadataURL, path); err != nil {
		// If we already have a stale copy, prefer it over hard failure.
		if _, statErr := os.Stat(path); statErr == nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "warning: refresh failed (%v); using stale cache\n", err)
			}
			return path, nil
		}
		return "", fmt.Errorf("download metadata: %w", err)
	}
	return path, nil
}

func downloadTo(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(filepath.Clean(tmp)) // #nosec G304 -- internal cache path
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// searchCatalog performs a substring search across the cached resources table.
func searchCatalog(dbPath, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	var (
		conds []string
		args  []any
	)
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		conds = append(conds,
			"(LOWER(r.title) LIKE ? OR LOWER(COALESCE(r.description,'')) LIKE ? "+
				"OR LOWER(COALESCE(r.species,'')) LIKE ? OR LOWER(COALESCE(r.dataprovider,'')) LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if opts.Organism != "" {
		conds = append(conds, "LOWER(COALESCE(r.species,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(opts.Organism)+"%")
	}
	if opts.EntryType != "" {
		conds = append(conds, "LOWER(COALESCE(rp.rdataclass,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(opts.EntryType)+"%")
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	q := `
		SELECT r.ah_id, r.title,
		       COALESCE(r.species, ''), COALESCE(MIN(rp.rdataclass), ''),
		       COALESCE(r.rdatadateadded, '')
		FROM rdatapaths rp
		JOIN resources r ON rp.resource_id = r.id
		` + where + `
		GROUP BY r.id
		ORDER BY r.id DESC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []downloaders.SearchResult
	for rows.Next() {
		var (
			ahID    string
			title   string
			species string
			rclass  string
			added   string
		)
		if err := rows.Scan(&ahID, &title, &species, &rclass, &added); err != nil {
			return nil, err
		}
		out = append(out, downloaders.SearchResult{
			Accession: ahID,
			Title:     title,
			Organism:  species,
			EntryType: rclass,
			Date:      added,
		})
	}
	return out, rows.Err()
}

// lookupResource returns the resource with the given ah_id ("EH3796") and all
// its rdatapaths, or nil with no error when there is none.
func lookupResource(dbPath, ahID string) (*resourceInfo, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	const q = `
		SELECT r.ah_id, r.title, COALESCE(r.description, ''),
		       COALESCE(r.species, ''), COALESCE(r.dataprovider, ''),
		       COALESCE(r.maintainer, ''), rp.id,
		       COALESCE(rp.rdatapath, ''), COALESCE(rp.rdataclass, ''),
		       COALESCE(rp.dispatchclass, '')
		FROM resources r
		JOIN rdatapaths rp ON rp.resource_id = r.id
		WHERE r.ah_id = ?
		ORDER BY rp.id`
	rows, err := db.Query(q, ahID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ri *resourceInfo
	for rows.Next() {
		var (
			row resourceInfo
			p   rdataPath
		)
		if err := rows.Scan(&row.AHID, &row.Title, &row.Description, &row.Species,
			&row.DataProvider, &row.Maintainer, &p.ID, &row.RDataPath,
			&row.RDataClass, &row.DispatchClass); err != nil {
			return nil, err
		}
		p.Path = row.RDataPath
		if ri == nil {
			ri = &row
		}
		ri.Paths = append(ri.Paths, p)
	}
	return ri, rows.Err()
}
