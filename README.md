# Hapiq

**Hapiq** downloads datasets from scientific repositories with provenance tracking.
Point it at a source and accession ID; it handles metadata, file enumeration, filtering, and download.

_"Hapiq" means "the one who fetches" in Quechua._

[![CI/CD](https://github.com/btraven00/hapiq/workflows/CI%2FCD/badge.svg)](https://github.com/btraven00/hapiq/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/btraven00/hapiq)](https://goreportcard.com/report/github.com/btraven00/hapiq)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

---

## Installation

### go install (recommended)

```bash
go install github.com/btraven00/hapiq@latest
```

Requires Go 1.24+. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`).

### Nightly binary

Pre-built binaries are published nightly for Linux, macOS, and Windows:

```bash
# Linux amd64
curl -L https://github.com/btraven00/hapiq/releases/download/nightly/hapiq-linux-amd64.tar.gz \
  | tar -xz
sudo mv hapiq-linux-amd64 /usr/local/bin/hapiq

# macOS arm64 (Apple Silicon)
curl -L https://github.com/btraven00/hapiq/releases/download/nightly/hapiq-darwin-arm64.tar.gz \
  | tar -xz
sudo mv hapiq-darwin-arm64 /usr/local/bin/hapiq
```

Windows: download `hapiq-windows-amd64.zip` from the [nightly release](https://github.com/btraven00/hapiq/releases/tag/nightly), extract, and add to `PATH`.

### From source

```bash
git clone https://github.com/btraven00/hapiq.git
cd hapiq
go build -o hapiq .
```

---

## Supported sources

| Source | IDs | Notes |
|--------|-----|-------|
| `geo` | `GSE*`, `GSM*`, `GPL*`, `GDS*` | NCBI Gene Expression Omnibus |
| `sra` | `PRJNA*`, `SRR*`, `ERR*`, `DRR*`, `SRX*` | Raw FASTQ via ENA HTTPS mirror |
| `zenodo` | DOIs (`10.5281/zenodo.*`), record IDs | |
| `figshare` | Article/collection IDs, URLs | |
| `ensembl` | `bacteria:47:pep`, `fungi:47:gff3:saccharomyces_cerevisiae` | FTP + HTTP |
| `vcp` | 24-char hex IDs (e.g. `6946b5261d32b0e84ba87057`) | CZI Virtual Cell Platform; set `VCP_TOKEN` for private datasets |
| `scperturb` | `AuthorYear` or `AuthorYear_SubsetID` (e.g. `NormanWeissman2019`) | scPerturb compendium (Peidli et al., Nature Methods 2024); files via Zenodo |
| `biostudies` | `S-<COLLECTION><digits>`, `E-<TYPE>-<digits>` (e.g. `S-BSST1502`, `E-MTAB-8077`) | EBI BioStudies; combine with `--include-ext` / `--filename-glob` to target count matrices |
| `hca` | HCA project UUID (e.g. `cc95ff89-2e68-4a08-a234-480eca21ce79`) | Human Cell Atlas via Azul; serves DCP-processed and contributor matrices (loom, h5, h5ad) |

`ncbi` is an alias for `geo`. `ena` is an alias for `sra`.

---

## Quick start

```bash
# Search GEO, inspect before downloading
hapiq search geo "ATAC-seq human liver" --limit 5
hapiq download geo GSE133344 --out ./data --dry-run

# Download
hapiq download geo GSE133344 --out ./data

# Only grab specific file types
hapiq download geo GSE133344 --out ./data --include-ext .h5ad,.csv.gz

# Download only selected samples
hapiq download geo GSE133344 --out ./data --subset GSM3912345,GSM3912346

# Search → download pipeline
hapiq search geo "bulk RNA-seq liver" -q \
  | xargs -I{} hapiq download geo {} --out ./data --dry-run

# CZI Virtual Cell Platform
hapiq search vcp "norman" --limit 10
hapiq download vcp 6946b5261d32b0e84ba87057 --out ./data --dry-run
hapiq download vcp 6946b5261d32b0e84ba87057 --out ./data --limit-files 1
```

---

## Commands

### `hapiq search`

Search for datasets using a repository's native query API.

```
hapiq search <source> <query> [flags]
```

Supported sources: `geo`, `vcp`, `scperturb`

| Flag | Default | Description |
|------|---------|-------------|
| `--limit N` | 10 | Maximum results to return |
| `--organism X` | — | Filter by organism (e.g. `"Homo sapiens"`) |
| `--type X` | — | GEO: entry type (`GSE`/`GSM`/`GPL`/`GDS`); VCP: assay filter (e.g. `"Perturb-Seq"`) |
| `-o, --output` | human | Output format: `human`, `json` |
| `-q, --quiet` | false | Print accessions only (one per line, pipe-friendly) |

**Output modes:**

- `human` — formatted table on stderr, accessions on stdout
- `json` — JSON array of result objects
- quiet (`-q`) — bare accessions only, ideal for piping

**Examples:**

```bash
hapiq search geo "ATAC-seq human liver" --limit 20
hapiq search geo "scRNA-seq pancreas" --organism "Mus musculus"
hapiq search geo "ChIP-seq H3K27ac" --type GSE --output json

hapiq search vcp "norman" --limit 10
hapiq search vcp "Perturb-Seq" --organism "Homo sapiens" --type "Perturb-Seq"

hapiq search scperturb "CRISPR" --limit 10
hapiq search scperturb "pancreas" --organism "Homo sapiens" --type "Perturb-seq"

# Pipe into download
hapiq search geo "bulk RNA-seq liver" -q \
  | head -3 \
  | xargs -I{} hapiq download geo {} --out ./data
```

---

### `hapiq download`

Download a dataset from a repository.

```
hapiq download <source> <id> --out <dir> [flags]
```

#### Required

| Flag | Description |
|------|-------------|
| `--out <dir>` | Output directory (created if it doesn't exist) |

#### File-level filters

Applied per file before anything is written to disk.

| Flag | Description |
|------|-------------|
| `--include-ext .h5ad,.csv.gz` | Only download files with these extensions (comma-separated) |
| `--exclude-ext .bam,.fastq.gz` | Skip files with these extensions |
| `--max-file-size 500MB` | Skip files larger than this (supports B, KB, MB, GB, TB) |
| `--filename-pattern '*.counts.*'` | Only download filenames matching this glob |

#### Source-specific filters

| Flag | Description |
|------|-------------|
| `--subset GSM123,GSM456` | GEO only: download only these sample accessions from a series |
| `--organism "Homo sapiens"` | Skip the dataset if its organism doesn't match (case-insensitive partial) |
| `--dry-run` | List files that would be downloaded without writing anything |

#### Download behaviour

| Flag | Default | Description |
|------|---------|-------------|
| `--exclude-raw` | false | Skip raw data files (FASTQ, BAM, SRA, CEL…) |
| `--exclude-supplementary` | false | Skip supplementary/readme/manifest files |
| `--parallel N` | 8 | Concurrent downloads |
| `--resume` | false | Resume interrupted downloads |
| `--skip-existing` | false | Skip files that already exist locally |
| `-y, --yes` | false | Non-interactive mode (auto-confirm prompts) |
| `-t, --timeout N` | 300 | Timeout in seconds |

#### Output

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output` | human | `human` or `json` |
| `-q, --quiet` | false | Suppress progress output |

**Examples:**

```bash
# Basic download
hapiq download geo GSE133344 --out ./data

# Inspect first
hapiq download geo GSE133344 --out ./data --dry-run

# Only processed files, no raw sequences
hapiq download geo GSE133344 --out ./data --exclude-raw

# Only .h5ad and .csv.gz files under 2 GB
hapiq download geo GSE133344 --out ./data \
  --include-ext .h5ad,.csv.gz \
  --max-file-size 2GB

# Only specific samples from a large series
hapiq download geo GSE133344 --out ./data \
  --subset GSM3912345,GSM3912346,GSM3912347

# Zenodo
hapiq download zenodo 10.5281/zenodo.3242074 --out ./data

# Figshare
hapiq download figshare 12345678 --out ./data --exclude-raw

# Ensembl
hapiq download ensembl bacteria:47:pep --out ./data
hapiq download ensembl fungi:47:gff3:saccharomyces_cerevisiae --out ./data

# CZI Virtual Cell Platform (VCP)
hapiq download vcp 6946b5261d32b0e84ba87057 --out ./data --dry-run
hapiq download vcp 6946b5261d32b0e84ba87057 --out ./data
hapiq download vcp 6946b5261d32b0e84ba87057 --out ./data --limit-files 1  # test first

# scPerturb (all datasets for a publication)
hapiq download scperturb NormanWeissman2019 --out ./data --dry-run
hapiq download scperturb NormanWeissman2019 --out ./data
# single dataset variant
hapiq download scperturb NormanWeissman2019_filtered --out ./data
```

Each download writes a `hapiq.json` witness file containing the full metadata, per-file checksums (SHA-256), and download statistics for reproducibility.

---

### `hapiq downloaders`

List all registered downloaders with their supported IDs and examples.

```bash
hapiq downloaders
hapiq downloaders --output json
```

---

### `hapiq species`

Browse Ensembl Genomes databases to find the right identifier for `hapiq download ensembl`.

```bash
hapiq species                         # list available databases
hapiq species bacteria 47             # list species in bacteria release 47
hapiq species fungi 47 --filter yeast # filter by name
hapiq species plants --examples       # show example download IDs
```

---

## Environment variables

| Variable | Description |
|----------|-------------|
| `NCBI_API_KEY` | NCBI API key — raises rate limit from 3 to 10 req/s for GEO. Get one at [ncbi.nlm.nih.gov/account](https://www.ncbi.nlm.nih.gov/account/). |
| `VCP_TOKEN` | JWT for the CZI Virtual Cell Platform. Required for private/restricted VCP datasets. Public datasets (e.g. Billion Cell Project) work without it. |

```bash
export NCBI_API_KEY=your_key_here
hapiq download geo GSE133344 --out ./data
```

---

## Provenance

Every `hapiq download` writes a `hapiq.json` file alongside the downloaded data:

```json
{
  "hapiq_version": "nightly-20260416-a1b2c3d",
  "download_time": "2026-04-16T02:00:00Z",
  "source": "geo",
  "original_id": "GSE133344",
  "metadata": { "title": "...", "organism": "Homo sapiens", ... },
  "files": [
    { "path": "supplementary/GSE133344_RAW.tar.gz", "size": 131072000,
      "checksum": "sha256:abc123...", "source_url": "https://ftp.ncbi..." }
  ],
  "download_stats": { "duration": "4m32s", "bytes_downloaded": 134217728 }
}
```

---

## Further reading

- [Local download cache](docs/cache.md) — enable caching to avoid re-fetching the same files

---

## License

GPL-3-or-later © 2025 btraven
