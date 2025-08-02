# Hapiq

**Hapiq** is a CLI tool for extracting and inspecting dataset links from
scientific papers.

To extract and check links, it verifies and analyzes data sources to estimate
the likelihood of a valid dataset.

Hapiq can also be used to directly download datasets into local folders.

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

Check with quiet output (suppress verbose messages):
```bash
hapiq check https://figshare.com/articles/dataset/example/123456 --quiet
```

Output as JSON:
```bash
hapiq check "10.5061/dryad.example" --output json
```

Get only the URL if found with confidence:
```bash
hapiq check --get-url https://zenodo.org/record/1234567
hapiq check --get-url 10.5281/zenodo.1234567
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

## Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

## License

GPL-3-or-later © 2025 btraven

This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

## References

* [iSeq](https://github.com/BioOmics/iSeq)
