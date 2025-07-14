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
# Extract links from PDF with HTTP validation
./hapiq extract --validate-links paper.pdf

# Extract with domain filtering and validation
./hapiq extract --validate-links --filter-domains zenodo.org,figshare.com paper.pdf

# Batch processing with validation
./hapiq extract --batch --validate-links --format csv *.pdf
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