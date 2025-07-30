package accessions

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchAccession(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType AccessionType
		expectedDB   string
		expectMatch  bool
	}{
		// SRA Run accessions
		{
			name:         "Valid SRR accession",
			input:        "SRR123456",
			expectMatch:  true,
			expectedType: RunSRA,
			expectedDB:   "sra",
		},
		{
			name:         "Valid ERR accession",
			input:        "ERR1234567",
			expectMatch:  true,
			expectedType: RunSRA,
			expectedDB:   "sra",
		},
		{
			name:         "Valid DRR accession",
			input:        "DRR123456",
			expectMatch:  true,
			expectedType: RunSRA,
			expectedDB:   "sra",
		},
		{
			name:         "Lowercase SRR should work",
			input:        "srr123456",
			expectMatch:  true,
			expectedType: RunSRA,
			expectedDB:   "sra",
		},

		// SRA Experiment accessions
		{
			name:         "Valid SRX accession",
			input:        "SRX123456",
			expectMatch:  true,
			expectedType: ExperimentSRA,
			expectedDB:   "sra",
		},
		{
			name:         "Valid ERX accession",
			input:        "ERX1234567",
			expectMatch:  true,
			expectedType: ExperimentSRA,
			expectedDB:   "sra",
		},

		// SRA Sample accessions
		{
			name:         "Valid SRS accession",
			input:        "SRS123456",
			expectMatch:  true,
			expectedType: SampleSRA,
			expectedDB:   "sra",
		},
		{
			name:         "Valid ERS accession",
			input:        "ERS1234567",
			expectMatch:  true,
			expectedType: SampleSRA,
			expectedDB:   "sra",
		},

		// SRA Study accessions
		{
			name:         "Valid SRP accession",
			input:        "SRP123456",
			expectMatch:  true,
			expectedType: StudySRA,
			expectedDB:   "sra",
		},
		{
			name:         "Valid ERP accession",
			input:        "ERP123456",
			expectMatch:  true,
			expectedType: StudySRA,
			expectedDB:   "sra",
		},

		// BioProject accessions
		{
			name:         "Valid PRJNA accession",
			input:        "PRJNA123456",
			expectMatch:  true,
			expectedType: ProjectBioProject,
			expectedDB:   "bioproject",
		},
		{
			name:         "Valid PRJEB accession",
			input:        "PRJEB123456",
			expectMatch:  true,
			expectedType: ProjectBioProject,
			expectedDB:   "bioproject",
		},
		{
			name:         "Valid PRJDB accession",
			input:        "PRJDB123456",
			expectMatch:  true,
			expectedType: ProjectBioProject,
			expectedDB:   "bioproject",
		},

		// BioSample accessions
		{
			name:         "Valid SAMN accession",
			input:        "SAMN12345678",
			expectMatch:  true,
			expectedType: BioSampleNCBI,
			expectedDB:   "biosample",
		},
		{
			name:         "Valid SAME accession",
			input:        "SAME12345678",
			expectMatch:  true,
			expectedType: BioSampleEBI,
			expectedDB:   "biosample",
		},
		{
			name:         "Valid SAMD accession",
			input:        "SAMD12345678",
			expectMatch:  true,
			expectedType: BioSampleDDBJ,
			expectedDB:   "biosample",
		},
		{
			name:         "Valid SAMC accession",
			input:        "SAMC12345678",
			expectMatch:  true,
			expectedType: BioSampleGSA,
			expectedDB:   "gsa",
		},

		// GSA accessions
		{
			name:         "Valid CRR accession",
			input:        "CRR123456",
			expectMatch:  true,
			expectedType: RunGSA,
			expectedDB:   "gsa",
		},
		{
			name:         "Valid CRX accession",
			input:        "CRX123456",
			expectMatch:  true,
			expectedType: ExperimentGSA,
			expectedDB:   "gsa",
		},
		{
			name:         "Valid CRA accession",
			input:        "CRA123456",
			expectMatch:  true,
			expectedType: StudyGSA,
			expectedDB:   "gsa",
		},
		{
			name:         "Valid PRJCA accession",
			input:        "PRJCA123456",
			expectMatch:  true,
			expectedType: ProjectGSA,
			expectedDB:   "gsa",
		},

		// GEO accessions
		{
			name:         "Valid GSE accession",
			input:        "GSE123456",
			expectMatch:  true,
			expectedType: ProjectGEO,
			expectedDB:   "geo",
		},
		{
			name:         "Valid GSM accession",
			input:        "GSM123456",
			expectMatch:  true,
			expectedType: SampleGEO,
			expectedDB:   "geo",
		},

		// Invalid accessions
		{
			name:        "Too short accession",
			input:       "SRR12",
			expectMatch: false,
		},
		{
			name:        "Invalid prefix",
			input:       "XYZ123456",
			expectMatch: false,
		},
		{
			name:        "Missing numbers",
			input:       "SRR",
			expectMatch: false,
		},
		{
			name:        "Empty string",
			input:       "",
			expectMatch: false,
		},
		{
			name:        "Only numbers",
			input:       "123456",
			expectMatch: false,
		},
		{
			name:        "Invalid characters",
			input:       "SRR123@456",
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, matched := MatchAccession(tt.input)

			if matched != tt.expectMatch {
				t.Errorf("MatchAccession(%q) matched = %v, expected %v", tt.input, matched, tt.expectMatch)
				return
			}

			if tt.expectMatch {
				if pattern == nil {
					t.Errorf("MatchAccession(%q) returned nil pattern but expected match", tt.input)
					return
				}

				if pattern.Type != tt.expectedType {
					t.Errorf("MatchAccession(%q) type = %v, expected %v", tt.input, pattern.Type, tt.expectedType)
				}

				if pattern.Database != tt.expectedDB {
					t.Errorf("MatchAccession(%q) database = %v, expected %v", tt.input, pattern.Database, tt.expectedDB)
				}
			}
		})
	}
}

