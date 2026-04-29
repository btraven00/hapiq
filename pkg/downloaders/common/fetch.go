package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/btraven00/hapiq/pkg/cache"
)

// FetchOptions parameterises Fetch.
type FetchOptions struct {
	// Client is the HTTP client to use on cache miss. Defaults to http.DefaultClient.
	Client *http.Client
	// ExtraHeaders are added to the outbound request on cache miss.
	ExtraHeaders map[string]string
}

// FetchResult is returned by Fetch.
type FetchResult struct {
	// ContentType is the HTTP Content-Type header value (empty on cache hit).
	ContentType string
	// SHA256 is the hex-encoded sha256 of the file content.
	SHA256 string
	// N is the number of bytes in the file.
	N int64
	// Hit is true when the file was served from the local cache.
	Hit bool
}

// Fetch downloads rawURL to destPath, consulting the local cache when one is
// attached to ctx. On a cache hit the blob is materialized without a network
// round-trip. On a miss the response is streamed to a tmp file while computing
// sha256 in parallel; if a cache is present the blob is promoted before
// materializing to destPath.
func Fetch(ctx context.Context, rawURL, destPath string, opts FetchOptions) (FetchResult, error) {
	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}

	c := cache.FromContext(ctx)

	// ── cache hit path ────────────────────────────────────────────────────────
	if c != nil {
		hash, size, hit, err := c.Get(ctx, rawURL)
		if err != nil {
			return FetchResult{}, fmt.Errorf("cache get: %w", err)
		}
		if hit {
			if err := c.Materialize(hash, destPath); err != nil {
				return FetchResult{}, fmt.Errorf("materialize: %w", err)
			}
			return FetchResult{
				SHA256: hash,
				N:      size,
				Hit:    true,
			}, nil
		}
	}

	// ── cache miss / no-cache path ────────────────────────────────────────────
	var tmpPath string
	var sha256hex string
	var contentType string
	var n int64

	if c != nil {
		// Stream to a tmp file inside the cache dir so promotion is an atomic rename.
		tmpFile, err := c.NewTmpFile()
		if err != nil {
			return FetchResult{}, fmt.Errorf("create tmp: %w", err)
		}
		tmpPath = tmpFile.Name()

		n, sha256hex, contentType, err = streamToFile(ctx, client, rawURL, opts.ExtraHeaders, tmpFile)
		if closeErr := tmpFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tmpPath)
			return FetchResult{}, err
		}

		if err := c.Put(ctx, rawURL, tmpPath, sha256hex); err != nil {
			// Quota or disk error: fall through to direct write without cache.
			_, _ = fmt.Fprintf(os.Stderr, "cache: warning: skipping cache: %v\n", err)
			// tmpPath was either removed by Put or still exists; clean up.
			_ = os.Remove(tmpPath)
			return directFetch(ctx, client, rawURL, destPath, opts.ExtraHeaders)
		}

		if err := c.Materialize(sha256hex, destPath); err != nil {
			return FetchResult{}, fmt.Errorf("materialize: %w", err)
		}
	} else {
		// No cache: stream directly to destPath.
		f, err := os.Create(filepath.Clean(destPath)) // #nosec G304 -- caller-controlled destination
		if err != nil {
			return FetchResult{}, err
		}
		n, sha256hex, contentType, err = streamToFile(ctx, client, rawURL, opts.ExtraHeaders, f)
		_ = f.Close()
		if err != nil {
			_ = os.Remove(destPath)
			return FetchResult{}, err
		}
	}

	return FetchResult{
		ContentType: contentType,
		SHA256:      sha256hex,
		N:           n,
		Hit:         false,
	}, nil
}

// directFetch streams rawURL directly to destPath without cache involvement.
func directFetch(ctx context.Context, client *http.Client, rawURL, destPath string, extra map[string]string) (FetchResult, error) {
	f, err := os.Create(filepath.Clean(destPath)) // #nosec G304 -- caller-controlled destination
	if err != nil {
		return FetchResult{}, err
	}
	n, sha256hex, ct, err := streamToFile(ctx, client, rawURL, extra, f)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(destPath)
		return FetchResult{}, err
	}
	return FetchResult{ContentType: ct, SHA256: sha256hex, N: n}, nil
}

// streamToFile makes a GET request and copies the body into w, computing sha256
// as it goes. Returns (bytes written, sha256hex, Content-Type, error).
func streamToFile(ctx context.Context, client *http.Client, rawURL string, extra map[string]string, w io.Writer) (int64, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, http.NoBody)
	if err != nil {
		return 0, "", "", err
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), resp.Body)
	if err != nil {
		return 0, "", "", fmt.Errorf("read body: %w", err)
	}

	return n, hex.EncodeToString(h.Sum(nil)), resp.Header.Get("Content-Type"), nil
}

