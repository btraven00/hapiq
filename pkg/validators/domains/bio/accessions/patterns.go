package accessions

import (
	"regexp"
	"strings"
)

// AccessionType represents the type of biological database accession
type AccessionType string

const (
	// Project level accessions
	ProjectBioProject AccessionType = "bioproject"  // PRJNA, PRJEB, PRJDB
	ProjectGSA        AccessionType = "gsa_project" // PRJC
	ProjectGEO        AccessionType = "geo_series"  // GSE

	// Study level accessions
	StudySRA AccessionType = "sra_study" // SRP, ERP, DRP
	StudyGSA AccessionType = "gsa_study" // CRA

	// Sample level accessions
	BioSampleNCBI AccessionType = "biosample_ncbi" // SAMN
	BioSampleEBI  AccessionType = "biosample_ebi"  // SAME
	BioSampleDDBJ AccessionType = "biosample_ddbj" // SAMD
	BioSampleGSA  AccessionType = "biosample_gsa"  // SAMC
	SampleSRA     AccessionType = "sra_sample"     // SRS, ERS, DRS
	SampleGEO     AccessionType = "geo_sample"     // GSM

	// Experiment level accessions
	ExperimentSRA AccessionType = "sra_experiment" // SRX, ERX, DRX
	ExperimentGSA AccessionType = "gsa_experiment" // CRX

	// Run level accessions
	RunSRA AccessionType = "sra_run" // SRR, ERR, DRR
	RunGSA AccessionType = "gsa_run" // CRR

	// Unknown or invalid
	Unknown AccessionType = "unknown"
)

// AccessionPattern defines a regex pattern for matching accession IDs
type AccessionPattern struct {
	Type        AccessionType
	Regex       *regexp.Regexp
	Description string
	Examples    []string
	Database    string
	Priority    int // Higher priority patterns are checked first
}

// AccessionDatabase represents information about a biological database
type AccessionDatabase struct {
	Name        string
	FullName    string
	URL         string
	Description string
	Region      string // "international", "europe", "asia", "usa"
}

// Known biological databases
var KnownDatabases = map[string]AccessionDatabase{
	"sra": {
		Name:        "SRA",
		FullName:    "Sequence Read Archive",
		URL:         "https://www.ncbi.nlm.nih.gov/sra",
		Description: "NCBI's primary archive for high-throughput sequencing data",
		Region:      "usa",
	},
	"ena": {
		Name:        "ENA",
		FullName:    "European Nucleotide Archive",
		URL:         "https://www.ebi.ac.uk/ena",
		Description: "Europe's primary nucleotide sequence resource",
		Region:      "europe",
	},
	"ddbj": {
		Name:        "DDBJ",
		FullName:    "DNA Data Bank of Japan",
		URL:         "https://www.ddbj.nig.ac.jp",
		Description: "Japan's primary nucleotide sequence database",
		Region:      "asia",
	},
	"gsa": {
		Name:        "GSA",
		FullName:    "Genome Sequence Archive",
		URL:         "https://ngdc.cncb.ac.cn/gsa",
		Description: "China's genomic data repository",
		Region:      "asia",
	},
	"geo": {
		Name:        "GEO",
		FullName:    "Gene Expression Omnibus",
		URL:         "https://www.ncbi.nlm.nih.gov/geo",
		Description: "NCBI's gene expression and genomics data repository",
		Region:      "usa",
	},
	"biosample": {
		Name:        "BioSample",
		FullName:    "BioSample Database",
		URL:         "https://www.ncbi.nlm.nih.gov/biosample",
		Description: "Database of biological sample metadata",
		Region:      "international",
	},
}

// Initialize all accession patterns based on iSeq patterns
var AccessionPatterns []AccessionPattern

