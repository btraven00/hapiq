package test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/internal/checker"
	"github.com/btraven00/hapiq/pkg/validators"
)

// TestIntegration_ValidatorPackage tests the validators package.
func TestIntegration_ValidatorPackage(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectType    string
		expectValid   bool
		expectDataset bool
	}{
		{
			name:          "Valid Zenodo URL",
			input:         "https://zenodo.org/record/123456",
			expectValid:   true,
			expectType:    "zenodo_record",
			expectDataset: true,
		},
		{
			name:          "Valid Zenodo DOI",
			input:         "10.5281/zenodo.123456",
			expectValid:   true,
			expectType:    "zenodo_doi",
			expectDataset: true,
		},
		{
			name:          "Valid Figshare URL",
			input:         "https://figshare.com/articles/dataset/test/123456",
			expectValid:   true,
			expectType:    "figshare_article",
			expectDataset: true,
		},
		{
			name:          "Invalid URL",
			input:         "not-a-url",
			expectValid:   false,
			expectType:    "unknown",
			expectDataset: false,
		},
		{
			name:          "Valid SRA Accession",
			input:         "SRR123456",
			expectValid:   true,
			expectType:    "accession",
			expectDataset: true,
		},
		{
			name:          "Valid GEO Series",
			input:         "GSE123456",
			expectValid:   true,
			expectType:    "accession",
			expectDataset: true,
		},
		{
			name:          "Valid BioProject",
			input:         "PRJNA123456",
			expectValid:   true,
			expectType:    "accession",
			expectDataset: true,
		},
		{
			name:          "Valid ENA Accession",
			input:         "ERR123456",
			expectValid:   true,
			expectType:    "accession",
			expectDataset: true,
		},
		{
			name:          "Invalid Accession",
			input:         "XYZ123",
			expectValid:   false,
			expectType:    "",
			expectDataset: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validators.ValidateIdentifier(tt.input)

			if result.Valid != tt.expectValid {
				t.Errorf("Expected valid=%v, got %v", tt.expectValid, result.Valid)
			}

			if tt.expectType != "" && result.Type != tt.expectType {
				t.Errorf("Expected type=%s, got %s", tt.expectType, result.Type)
			}

			isDataset := validators.IsDatasetRepository(result.Type)
			if isDataset != tt.expectDataset {
				t.Errorf("Expected dataset=%v, got %v", tt.expectDataset, isDataset)
			}
		})
	}
}

// TestIntegration_CheckerPackage tests the checker package with mock server.
func TestIntegration_CheckerPackage(t *testing.T) {
	// Create mock server
	server := CreateMockServer()
	defer server.Close()

	config := checker.Config{
		Verbose:        false,
		Download:       false,
		TimeoutSeconds: 5,
		OutputFormat:   "json",
	}

	c := checker.New(config)

	tests := []struct {
		name           string
		target         string
		expectValid    bool
		expectMinScore float64
	}{
		{
			name:           "Mock Zenodo record",
			target:         server.URL + "/record/1234567",
			expectValid:    false, // Mock server returns 404 for unknown URLs
			expectMinScore: 0.0,
		},
		{
			name:           "Invalid DOI format",
			target:         "invalid-doi",
			expectValid:    false,
			expectMinScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := c.Check(tt.target)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Valid != tt.expectValid {
				t.Errorf("Expected valid=%v, got %v", tt.expectValid, result.Valid)
			}

			if result.LikelihoodScore < tt.expectMinScore {
				t.Errorf("Expected score >= %v, got %v", tt.expectMinScore, result.LikelihoodScore)
			}
		})
	}
}

// TestIntegration_CLIBasicFunctionality tests basic CLI functionality.
func TestIntegration_CLIBasicFunctionality(t *testing.T) {
	// Skip if no binary is available
	binaryPath := findBinary(t)
	if binaryPath == "" {
		t.Skip("Binary not found, run 'make build' first")
	}

	tests := []struct {
		name       string
		expectOut  string
		args       []string
		expectCode int
	}{
		{
			name:       "Help command",
			args:       []string{"--help"},
			expectCode: 0,
			expectOut:  "hapiq",
		},
		{
			name:       "Check help",
			args:       []string{"check", "--help"},
			expectCode: 0,
			expectOut:  "Check validates",
		},
		{
			name:       "Invalid DOI",
			args:       []string{"check", "invalid-input"},
			expectCode: 0, // hapiq handles invalid input gracefully without exit code 1
			expectOut:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath, tt.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			_ = cmd.Run()

			// Check exit code
			if exitCode := cmd.ProcessState.ExitCode(); exitCode != tt.expectCode {
				t.Errorf("Expected exit code %d, got %d", tt.expectCode, exitCode)
				t.Logf("Stdout: %s", stdout.String())
				t.Logf("Stderr: %s", stderr.String())
			}

			// Check output contains expected string
			if tt.expectOut != "" {
				output := stdout.String() + stderr.String()
				if !strings.Contains(output, tt.expectOut) {
					t.Errorf("Expected output to contain '%s', got: %s", tt.expectOut, output)
				}
			}
		})
	}
}

