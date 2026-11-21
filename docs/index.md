---
icon: lucide/house
---

# hapiq

**Hapiq** downloads datasets from scientific repositories with provenance
tracking. _"Hapiq" means "the one who fetches" in Quechua._

```bash
hapiq search geo "ATAC-seq human liver" --limit 5
hapiq download geo GSE133344 --out ./data --dry-run
hapiq download geo GSE133344 --out ./data --include-ext .h5ad,.csv.gz
```

- **Many sources.** GEO, SRA/ENA, Zenodo, Figshare, Ensembl, CZI VCP,
  scPerturb, BioStudies, Human Cell Atlas, CELLxGENE Discover, ExperimentHub,
  Hugging Face, 10x Genomics and plain URLs. Run `hapiq downloaders` for IDs
  and examples.
- **Provenance.** Each download writes a `hapiq.json` with metadata and
  per-file SHA-256, so the data can be deleted and restored later with
  `hapiq refetch`.
- **Shared cache.** A local cache, optionally served to the rest of a lab or a
  workshop, avoids fetching the same bytes twice.

[Install](install.md){ .md-button .md-button--primary }
[Source on GitHub](https://github.com/btraven00/hapiq){ .md-button }

hapiq is free software under the GNU General Public License, version 3 or
later.
