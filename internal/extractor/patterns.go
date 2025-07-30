package extractor

import (
	"regexp"
)

// getExtractionPatterns returns patterns for extracting identifiers that can be validated by domain validators
func getExtractionPatterns() []ExtractionPattern {
	return []ExtractionPattern{
		// High-confidence patterns for known repositories
		{
			Name:        "DOI Pattern",
			Regex:       regexp.MustCompile(`(?i)(?:doi:?\s*|https?://(?:dx\.)?doi\.org/)(10\.\d{4,}/[^\s\]}>)]{1,100})`),
			Type:        LinkTypeDOI,
			Confidence:  0.95,
			Description: "Digital Object Identifier (DOI) patterns",
			Examples:    []string{"doi: 10.1234/example", "https://doi.org/10.1234/example"},
		},
		{
			Name:        "DOI Simple",
			Regex:       regexp.MustCompile(`\b(10\.\d{4,}/[^\s\]}>)]{1,100})\b`),
			Type:        LinkTypeDOI,
			Confidence:  0.9,
			Description: "Simple DOI pattern without prefix",
			Examples:    []string{"10.1234/example.dataset.2024"},
		},

		// GEO patterns (will be validated by bio domain validator)
		{
			Name:        "GEO Series",
			Regex:       regexp.MustCompile(`\b(GSE\d+)\b`),
			Type:        LinkTypeGeoID,
			Confidence:  0.9,
			Description: "Gene Expression Omnibus Series identifiers",
			Examples:    []string{"GSE123456", "GSE000001"},
		},
		{
			Name:        "GEO Sample",
			Regex:       regexp.MustCompile(`\b(GSM\d+)\b`),
			Type:        LinkTypeGeoID,
			Confidence:  0.9,
			Description: "Gene Expression Omnibus Sample identifiers",
			Examples:    []string{"GSM1234567", "GSM000001"},
		},
		{
			Name:        "GEO Platform",
			Regex:       regexp.MustCompile(`\b(GPL\d+)\b`),
			Type:        LinkTypeGeoID,
			Confidence:  0.9,
			Description: "Gene Expression Omnibus Platform identifiers",
			Examples:    []string{"GPL570", "GPL96"},
		},
		{
			Name:        "GEO Dataset",
			Regex:       regexp.MustCompile(`\b(GDS\d+)\b`),
			Type:        LinkTypeGeoID,
			Confidence:  0.9,
			Description: "Gene Expression Omnibus Dataset identifiers",
			Examples:    []string{"GDS1234", "GDS5678"},
		},

		// Repository URLs (high confidence for known data repositories)
		{
			Name:        "Zenodo URLs",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?zenodo\.org/[a-zA-Z0-9/_.-]+)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeZenodo,
			Confidence:  0.95,
			Description: "Zenodo repository URLs",
			Examples:    []string{"https://zenodo.org/record/123456", "https://www.zenodo.org/record/123456"},
		},
		{
			Name:        "Zenodo DOI",
			Regex:       regexp.MustCompile(`(https?://doi\.org/10\.5281/zenodo\.\d+)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeZenodo,
			Confidence:  0.98,
			Description: "Zenodo DOI URLs",
			Examples:    []string{"https://doi.org/10.5281/zenodo.123456"},
		},
		{
			Name:        "Figshare URLs with IDs",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?figshare\.com/[a-zA-Z0-9/_.-]+/?\s*\d+)`),
			Type:        LinkTypeFigshare,
			Confidence:  0.98,
			Description: "Figshare repository URLs with numeric IDs (may have spaces)",
			Examples:    []string{"https://figshare.com/articles/dataset/scPSM/ 19306661"},
		},
		{
			Name:        "Figshare URLs",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?figshare\.com/[a-zA-Z0-9/_.-]+)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeFigshare,
			Confidence:  0.95,
			Description: "Figshare repository URLs",
			Examples:    []string{"https://figshare.com/s/865e694ad06d5857db4b"},
		},

		// GitHub repository patterns (potential datasets)
		{
			Name:        "GitHub Repository",
			Regex:       regexp.MustCompile(`(https?://github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+(?:/[a-zA-Z0-9/_.-]*)?)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeURL,
			Confidence:  0.7,
			Description: "GitHub repository URLs (potential datasets)",
			Examples:    []string{"https://github.com/user/dataset-repo"},
		},

		// Data-specific file extensions in URLs
		{
			Name:        "Dataset Files",
			Regex:       regexp.MustCompile(`(https?://[a-zA-Z0-9._/-]+\.(?:csv|tsv|xlsx?|json|xml|h5|hdf5|parquet|feather|arrow|zip|tar\.gz|tar\.bz2))(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeURL,
			Confidence:  0.85,
			Description: "URLs pointing to dataset files",
			Examples:    []string{"https://example.com/data.csv", "https://example.com/dataset.zip"},
		},

		// Common biological databases
		{
			Name:        "NCBI URLs",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?(?:ncbi\.nlm\.nih\.gov|ebi\.ac\.uk|embl\.de|ddbj\.nig\.ac\.jp)/[a-zA-Z0-9/_.-]+)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeURL,
			Confidence:  0.9,
			Description: "Biological database URLs",
			Examples:    []string{"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123"},
		},

		// Data repository platforms
		{
			Name:        "Data Repositories",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?(?:kaggle\.com|data\.mendeley\.com|osf\.io|dataverse\.org|dryad\.org|pangaea\.de)/[a-zA-Z0-9/_.-]+)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeURL,
			Confidence:  0.88,
			Description: "Data repository platform URLs",
			Examples:    []string{"https://data.mendeley.com/datasets/abc123", "https://osf.io/xyz789/"},
		},

		// PubMed IDs
		{
			Name:        "PubMed ID",
			Regex:       regexp.MustCompile(`(?i)(?:PMID:?\s*)(\d{7,8})`),
			Type:        LinkTypeURL,
			Confidence:  0.85,
			Description: "PubMed identifiers",
			Examples:    []string{"PMID: 12345678", "PMID:12345678"},
		},

		// arXiv patterns
		{
			Name:        "arXiv ID",
			Regex:       regexp.MustCompile(`(?i)arXiv:(\d{4}\.\d{4,5}(?:v\d+)?)`),
			Type:        LinkTypeURL,
			Confidence:  0.9,
			Description: "arXiv preprint identifiers",
			Examples:    []string{"arXiv:2024.12345", "arXiv:2024.12345v1"},
		},

		// bioRxiv and medRxiv
		{
			Name:        "bioRxiv URLs",
			Regex:       regexp.MustCompile(`(https?://(?:www\.)?(?:biorxiv|medrxiv)\.org/[^\s\]}>)]{1,200})`),
			Type:        LinkTypeURL,
			Confidence:  0.9,
			Description: "bioRxiv and medRxiv preprint URLs",
			Examples:    []string{"https://www.biorxiv.org/content/10.1101/2024.01.01.123456v1"},
		},

		// FTP URLs (often used for large datasets)
		{
			Name:        "FTP URLs",
			Regex:       regexp.MustCompile(`(ftp://[^\s\]}>)]{1,300})`),
			Type:        LinkTypeURL,
			Confidence:  0.75,
			Description: "FTP URLs (potential datasets)",
			Examples:    []string{"ftp://ftp.ncbi.nlm.nih.gov/geo/series/GSE123nnn/GSE123456/"},
		},

		// Generic HTTP/HTTPS URLs (lowest priority, will be filtered by domain validators)
		{
			Name:        "Generic URLs",
			Regex:       regexp.MustCompile(`(https?://[a-zA-Z0-9.-]+(?:\.[a-zA-Z]{2,})+(?:/[^\s\]}>)]{0,300})?)(?:\s|$|[^a-zA-Z0-9._/-])`),
			Type:        LinkTypeURL,
			Confidence:  0.3,
			Description: "Generic HTTP/HTTPS URLs",
			Examples:    []string{"https://example.com/data", "http://data.example.org/dataset"},
		},

		// Additional bioinformatics patterns
		{
			Name:        "SRA Accession",
			Regex:       regexp.MustCompile(`\b(SRR\d+|ERR\d+|DRR\d+)\b`),
			Type:        LinkTypeURL,
			Confidence:  0.9,
			Description: "Sequence Read Archive accession numbers",
			Examples:    []string{"SRR123456", "ERR123456", "DRR123456"},
		},
		{
			Name:        "BioProject",
			Regex:       regexp.MustCompile(`\b(PRJNA\d+|PRJEB\d+|PRJDB\d+)\b`),
			Type:        LinkTypeURL,
			Confidence:  0.9,
			Description: "BioProject identifiers",
			Examples:    []string{"PRJNA123456", "PRJEB123456"},
		},
		{
			Name:        "PDB ID",
			Regex:       regexp.MustCompile(`\b([1-9][A-Za-z][A-Za-z0-9]{2})\b`),
			Type:        LinkTypeURL,
			Confidence:  0.7,
			Description: "Protein Data Bank identifiers (must have at least one letter)",
			Examples:    []string{"1ABC", "2XYZ"},
		},

		// Chemical databases
		{
			Name:        "ChEMBL ID",
			Regex:       regexp.MustCompile(`\b(CHEMBL\d+)\b`),
			Type:        LinkTypeURL,
			Confidence:  0.85,
			Description: "ChEMBL compound identifiers",
			Examples:    []string{"CHEMBL123456"},
		},
		{
			Name:        "PubChem CID",
			Regex:       regexp.MustCompile(`(?i)(?:CID:?\s*)(\d+)`),
			Type:        LinkTypeURL,
			Confidence:  0.85,
			Description: "PubChem Compound identifiers",
			Examples:    []string{"CID: 123456", "CID:123456"},
		},

		// UniProt
		{
			Name:        "UniProt ID",
			Regex:       regexp.MustCompile(`\b([A-NR-Z][0-9][A-Z][A-Z0-9]{2}[0-9]|[OPQ][0-9][A-Z0-9]{3}[0-9])\b`),
			Type:        LinkTypeURL,
			Confidence:  0.8,
			Description: "UniProt protein identifiers",
			Examples:    []string{"P01234", "O43657"},
		},

		// RefSeq
		{
			Name:        "RefSeq ID",
			Regex:       regexp.MustCompile(`\b(N[CGMRWT]_\d+(?:\.\d+)?)\b`),
			Type:        LinkTypeURL,
			Confidence:  0.85,
			Description: "RefSeq identifiers",
			Examples:    []string{"NC_000001.11", "NM_000014.6"},
		},
	}
}

