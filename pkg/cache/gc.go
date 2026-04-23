package cache

import (
	"context"
	"fmt"
	"os"
	"time"
)

// TotalSize returns the sum of all blob sizes recorded in the index.
func (c *Cache) TotalSize(ctx context.Context) (int64, error) {
	var n int64
	if err := c.s.totalSize.QueryRowContext(ctx).Scan(&n); err != nil {
		return 0, fmt.Errorf("read total size: %w", err)
	}
	return n, nil
}

// checkQuota returns an error if admitting blobSize bytes would violate the
// configured max_size or min_free_disk. Must be called with c.mu held.
func (c *Cache) checkQuota(ctx context.Context, blobSize int64) error {
	if c.cfg.MaxSize > 0 {
		var total int64
		if err := c.s.totalSize.QueryRowContext(ctx).Scan(&total); err != nil {
			return fmt.Errorf("read total size: %w", err)
		}
		if total+blobSize > c.cfg.MaxSize {
			return fmt.Errorf(
				"cache quota exceeded: %s used + %s new > %s limit (run 'hapiq cache gc' to free space)",
				formatSize(total), formatSize(blobSize), formatSize(c.cfg.MaxSize),
			)
		}
	}

	if c.cfg.MinFreeDisk > 0 {
		free, err := diskFreeBytes(c.cfg.Dir)
		if err == nil && free-blobSize < c.cfg.MinFreeDisk {
			return fmt.Errorf(
				"insufficient free disk: %s available, need %s more (min_free_disk = %s)",
				formatSize(free), formatSize(blobSize), formatSize(c.cfg.MinFreeDisk),
			)
		}
	}

	return nil
}

// Evict removes a blob and all its URL mappings from the cache.
func (c *Cache) Evict(ctx context.Context, sha256hex string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictLocked(ctx, sha256hex)
}

// evictLocked removes blob + URLs from the DB and filesystem.
// Caller must hold c.mu.
func (c *Cache) evictLocked(ctx context.Context, sha256hex string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM urls WHERE sha256 = ?`, sha256hex); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE sha256 = ?`, sha256hex); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	_ = os.Remove(c.blobPath(sha256hex))
	return nil
}

// GCResult holds the outcome of a GC run.
type GCResult struct {
	Evicted int
	Freed   int64
	// Skipped counts blobs that were candidates for eviction but were skipped
	// because they have live hardlinks (Nlink > 1) from output directories.
	Skipped int
	DryRun  bool
}

// GC evicts blobs by LRU until the cache is under max_size.
// When keepDuration > 0, blobs accessed within that duration are spared.
func (c *Cache) GC(ctx context.Context, dryRun bool, keepDuration time.Duration) (GCResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfg.MaxSize == 0 {
		return GCResult{DryRun: dryRun}, nil
	}

	var total int64
	if err := c.s.totalSize.QueryRowContext(ctx).Scan(&total); err != nil {
		return GCResult{}, err
	}

	target := total - c.cfg.MaxSize
	if target <= 0 {
		return GCResult{DryRun: dryRun}, nil
	}

	rows, err := c.s.listLRU.QueryContext(ctx)
	if err != nil {
		return GCResult{}, err
	}

	keepAfter := int64(0)
	if keepDuration > 0 {
		keepAfter = time.Now().Add(-keepDuration).Unix()
	}

	type candidate struct {
		sha256   string
		size     int64
		lastUsed int64
	}
	var toEvict []candidate
	var freed int64
	for rows.Next() && freed < target {
		var hash string
		var size, lastUsed int64
		if err := rows.Scan(&hash, &size, &lastUsed); err != nil {
			continue
		}
		if keepDuration > 0 && lastUsed >= keepAfter {
			continue
		}
		toEvict = append(toEvict, candidate{hash, size, lastUsed})
		freed += size
	}
	rows.Close()

	// Filter out pinned blobs before reporting or acting.
	var unpinned []candidate
	skipped := 0
	for _, e := range toEvict {
		if blobNlink(c.blobPath(e.sha256)) > 1 {
			skipped++
		} else {
			unpinned = append(unpinned, e)
		}
	}

	res := GCResult{DryRun: dryRun, Freed: freed, Evicted: len(unpinned), Skipped: skipped}
	if dryRun {
		return res, nil
	}

	res.Freed = 0
	res.Evicted = 0
	for _, e := range unpinned {
		blobSz := fileSizeOrZero(c.blobPath(e.sha256))
		if err := c.evictLocked(ctx, e.sha256); err == nil {
			res.Evicted++
			res.Freed += blobSz
		}
	}
	return res, nil
}

// PruneURLs removes url rows whose corresponding blob is missing from the index.
func (c *Cache) PruneURLs(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.db.ExecContext(ctx,
		`DELETE FROM urls WHERE sha256 NOT IN (SELECT sha256 FROM blobs)`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
