# Hapiq Architecture

## Overview

Hapiq is a CLI tool for extracting and inspecting dataset links from scientific papers. It validates URLs and DOIs, performs HTTP checks, and estimates the likelihood that a given identifier points to a valid dataset.

## Project Structure

```
hapiq/
├── cmd/                    # CLI commands (Cobra framework)
│   ├── root.go            # Root command with global flags
│   └── check.go           # Check subcommand implementation
├── internal/              # Internal packages (not for external use)
│   └── checker/           # Core checking and validation logic
│       └── checker.go     # Main checker implementation
├── pkg/                   # Public packages (reusable)
│   └── validators/        # URL and DOI validation
│       ├── validators.go  # Validation logic
│       └── validators_test.go # Comprehensive tests
├── test/                  # Test utilities and integration tests
│   ├── testdata.go       # Test data and mock servers
│   └── integration_test.go # End-to-end tests
├── .github/workflows/     # CI/CD pipelines
│   └── ci.yml            # GitHub Actions workflow
├── main.go               # Application entry point
├── go.mod                # Go module definition
├── Makefile              # Development automation
├── .gitignore            # Git ignore patterns
├── .golangci.yml         # Linting configuration
└── README.md             # Project documentation
```

## Core Components

### 1. CLI Interface (`cmd/`)

Built using the Cobra framework for robust command-line interfaces:

- **Root Command**: Defines global flags and configuration
- **Check Command**: Main functionality for validating URLs/DOIs
- **Configuration**: Support for YAML config files via Viper

### 2. Validators (`pkg/validators/`)

Public package for URL and DOI validation:

- **URL Validator**: Validates HTTP/HTTPS URLs and classifies repository types
- **DOI Validator**: Validates DOI format and extracts metadata
- **Repository Classification**: Identifies dataset repositories (Zenodo, Figshare, Dryad, etc.)
- **Scoring System**: Confidence scores for dataset likelihood

### 3. Checker (`internal/checker/`)

Core business logic for dataset checking:

- **HTTP Operations**: Performs HEAD/GET requests with timeout handling
- **Metadata Extraction**: Extracts useful information from HTTP headers
- **Likelihood Calculation**: Estimates probability of valid dataset
- **Output Formatting**: Human-readable and JSON output formats

### 4. Test Infrastructure (`test/`)

Comprehensive testing with multiple levels:

- **Unit Tests**: Individual component testing
- **Integration Tests**: End-to-end functionality testing
- **Mock Servers**: HTTP test servers for reliable testing
- **Performance Tests**: Timing and benchmark tests

## Design Principles

### 1. Functional Programming Paradigms

- **Immutable Data Structures**: Result types contain all necessary information
- **Pure Functions**: Validation functions have no side effects
- **Composability**: Small, focused functions that can be combined
- **Error Handling**: Explicit error handling with result types

### 2. Testability

- **Dependency Injection**: HTTP clients and configurations are injectable
- **Mock Interfaces**: Test doubles for external dependencies
- **Test Data**: Comprehensive test cases with edge cases
- **Performance Constraints**: Timing assertions for critical paths

### 3. Corner Cases and Robustness

- **Input Validation**: Comprehensive validation of all inputs
- **Error Recovery**: Graceful handling of network failures
- **Timeout Handling**: Configurable timeouts for HTTP operations
- **Format Support**: Multiple input formats (URLs, DOIs, various prefixes)

### 4. Complexity Analysis

- **Time Complexity**: 
  - URL/DOI validation: O(1)
  - HTTP requests: O(1) per request
  - Repository classification: O(1) lookup
- **Space Complexity**: O(1) for most operations
- **Network Complexity**: Minimized with HEAD requests and caching

## Supported Repositories

| Repository | URL Pattern | DOI Pattern | Confidence Score |
|------------|-------------|-------------|------------------|
| Zenodo | `zenodo.org/record/*` | `10.5281/zenodo.*` | 0.95 |
| Figshare | `figshare.com/articles/*` | `10.6084/m9.figshare.*` | 0.95 |
| Dryad | `datadryad.org/stash/*` | `10.5061/dryad.*` | 0.95 |
| GitHub | `github.com/*/releases/*` | N/A | 0.60 |
| OSF | `osf.io/*` | N/A | 0.80 |
| Dataverse | `dataverse.*` | Various | 0.80 |

## Error Handling Strategy

### 1. Validation Errors
- **Invalid Format**: Clear error messages for malformed inputs
- **Unsupported Schemes**: Explicit rejection of non-HTTP protocols
- **Missing Components**: Detailed feedback on incomplete URLs/DOIs

### 2. Network Errors
- **Timeout Handling**: Configurable timeouts with sensible defaults
- **Connection Failures**: Graceful degradation with error reporting
- **HTTP Errors**: Status code interpretation and user feedback

### 3. Runtime Errors
- **Panic Recovery**: No panics in normal operation
- **Resource Cleanup**: Proper cleanup of HTTP connections
- **Logging**: Verbose mode for debugging

## Performance Characteristics

### Validation Performance
- URL validation: < 100µs
- DOI validation: < 100µs
- Repository classification: < 10µs

### Network Performance
- HTTP timeout: 30s (configurable)
- Response time tracking: Included in output
- Concurrent requests: Single-threaded (future enhancement)

## Future Architecture Considerations

### Phase 2: File Analysis
- **Download Manager**: Parallel download capabilities
- **Archive Extraction**: Support for ZIP, TAR, etc.
- **File Type Detection**: MIME type and extension analysis
- **Structure Analysis**: Directory tree inspection

### Phase 3: Content Analysis
- **Text Extraction**: PDF and HTML parsing
- **Entity Recognition**: NLP for dataset identification
- **Machine Learning**: Classification models
- **Database Integration**: Persistent storage

### Phase 4: Scalability
- **API Server**: HTTP API for programmatic access
- **Batch Processing**: Multiple URL processing
- **Caching Layer**: Redis/Memcached for performance
- **Horizontal Scaling**: Microservices architecture

## Development Workflow

### Code Quality
- **golangci-lint**: Comprehensive linting rules
- **gofmt**: Standard Go formatting
- **Testing**: >80% code coverage target
- **Documentation**: Godoc for all public APIs

### CI/CD Pipeline
- **GitHub Actions**: Automated testing and building
- **Multi-platform**: Linux, macOS, Windows builds
- **Security Scanning**: Gosec and dependency checking
- **Release Automation**: Tagged releases with binaries

### Development Tools
- **Makefile**: Common development tasks
- **Docker**: Containerization support
- **Debugging**: Verbose mode and structured logging
- **Profiling**: Performance analysis tools

## Security Considerations

### Input Validation
- **URL Sanitization**: Prevent injection attacks
- **DOI Validation**: Strict format checking
- **Timeout Limits**: Prevent DoS via slow responses

### Network Security
- **HTTPS Preference**: Prefer secure connections
- **Certificate Validation**: Standard TLS verification
- **Rate Limiting**: Respect server rate limits

### Data Privacy
- **No Data Storage**: No persistent data collection
- **Minimal Logging**: Only essential information logged
- **Configuration Security**: Secure handling of API keys