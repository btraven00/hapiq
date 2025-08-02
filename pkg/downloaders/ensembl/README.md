# Ensembl Genomes Downloader

This package provides a downloader implementation for Ensembl Genomes databases, supporting automated download of genomic data from bacteria, fungi, metazoa, plants, and protists.

## Overview

The Ensembl downloader enables batch downloading of genomic datasets from the Ensembl Genomes FTP servers. It supports multiple content types including protein sequences, coding sequences, genome annotations, and genomic DNA sequences.

## Supported Databases

- **bacteria** - Bacterial genomes (largest collection, ~50,000 species)
- **fungi** - Fungal genomes (~1,000 species)
- **metazoa** - Invertebrate animal genomes (~500 species)
- **plants** - Plant genomes (~100 species)
- **protists** - Protist genomes (~200 species)

## Content Types

- **pep** - Protein/peptide sequences (amino acid sequences)
- **cds** - Coding DNA sequences (nucleotide sequences of genes)
- **gff3** - Genome annotations in GFF3 format
- **dna** - Genomic DNA sequences (chromosomes/scaffolds)

## ID Format

Ensembl IDs follow the format: `database:version:content[:species]`

- `database` - One of: bacteria, fungi, metazoa, plants, protists
- `version` - Ensembl release version (e.g., 47, 50)
- `content` - Content type: pep, cds, gff3, dna
- `species` - Optional species filter (e.g., escherichia_coli)

## Examples

### Basic Usage

```bash
# Download all bacterial protein sequences from release 47
hapiq download ensembl bacteria:47:pep --out ./data

# Download fungal GFF3 annotations
hapiq download ensembl fungi:47:gff3 --out ./annotations

# Download plant genomic DNA sequences
hapiq download ensembl plants:50:dna --out ./genomes
```

### Species-Specific Downloads

```bash
# Download only E. coli protein sequences
hapiq download ensembl bacteria:47:pep:escherichia_coli --out ./ecoli

# Download Saccharomyces cerevisiae annotations
hapiq download ensembl fungi:47:gff3:saccharomyces_cerevisiae --out ./yeast

# Download Arabidopsis genomic DNA
hapiq download ensembl plants:50:dna:arabidopsis_thaliana --out ./arabidopsis
```

### Advanced Options

```bash
# Parallel downloads with custom settings
hapiq download ensembl bacteria:47:pep --out ./data --parallel 4 --timeout 600

# Skip existing files and exclude raw data
hapiq download ensembl fungi:47:cds --out ./data --skip-existing --exclude-raw

# Non-interactive mode for automation
hapiq download ensembl metazoa:47:pep --out ./data --yes --quiet
```

## Implementation Details

### Architecture

The Ensembl downloader follows the standard hapiq downloader interface:

- **Validation** - Validates ID format and checks database/version existence
- **Metadata** - Retrieves comprehensive dataset information with size estimates
- **Download** - Performs parallel downloads with progress tracking and provenance

### Download Process

1. **Species List Retrieval** - Downloads the species list for the specified database/version
2. **URL Generation** - Constructs FTP URLs for each species based on content type
3. **Parallel Download** - Downloads files concurrently with configurable limits
4. **Progress Tracking** - Reports download progress and statistics
5. **Provenance** - Creates hapiq.json witness files with full metadata

### File Organization

Downloaded files are organized in directories following the pattern:
```
output_dir/
└── ensembl_{database}_release-{version}_{content}/
    ├── hapiq.json                     # Provenance metadata
    ├── species_Ensembl{Database}.txt  # Species list
    ├── Species1.Assembly1.pep.all.fa.gz
    ├── Species2.Assembly2.pep.all.fa.gz
    └── ...
```

### URL Construction

Files are downloaded from Ensembl FTP servers using URLs like:
```
ftp://ftp.ensemblgenomes.org/pub/{database}/release-{version}/fasta/{collection}/{content}/{Species}.{Assembly}.{extension}
```

Special handling is implemented for:
- Different assembly naming conventions
- Collection hierarchies (some species are grouped)
- Content-specific file extensions
- Fungi-specific assembly logic

## Configuration Options

### Timeout Settings

```go
downloader := ensembl.NewEnsemblDownloader(
    ensembl.WithTimeout(10 * time.Minute),
)
```

### Verbose Output

```go
downloader := ensembl.NewEnsemblDownloader(
    ensembl.WithVerbose(true),
)
```

### Custom HTTP Client

```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns: 10,
    },
}

downloader := ensembl.NewEnsemblDownloader(
    ensembl.WithHTTPClient(client),
)
```

## Size Estimates

The downloader provides size estimates based on database and content type:

| Database | Peptides | CDS | GFF3 | DNA |
|----------|----------|-----|------|-----|
| Bacteria | 5MB | 8MB | 10MB | 50MB |
| Fungi | 20MB | 30MB | 40MB | 200MB |
| Metazoa | 50MB | 80MB | 100MB | 1GB |
| Plants | 30MB | 50MB | 60MB | 500MB |
| Protists | 15MB | 25MB | 30MB | 100MB |

*Note: These are per-species estimates. Total download size depends on species count.*

## Error Handling

The downloader handles various error conditions:

- **Invalid ID format** - Detailed validation error messages
- **Missing database/version** - FTP server validation
- **Network errors** - Retry logic and timeout handling
- **File conflicts** - User confirmation for existing directories
- **Partial downloads** - Resume capability and skip-existing options

## Limitations

### Current Limitations

- **FTP Protocol** - Requires FTP support in the HTTP client
- **Species Parsing** - Complex assembly naming requires careful handling
- **Large Downloads** - Bacteria database can be very large (50,000+ species)
- **Rate Limiting** - No built-in rate limiting for FTP servers

### Future Improvements

- **HTTP Mirrors** - Support for HTTP-based Ensembl mirrors
- **Incremental Updates** - Download only changed files between releases
- **Taxonomic Filtering** - Filter by taxonomic groups (e.g., all Enterobacteriaceae)
- **Format Conversion** - Optional decompression and format conversion
- **Checksum Verification** - Validate file integrity using Ensembl checksums

## Testing

Run the test suite:

```bash
go test ./pkg/downloaders/ensembl/...
```

Integration tests (require network access):

```bash
go test ./pkg/downloaders/ensembl/... -tags=integration
```

Benchmark tests:

```bash
go test ./pkg/downloaders/ensembl/... -bench=.
```

## Related Documentation

- [Ensembl Genomes Documentation](https://ensemblgenomes.org/)
- [Ensembl FTP Structure](https://ensemblgenomes.org/info/data/ftp/index.html)
- [Hapiq Downloader Interface](../interface.go)
- [Common Utilities](../common/README.md)

## License

This implementation follows the same license as the hapiq project. Ensembl data is available under the Apache License 2.0.