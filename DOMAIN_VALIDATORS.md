# Domain Validators

Domain validators in Hapiq provide specialized validation for scientific identifiers and URLs from specific research domains. This extensible system allows for precise identification and validation of datasets from various scientific databases and repositories.

## Overview

The domain validator system extends Hapiq's basic URL/DOI validation with domain-specific knowledge about scientific databases. Each validator can:

- Recognize domain-specific identifier patterns
- Extract metadata from identifiers
- Generate relevant URLs for data access
- Provide confidence scores based on domain knowledge
- Classify data types within the domain

## Architecture

### Core Interface

All domain validators implement the `DomainValidator` interface:

```go
type DomainValidator interface {
    Name() string                                    // Unique validator name
    Domain() string                                  // Scientific domain
    Description() string                             // Human-readable description
    CanValidate(input string) bool                   // Quick validation check
    Validate(ctx context.Context, input string) (*DomainValidationResult, error)
    GetPatterns() []Pattern                          // Recognition patterns
    Priority() int                                   // Validation priority
}
```

### Registry System

The `ValidatorRegistry` manages domain validators:

- **Registration**: Automatic registration via `init()` functions
- **Discovery**: Find validators by domain or input pattern
- **Priority**: Higher priority validators are tried first
- **Conflict Resolution**: Multiple validators can handle the same input

## Available Domains

### Bioinformatics

The bioinformatics domain provides validators for biological databases and genomics repositories.

#### GEO Validator (Gene Expression Omnibus)

**Patterns Supported:**
- `GSE\d+` - GEO Series (experiments/studies)
- `GSM\d+` - GEO Samples (individual samples)
- `GPL\d+` - GEO Platforms (array/sequencing platforms)
- `GDS\d+` - GEO Datasets (curated datasets)
- `GSC\d+` - GEO SuperSeries Collections
- `GCF_\d+\.\d+` - GenBank Complete genome Format
- `GCA_\d+\.\d+` - GenBank Complete genome Assembly

**URL Patterns:**
- `https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=*`
- `https://www.ncbi.nlm.nih.gov/geo/browse/?view=*`

**Examples:**
```bash
# Direct GEO ID validation
hapiq check GSE185917

# GEO URL validation
hapiq check "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917"

# Platform validation
hapiq check GPL570

# Sample validation
hapiq check GSM1234567
```

**Metadata Extracted:**
- GEO type classification (Series, Sample, Platform, Dataset)
- Data domain (gene expression, genomics)
- Provider information (NCBI)
- HTTP accessibility status
- Generated alternate URLs for data access

#### SRA Validator (Sequence Read Archive)

**Patterns Supported:**
- `SRR\d{6,}` - SRA Run identifiers (NCBI)
- `ERR\d{6,}` - ENA Run identifiers (EBI)
- `DRR\d{6,}` - DDBJ Run identifiers
- `SRX\d{6,}` - SRA Experiment identifiers (NCBI)
- `ERX\d{6,}` - ENA Experiment identifiers (EBI)
- `DRX\d{6,}` - DDBJ Experiment identifiers
- `SRS\d{6,}` - SRA Sample identifiers (NCBI)
- `ERS\d{6,}` - ENA Sample identifiers (EBI)
- `DRS\d{6,}` - DDBJ Sample identifiers
- `SRP\d{6,}` - SRA Study identifiers (NCBI)
- `ERP\d{6,}` - ENA Study identifiers (EBI)
- `DRP\d{6,}` - DDBJ Study identifiers
- `PRJNA\d+` - BioProject identifiers (NCBI)
- `PRJEB\d+` - BioProject identifiers (EBI)
- `PRJDB\d+` - BioProject identifiers (DDBJ)

**URL Patterns:**
- `https://www.ncbi.nlm.nih.gov/sra/*`
- `https://www.ebi.ac.uk/ena/browser/view/*`
- `https://trace.ncbi.nlm.nih.gov/Traces/sra/*`
- `https://ddbj.nig.ac.jp/resource/sra-*`

**Examples:**
```bash
# SRA Run validation
hapiq check SRR123456

# ENA Run validation
hapiq check ERR123456

# BioProject validation
hapiq check PRJNA123456

# SRA URL validation
hapiq check "https://www.ncbi.nlm.nih.gov/sra/SRR123456"
```

