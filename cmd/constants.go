// Package cmd provides shared constants for command-line interface commands.
package cmd

const (
	// Common numeric constants.
	percentageMultiplier = 100

	// Timeout constants.
	defaultCheckTimeoutSec    = 30
	defaultDownloadTimeoutSec = 300

	// UI and formatting constants.
	tabWriterPadding       = 2
	maxDescriptionLength   = 100
	maxDescriptionChars    = 60
	truncationSuffix       = 3 // for "..."
	defaultContextLength   = 100
	progressUpdateInterval = 500
	maxDisplayLinks        = 10
	minURLParts            = 3

	// Processing constants.
	minPartsRequired       = 2
	requiredArgsCount      = 2
	defaultMinConfidence   = 0.85
	defaultMaxLinksPerPage = 50
	defaultConcurrentDL    = 8

	// File and directory constants.
	defaultDirPermissions = 0o750

	// Output format constants.
	outputFormatJSON = "json"
	outputFormatCSV  = "csv"
)