// getDefaultSectionRegexes returns patterns for detecting document sections
func getDefaultSectionRegexes() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*(?:abstract|summary)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:introduction|background)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:methods?|methodology|materials?\s+and\s+methods?)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:results?|findings?)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:discussion|conclusions?)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:references?|bibliography|works?\s+cited)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:data\s+availability|data\s+statement|data\s+access)\s*$`),
		regexp.MustCompile(`(?i)^\s*(?:supplementary|supporting)\s+(?:materials?|information)\s*$`),
	}
}

// getDefaultCleaners returns patterns for cleaning extracted text
func getDefaultCleaners() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\s+`),                                    // Multiple whitespace
		regexp.MustCompile(`[\r\n]+`),                                // Line breaks
		regexp.MustCompile(`\x00+`),                                  // Null bytes
		regexp.MustCompile(`\s*\(\s*\)\s*`),                          // Empty parentheses
		regexp.MustCompile(`\s*\[\s*\]\s*`),                          // Empty brackets
		regexp.MustCompile(`\s*\{\s*\}\s*`),                          // Empty braces
		regexp.MustCompile(`(?i)\b(?:see|cf|compare|refer\s+to)\s+`), // Reference words that might confuse pattern matching
	}
}

// Pattern confidence levels explanation:
// 0.95-1.0: Very high confidence (specific format identifiers, known repository URLs)
// 0.85-0.94: High confidence (database-specific patterns with validation)
// 0.70-0.84: Medium confidence (likely relevant but needs validation)
// 0.50-0.69: Low confidence (generic patterns, needs careful validation)
// 0.30-0.49: Very low confidence (catch-all patterns, high false positive rate)
