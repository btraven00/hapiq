# hapiq — Python library

Python bindings for [hapiq](https://github.com/btraven00/hapiq), a CLI tool for downloading scientific datasets from GEO, SRA, Zenodo, Figshare, Ensembl, scPerturb, BioStudies, HCA, and VCP with full provenance tracking.

The library implements the **HDL (Has Data Locally)** pattern: a `Dataset` object holds your download parameters but only fetches data when you explicitly call `fetch()`. Repeated calls with `skip_existing=True` (the default) are instant if the data is already on disk.

## Installation

The wheel ships a pre-compiled `hapiq` binary — no Go toolchain required.

```bash
pip install hapiq   # TODO: publish to PyPI
```

For development, install from the repo:

```bash
cd python/
pip install -e .
```

## Usage

### Download a dataset

```python
from hapiq import Dataset

ds = Dataset("geo", "GSE133344", include_ext=[".h5ad", ".csv.gz"])

# Inspect metadata without downloading
print(ds.metadata.title)
print(f"{ds.metadata.file_count} files, {ds.metadata.total_size} bytes")

# Download (skips files already present by default)
paths = ds.fetch("/data/geo/GSE133344")
# → [PosixPath('/data/geo/GSE133344/sample.h5ad'), ...]
```

### Search for datasets

```python
from hapiq import search

results = search("geo", "single cell RNA-seq lung cancer", organism="Homo sapiens")
for r in results:
    print(r.accession, r.title)
```

### Full download result with provenance

```python
result = ds.fetch_result("/data/geo/GSE133344")
print(result.witness_file)      # path to hapiq.json provenance file
print(result.bytes_downloaded)  # bytes transferred this run
for f in result.files:
    print(f.path, f.checksum)   # SHA-256 per file
```

## Supported sources

| Source | ID format | Search |
|--------|-----------|--------|
| `geo` | `GSE123456` | ✓ |
| `sra` | `SRR123456`, `SRP123456` | |
| `zenodo` | `10.5281/zenodo.1234567` | |
| `figshare` | DOI or article ID | |
| `ensembl` | species + release | |
| `scperturb` | dataset name | ✓ |
| `biostudies` | `S-BSST123` | |
| `hca` | project UUID | |
| `vcp` | dataset ID | ✓ |

## API reference

### `Dataset(source, id, **filters)`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `source` | `str` | — | Source name (e.g. `"geo"`) |
| `id` | `str` | — | Dataset accession |
| `include_ext` | `list[str]` | `None` | Only download files with these extensions |
| `exclude_ext` | `list[str]` | `None` | Skip files with these extensions |
| `max_file_size` | `str` | `None` | Skip files larger than this (e.g. `"500MB"`) |
| `filename_pattern` | `str` | `None` | Glob pattern for filenames |
| `organism` | `str` | `None` | Filter by organism (case-insensitive) |
| `subset` | `list[str]` | `None` | Download only these sub-accessions |
| `exclude_raw` | `bool` | `False` | Skip raw data files (FASTQ, BAM) |
| `exclude_supplementary` | `bool` | `False` | Skip supplementary files |
| `parallel` | `int` | `8` | Concurrent downloads |
| `timeout` | `int` | `300` | Per-request timeout in seconds |
| `limit_files` | `int` | `0` | Stop after N files (0 = no limit) |

#### `ds.metadata` → `Metadata`
Cached property. Fetches dataset info (title, file count, organisms, etc.) without downloading any files.

#### `ds.fetch(out_dir, *, resume=False, skip_existing=True, dry_run=False)` → `list[Path]`
Download files, return their local paths. With `skip_existing=True` (default), files already present are not re-downloaded.

#### `ds.fetch_result(out_dir, *, resume=False, skip_existing=True)` → `DownloadResult`
Like `fetch()` but returns the full result including checksum info, download stats, and the path to the `hapiq.json` witness file.

### `search(source, query, *, organism=None, entry_type=None, limit=0, timeout=60)` → `list[SearchResult]`

### Environment variables

| Variable | Effect |
|----------|--------|
| `NCBI_API_KEY` | Raises GEO rate limit from 3 → 10 req/s |
| `VCP_TOKEN` | JWT for private CZI Virtual Cell Platform datasets |

## Development

```bash
cd python/
pip install -e .
pytest tests/ -m "not integration"          # unit tests (no network)
pytest tests/ -m integration                # integration tests (hits GEO)
HAPIQ_DOWNLOAD_TEST=1 pytest tests/ -m integration  # includes actual file download
```

## TODO

- [ ] Publish to PyPI with platform-specific wheels (Linux x86_64, macOS arm64/x86_64, Windows x86_64)
- [ ] GitHub Actions workflow for cross-platform wheel builds via `cibuildwheel`
- [ ] Async `fetch()` variant for use in notebook environments
