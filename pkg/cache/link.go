package cache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Strategy controls how a cached blob is materialized into the destination path.
type Strategy string

const (
	StrategyAuto     Strategy = "auto"
	StrategyHardlink Strategy = "hardlink"
	StrategySymlink  Strategy = "symlink"
	StrategyCopy     Strategy = "copy"
)

// tryLink materializes src to dst using the given strategy.
// StrategyAuto tries reflink → hardlink → symlink (with warning) → copy.
func tryLink(src, dst string, strategy Strategy) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}
	switch strategy {
	case StrategyHardlink:
		return os.Link(src, dst)
	case StrategySymlink:
		return symlink(src, dst)
	case StrategyCopy:
		return copyFile(src, dst)
	default:
		return tryLinkAuto(src, dst)
	}
}

func tryLinkAuto(src, dst string) error {
	if err := tryReflink(src, dst); err == nil {
		return nil
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	if err := symlink(src, dst); err == nil {
		_, _ = fmt.Fprintf(os.Stderr, "cache: warning: using symlink (cross-device or btrfs unavailable)\n")
		return nil
	}
	return copyFile(src, dst)
}

func symlink(src, dst string) error {
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return os.Symlink(abs, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(filepath.Clean(src)) // #nosec G304 -- internal cache blob path
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(filepath.Clean(dst)) // #nosec G304 -- caller-controlled destination
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
