# Hapiq Examples

This directory contains example programs demonstrating various features of the Hapiq link extraction and validation system.

## Examples

### `http_validation_demo.go`

Demonstrates the HTTP validation functionality with browser spoofing and smart request handling.

**Features shown:**
- Browser user-agent spoofing to avoid bot detection
- HEAD-first requests with GET fallback to minimize bandwidth usage
- Range requests to avoid downloading large files
- Redirect following (up to 10 redirects)
- Dataset likelihood scoring based on content type and URL patterns
- Concurrent batch validation for efficiency
- Comprehensive error handling for network issues
- Response time tracking for performance analysis
- Content type analysis and categorization

**Run the demo:**
```bash
cd examples
go run http_validation_demo.go
```

**Sample output:**
```
🔗 HTTP Validation Demo
========================
[1/8] Testing: https://httpbin.org/json
   ✅ Accessible (HTTP 200) via HEAD [905ms]
   📄 Content-Type: application/json
   📏 Size: 429 B
   📊 Dataset likelihood: 70.0% ✅
   🏷️  Category: structured_data
```

## Usage with Hapiq CLI

The examples demonstrate functionality that's integrated into the main Hapiq CLI:

```bash
# Convert PDF to Markdown text with advanced tokenization
./hapiq convert paper.pdf

# Convert PDF with output to file and page annotations
./hapiq convert --output paper.md --pages paper.pdf

# Convert preserving original layout
./hapiq convert --preserve-layout --headers paper.pdf

# Check individual DOI/URL
./hapiq check https://zenodo.org/record/123456

# Check with download attempt
./hapiq check --download 10.5281/zenodo.123456

# Batch processing from file (with automatic cleanup)
./hapiq check -i links.txt

# Extract links from PDF with HTTP validation
./hapiq extract --validate-links paper.pdf

# Extract with domain filtering and validation
./hapiq extract --validate-links --filter-domains zenodo.org,figshare.com paper.pdf

# Batch processing with validation
./hapiq extract --batch --validate-links --format csv *.pdf
```

## PDF Text Conversion Features

The `convert` command includes sophisticated word segmentation for handling concatenated text commonly found in PDF extraction:

**Input**: `SupplementaryinformationTheonlineversioncontainssupplementarymaterial`
**Output**: `Supplementary information The online version contains supplementary material`

Key improvements:
- **Dictionary-based segmentation**: Uses dynamic programming with academic/scientific vocabulary
- **Case preservation**: Maintains original capitalization patterns
- **URL handling**: Properly separates URLs and DOIs from surrounding text
- **Pattern recognition**: Handles CamelCase, numbers, and punctuation boundaries
- **Academic terminology**: Recognizes scientific terms and compound words

## Batch DOI/URL Processing

The `check` command supports batch processing with the `-i` flag for validating multiple DOIs/URLs from a file:

**Input file format** (one entry per line):
```
10.5281/zenodo.123456
https://doi.org/10.1038/s41467-021-23778-6 (Nature Communications, 2021)
10.5061/dryad.abc123, Smith et al. (2023)
https://figshare.com/articles/dataset/Example/12345678 [Data file]
# Comments and empty lines are ignored
```

**Automatic cleanup** removes:
- Text in parentheses: `(accessed 2023)` → removed
- Text in brackets: `[supplementary data]` → removed  
- Text after commas/semicolons: `, Smith et al.` → removed
- Trailing punctuation: `...` → removed
- Extra whitespace

**Usage**:
```bash
# Process all links in file
hapiq check -i links.txt

# With download attempts and quiet output
hapiq check -i links.txt --download --quiet
```

## Requirements

- Go 1.21 or later
- Internet connection for HTTP validation demos
- The examples use the internal extractor package, so they must be run from the examples directory

## Development

These examples are useful for:
- Testing HTTP validation functionality
- Understanding the link extraction workflow
- Debugging network issues
- Performance testing
- Demonstrating features to users

Feel free to modify the examples to test specific scenarios or add new examples for other features.