# Zenodo Downloader

The Zenodo downloader provides comprehensive support for downloading datasets from the [Zenodo](https://zenodo.org) research data repository, with full metadata tracking and provenance information.

## Features

- **Multiple ID Formats**: Supports various Zenodo identifier formats
- **Smart Artifact Detection**: Automatically detects and handles different Zenodo artifact types
- **Metadata Extraction**: Retrieves comprehensive dataset information
- **Selective Downloads**: Filter files by type, size, and custom criteria
- **Progress Tracking**: Real-time download progress with speed monitoring
- **Resume Support**: Resume interrupted downloads
- **Integrity Verification**: MD5 checksum validation
- **Provenance Tracking**: Creates hapiq.json witness files

## Supported Identifier Formats

The Zenodo downloader accepts multiple identifier formats:

```bash
# Direct record ID
hapiq download zenodo 123456

# Zenodo URL
hapiq download zenodo "https://zenodo.org/record/123456"

# Zenodo DOI
hapiq download zenodo "10.5281/zenodo.123456"

# DOI URL
hapiq download zenodo "https://doi.org/10.5281/zenodo.123456"

# Versioned DOI (resolves to concept record)
hapiq download zenodo "10.5281/zenodo.123456.v1"
```

## Supported Artifact Types

The Zenodo downloader intelligently detects different types of Zenodo artifacts:

| Artifact Type | Description | Downloadable | Examples |
|---------------|-------------|--------------|----------|
| **Records** | Published, finalized datasets | ✅ Yes | `https://zenodo.org/record/123456` |
| **Concepts** | Version groups (parent of multiple versions) | ✅ Yes (latest) | `10.5281/zenodo.123456.v1` |
| **Deposits** | Unpublished drafts | ❌ No | `https://zenodo.org/deposit/123456` |
| **Communities** | Organizational spaces that curate records | ❌ No | `https://zenodo.org/communities/example` |

### Understanding Zenodo's Data Model

#### Records vs Concepts (Versioning)
When you publish research on Zenodo, two DOIs are created:
- **Version DOI** (Record): Points to a specific version (e.g., `10.5281/zenodo.123456`)
- **Concept DOI** (Concept): Points to ALL versions, resolves to latest (e.g., `10.5281/zenodo.123455`)

Example versioning structure:
```
📦 MyResearchData (Concept DOI: 10.5281/zenodo.123455)
├── 📁 v1.0 (Record DOI: 10.5281/zenodo.123456) ← First version
├── 📁 v1.1 (Record DOI: 10.5281/zenodo.123457) ← Bug fixes  
└── 📁 v2.0 (Record DOI: 10.5281/zenodo.123458) ← Latest version
```

#### Communities vs Collections
- **Communities**: Organizational spaces (like journals or institutions) where multiple researchers can submit records
- **Collections**: User-curated lists of records (not currently implemented in Zenodo)

### Artifact Type Handling

- **Records**: Direct download of all files from a specific version
- **Concepts**: Automatically resolves to and downloads the latest published version  
- **Deposits**: Returns error with helpful message (unpublished drafts can't be downloaded)
- **Communities**: Returns error suggesting to browse and download individual records from the community

## Usage Examples

### Basic Download

```bash
# Download all files from a Zenodo record
hapiq download zenodo 123456 --out ./datasets
```

### Selective Downloads

```bash
# Exclude raw data files
hapiq download zenodo 123456 --out ./data --exclude-raw

# Exclude supplementary files (README, metadata, etc.)
hapiq download zenodo 123456 --out ./data --exclude-supplementary

# Custom file filtering
hapiq download zenodo 123456 --out ./data --filter extension=.csv
hapiq download zenodo 123456 --out ./data --filter max_size=1000000
```

### Advanced Options

```bash
# Concurrent downloads with resume support
hapiq download zenodo 123456 --out ./data --parallel 4 --resume

# Skip existing files
hapiq download zenodo 123456 --out ./data --skip-existing

# Non-interactive mode
hapiq download zenodo 123456 --out ./data --yes --quiet
```

## Download Options

| Option | Description |
|--------|-------------|
| `--exclude-raw` | Skip raw data files (FASTQ, BAM, SRA, etc.) |
| `--exclude-supplementary` | Skip documentation files (README, metadata, etc.) |
| `--filter extension=.ext` | Only download files with specific extension |
| `--filter contains=text` | Only download files containing text in filename |
| `--filter excludes=text` | Skip files containing text in filename |
| `--filter max_size=N` | Skip files larger than N bytes |
| `--filter min_size=N` | Skip files smaller than N bytes |
| `--parallel N` | Download up to N files concurrently |
| `--resume` | Resume interrupted downloads |
| `--skip-existing` | Skip files that already exist |
| `--timeout N` | Set download timeout in seconds |

## File Type Detection

The downloader automatically detects and categorizes file types:

### Raw Data Files (excluded with `--exclude-raw`)
- `.fastq`, `.fq` - Sequence data
- `.bam`, `.sam` - Alignment data
- `.sra` - Sequence Read Archive
- `.cel` - Microarray data
- `.fcs` - Flow cytometry data
- `.h5` - HDF5 scientific data

### Supplementary Files (excluded with `--exclude-supplementary`)
- `readme.*` - Documentation
- `license.*` - License files
- `metadata.*` - Metadata files
- `manifest.*` - File listings
- `documentation.*` - Additional docs

## Metadata Extraction

The downloader extracts comprehensive metadata from Zenodo records:

```json
{
  "source": "zenodo",
  "id": "123456",
  "title": "Research Dataset Title",
  "description": "Dataset description...",
  "doi": "10.5281/zenodo.123456",
  "license": "cc-by-4.0",
  "version": "1.0.0",
  "authors": ["Author Name"],
  "keywords": ["keyword1", "keyword2"],
  "tags": ["tag1", "tag2"],
  "created": "2023-01-01T12:00:00Z",
  "last_modified": "2023-01-02T12:00:00Z",
  "file_count": 5,
  "total_size": 1048576,
  "custom": {
    "conceptdoi": "10.5281/zenodo.123455",
    "conceptrecid": 123455,
    "resource_type": "dataset",
    "communities": ["community-id"],
    "related_identifiers": [...],
    "artifact_type": "record",
    "is_versioned": false,
    "original_identifier": "10.5281/zenodo.123456"
  }
}
```

## Provenance Tracking

Every download creates a `hapiq.json` witness file containing:

- **Download metadata**: When, what, and how files were downloaded
- **File inventory**: Complete list of downloaded files with checksums
- **Verification info**: Integrity check results
- **Source information**: Original Zenodo record details
- **Download options**: Parameters used for the download

## API Integration

The downloader uses the [Zenodo REST API](https://developers.zenodo.org/) for metadata retrieval and the direct download links for file access. No API key is required for public records.

## Error Handling

Common error scenarios and their handling:

| Error | Cause | Solution |
|-------|-------|----------|
| "record not found" | Invalid record ID or private record | Verify the record ID and access permissions |
| "access denied" | Private or restricted record | Contact record owner or use institutional access |
| "deposits cannot be downloaded" | Trying to download unpublished draft | Wait for publication or contact author |
| "communities are organizational spaces" | Trying to download entire community | Browse the community and download individual records |
| "download failed" | Network issues or server problems | Retry with `--resume` option |
| "checksum mismatch" | File corruption during download | Re-download the affected file |
| "insufficient space" | Not enough disk space | Free up space or use filters to reduce download size |

## Performance Considerations

- **Concurrent Downloads**: Use `--parallel N` (default: 8) for faster downloads
- **Large Files**: Consider using `--resume` for very large files
- **Bandwidth**: Zenodo has no explicit rate limits but be respectful
- **Storage**: Check available disk space before downloading large datasets

## Examples with Real Records

```bash
# Download a small example dataset
hapiq download zenodo 4321059 --out ./example

# Download only CSV files from a large dataset
hapiq download zenodo 5645234 --out ./data \
  --filter extension=.csv --exclude-supplementary

# Resume a large download
hapiq download zenodo 6789012 --out ./bigdata \
  --parallel 4 --resume --timeout 600
```

## Integration with Check Command

The Zenodo downloader integrates with the check command for validation:

```bash
# Validate a Zenodo record
hapiq check "10.5281/zenodo.123456"

# Validate and download
hapiq check "https://zenodo.org/record/123456" --download

# Check different artifact types with detailed information
hapiq check "https://zenodo.org/deposit/123456"        # Shows "deposit" type with warning
hapiq check "https://zenodo.org/communities/example"   # Shows "community" type with guidance
hapiq check "10.5281/zenodo.123456.v1"               # Shows "concept" type, resolves to latest

# Understanding what you get:
# - Record DOI → Specific version
# - Concept DOI → Latest version automatically
# - Community → Browse page with multiple records to choose from
```

## Development and Testing

The downloader includes comprehensive tests with mock servers for reliable testing:

```bash
# Run tests
cd pkg/downloaders/zenodo
go test -v

# Run benchmarks
go test -bench=.
```

## Contributing

When contributing to the Zenodo downloader:

1. Follow the existing patterns from Figshare and GEO downloaders
2. Add comprehensive tests for new features
3. Update this documentation
4. Ensure compatibility with the common downloader interface
5. Test with real Zenodo records when possible

## Limitations

- Public records only (no authentication support yet)
- Deposits and communities cannot be downloaded directly (only individual published records)
- English metadata only (no internationalization)  
- Limited Zenodo communities API integration (communities serve as browsable catalogs)
- Collections feature not implemented in Zenodo (use communities instead)

## Troubleshooting

### Debug Mode

Enable verbose output for debugging:

```bash
hapiq download zenodo 123456 --out ./data --verbose
```

### Network Issues

For network-related problems:

```bash
# Increase timeout
hapiq download zenodo 123456 --out ./data --timeout 600

# Use resume for unreliable connections
hapiq download zenodo 123456 --out ./data --resume
```

### File Conflicts

Handle existing files:

```bash
# Skip existing files
hapiq download zenodo 123456 --out ./data --skip-existing

# Interactive conflict resolution
hapiq download zenodo 123456 --out ./data
```
