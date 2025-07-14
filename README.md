# Hapiq

**Hapiq** is a CLI tool for extracting and inspecting dataset links from
scientific papers. It verifies, downloads, and analyzes data sources to
estimate the likelihood of a valid dataset.

_"Hapiq" means "the one who fetches" in Quechua._

[![CI/CD](https://github.com/btraven00/hapiq/workflows/CI%2FCD/badge.svg)](https://github.com/btraven00/hapiq/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/btraven00/hapiq)](https://goreportcard.com/report/github.com/btraven00/hapiq)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

---

## Features

- ✅ Validate URLs and identifiers (e.g. Zenodo, Figshare, Dryad)
- 🔍 Support for DOI resolution and repository classification
- 📊 Estimate likelihood of dataset validity
- 🌐 HTTP status and metadata inspection
- 📝 JSON or human-readable output formats
- ⚡ Fast validation with comprehensive error handling
- 🧪 Extensible architecture for future enhancements

---

## Installation

### From Source

```bash
git clone https://github.com/btraven00/hapiq.git
cd hapiq
make install
```

### Using Go Install

```bash
go install github.com/btraven00/hapiq@latest
```

### Download Binary

Download pre-built binaries from the [releases page](https://github.com/btraven00/hapiq/releases).

## Usage

### Basic Usage

```bash
hapiq check <url-or-identifier>
```

### Examples

Check a Zenodo record:
```bash
hapiq check https://zenodo.org/record/1234567
```

Check using DOI:
```bash
hapiq check 10.5281/zenodo.1234567
```

Check with verbose output:
```bash
hapiq check https://figshare.com/articles/dataset/example/123456 --verbose
```

Output as JSON:
```bash
hapiq check "10.5061/dryad.example" --output json
```

With download attempt:
```bash
hapiq check https://zenodo.org/record/123456 --download --timeout 60
```

### Supported Repositories

- **Zenodo** - `zenodo.org`
- **Figshare** - `figshare.com`
- **Dryad** - `datadryad.org`
- **OSF** - `osf.io`
- **GitHub** - `github.com` (releases)
- **Dataverse** - Various Dataverse instances
- **DOI Resolution** - `doi.org`

## Output Format

### Human-readable Output
```
Target: https://zenodo.org/record/1234567
✅ Status: Valid (HTTP 200)
📂 Dataset Type: zenodo_record
🔗 Content Type: text/html
📏 Size: 15234 bytes
⏱️  Response Time: 245ms
🧠 Dataset Likelihood: 0.95
```

### JSON Output
```json
{
  "target": "https://zenodo.org/record/1234567",
  "valid": true,
  "http_status": 200,
  "content_type": "text/html",
  "content_length": 15234,
  "response_time": "245ms",
  "dataset_type": "zenodo_record",
  "likelihood_score": 0.95,
  "metadata": {
    "server": "nginx/1.18.0",
    "last-modified": "Wed, 15 Mar 2023 10:30:00 GMT"
  }
}
```

## Development

### Prerequisites

- Go 1.21 or later
- Make (optional, for convenience)

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Code Quality

```bash
make check  # Run all checks (fmt, vet, lint, test)
```

### Available Make Targets

```bash
make help   # Show all available targets
```

Common targets:
- `build` - Build the binary
- `test` - Run tests
- `coverage` - Generate test coverage report
- `lint` - Run golangci-lint
- `clean` - Clean build artifacts
- `install` - Install to GOPATH/bin

## Architecture

```
hapiq/
├── cmd/           # CLI commands (Cobra)
├── internal/      # Internal packages
│   └── checker/   # Core checking logic
├── pkg/           # Public packages
│   └── validators/ # URL and DOI validation
└── test/          # Test utilities and data
```

### Key Components

- **Validators** (`pkg/validators`) - URL and DOI validation with repository classification
- **Checker** (`internal/checker`) - Main checking logic and HTTP operations
- **CLI** (`cmd/`) - Command-line interface using Cobra

## Roadmap

### Phase 1 (Current)
- [x] URL/DOI validation
- [x] HTTP status checking
- [x] Repository classification
- [x] Likelihood scoring

### Phase 2
- [ ] Archive and file extraction
- [ ] File structure analysis
- [ ] Content type detection
- [ ] Download size estimation

### Phase 3
- [ ] PDF/HTML paper parsing
- [ ] Named entity extraction (methods, metrics, datasets)
- [ ] LangChain integration
- [ ] Machine learning-based classification

### Phase 4
- [ ] Web UI (optional)
- [ ] API server mode
- [ ] Database integration
- [ ] Batch processing

## Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

### Development Workflow

1. Fork the repository
2. Create a feature branch
3. Make changes with tests
4. Run `make check` to ensure code quality
5. Submit a pull request

### Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports`
- Write tests for new functionality
- Document public APIs

## License

GPL-3-or-later © 2025 btraven

This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

## References

* [iSeq](https://github.com/BioOmics/iSeq)
