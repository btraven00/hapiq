package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultConnections is how many ranged requests Fetch runs at once for one
// file unless WithConnections says otherwise.
const DefaultConnections = 8

// Parallel range download tunables. Vars so tests can shrink them.
var (
	rangeChunk   int64 = 64 << 20
	rangeWorkers       = DefaultConnections
	rangeRetries       = 3
	rangeBackoff       = time.Second
)

type connectionsKey struct{}

// WithConnections sets how many ranged requests Fetch runs at once for one
// file (--connections). 1 still fetches in chunks, one after another, so a
// broken transfer costs one chunk, not the whole file.
func WithConnections(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, connectionsKey{}, n)
}

// connections returns the WithConnections value on ctx, or rangeWorkers.
func connections(ctx context.Context) int {
	if n, ok := ctx.Value(connectionsKey{}).(int); ok && n > 0 {
		return n
	}
	return rangeWorkers
}

// fetchToFile downloads rawURL into f. A server that honours Range is read in
// rangeChunk pieces by connections(ctx) concurrent requests; any other server gets
// a single plain GET. The first chunk doubles as the probe, so a file smaller
// than one chunk still costs one request.
func fetchToFile(ctx context.Context, client *http.Client, rawURL string, extra map[string]string, f *os.File) (int64, string, string, string, error) {
	hdr := maps.Clone(extra)
	if hdr == nil {
		hdr = map[string]string{}
	}
	hdr["Range"] = fmt.Sprintf("bytes=0-%d", rangeChunk-1)
	resp, err := getWaitingForReady(ctx, client, rawURL, hdr)
	if err != nil {
		return 0, "", "", "", err
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// An empty file has no byte 0 to ask for.
		_ = resp.Body.Close()
		return streamToFile(ctx, client, rawURL, extra, f)
	}
	if resp.StatusCode != http.StatusPartialContent {
		return copyBody(rawURL, resp, f)
	}

	start, total := contentRange(resp.Header.Get("Content-Range"))
	if start != 0 || total < 0 {
		_ = resp.Body.Close()
		return 0, "", "", "", fmt.Errorf("unexpected Content-Range %q for %s", resp.Header.Get("Content-Range"), rawURL)
	}
	ct := resp.Header.Get("Content-Type")
	name := FilenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	// Every later chunk must come from the same version of the file: with
	// If-Range a changed file answers 200, which fetchChunk refuses.
	if etag := resp.Header.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		hdr["If-Range"] = etag
	}

	from := rangeChunk
	if writeChunk(resp, f, 0, min(rangeChunk, total)) != nil {
		from = 0 // the probe broke off; fetch its chunk again with the rest
	}
	if from < total {
		if err := fetchChunks(ctx, client, rawURL, hdr, f, from, total); err != nil {
			return 0, "", "", "", err
		}
	}

	// ponytail: hashes in a second pass over the file (disk speed, ~4 min for
	// 227 GB); hash chunks in order as they land if that pass ever matters.
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, total)); err != nil {
		return 0, "", "", "", fmt.Errorf("hash: %w", err)
	}
	return total, hex.EncodeToString(h.Sum(nil)), ct, name, nil
}

// fetchChunks fills bytes [from, total) of f with connections(ctx) workers, each
// taking the next unclaimed chunk. The first chunk that fails for good stops
// the rest.
// ponytail: http.DefaultTransport keeps 2 idle conns per host, so most chunks
// open a new connection; give the client its own Transport if that shows up.
func fetchChunks(ctx context.Context, client *http.Client, rawURL string, hdr map[string]string, f *os.File, from, total int64) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var next atomic.Int64
	next.Store(from)
	var wg sync.WaitGroup
	for range connections(ctx) {
		wg.Go(func() {
			for ctx.Err() == nil {
				start := next.Add(rangeChunk) - rangeChunk
				if start >= total {
					return
				}
				if err := fetchChunk(ctx, client, rawURL, hdr, f, start, min(start+rangeChunk, total)); err != nil {
					cancel(err)
					return
				}
			}
		})
	}
	wg.Wait()
	return context.Cause(ctx)
}

// fetchChunk writes bytes [start, end) of rawURL into f at start, retrying a
// transfer that breaks off or a server that is briefly overloaded.
func fetchChunk(ctx context.Context, client *http.Client, rawURL string, hdr map[string]string, f *os.File, start, end int64) error {
	h := maps.Clone(hdr)
	h["Range"] = fmt.Sprintf("bytes=%d-%d", start, end-1)
	backoff := rangeBackoff
	var err error
	for attempt := 1; ; attempt++ {
		var resp *http.Response
		resp, err = getWaitingForReady(ctx, client, rawURL, h)
		if err == nil {
			code := resp.StatusCode
			got, _ := contentRange(resp.Header.Get("Content-Range"))
			switch {
			case code == http.StatusPartialContent && got == start:
				if err = writeChunk(resp, f, start, end-start); err == nil {
					return nil
				}
			case code == http.StatusTooManyRequests || code >= 500:
				_ = resp.Body.Close()
				err = fmt.Errorf("HTTP %d for bytes %d-%d of %s", code, start, end-1, rawURL)
			default:
				_ = resp.Body.Close()
				return fmt.Errorf("HTTP %d (Content-Range %q) for bytes %d-%d of %s; did the file change on the server?",
					code, resp.Header.Get("Content-Range"), start, end-1, rawURL)
			}
		}
		if attempt == rangeRetries {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// writeChunk copies exactly n bytes of resp's body into f at off. It closes the body.
func writeChunk(resp *http.Response, f *os.File, off, n int64) error {
	defer resp.Body.Close()
	got, err := io.Copy(io.NewOffsetWriter(f, off), io.LimitReader(resp.Body, n))
	if err == nil && got != n {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// contentRange parses "bytes a-b/n" into (a, n). Either is -1 when missing
// or unparseable (n is "*" when the server does not know the size).
func contentRange(cr string) (start, total int64) {
	start, total = -1, -1
	spec, ok := strings.CutPrefix(cr, "bytes ")
	if !ok {
		return
	}
	span, size, _ := strings.Cut(spec, "/")
	a, _, _ := strings.Cut(span, "-")
	if v, err := strconv.ParseInt(a, 10, 64); err == nil {
		start = v
	}
	if v, err := strconv.ParseInt(size, 10, 64); err == nil {
		total = v
	}
	return
}