func TestMatchAllAccessions(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedTypes []AccessionType
		expectedCount int
	}{
		{
			name:          "Unique SRR match",
			input:         "SRR123456",
			expectedCount: 1,
			expectedTypes: []AccessionType{RunSRA},
		},
		{
			name:          "No matches",
			input:         "INVALID123",
			expectedCount: 0,
			expectedTypes: nil,
		},
		{
			name:          "Valid GSE",
			input:         "GSE123456",
			expectedCount: 1,
			expectedTypes: []AccessionType{ProjectGEO},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := MatchAllAccessions(tt.input)

			if len(patterns) != tt.expectedCount {
				t.Errorf("MatchAllAccessions(%q) returned %d patterns, expected %d", tt.input, len(patterns), tt.expectedCount)
				return
			}

			var actualTypes []AccessionType
			for _, pattern := range patterns {
				actualTypes = append(actualTypes, pattern.Type)
			}

			if !reflect.DeepEqual(actualTypes, tt.expectedTypes) {
				t.Errorf("MatchAllAccessions(%q) types = %v, expected %v", tt.input, actualTypes, tt.expectedTypes)
			}
		})
	}
}

func TestValidateAccessionFormat(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedIssues []string
		expectedValid  bool
	}{
		{
			name:           "Valid format",
			input:          "SRR123456",
			expectedValid:  true,
			expectedIssues: nil,
		},
		{
			name:           "Empty input",
			input:          "",
			expectedValid:  false,
			expectedIssues: []string{"empty input"},
		},
		{
			name:           "Too short",
			input:          "SRR12",
			expectedValid:  false,
			expectedIssues: []string{"accession too short (minimum 6 characters)"},
		},
		{
			name:           "Too long",
			input:          "SRR123456789012345678901",
			expectedValid:  false,
			expectedIssues: []string{"accession too long (maximum 20 characters)"},
		},
		{
			name:           "Lowercase should warn",
			input:          "srr123456",
			expectedValid:  false,
			expectedIssues: []string{"accession should be uppercase"},
		},
		{
			name:           "Contains spaces",
			input:          "SRR 123456",
			expectedValid:  false,
			expectedIssues: []string{"accession contains spaces", "accession contains invalid characters"},
		},
		{
			name:           "Invalid characters",
			input:          "SRR123@456",
			expectedValid:  false,
			expectedIssues: []string{"accession contains invalid characters"},
		},
		{
			name:          "Multiple issues",
			input:         "srr123@456 extra",
			expectedValid: false,
			expectedIssues: []string{
				"accession should be uppercase",
				"accession contains spaces",
				"accession contains invalid characters",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, issues := ValidateAccessionFormat(tt.input)

			if valid != tt.expectedValid {
				t.Errorf("ValidateAccessionFormat(%q) valid = %v, expected %v", tt.input, valid, tt.expectedValid)
			}

			if !reflect.DeepEqual(issues, tt.expectedIssues) {
				t.Errorf("ValidateAccessionFormat(%q) issues = %v, expected %v", tt.input, issues, tt.expectedIssues)
			}
		})
	}
}