func init() {
	AccessionPatterns = []AccessionPattern{
		// BioProject patterns (highest priority for projects)
		{
			Type:        ProjectBioProject,
			Regex:       regexp.MustCompile(`^PRJ[EDN][A-Z]\d+$`),
			Description: "BioProject identifiers from NCBI (PRJNA), EBI (PRJEB), or DDBJ (PRJDB)",
			Examples:    []string{"PRJNA123456", "PRJEB123456", "PRJDB123456"},
			Database:    "bioproject",
			Priority:    100,
		},

		// GSA Project patterns
		{
			Type:        ProjectGSA,
			Regex:       regexp.MustCompile(`^PRJC[A-Z]\d+$`),
			Description: "GSA (Genome Sequence Archive) project identifiers",
			Examples:    []string{"PRJCA123456", "PRJCB123456"},
			Database:    "gsa",
			Priority:    99,
		},

		// GEO Series patterns
		{
			Type:        ProjectGEO,
			Regex:       regexp.MustCompile(`^GSE\d+$`),
			Description: "GEO Series identifiers for gene expression studies",
			Examples:    []string{"GSE123456", "GSE000001"},
			Database:    "geo",
			Priority:    98,
		},

		// Study patterns
		{
			Type:        StudySRA,
			Regex:       regexp.MustCompile(`^[EDS]RP\d{6,}$`),
			Description: "SRA Study identifiers from ENA (ERP), DDBJ (DRP), or NCBI (SRP)",
			Examples:    []string{"SRP123456", "ERP123456", "DRP123456"},
			Database:    "sra",
			Priority:    90,
		},

		{
			Type:        StudyGSA,
			Regex:       regexp.MustCompile(`^CRA\d+$`),
			Description: "GSA Study identifiers",
			Examples:    []string{"CRA123456", "CRA000001"},
			Database:    "gsa",
			Priority:    89,
		},

		// BioSample patterns (high priority due to specificity)
		{
			Type:        BioSampleNCBI,
			Regex:       regexp.MustCompile(`^SAMN\d+$`),
			Description: "NCBI BioSample identifiers",
			Examples:    []string{"SAMN12345678", "SAMN00000001"},
			Database:    "biosample",
			Priority:    85,
		},

		{
			Type:        BioSampleEBI,
			Regex:       regexp.MustCompile(`^SAME\d+$`),
			Description: "EBI BioSample identifiers",
			Examples:    []string{"SAME12345678", "SAME00000001"},
			Database:    "biosample",
			Priority:    84,
		},

		{
			Type:        BioSampleDDBJ,
			Regex:       regexp.MustCompile(`^SAMD\d+$`),
			Description: "DDBJ BioSample identifiers",
			Examples:    []string{"SAMD12345678", "SAMD00000001"},
			Database:    "biosample",
			Priority:    83,
		},

		{
			Type:        BioSampleGSA,
			Regex:       regexp.MustCompile(`^SAMC\d+$`),
			Description: "GSA BioSample identifiers",
			Examples:    []string{"SAMC12345678", "SAMC00000001"},
			Database:    "gsa",
			Priority:    82,
		},

		// Sample patterns
		{
			Type:        SampleSRA,
			Regex:       regexp.MustCompile(`^[EDS]RS\d{6,}$`),
			Description: "SRA Sample identifiers from ENA (ERS), DDBJ (DRS), or NCBI (SRS)",
			Examples:    []string{"SRS123456", "ERS123456", "DRS123456"},
			Database:    "sra",
			Priority:    80,
		},

		{
			Type:        SampleGEO,
			Regex:       regexp.MustCompile(`^GSM\d+$`),
			Description: "GEO Sample identifiers",
			Examples:    []string{"GSM123456", "GSM000001"},
			Database:    "geo",
			Priority:    79,
		},

		// Experiment patterns
		{
			Type:        ExperimentSRA,
			Regex:       regexp.MustCompile(`^[EDS]RX\d{6,}$`),
			Description: "SRA Experiment identifiers from ENA (ERX), DDBJ (DRX), or NCBI (SRX)",
			Examples:    []string{"SRX123456", "ERX123456", "DRX123456"},
			Database:    "sra",
			Priority:    70,
		},

		{
			Type:        ExperimentGSA,
			Regex:       regexp.MustCompile(`^CRX\d+$`),
			Description: "GSA Experiment identifiers",
			Examples:    []string{"CRX123456", "CRX000001"},
			Database:    "gsa",
			Priority:    69,
		},

		// Run patterns (lowest priority as they're most granular)
		{
			Type:        RunSRA,
			Regex:       regexp.MustCompile(`^[EDS]RR\d{6,}$`),
			Description: "SRA Run identifiers from ENA (ERR), DDBJ (DRR), or NCBI (SRR)",
			Examples:    []string{"SRR123456", "ERR123456", "DRR123456"},
			Database:    "sra",
			Priority:    60,
		},

		{
			Type:        RunGSA,
			Regex:       regexp.MustCompile(`^CRR\d+$`),
			Description: "GSA Run identifiers",
			Examples:    []string{"CRR123456", "CRR000001"},
			Database:    "gsa",
			Priority:    59,
		},
	}

	// Sort patterns by priority (descending)
	for i := 0; i < len(AccessionPatterns)-1; i++ {
		for j := i + 1; j < len(AccessionPatterns); j++ {
			if AccessionPatterns[j].Priority > AccessionPatterns[i].Priority {
				AccessionPatterns[i], AccessionPatterns[j] = AccessionPatterns[j], AccessionPatterns[i]
			}
		}
	}
}

// MatchAccession attempts to match an input string against known accession patterns
// Returns the matched pattern and whether a match was found
func MatchAccession(input string) (*AccessionPattern, bool) {
	normalized := strings.TrimSpace(strings.ToUpper(input))

	// Try each pattern in priority order
	for i := range AccessionPatterns {
		pattern := &AccessionPatterns[i]
		if pattern.Regex.MatchString(normalized) {
			return pattern, true
		}
	}

	return nil, false
}

