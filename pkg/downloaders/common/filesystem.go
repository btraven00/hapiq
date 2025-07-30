// Package common provides shared utilities for downloader implementations
// including filesystem operations, progress tracking, and user interaction.
package common

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// DirectoryChecker handles directory validation and conflict resolution.
type DirectoryChecker struct {
	OutputDir string
}

// NewDirectoryChecker creates a new directory checker.
func NewDirectoryChecker(outputDir string) *DirectoryChecker {
	return &DirectoryChecker{
		OutputDir: outputDir,
	}
}

// CheckAndPrepare validates the target directory and detects conflicts.
func (dc *DirectoryChecker) CheckAndPrepare(id string) (*downloaders.DirectoryStatus, error) {
	targetDir := filepath.Join(dc.OutputDir, SanitizeFilename(id))

	status := &downloaders.DirectoryStatus{
		TargetPath: targetDir,
		Exists:     false,
		HasWitness: false,
		Conflicts:  []string{},
	}

	// Check if target directory exists
	if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		status.Exists = true

		// Check for existing hapiq.json
		witnessPath := filepath.Join(targetDir, "hapiq.json")
		if _, err := os.Stat(witnessPath); err == nil {
			status.HasWitness = true
		}

		// Scan for file conflicts
		conflicts, err := dc.scanForConflicts(targetDir)
		if err != nil {
			return status, fmt.Errorf("failed to scan for conflicts: %w", err)
		}

		status.Conflicts = conflicts
	}

	// Check available disk space
	freeSpace, err := dc.getFreeSpace(dc.OutputDir)
	if err != nil {
		// Don't fail on this, just log a warning
		freeSpace = -1
	}

	status.FreeSpace = freeSpace

	return status, nil
}

// CreateDirectory creates the target directory with proper permissions.
func (dc *DirectoryChecker) CreateDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

// scanForConflicts identifies existing files that might conflict.
func (dc *DirectoryChecker) scanForConflicts(targetDir string) ([]string, error) {
	var conflicts []string

	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the witness file itself
		if filepath.Base(path) == "hapiq.json" {
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Add relative path to conflicts
		relPath, err := filepath.Rel(targetDir, path)
		if err != nil {
			relPath = path
		}

		conflicts = append(conflicts, relPath)

		// Limit the number of conflicts we report
		if len(conflicts) >= 100 {
			return filepath.SkipDir
		}

		return nil
	})

	return conflicts, err
}

// getFreeSpace returns available disk space in bytes.
func (dc *DirectoryChecker) getFreeSpace(path string) (int64, error) {
	var stat syscall.Statfs_t

	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0, err
	}

	// Available blocks * block size
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// HandleDirectoryConflicts presents options to the user for conflict resolution.
func HandleDirectoryConflicts(status *downloaders.DirectoryStatus, nonInteractive bool) (downloaders.Action, error) {
	if !status.Exists {
		return downloaders.ActionProceed, nil
	}

	if nonInteractive {
		// In non-interactive mode, default to merge if witness exists, otherwise skip
		if status.HasWitness {
			return downloaders.ActionMerge, nil
		}

		return downloaders.ActionSkip, nil
	}

	fmt.Printf("⚠️  Directory already exists: %s\n", status.TargetPath)

	if status.HasWitness {
		witness, err := LoadWitnessFile(status.TargetPath)
		if err == nil {
			fmt.Printf("   Previous download: %s (%s)\n",
				witness.DownloadTime.Format("2006-01-02 15:04"),
				witness.Source)
		}
	}

	if len(status.Conflicts) > 0 {
		fmt.Printf("   Conflicting files: %d\n", len(status.Conflicts))

		maxShow := 3
		for i, conflict := range status.Conflicts {
			if i >= maxShow {
				fmt.Printf("     ... and %d more\n", len(status.Conflicts)-maxShow)
				break
			}

			fmt.Printf("     %s\n", conflict)
		}
	}

	options := []string{
		"Skip (keep existing)",
		"Merge (add new files)",
		"Overwrite (replace all)",
		"Abort",
	}

	return promptUserChoice(options)
}

// promptUserChoice presents options to the user and returns their choice.
func promptUserChoice(options []string) (downloaders.Action, error) {
	fmt.Printf("\nChoose an action:\n")

	for i, option := range options {
		fmt.Printf("  %d) %s\n", i+1, option)
	}

	fmt.Printf("Enter choice [1-%d]: ", len(options))

	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return downloaders.ActionAbort, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(options) {
		fmt.Printf("Invalid choice. Please enter a number between 1 and %d.\n", len(options))
		return promptUserChoice(options) // Recursive retry
	}

	actions := []downloaders.Action{
		downloaders.ActionSkip,
		downloaders.ActionMerge,
		downloaders.ActionOverwrite,
		downloaders.ActionAbort,
	}

	return actions[choice-1], nil
}

// SanitizeFilename removes invalid characters from filenames.
func SanitizeFilename(filename string) string {
	// Replace invalid characters with underscores
	invalidChars := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	sanitized := invalidChars.ReplaceAllString(filename, "_")

	// Remove leading/trailing dots and spaces
	sanitized = strings.Trim(sanitized, ". ")

	// Ensure it's not empty
	if sanitized == "" {
		sanitized = "unnamed"
	}

	// Limit length to prevent filesystem issues
	if len(sanitized) > 255 {
		sanitized = sanitized[:255]
	}

	return sanitized
}

// EnsureDirectory creates a directory if it doesn't exist.
func EnsureDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

// CalculateFileChecksum computes SHA256 checksum for a file.
func CalculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// WriteWitnessFile creates a hapiq.json file with download metadata.
func WriteWitnessFile(targetDir string, witness *downloaders.WitnessFile) error {
	witnessPath := filepath.Join(targetDir, "hapiq.json")

	file, err := os.Create(witnessPath)
	if err != nil {
		return fmt.Errorf("failed to create witness file: %w", err)
	}
	defer file.Close()

	encoder := NewJSONEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(witness); err != nil {
		return fmt.Errorf("failed to encode witness file: %w", err)
	}

	return nil
}

// LoadWitnessFile reads and parses a hapiq.json file.
func LoadWitnessFile(targetDir string) (*downloaders.WitnessFile, error) {
	witnessPath := filepath.Join(targetDir, "hapiq.json")

	file, err := os.Open(witnessPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open witness file: %w", err)
	}
	defer file.Close()

	var witness downloaders.WitnessFile

	decoder := NewJSONDecoder(file)

	if err := decoder.Decode(&witness); err != nil {
		return nil, fmt.Errorf("failed to decode witness file: %w", err)
	}

	return &witness, nil
}

// FormatBytes converts bytes to human-readable format.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}

	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// EstimateDownloadTime estimates download time based on file size and average speed.
func EstimateDownloadTime(totalBytes int64, averageSpeedBps float64) string {
	if averageSpeedBps <= 0 {
		return "unknown"
	}

	seconds := float64(totalBytes) / averageSpeedBps

	if seconds < 60 {
		return fmt.Sprintf("%.0f seconds", seconds)
	} else if seconds < 3600 {
		return fmt.Sprintf("%.1f minutes", seconds/60)
	} else {
		return fmt.Sprintf("%.1f hours", seconds/3600)
	}
}

// AskUserConfirmation prompts the user for yes/no confirmation.
func AskUserConfirmation(message string) (bool, error) {
	fmt.Printf("%s [y/N]: ", message)

	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.ToLower(strings.TrimSpace(input))

	return input == "y" || input == "yes", nil
}
