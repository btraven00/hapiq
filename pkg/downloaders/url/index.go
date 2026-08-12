package url

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// A directory URL is one whose path ends in "/". Servers redirect the
// extensionless form (".../be1-fixture") to it, but we do not chase that here:
// requiring the slash keeps directory intent explicit in the manifest.
func isDirectoryURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Path, "/")
}

// indexEntry is one row of a server-generated JSON directory index. Caddy
// (file_server browse) and nginx (autoindex_format json) agree on "name" and
// "size" but disagree on how they mark a subdirectory, so both spellings are
// accepted.
type indexEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // nginx: "file" | "directory"
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"` // caddy
}

func (e indexEntry) isDir() bool {
	return e.IsDir || e.Type == "directory"
}

// listDirectory asks rawURL for a machine-readable listing and returns its
// entries.
//
// The request sets Accept: application/json, which both Caddy and nginx honour
// by emitting JSON instead of the browsable HTML page. A server that ignores it
// and sends HTML anyway is an error, not something to parse: the HTML index is
// a *page about* the files, and saving it as though it were data is exactly the
// silent-garbage failure this function exists to prevent.
func (d *URLDownloader) listDirectory(ctx context.Context, rawURL string) ([]indexEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing %s: HTTP %d", rawURL, resp.StatusCode)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		return nil, fmt.Errorf(
			"listing %s: server sent %q, not a JSON index — it has no machine-readable "+
				"directory listing (Caddy: file_server browse; nginx: autoindex_format json), "+
				"so fetch the files individually instead",
			rawURL, mediaType)
	}

	var entries []indexEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("listing %s: parse JSON index: %w", rawURL, err)
	}
	return entries, nil
}

// fileLooksLikeHTML sniffs the first bytes of a downloaded file. It inspects
// the file rather than the response header so that the verdict is the same
// whether the bytes came off the network or out of the local cache.
func fileLooksLikeHTML(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mediaType, _, _ := mime.ParseMediaType(http.DetectContentType(buf[:n]))

	return mediaType == "text/html"
}

// childURL resolves a listing entry's name against its directory URL, escaping
// it so that names with spaces or other reserved characters stay fetchable.
func childURL(dirURL, name string) string {
	base, err := url.Parse(dirURL)
	if err != nil {
		return strings.TrimSuffix(dirURL, "/") + "/" + url.PathEscape(name)
	}
	return base.ResolveReference(&url.URL{Path: name}).String()
}