// MatchAllAccessions returns all patterns that match the input
// Useful for ambiguous cases or validation
func MatchAllAccessions(input string) []*AccessionPattern {
	normalized := strings.TrimSpace(strings.ToUpper(input))
	var matches []*AccessionPattern

	for i := range AccessionPatterns {
		pattern := &AccessionPatterns[i]
		if pattern.Regex.MatchString(normalized) {
			matches = append(matches, pattern)
		}
	}

	return matches
}

// ValidateAccessionFormat performs basic format validation
func ValidateAccessionFormat(input string) (bool, []string) {
	if input == "" {
		return false, []string{"empty input"}
	}

	normalized := strings.TrimSpace(input)
	var issues []string

	// Check for common formatting issues
	if normalized != strings.ToUpper(normalized) {
		issues = append(issues, "accession should be uppercase")
	}

	if strings.Contains(normalized, " ") {
		issues = append(issues, "accession contains spaces")
	}

	if len(normalized) < 6 {
		issues = append(issues, "accession too short (minimum 6 characters)")
	}

	if len(normalized) > 20 {
		issues = append(issues, "accession too long (maximum 20 characters)")
	}

	// Check for invalid characters
	validChars := regexp.MustCompile(`^[A-Z0-9._-]+$`)
	if !validChars.MatchString(strings.ToUpper(normalized)) {
		issues = append(issues, "accession contains invalid characters")
	}

	return len(issues) == 0, issues
}

// ExtractAccessionFromText attempts to find accession IDs within a text string
func ExtractAccessionFromText(text string) []string {
	var found []string
	seen := make(map[string]bool)

	// Look for patterns that might be accessions
	words := regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9._-]{5,19}\b`).FindAllString(text, -1)

	for _, word := range words {
		upperWord := strings.ToUpper(word)
		if _, matched := MatchAccession(upperWord); matched {
			if !seen[upperWord] {
				found = append(found, upperWord)
				seen[upperWord] = true
			}
		}
	}

	return found
}

// GetAccessionHierarchy returns the hierarchical relationship for an accession type
// Returns slice from most general to most specific
func GetAccessionHierarchy(accType AccessionType) []AccessionType {
	switch accType {
	case RunSRA:
		return []AccessionType{ProjectBioProject, StudySRA, ExperimentSRA, RunSRA}
	case RunGSA:
		return []AccessionType{ProjectGSA, StudyGSA, ExperimentGSA, RunGSA}
	case ExperimentSRA:
		return []AccessionType{ProjectBioProject, StudySRA, ExperimentSRA}
	case ExperimentGSA:
		return []AccessionType{ProjectGSA, StudyGSA, ExperimentGSA}
	case SampleSRA:
		return []AccessionType{ProjectBioProject, StudySRA, SampleSRA}
	case SampleGEO:
		return []AccessionType{ProjectGEO, SampleGEO}
	case StudySRA:
		return []AccessionType{ProjectBioProject, StudySRA}
	case StudyGSA:
		return []AccessionType{ProjectGSA, StudyGSA}
	case BioSampleNCBI, BioSampleEBI, BioSampleDDBJ, BioSampleGSA:
		return []AccessionType{accType}
	default:
		return []AccessionType{accType}
	}
}

// IsDataLevel returns true if the accession represents actual data (not just metadata)
func IsDataLevel(accType AccessionType) bool {
	dataLevels := map[AccessionType]bool{
		RunSRA:        true,
		RunGSA:        true,
		ExperimentSRA: true,
		ExperimentGSA: true,
		SampleGEO:     true,
		SampleSRA:     true,
	}
	return dataLevels[accType]
}

// GetPreferredDatabase returns the primary database for an accession type
func GetPreferredDatabase(accType AccessionType) string {
	switch accType {
	case ProjectBioProject, StudySRA, SampleSRA, ExperimentSRA, RunSRA:
		return "sra"
	case ProjectGSA, StudyGSA, BioSampleGSA, ExperimentGSA, RunGSA:
		return "gsa"
	case ProjectGEO, SampleGEO:
		return "geo"
	case BioSampleNCBI, BioSampleEBI, BioSampleDDBJ:
		return "biosample"
	default:
		return "unknown"
	}
}

// GetRegionalMirrors returns database mirrors for different regions
func GetRegionalMirrors(database string) []AccessionDatabase {
	var mirrors []AccessionDatabase

	switch database {
	case "sra":
		mirrors = append(mirrors, KnownDatabases["sra"])
		mirrors = append(mirrors, KnownDatabases["ena"])
		mirrors = append(mirrors, KnownDatabases["ddbj"])
	case "ena":
		mirrors = append(mirrors, KnownDatabases["ena"])
		mirrors = append(mirrors, KnownDatabases["sra"])
		mirrors = append(mirrors, KnownDatabases["ddbj"])
	case "ddbj":
		mirrors = append(mirrors, KnownDatabases["ddbj"])
		mirrors = append(mirrors, KnownDatabases["ena"])
		mirrors = append(mirrors, KnownDatabases["sra"])
	default:
		if db, exists := KnownDatabases[database]; exists {
			mirrors = append(mirrors, db)
		}
	}

	return mirrors
}