**Metadata Extracted:**
- SRA accession type (Run, Experiment, Sample, Study, Project)
- Database provider (NCBI/SRA, EBI/ENA, DDBJ)
- Regional database URLs
- Hierarchical relationships between accessions
- Data download capabilities
- HTTP accessibility status

#### GSA Validator (Genome Sequence Archive - China)

**Patterns Supported:**
- `CRR\d{6,}` - GSA Run identifiers
- `CRX\d{6,}` - GSA Experiment identifiers
- `CRA\d{6,}` - GSA Study identifiers
- `PRJCA\d+` - GSA Project identifiers
- `SAMC\d+` - GSA BioSample identifiers

**URL Patterns:**
- `https://ngdc.cncb.ac.cn/gsa/*`
- `https://bigd.big.ac.cn/gsa/*`

**Examples:**
```bash
# GSA Run validation
hapiq check CRR123456

# GSA Project validation
hapiq check PRJCA123456
```

**Metadata Extracted:**
- GSA accession type classification
- Chinese database provider information
- Regional access URLs
- Data availability status

## Usage Examples

### Basic Validation

```bash
# Check a GEO series ID
hapiq check GSE185917

# Check an SRA run ID
hapiq check SRR123456

# Check a BioProject ID
hapiq check PRJNA123456
```

Example output for SRA validation:
```
✅ Status: Valid (HTTP 200)
📂 Dataset Type: sequence_data
🔬 Domain Analysis:
   ✅ sra (bioinformatics): confidence=1.00, likelihood=1.00
      Type: sequence_data (sra_run)
      Tags: sra, sequencing, run, raw_data, downloadable_data
```

Example output for GEO validation:
```
✅ Status: Valid (HTTP 200)
📂 Dataset Type: expression_data
🔬 Domain Analysis:
   ✅ geo (bioinformatics): confidence=1.00, likelihood=0.50
      Type: expression_data (series)
      Tags: experiment, series, study, ncbi, geo, gene_expression
```

### Verbose Mode

```bash
# Get detailed domain validation information (verbose is default)
hapiq check GSE185917

# Quiet SRA validation (suppress verbose messages)
hapiq check SRR123456 --quiet
```

### JSON Output

```bash
# Get machine-readable output with domain results
hapiq check SRR123456 --output json
```

Example JSON output for SRA validation:
```json
{
  "target": "SRR123456",
  "valid": true,
  "dataset_type": "sequence_data",
  "likelihood_score": 1.0,
  "domain_results": [
    {
      "valid": true,
      "validator_name": "sra",
      "domain": "bioinformatics",
      "normalized_id": "SRR123456",
      "primary_url": "https://www.ncbi.nlm.nih.gov/sra/SRR123456",
      "alternate_urls": [
        "https://www.ebi.ac.uk/ena/browser/view/SRR123456",
        "https://trace.ncbi.nlm.nih.gov/Traces/sra/?run=SRR123456"
      ],
      "dataset_type": "sequence_data",
      "subtype": "sra_run",
      "confidence": 1.0,
      "likelihood": 1.0,
      "metadata": {
        "accession_type": "sra_run",
        "database": "sra",
        "database_full_name": "Sequence Read Archive",
        "hierarchical_level": "4 of 4",
        "hierarchy": "bioproject > sra_study > sra_experiment > sra_run"
      },
      "tags": ["sra", "sequencing", "run", "raw_data", "downloadable_data"]
    }
  ]
}
```

Example JSON output for GEO validation:
```json
{
  "target": "GSE185917",
  "valid": true,
  "dataset_type": "expression_data",
  "likelihood_score": 0.50,
  "domain_results": [
    {
      "valid": true,
      "validator_name": "geo",
      "domain": "bioinformatics",
      "normalized_id": "GSE185917",
      "primary_url": "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
      "dataset_type": "expression_data",
      "subtype": "series",
      "confidence": 1.0,
      "likelihood": 0.50,
      "metadata": {
        "geo_type": "Series",
        "database": "GEO",
        "provider": "NCBI",
        "description": "Gene expression experiment or study"
      },
      "tags": ["experiment", "series", "study", "ncbi", "geo", "gene_expression"]
    }
  ]
}
```

### Exploring Available Validators

```bash
# List all domain validators
hapiq domains

# Show recognition patterns
hapiq domains --patterns

# Focus on a specific domain
hapiq domains --domain bioinformatics

# Get JSON output
hapiq domains --output json
```

