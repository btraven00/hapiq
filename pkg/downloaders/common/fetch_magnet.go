package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/btraven00/hapiq/pkg/cache"
)

// fetchViaMagnet downloads hit through the external magnet helper and verifies
// it against the sha256 the peer resolved. It reports false for every failure,
// leaving nothing behind, so the caller can carry on with plain HTTP.
func fetchViaMagnet(ctx context.Context, peers *cache.Peers, hit cache.PeerHit, rawURL, destPath string, c *cache.Cache) (FetchResult, bool) {
	// With a cache the helper writes into the cache's tmp dir so promotion is
	// an atomic rename; without one it writes the destination directly.
	target := destPath
	if c != nil {
		tmpFile, err := c.NewTmpFile()
		if err != nil {
			return FetchResult{}, false
		}
		_ = tmpFile.Close()
		target = tmpFile.Name()
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return FetchResult{}, false
	}

	if err := peers.FetchMagnet(ctx, hit, target); err != nil {
		if !errors.Is(err, cache.ErrNoMagnetHelper) {
			_, _ = fmt.Fprintf(os.Stderr, "cache: magnet from %s failed, falling back: %v\n", hit.Base, err)
		}
		_ = os.Remove(target)
		return FetchResult{}, false
	}

	// The swarm is as untrusted as the peer that named it. The helper refuses
	// a torrent of any length but hit.Size, so that is the byte count.
	gotHash, err := CalculateFileChecksum(target)
	n := hit.Size
	if err != nil || gotHash != hit.SHA256 {
		warnPeer(hit, err, gotHash)
		_ = os.Remove(target)
		return FetchResult{}, false
	}

	if c != nil {
		return keepPeerFile(ctx, c, hit, rawURL, target, destPath, n)
	}
	return FetchResult{SHA256: gotHash, Filename: hit.Filename, N: n, Hit: true, PeerURL: hit.Base}, true
}

// keepPeerFile files tmpPath, a peer download in c's tmp dir already verified
// against hit.SHA256, into the cache and materializes it at destPath. When the
// cache refuses it (quota full), the verified file becomes destPath instead of
// being thrown away and downloaded again.
func keepPeerFile(ctx context.Context, c *cache.Cache, hit cache.PeerHit, rawURL, tmpPath, destPath string, n int64) (FetchResult, bool) {
	res := FetchResult{SHA256: hit.SHA256, Filename: hit.Filename, N: n, Hit: true, PeerURL: hit.Base}
	if err := c.Put(ctx, rawURL, tmpPath, hit.SHA256); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cache: warning: skipping cache: %v\n", err)
		// ponytail: a rename, so a destination on another filesystem than the
		// cache falls back to downloading again; copy if that shows up.
		if err := os.Rename(tmpPath, destPath); err != nil {
			_ = os.Remove(tmpPath)
			return FetchResult{}, false
		}
		return res, true
	}
	_ = c.RecordFilename(ctx, rawURL, hit.Filename)
	if err := c.Materialize(hit.SHA256, destPath); err != nil {
		return FetchResult{}, false
	}
	return res, true
}
