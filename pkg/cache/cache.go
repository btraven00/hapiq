// Package cache implements a content-addressable local blob cache for hapiq.
// Blobs are keyed by sha256 and materialized into download destinations via
// reflink, hardlink, symlink, or copy (in that order of preference).
package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type contextKey struct{}

// Cache is a content-addressable store backed by the local filesystem and a
// SQLite index. It is safe for concurrent use.
type Cache struct {
	cfg Config
	db  *sql.DB
	s   *dbStmts
	mu  sync.Mutex
}

// Open opens (and if necessary initialises) the cache rooted at cfg.Dir.
func Open(cfg Config) (*Cache, error) {
	for _, sub := range []string{"blobs/sha256", "tmp"} {
		if err := os.MkdirAll(filepath.Join(cfg.Dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("cache init %s: %w", sub, err)
		}
	}

	db, s, err := openDB(cfg.Dir)
	if err != nil {
		return nil, err
	}

	return &Cache{cfg: cfg, db: db, s: s}, nil
}

// Close releases the database connection and prepared statements.
func (c *Cache) Close() error {
	c.s.close()
	return c.db.Close()
}

// WithCache returns a child context carrying c.
func WithCache(ctx context.Context, c *Cache) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext extracts a Cache from ctx; returns nil if none is set.
func FromContext(ctx context.Context) *Cache {
	v, _ := ctx.Value(contextKey{}).(*Cache)
	return v
}

// blobPath returns the filesystem path for a blob identified by its sha256.
func (c *Cache) blobPath(sha256hex string) string {
	return filepath.Join(c.cfg.Dir, "blobs", "sha256", sha256hex[:2], sha256hex)
}

// Get looks up rawURL in the index. On hit it refreshes last_used and returns
// the sha256 hash. On miss it returns ("", false, nil).
func (c *Cache) Get(ctx context.Context, rawURL string) (sha256hex string, hit bool, err error) {
	canonical, err := canonicalizeURL(rawURL)
	if err != nil {
		return "", false, err
	}

	var hash string
	var size int64
	row := c.s.getByURL.QueryRowContext(ctx, canonical)
	if err := row.Scan(&hash, &size); err == sql.ErrNoRows {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("cache lookup: %w", err)
	}

	// Verify the blob file still exists on disk.
	if _, err := os.Stat(c.blobPath(hash)); err != nil {
		return "", false, nil
	}

	now := time.Now().Unix()
	_, _ = c.s.touchBlob.ExecContext(ctx, now, hash)

	return hash, true, nil
}

// Put promotes tmpPath into the CAS and records rawURL → sha256hex.
// If a blob with the same hash already exists, tmpPath is removed and the URL
// is re-indexed pointing at the existing blob.
// Returns an error if the new blob would violate the configured quota.
func (c *Cache) Put(ctx context.Context, rawURL, tmpPath, sha256hex string) error {
	canonical, err := canonicalizeURL(rawURL)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	blobSz := fileSizeOrZero(tmpPath)
	if err := c.checkQuota(ctx, blobSz); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	blob := c.blobPath(sha256hex)
	now := time.Now().Unix()

	if _, statErr := os.Stat(blob); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
			return fmt.Errorf("create shard dir: %w", err)
		}
		if err := os.Rename(tmpPath, blob); err != nil {
			return fmt.Errorf("promote blob: %w", err)
		}
		if _, err := c.s.insertBlob.ExecContext(ctx, sha256hex, fileSizeOrZero(blob), now, now); err != nil {
			return fmt.Errorf("record blob: %w", err)
		}
	} else {
		// Blob already in CAS; discard the duplicate tmp.
		_ = os.Remove(tmpPath)
		_, _ = c.s.touchBlob.ExecContext(ctx, now, sha256hex)
	}

	if _, err := c.s.insertURL.ExecContext(ctx, canonical, sha256hex, "", "", now); err != nil {
		return fmt.Errorf("record url: %w", err)
	}
	return nil
}

// Materialize links or copies the blob identified by sha256hex to destPath,
// using the strategy configured in cfg.LinkStrategy.
func (c *Cache) Materialize(sha256hex, destPath string) error {
	return tryLink(c.blobPath(sha256hex), destPath, c.cfg.LinkStrategy)
}

// NewTmpFile creates a new temporary file in the cache's tmp directory.
// The caller should close and either keep or remove it.
func (c *Cache) NewTmpFile() (*os.File, error) {
	tmpDir := filepath.Join(c.cfg.Dir, "tmp")
	return os.CreateTemp(tmpDir, "download-*")
}

// VerifyBlob re-hashes the blob at sha256hex, evicts it if corrupt, and
// returns whether it is valid.
func (c *Cache) VerifyBlob(ctx context.Context, sha256hex string) (bool, error) {
	actual, err := hashFile(c.blobPath(sha256hex))
	if err != nil {
		return false, err
	}
	if actual != sha256hex {
		_ = c.Evict(ctx, sha256hex)
		return false, nil
	}
	return true, nil
}

// BlobCount returns the number of blobs in the index.
func (c *Cache) BlobCount(ctx context.Context) (int, error) {
	var n int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM blobs`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListBlobs returns a snapshot of all blobs with their URL mappings.
func (c *Cache) ListBlobs(ctx context.Context, urlGlob string) ([]BlobInfo, error) {
	query := `
SELECT b.sha256, b.size, b.last_used, GROUP_CONCAT(u.url, char(10))
FROM blobs b LEFT JOIN urls u ON u.sha256 = b.sha256
GROUP BY b.sha256
ORDER BY b.last_used DESC`
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlobInfo
	for rows.Next() {
		var bi BlobInfo
		var urls *string
		var lastUsed int64
		if err := rows.Scan(&bi.SHA256, &bi.Size, &lastUsed, &urls); err != nil {
			continue
		}
		bi.LastUsed = time.Unix(lastUsed, 0)
		if urls != nil {
			bi.URLs = splitLines(*urls)
		}
		if urlGlob == "" || matchGlob(urlGlob, bi.URLs) {
			out = append(out, bi)
		}
	}
	return out, rows.Err()
}

// BlobInfo is returned by ListBlobs.
type BlobInfo struct {
	LastUsed time.Time
	SHA256   string
	URLs     []string
	Size     int64
}

// Dir returns the cache root directory.
func (c *Cache) Dir() string { return c.cfg.Dir }

// --- helpers ---

func fileSizeOrZero(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func matchGlob(pattern string, urls []string) bool {
	for _, u := range urls {
		if matched, _ := filepath.Match(pattern, u); matched {
			return true
		}
	}
	return false
}
