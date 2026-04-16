# GEO Download How-To

This guide covers the full workflow from finding a dataset to downloading
its raw reads (FASTQ).

---

## Understanding the accession hierarchy

```
BioProject  PRJNA551220        ← administrative umbrella for the whole study
  └── GEO Series  GSE133344   ← processed data, count matrices, metadata
        └── GEO Sample  GSM39123xx  (one per biological sample)
              └── SRA Experiment  SRX62748xx  (library info)
                    └── SRA Run  SRR96025xx   ← the actual FASTQ.gz files
```

**GEO** (hapiq's `geo` source) gives you the **processed data** the authors
deposited — count matrices, `.h5ad` files, supplementary tables, etc.

**SRA/ENA** gives you the **raw reads** — what you need to rerun alignment,
QC, or any analysis from scratch. Use `--raw` on `hapiq download geo` to get
these too.

---

## Step 1 — Find the GEO accession

If you have a BioProject (`PRJNA*`), hapiq resolves it via NCBI ELink:

```bash
hapiq search geo PRJNA551220
```

If you already have the GSE number, skip ahead.

---

## Step 2 — Inspect before downloading

Always run with `--dry-run` first:

```bash
# What processed supplementary files exist?
hapiq download geo GSE133344 --out ./data --dry-run

# What raw FASTQ files exist, with sizes and MD5s?
hapiq download geo GSE133344 --out ./data --raw --dry-run
```

`--raw --dry-run` prints a JSON manifest to stdout:

```json
{
  "series": "GSE133344",
  "total_files": 32,
  "total_bytes": 2897519206,
  "runs": [
    {
      "run": "SRR9602561",
      "experiment": "SRX6367793",
      "sample": "SAMN12142214",
      "layout": "PAIRED",
      "files": [
        {
          "name": "SRR9602561.fastq.gz",
          "bytes": 181579898,
          "md5": "6d5729cff6ab45532e6afa578e0a1d10",
          "url": "https://ftp.sra.ebi.ac.uk/vol1/fastq/SRR960/001/SRR9602561/SRR9602561.fastq.gz"
        }
      ]
    }
  ]
}
```

Use the manifest to calculate total download size and decide what to fetch.

---

## Step 3 — Test with a single file

```bash
# Download only the first file to verify the setup
hapiq download geo GSE133344 --out ./data --raw --limit-files 1
```

---

## Step 4 — Download everything

```bash
# Download all raw FASTQ files (prompts for confirmation with total size)
hapiq download geo GSE133344 --out ./data --raw

# Skip the confirmation prompt (e.g. in scripts)
hapiq download geo GSE133344 --out ./data --raw --yes

# Limit parallel downloads (default: 2 for large FASTQ files)
hapiq download geo GSE133344 --out ./data --raw --parallel 4
```

Hapiq downloads from ENA's public HTTPS mirror — no `sra-tools` required.
Each file is verified against the ENA-provided MD5 checksum after download.

---

## Filtering

### By file extension

```bash
# Only .h5ad files from the processed data
hapiq download geo GSE133344 --out ./data --include-ext .h5ad

# Skip BAM and FASTQ from the processed supplementary folder
hapiq download geo GSE133344 --out ./data --exclude-ext .bam,.fastq.gz
```

### By file size

```bash
# Skip files larger than 500 MB (useful for spotty connections)
hapiq download geo GSE133344 --out ./data --raw --max-file-size 500MB
```

### By sample subset

```bash
# Download raw reads for specific samples only
hapiq download geo GSE133344 --out ./data --raw \
  --subset GSM3912345,GSM3912346
```

### Filename glob

```bash
# Only count matrices
hapiq download geo GSE133344 --out ./data --filename-pattern '*.counts.*'
```

---

## Organism filter (for batch scripts)

When iterating over multiple accessions, skip non-matching datasets:

```bash
hapiq search geo "bulk RNA-seq liver" -q | while read gse; do
  hapiq download geo "$gse" --out ./data --organism "Homo sapiens" --yes --quiet
done
```

---

## Provenance

Every download writes `hapiq.json` alongside the files:

```
./data/
  hapiq.json            ← metadata, checksums, download stats
  supplementary/
    GSE133344_RAW.tar.gz
  SRR9602561/
    SRR9602561.fastq.gz
  SRR9602562/
    ...
```

The witness file includes per-file MD5 checksums, source URLs, and the exact
hapiq version used — enough to reproduce the download exactly.

---

## Environment variables

| Variable | Effect |
|----------|--------|
| `NCBI_API_KEY` | Raises NCBI eutils rate limit from 3 to 10 req/s. Get one at [ncbi.nlm.nih.gov/account](https://www.ncbi.nlm.nih.gov/account/). Strongly recommended for large series. |

```bash
export NCBI_API_KEY=your_key_here
```
