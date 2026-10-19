# BUGS

## 2026-10-19 — `--dry-run` ignored by zenodo downloader: starts the real download

- commit: 2dd6ab8  (`make build`)
- command: `hapiq download zenodo 10.5281/zenodo.14261731 --out <dir> --dry-run -y`
- accession: Zenodo 14261731 (resolves to 14261732), https://zenodo.org/records/14261732
- expected: list 1 file and exit without writing: `C_elegans_wt_early_embryo_GEX_ATAC_multiome.rds`, 4,510,200,259 B, md5:3e8d490ec58286c38dec0b619aa57a0a (metadata block it printed was correct: 1 file, 4.2 GB)
- actual: after the metadata block it printed "⬇️  Starting download... Downloading: C_elegans_wt_early_embryo_GEX_ATAC_multiome.rds (4.2 GB)" and wrote 3.16 GB to `/home/hapiq/tmp/download-*` in ~2 min before being killed (partial removed by hand).
- fallback used: `curl -sL https://zenodo.org/api/records/14261731` for the file list
- notes: `DryRun` is never read in `pkg/downloaders/zenodo/`; same gap in `figshare/` and `ensembl/` (grep `DryRun` per downloader dir). cmd/download.go only prints "Dry-run complete" after the downloader returns, so any downloader that ignores `opts.DryRun` silently does a full download. Safer: short-circuit in cmd before calling Download for downloaders that don't declare dry-run support. Also: the skill's "always dry-run first" is unsafe for these sources until fixed.

## 2026-10-19 — `--dry-run` still downloads; system config cache dir on a read-only FS breaks downloads

- commit: 2dd6ab8  (`make build`)
- command: `hapiq download zenodo 14518762 --out <dir> --dry-run --include-ext .zip,.txt`
- accession: https://zenodo.org/records/14518762
- expected: dry-run lists 2 files (DE_results_all_pathway.zip 309.1 MB, A_readme.txt) without writing anything
- actual: prints "Starting download..." and tries to fetch both files. It fails with `create tmp: open /home/hapiq/tmp/download-...: read-only file system`, because /etc/hapiq/config.toml sets `[cache] dir = "/home/hapiq"`, which is not writable. Two issues: (1) --dry-run is ignored; (2) an unwritable cache dir is a hard error instead of a warning that falls back to no-cache.
- fallback used: none needed. `hapiq --config <file with [cache] mode="off"> download zenodo 14518762 ... -y` worked, and the md5 matches Zenodo.
- notes: config is picked up from /etc/hapiq/config.toml ("Using config file"); there is no CLI flag to disable the cache.