// TestIntegration_CLIJSONOutput tests JSON output format.
func TestIntegration_CLIJSONOutput(t *testing.T) {
	binaryPath := findBinary(t)
	if binaryPath == "" {
		t.Skip("Binary not found, run 'make build' first")
	}

	// Test with a simple DOI that should validate but fail HTTP check
	cmd := exec.Command(binaryPath, "check", "10.5281/zenodo.999999", "--output", "json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	_ = cmd.Run()
	// Command might fail with HTTP error, but should still produce JSON

	output := stdout.String()
	if output == "" {
		t.Skip("No output produced, might need network access")
	}

	// Try to parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Check for expected fields
	expectedFields := []string{"target", "valid", "dataset_type", "likelihood_score"}
	for _, field := range expectedFields {
		if _, exists := result[field]; !exists {
			t.Errorf("Expected field '%s' not found in JSON output", field)
		}
	}
}

// TestIntegration_PerformanceConstraints tests performance requirements.
func TestIntegration_PerformanceConstraints(t *testing.T) {
	config := checker.Config{
		Verbose:        false,
		Download:       false,
		TimeoutSeconds: 5,
		OutputFormat:   "json",
	}

	c := checker.New(config)

	// Test validation performance
	start := time.Now()
	validators.ValidateIdentifier("10.5281/zenodo.123456")
	validationTime := time.Since(start)

	if validationTime > TimingConstraints.MaxValidationTime {
		t.Errorf("Validation too slow: %v > %v", validationTime, TimingConstraints.MaxValidationTime)
	}

	// Test checker performance with invalid target (no network)
	start = time.Now()
	_, err := c.Check("invalid-target")
	checkTime := time.Since(start)

	// Should fail quickly for invalid targets
	if checkTime > 1*time.Second {
		t.Errorf("Check too slow for invalid target: %v", checkTime)
	}

	// Error is expected for invalid target
	if err != nil {
		t.Logf("Expected error for invalid target: %v", err)
	}
}

// TestIntegration_ErrorHandling tests error handling scenarios.
func TestIntegration_ErrorHandling(t *testing.T) {
	config := checker.Config{
		Verbose:        false,
		Download:       false,
		TimeoutSeconds: 1, // Short timeout
		OutputFormat:   "json",
	}

	c := checker.New(config)

	errorCases := []string{
		"",                 // Empty string
		"not-a-url-or-doi", // Invalid format
		"https://definitely-not-a-real-domain-12345.com", // Non-existent domain
		"10.9999/definitely.not.real",                    // Invalid DOI
	}

	for i, testCase := range errorCases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			result, err := c.Check(testCase)

			// Should not panic and should handle errors gracefully
			if result == nil {
				t.Error("Result should not be nil even for invalid inputs")
			}

			// For empty string, might return error
			if testCase == "" && err == nil {
				t.Log("Empty string handled gracefully")
			}

			// Invalid inputs should be marked as invalid
			if result != nil && result.Valid {
				t.Errorf("Invalid input '%s' was marked as valid", testCase)
			}
		})
	}
}

// TestIntegration_ConfigurationHandling tests different configuration options.
func TestIntegration_ConfigurationHandling(t *testing.T) {
	testCases := []checker.Config{
		{
			Verbose:        true,
			Download:       false,
			TimeoutSeconds: 5,
			OutputFormat:   "human",
		},
		{
			Verbose:        false,
			Download:       false,
			TimeoutSeconds: 10,
			OutputFormat:   "json",
		},
		{
			Verbose:        false,
			Download:       true,
			TimeoutSeconds: 30,
			OutputFormat:   "json",
		},
	}

	for i, config := range testCases {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			c := checker.New(config)

			// Test with a DOI (won't make network call for invalid DOI)
			result, err := c.Check("10.5281/zenodo.123456")

			// Should not panic with different configurations
			if result == nil && err == nil {
				t.Error("Both result and error are nil")
			}

			// Should handle output formatting without errors
			if result != nil {
				err := c.OutputResult(result)
				if err != nil {
					t.Errorf("OutputResult failed with config %+v: %v", config, err)
				}
			}
		})
	}
}

// Helper function to find the binary for testing.
func findBinary(t *testing.T) string {
	t.Helper()

	possiblePaths := []string{
		"../bin/hapiq",
		"./bin/hapiq",
		"hapiq", // In PATH
	}

	// Add OS-specific extensions
	if runtime.GOOS == "windows" {
		windowsPaths := make([]string, 0, len(possiblePaths)*2)
		for _, path := range possiblePaths {
			windowsPaths = append(windowsPaths, path+".exe")
			windowsPaths = append(windowsPaths, path)
		}
		possiblePaths = windowsPaths
	}

	for _, path := range possiblePaths {
		if absPath, err := filepath.Abs(path); err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}

		// Try relative to test directory
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// BenchmarkIntegration_Validation benchmarks the validation process.
func BenchmarkIntegration_Validation(b *testing.B) {
	testCases := []string{
		"https://zenodo.org/record/123456",
		"10.5281/zenodo.123456",
		"https://figshare.com/articles/dataset/test/123456",
		"10.6084/m9.figshare.123456",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, testCase := range testCases {
			validators.ValidateIdentifier(testCase)
		}
	}
}

// BenchmarkIntegration_Checker benchmarks the checker process.
func BenchmarkIntegration_Checker(b *testing.B) {
	config := checker.Config{
		Verbose:        false,
		Download:       false,
		TimeoutSeconds: 5,
		OutputFormat:   "json",
	}

	c := checker.New(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use invalid target to avoid network calls in benchmark
		c.Check("invalid-target")
	}
}