## Likelihood Calculation

Domain validators use sophisticated likelihood scoring:

### Base Factors
- **Valid Identification** (40%): Correct pattern matching
- **HTTP Accessibility** (30%): Successful URL access
- **Content Analysis** (20%): Response content type and headers
- **Domain Knowledge** (10%): Database-specific factors

### GEO-Specific Scoring
- **Series/Datasets**: Higher scores (primary data objects)
- **Platforms**: Medium scores (metadata/infrastructure)
- **Samples**: Variable based on accessibility
- **HTTP Success**: +15% confidence boost
- **Content Type Bonuses**: JSON/XML (+15%), HTML (+10%)

## Integration with Generic Validation

Domain validators integrate seamlessly with Hapiq's existing validation:

1. **Domain-First**: Domain validators are tried before generic validation
2. **Fallback**: Generic validation provides backup for unrecognized patterns
3. **Enhancement**: Domain results enhance generic validation with metadata
4. **Priority**: Higher-priority validators are attempted first

## Development Guide

### Adding New Validators

1. **Create Validator Struct**:
```go
type MyValidator struct {
    *bio.BioDomainValidator
    // Custom fields
}
```

2. **Implement Interface**:
```go
func (v *MyValidator) CanValidate(input string) bool {
    // Pattern matching logic
}

func (v *MyValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
    // Validation logic
}
```

3. **Register Validator**:
```go
func init() {
    validator := NewMyValidator()
    domains.Register(validator)
}
```

### Best Practices

- **Pattern Specificity**: Make patterns as specific as possible to avoid conflicts
- **HTTP Efficiency**: Use HEAD requests when possible
- **Error Handling**: Graceful degradation for network failures
- **Metadata**: Extract rich metadata for enhanced user experience
- **Testing**: Comprehensive tests including edge cases and performance

### Testing Domain Validators

```go
func TestMyValidator_CanValidate(t *testing.T) {
    validator := NewMyValidator()
    
    tests := []struct {
        input    string
        expected bool
    }{
        {"valid-pattern", true},
        {"invalid-pattern", false},
    }
    
    for _, tt := range tests {
        result := validator.CanValidate(tt.input)
        assert.Equal(t, tt.expected, result)
    }
}
```

## Future Domains

The domain validator system is designed for easy extension:

### Planned Domains

- **Chemistry**: PubChem, ChEMBL, Chemical Abstracts
- **Physics**: arXiv, INSPIRE-HEP, particle data
- **Astronomy**: NED, SIMBAD, Vizier catalogs
- **Materials Science**: Materials Project, NOMAD
- **Climate**: Climate Data Gateway, ECMWF
- **Social Sciences**: ICPSR, UK Data Service

### Domain Templates

Each new domain should provide:
- Base validator class with common functionality
- Specific validators for major databases
- Comprehensive test coverage
- Documentation and examples
- Performance benchmarks

## Performance Characteristics

### Validation Speed
- **Pattern Matching**: < 1ms
- **HTTP Validation**: 100-2000ms (network dependent)
- **Metadata Extraction**: < 10ms
- **Invalid Inputs**: < 1ms (fast rejection)

### Memory Usage
- **Registry Size**: O(number of validators)
- **Per Validation**: O(1) additional memory
- **Caching**: Future enhancement for repeated validations

## Configuration

Domain validators can be configured via:

1. **Environment Variables**:
```bash
export HAPIQ_DOMAIN_TIMEOUT=30s
export HAPIQ_DOMAIN_RETRY=3
```

2. **Config File** (`.hapiq.yaml`):
```yaml
domains:
  timeout: 30s
  retry_count: 3
  bio:
    geo:
      priority: 90
      timeout: 10s
```

3. **Command Line Flags**:
```bash
hapiq check GSE123 --timeout 60
```

## Research Applications

Domain validators enable advanced research workflows:

### Dataset Discovery
- Identify datasets mentioned in papers
- Extract structured metadata
- Generate download URLs
- Classify data types

### Literature Mining
- Parse DOIs and URLs from PDFs
- Validate dataset accessibility
- Track dataset citations
- Build dataset networks

### Reproducibility Checks
- Verify dataset availability
- Check data freshness
- Validate download links
- Monitor dataset health

This extensible system provides a foundation for domain-specific dataset validation that grows with the scientific community's needs.