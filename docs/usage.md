# Usage

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
| `experimenthub` | `EH<digits>` (e.g. `EH1039`) | Bioconductor ExperimentHub; metadata catalog cached locally for a week |
| `hf` | `<owner>/<name>` or `<owner>/<name>@<revision>` (e.g. `scikit-learn/iris`) | Hugging Face Hub datasets; the revision is pinned to a commit and every file is checked against the Hub's sha256 / git hash. Set `HF_TOKEN` for gated or private datasets; their files are never cached or shared with LAN peers |
| `10x` | Dataset slug from the page URL (e.g. `10k-hgmm-5p-gemx`), `<slug>@<timestamp>`, or a saved page | 10x Genomics example datasets; every file checked against the md5 10x publishes |
| `url` | Any `http://` or `https://` URL | Direct single-file fetch; filename from `Content-Disposition` or URL path |

`ncbi` is an alias for `geo`. `ena` is an alias for `sra`. `huggingface` is an alias for `hf`. `10xgenomics` is an alias for `10x`.

---

## `hapiq search`

Search for datasets using a repository's native query API.

```
hapiq search <source> <query> [flags]
```

Supported sources: `geo`, `vcp`, `scperturb`, `experimenthub`, `hf`

| Flag | Default | Description |
|------|---------|-------------|
| `--limit N` | 10 | Maximum results to return |
| `--organism X` | — | Filter by organism (e.g. `"Homo sapiens"`); not available for `hf` |
| `--type X` | — | GEO: entry type (`GSE`/`GSM`/`GPL`/`GDS`); VCP: assay filter (e.g. `"Perturb-Seq"`); hf: Hub tag (e.g. `modality:tabular`) |
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

hapiq search hf "single-cell" --limit 10          # most downloaded first
hapiq search hf "cells" --type modality:tabular

# Pipe into download
hapiq search geo "bulk RNA-seq liver" -q \
  | head -3 \
  | xargs -I{} hapiq download geo {} --out ./data
```

---

## `hapiq download`

Download a dataset from a repository.

```
hapiq download <source> <id> --out <dir> [flags]
```

### Required

| Flag | Description |
|------|-------------|
| `--out <dir>` | Output directory (created if it doesn't exist) |

### File-level filters

Applied per file before anything is written to disk.

| Flag | Description |
|------|-------------|
| `--include-ext .h5ad,.csv.gz` | Only download files with these extensions (comma-separated) |
| `--exclude-ext .bam,.fastq.gz` | Skip files with these extensions |
| `--max-file-size 500MB` | Skip files larger than this (supports B, KB, MB, GB, TB) |
| `--filename-pattern '*.counts.*'` | Only download filenames matching this glob |

### Source-specific filters

| Flag | Description |
|------|-------------|
| `--subset GSM123,GSM456` | GEO only: download only these sample accessions from a series |
| `--organism "Homo sapiens"` | Skip the dataset if its organism doesn't match (case-insensitive partial) |
| `--dry-run` | List files that would be downloaded without writing anything |

### Download behaviour

| Flag | Default | Description |
|------|---------|-------------|
| `--exclude-raw` | false | Skip raw data files (FASTQ, BAM, SRA, CEL…) |
| `--exclude-supplementary` | false | Skip supplementary/readme/manifest files |
| `--parallel N` | 8 | Concurrent downloads |
| `--resume` | false | Resume interrupted downloads |
| `--skip-existing` | false | Skip files that already exist locally |
| `--force` | false | Overwrite existing files without prompting |
| `-y, --yes` | false | Non-interactive mode (auto-confirm prompts) |
| `-t, --timeout N` | 300 | Seconds without any data received before a request is abandoned |

### Output

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

# Bioconductor ExperimentHub
hapiq download experimenthub EH1039 --out ./data

# Hugging Face Hub dataset (pinned to the commit main points at now)
hapiq download hf cornell-movie-review-data/rotten_tomatoes --out ./data/rt

# 10x Genomics: matrices and summaries only, no BAM/FASTQ (slug from the dataset page URL)
hapiq download 10x 10k-hgmm-5p-gemx --out ./data/10k-hgmm-5p --include-ext .h5,.csv,.html

# Direct URL (single file)
hapiq download url https://example.com/data.h5ad --out ./data
```

Each download writes a `hapiq.json` witness file containing the full metadata, per-file checksums (SHA-256), and download statistics for reproducibility.

---

## `hapiq refetch`

Re-download a dataset into its folder from the `hapiq.json` there, so the data
can be deleted and only the witness kept.

```bash
rm -r data/GSE264667/supplementary
hapiq refetch data/GSE264667            # several folders at once also work
```

It downloads the same source, ID and filters (`--include-ext` and so on) back
into the folder, keeping files that are still on disk. Every file listed in
`hapiq.json` is then checked against its recorded checksum: a file that changed
upstream fails the run, is removed, and `hapiq.json` is left as recorded. On
success the run is merged into `hapiq.json`.

GEO and figshare download into `<out>/<id>`, so their folders must keep that
name. Ensembl is not supported yet (its `hapiq.json` does not record the
species).

---

## 10x Genomics datasets

`hapiq download 10x <slug>` downloads the files listed on a 10x dataset page
and checks each against the md5 10x publishes. The slug is the last part of the
page URL (`https://www.10xgenomics.com/datasets/<slug>`).

`hapiq.json` records the dataset as `<slug>@<timestamp>`, so `hapiq refetch`
uses the same file list; pass that form to pin a version yourself. A copy of
the page data is kept as `<dir>/<slug>.10x.json`.

If a dataset is not found, save its page from your browser (Ctrl+S) and pass
the saved `.html` file instead of the slug.

---

## `hapiq cache serve`

Serve the local blob cache to other hapiq instances so a lab or a workshop
downloads a dataset once instead of once per machine. Peers verify every blob
by its SHA-256, so no trust between nodes is required.

```bash
hapiq cache serve --listen 0.0.0.0:7777
```

Peers find each other via a static list, an introducer (one hostname that
gossips the rest), or mDNS on the local link. See
[docs/lan-sharing.md](lan-sharing.md).

---

## `hapiq downloaders`

List all registered downloaders with their supported IDs and examples.

```bash
hapiq downloaders
hapiq downloaders --output json
```

---

## `hapiq species`

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
| `HF_TOKEN` | Hugging Face access token, for private or gated datasets (accept the dataset's terms on the Hub first). |
| `SSL_CERT_FILE` | PEM bundle to trust instead of the system roots (Linux/BSD). Use it for a server that omits its intermediate certificate (`x509: certificate signed by unknown authority`): concatenate the system bundle and the missing intermediate, after checking it with `openssl verify`. There is no option to skip verification. |

```bash
export NCBI_API_KEY=your_key_here
hapiq download geo GSE133344 --out ./data
```

---

## Provenance

Every `hapiq download` writes a `hapiq.json` file alongside the downloaded data:

```json
{
  "hapiq_version": "0.1.2",
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

The data can be deleted and restored later from this file alone with
[`hapiq refetch`](#hapiq-refetch).