func TestExtractAccessionFromText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Single accession",
			input:    "The dataset SRR123456 contains RNA-seq data",
			expected: []string{"SRR123456"},
		},
		{
			name:     "Multiple accessions",
			input:    "Data from SRR123456, ERX789012, and GSE456789",
			expected: []string{"SRR123456", "ERX789012", "GSE456789"},
		},
		{
			name:     "No accessions",
			input:    "This text contains no valid accessions",
			expected: nil,
		},
		{
			name:     "Accession with punctuation",
			input:    "See accession SRR123456.",
			expected: []string{"SRR123456"},
		},
		{
			name:     "Mixed case",
			input:    "accession srr123456 should be found",
			expected: []string{"SRR123456"},
		},
		{
			name:     "Duplicate accessions",
			input:    "SRR123456 and SRR123456 again",
			expected: []string{"SRR123456"}, // Should deduplicate
		},
		{
			name:     "GSA accessions",
			input:    "Chinese data: CRR123456 from study CRA789012",
			expected: []string{"CRR123456", "CRA789012"},
		},
		{
			name:     "BioSample accessions",
			input:    "Samples SAMN12345678, SAME87654321",
			expected: []string{"SAMN12345678", "SAME87654321"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractAccessionFromText(tt.input)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ExtractAccessionFromText(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetAccessionHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		accType  AccessionType
		expected []AccessionType
	}{
		{
			name:    "SRA Run hierarchy",
			accType: RunSRA,
			expected: []AccessionType{
				ProjectBioProject,
				StudySRA,
				ExperimentSRA,
				RunSRA,
			},
		},
		{
			name:    "GSA Run hierarchy",
			accType: RunGSA,
			expected: []AccessionType{
				ProjectGSA,
				StudyGSA,
				ExperimentGSA,
				RunGSA,
			},
		},
		{
			name:    "SRA Experiment hierarchy",
			accType: ExperimentSRA,
			expected: []AccessionType{
				ProjectBioProject,
				StudySRA,
				ExperimentSRA,
			},
		},
		{
			name:    "GEO Sample hierarchy",
			accType: SampleGEO,
			expected: []AccessionType{
				ProjectGEO,
				SampleGEO,
			},
		},
		{
			name:    "BioSample (standalone)",
			accType: BioSampleNCBI,
			expected: []AccessionType{
				BioSampleNCBI,
			},
		},
		{
			name:    "Project level",
			accType: ProjectBioProject,
			expected: []AccessionType{
				ProjectBioProject,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAccessionHierarchy(tt.accType)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetAccessionHierarchy(%v) = %v, expected %v", tt.accType, result, tt.expected)
			}
		})
	}
}

func TestIsDataLevel(t *testing.T) {
	tests := []struct {
		name     string
		accType  AccessionType
		expected bool
	}{
		{
			name:     "SRA Run is data level",
			accType:  RunSRA,
			expected: true,
		},
		{
			name:     "GSA Run is data level",
			accType:  RunGSA,
			expected: true,
		},
		{
			name:     "SRA Experiment is data level",
			accType:  ExperimentSRA,
			expected: true,
		},
		{
			name:     "GEO Sample is data level",
			accType:  SampleGEO,
			expected: true,
		},
		{
			name:     "SRA Sample is data level",
			accType:  SampleSRA,
			expected: true,
		},
		{
			name:     "Study is not data level",
			accType:  StudySRA,
			expected: false,
		},
		{
			name:     "Project is not data level",
			accType:  ProjectBioProject,
			expected: false,
		},
		{
			name:     "BioSample is not data level",
			accType:  BioSampleNCBI,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDataLevel(tt.accType)

			if result != tt.expected {
				t.Errorf("IsDataLevel(%v) = %v, expected %v", tt.accType, result, tt.expected)
			}
		})
	}
}

func TestGetPreferredDatabase(t *testing.T) {
	tests := []struct {
		name     string
		accType  AccessionType
		expected string
	}{
		{
			name:     "SRA types map to sra",
			accType:  RunSRA,
			expected: "sra",
		},
		{
			name:     "GSA types map to gsa",
			accType:  RunGSA,
			expected: "gsa",
		},
		{
			name:     "GEO types map to geo",
			accType:  ProjectGEO,
			expected: "geo",
		},
		{
			name:     "BioSample types map to biosample",
			accType:  BioSampleNCBI,
			expected: "biosample",
		},
		{
			name:     "BioProject maps to sra",
			accType:  ProjectBioProject,
			expected: "sra",
		},
		{
			name:     "Unknown type",
			accType:  Unknown,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetPreferredDatabase(tt.accType)

			if result != tt.expected {
				t.Errorf("GetPreferredDatabase(%v) = %v, expected %v", tt.accType, result, tt.expected)
			}
		})
	}
}

