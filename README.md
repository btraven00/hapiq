# Hapiq

**Hapiq** downloads datasets from scientific repositories with provenance tracking.

_"Hapiq" means "the one who fetches" in Quechua._

[![CI/CD](https://github.com/btraven00/hapiq/workflows/CI%2FCD/badge.svg)](https://github.com/btraven00/hapiq/actions)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

## Install

```bash
go install github.com/btraven00/hapiq@latest
```

Nightly binaries and building from source: [docs/install.md](docs/install.md).

## Quick start

```bash
hapiq search geo "ATAC-seq human liver" --limit 5
hapiq download geo GSE133344 --out ./data --dry-run
hapiq download geo GSE133344 --out ./data --include-ext .h5ad,.csv.gz
```

Sources: GEO, SRA/ENA, Zenodo, Figshare, Ensembl, CZI VCP, scPerturb,
BioStudies, Human Cell Atlas, ExperimentHub, Hugging Face, 10x Genomics and
plain URLs. Run `hapiq downloaders` for IDs and examples.

Each download writes a `hapiq.json` with metadata and per-file SHA-256, so the
data can be deleted and restored later with `hapiq refetch`.

## Docs

- [Usage](docs/usage.md) — commands, flags, environment variables, provenance
- [GEO how-to](docs/geo-howto.md)
- [Manifests](docs/manifest.md)
- [Local download cache](docs/cache.md)
- [Sharing a cache across a network](docs/lan-sharing.md)

## License

GPL-3-or-later © 2025-2026 btraven
