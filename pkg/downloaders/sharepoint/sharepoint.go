// Package sharepoint resolves anonymous SharePoint / OneDrive-for-Business
// "share links" into direct, byte-serving download URLs that hapiq's generic
// url downloader can stream.
//
// SharePoint share links come in a few flavours, distinguished by a marker
// segment right after the host:
//
//	https://tenant.sharepoint.com/:b:/s/Site/<token>   single file
//	https://tenant.sharepoint.com/:f:/s/Site/<token>   folder
//	https://tenant.sharepoint.com/:w:/ :x:/ :p:/ ...    word / excel / ppt
//
// None of these serve bytes directly: opened in a browser they redirect to a
// listing/preview page (AllItems.aspx / Doc.aspx) whose "id" query parameter is
// the server-relative path of the shared item. Once that path is known the file
// can be fetched anonymously from
//
//	https://tenant.sharepoint.com/<server-relative-path>?download=1
//
// which honours HTTP Range requests (so hapiq's resume/hash machinery applies).
//
// For a single-file (:b:) link the redirect's id is the file itself. For a
// folder (:f:) link the id is the folder, so the caller must name the desired
// file via the URL fragment, e.g.
//
//	https://tenant.sharepoint.com/:f:/s/Site/<token>#models.ckpt
package sharepoint

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// IsShareURL reports whether rawURL looks like a SharePoint share link that
// needs resolving before it can be downloaded. A bare server-relative path that
// already points at a file (the kind Resolve produces) returns false.
func IsShareURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(u.Host), ".sharepoint.com") {
		return false
	}
	// Share links carry a ":x:" marker segment immediately after the host.
	seg := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	return len(seg) > 0 && len(seg[0]) >= 3 &&
		strings.HasPrefix(seg[0], ":") && strings.HasSuffix(seg[0], ":")
}

// Resolve turns a SharePoint share link into a direct download URL and the
// filename it will produce. It performs one HTTP request to follow the share
// link's redirect to the listing/preview page, then reads the server-relative
// path from that page's "id" query parameter.
//
// client must follow redirects (the default http.Client does). It is only used
// for a single lightweight request; the response body is never read.
func Resolve(ctx context.Context, client *http.Client, rawURL string) (downloadURL, filename string, err error) {
	share, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse share url: %w", err)
	}
	// A folder link names its target file via the fragment.
	wantFile := share.Fragment
	share.Fragment = ""

	final, err := followToListing(ctx, client, share.String())
	if err != nil {
		return "", "", err
	}

	// The listing/preview URL encodes the shared item's server-relative path in
	// its "id" (sometimes "parent") query parameter, already URL-decoded by
	// url.Values.
	itemPath := final.Query().Get("id")
	if itemPath == "" {
		itemPath = final.Query().Get("parent")
	}
	if itemPath == "" {
		return "", "", fmt.Errorf("could not locate item path in resolved url %q "+
			"(link may require sign-in)", final.Redacted())
	}

	switch {
	case wantFile != "":
		// Folder link: id is the folder, append the requested filename.
		itemPath = path.Join(itemPath, wantFile)
		filename = wantFile
	default:
		// Single-file link: id is the file itself.
		filename = path.Base(itemPath)
	}

	dl := &url.URL{
		Scheme:   final.Scheme,
		Host:     final.Host,
		Path:     itemPath, // url.URL.String() escapes spaces -> %20, etc.
		RawQuery: "download=1",
	}
	return dl.String(), filename, nil
}

// followToListing requests shareURL, following redirects, and returns the final
// URL the server landed on. The body is closed without being read.
func followToListing(ctx context.Context, client *http.Client, shareURL string) (*url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shareURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	// SharePoint serves the redirect chain only to browser-like clients.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; hapiq)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resolve share link: %w", err)
	}
	_ = resp.Body.Close()

	// resp.Request is the final request after any redirects.
	if resp.Request == nil || resp.Request.URL == nil {
		return nil, fmt.Errorf("resolve share link: no final url")
	}
	return resp.Request.URL, nil
}