func TestGetRegionalMirrors(t *testing.T) {
	tests := []struct {
		name     string
		database string
		expected int // Number of expected mirrors
	}{
		{
			name:     "SRA has multiple mirrors",
			database: "sra",
			expected: 3, // SRA, ENA, DDBJ
		},
		{
			name:     "ENA has multiple mirrors",
			database: "ena",
			expected: 3, // ENA, SRA, DDBJ
		},
		{
			name:     "GSA has single entry",
			database: "gsa",
			expected: 1,
		},
		{
			name:     "Unknown database",
			database: "unknown",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetRegionalMirrors(tt.database)

			if len(result) != tt.expected {
				t.Errorf("GetRegionalMirrors(%q) returned %d mirrors, expected %d", tt.database, len(result), tt.expected)
			}

			// Verify each result has required fields
			for _, mirror := range result {
				if mirror.Name == "" || mirror.URL == "" {
					t.Errorf("GetRegionalMirrors(%q) returned mirror with empty Name or URL: %+v", tt.database, mirror)
				}
			}
		})
	}
}

func TestAccessionPatternPriorities(t *testing.T) {
	// Ensure patterns are sorted by priority
	for i := 0; i < len(AccessionPatterns)-1; i++ {
		current := AccessionPatterns[i]
		next := AccessionPatterns[i+1]

		if current.Priority < next.Priority {
			t.Errorf("AccessionPatterns not sorted by priority: pattern %d (priority %d) comes before pattern %d (priority %d)",
				i, current.Priority, i+1, next.Priority)
		}
	}
}

func TestKnownDatabases(t *testing.T) {
	expectedDatabases := []string{"sra", "ena", "ddbj", "gsa", "geo", "biosample"}

	for _, dbName := range expectedDatabases {
		db, exists := KnownDatabases[dbName]
		if !exists {
			t.Errorf("Expected database %q not found in KnownDatabases", dbName)
			continue
		}

		if db.Name == "" {
			t.Errorf("Database %q has empty Name", dbName)
		}

		if db.FullName == "" {
			t.Errorf("Database %q has empty FullName", dbName)
		}

		if db.URL == "" {
			t.Errorf("Database %q has empty URL", dbName)
		}

		if !strings.HasPrefix(db.URL, "http") {
			t.Errorf("Database %q URL should start with http: %q", dbName, db.URL)
		}

		if db.Region == "" {
			t.Errorf("Database %q has empty Region", dbName)
		}
	}
}

func TestAccessionTypeConstants(t *testing.T) {
	// Test that all AccessionType constants are defined and unique
	allTypes := []AccessionType{
		ProjectBioProject, ProjectGSA, ProjectGEO,
		StudySRA, StudyGSA,
		BioSampleNCBI, BioSampleEBI, BioSampleDDBJ, BioSampleGSA,
		SampleSRA, SampleGEO,
		ExperimentSRA, ExperimentGSA,
		RunSRA, RunGSA,
		Unknown,
	}

	seen := make(map[AccessionType]bool)
	for _, accType := range allTypes {
		if accType == "" {
			t.Error("Found empty AccessionType constant")
			continue
		}

		if seen[accType] {
			t.Errorf("Duplicate AccessionType constant: %q", accType)
		}
		seen[accType] = true
	}
}

// Benchmark tests for performance analysis.
func BenchmarkMatchAccession(b *testing.B) {
	testCases := []string{
		"SRR123456",
		"ERR1234567",
		"GSE123456",
		"CRR123456",
		"SAMN12345678",
		"PRJNA123456",
		"INVALID123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, testCase := range testCases {
			MatchAccession(testCase)
		}
	}
}

func BenchmarkExtractAccessionFromText(b *testing.B) {
	text := "This study includes data from SRR123456, ERX789012, GSE456789, and CRR654321 accessions from various databases."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractAccessionFromText(text)
	}
}

func BenchmarkValidateAccessionFormat(b *testing.B) {
	testCases := []string{
		"SRR123456",
		"invalid_accession",
		"srr123456",
		"SRR123@456",
		"",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, testCase := range testCases {
			ValidateAccessionFormat(testCase)
		}
	}
}
