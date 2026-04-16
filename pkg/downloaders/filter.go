package downloaders

import (
	"path"
	"strconv"
	"strings"
)

// ShouldDownload reports whether a file should be downloaded given its name,
// size (bytes), and the active options. It evaluates filters in this order:
//  1. IncludeRaw / ExcludeSupplementary coarse flags
//  2. IncludeExts / ExcludeExts extension lists
//  3. FilenameGlob pattern
//  4. MaxFileSize limit
//  5. Legacy CustomFilters map (kept for backward compatibility)
//
// size may be -1 when the caller does not know the file size ahead of time;
// MaxFileSize and size-based CustomFilters are then skipped.
func ShouldDownload(filename string, size int64, opts *DownloadOptions) bool {
	if opts == nil {
		return true
	}

	lower := strings.ToLower(filename)

	// --- coarse semantic flags ---
	if !opts.IncludeRaw {
		rawPatterns := []string{".fastq", ".fq", ".sra", ".bam", ".sam", "_raw", "raw_", ".cel", ".fcs"}
		for _, p := range rawPatterns {
			if strings.Contains(lower, p) {
				return false
			}
		}
	}

	if opts.ExcludeSupplementary {
		suppPatterns := []string{"supplementary", "suppl", "readme", "filelist", "license", "manifest", "metadata", "documentation"}
		for _, p := range suppPatterns {
			if strings.Contains(lower, p) {
				return false
			}
		}
	}

	// --- Phase 1: typed file filters ---

	// IncludeExts: file must end with one of the listed extensions.
	if len(opts.IncludeExts) > 0 {
		matched := false
		for _, ext := range opts.IncludeExts {
			if strings.HasSuffix(lower, strings.ToLower(ext)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// ExcludeExts: file must not end with any of the listed extensions.
	for _, ext := range opts.ExcludeExts {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return false
		}
	}

	// FilenameGlob: match against the base filename only.
	if opts.FilenameGlob != "" {
		base := filename
		if idx := strings.LastIndexAny(filename, "/\\"); idx >= 0 {
			base = filename[idx+1:]
		}
		matched, err := path.Match(opts.FilenameGlob, base)
		if err == nil && !matched {
			return false
		}
	}

	// MaxFileSize: skip files that exceed the limit.
	if opts.MaxFileSize > 0 && size >= 0 && size > opts.MaxFileSize {
		return false
	}

	// --- legacy CustomFilters ---
	for filterType, filterValue := range opts.CustomFilters {
		switch filterType {
		case "extension":
			if !strings.HasSuffix(lower, strings.ToLower(filterValue)) {
				return false
			}
		case "contains":
			if !strings.Contains(lower, strings.ToLower(filterValue)) {
				return false
			}
		case "excludes":
			if strings.Contains(lower, strings.ToLower(filterValue)) {
				return false
			}
		case "max_size":
			if size >= 0 {
				if maxSize, err := strconv.ParseInt(filterValue, 10, 64); err == nil && size > maxSize {
					return false
				}
			}
		case "min_size":
			if size >= 0 {
				if minSize, err := strconv.ParseInt(filterValue, 10, 64); err == nil && size < minSize {
					return false
				}
			}
		}
	}

	return true
}
