# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.1.2] - 2026-10-26

### Added

- **Hugging Face Hub datasets** (`hf`, alias `huggingface`):
  `hapiq download hf <owner>/<name>[@revision]`. The revision is pinned to a
  commit sha, and every file is checked against the Hub's sha256 (LFS) or git
  blob sha1. `HF_TOKEN` is sent for gated and private datasets; their files
  bypass the blob cache and LAN peers. `hapiq search hf <query>` lists Hub
  datasets, most downloaded first.
- **10x Genomics datasets** (`10x`, alias `10xgenomics`):
  `hapiq download 10x <slug>` reads the file list from the Internet Archive's
  copy of the dataset page and downloads from 10x's open hosts, checking each
  md5. `<slug>@<timestamp>` pins a snapshot; a page saved from the browser
  (HTML or JSON) also works.
- **`hapiq refetch <dir>`** re-downloads a dataset from its `hapiq.json` and
  checks every file against its recorded checksum. `ensembl` is not supported
  yet.

### Changed

- **`--timeout` is a stall limit.** It now abandons a request only when no
  data arrives for that many seconds. Before, it also capped the whole
  transfer, so files taking over 5 minutes were cut off.

### Fixed

- **GEO series filters:** `--filename-pattern`, `--include-ext`,
  `--exclude-ext`, `--max-file-size` and `--skip-existing` now apply to
  series-level supplementary files. Before, every file in `suppl/` was
  downloaded on every run. The dry run and the real download now use the
  same file list. The series matrix and SOFT files are always included.
  A filter that excludes every supplementary file prints a warning.
- **GEO size estimate:** a series' estimated size is now summed from the FTP
  listing. Before, every series reported a hardcoded 500 MB. When the
  listing can't be read, the size shows as "unknown".
- **Dry-run output:** each file now has its destination `path` and its
  approximate `size`. The zero `download_time` is no longer printed.
- **Witness stats:** `download_stats` describes the latest run only. Before,
  byte and file counts added up across runs into the same directory.
  GEO downloads now report `files_skipped`.
- **Failed files fail the run.** A file that failed mid-transfer in figshare,
  SRA and GEO (listed supplementary files) was only a warning: the run
  exited 0 and could leave the truncated file in place. It is now an error
  and the partial file is removed.
- **Checksums from the source.** figshare and Zenodo files are verified
  against the md5 they publish, and the md5 (also ENA's for SRA) is recorded
  as `source_checksum` in `hapiq.json`.
- **ExperimentHub:** EH ids are resolved to their rdatapath ids before
  fetching. Before, e.g. EH3796 downloaded EH3760's file and reported
  success. Search lists each resource once under its EH id.
- **`manifest get`:** geo, figshare and ensembl entries land in the entry
  folder instead of a nested `<identifier>/<id>/`.
- **`hapiq.json` options:** biostudies, experimenthub, hca, scperturb, url
  and vcp now record the download options (filters) in provenance.
- **GEO:** the series file count comes from the FTP listing; off-site links
  in FTP listings are ignored; E-utilities HTTP 429 is retried with backoff.
- **HTTP fetches send `Accept: */*`**, so Cloudflare-fronted hosts serve HTML
  files unmodified (they failed their published md5 before).
- **Cache:** an unwritable cache dir falls back to no cache instead of
  failing every download.
- **CLI:** errors are printed once; `search --help` lists scperturb; a
  dry-run summary no longer says "Files downloaded"; a plain `go build` on a
  tag reports the tag version.

### Security

- Go 1.26.10 (GO-2026-6809, GO-2026-6611..6617) and x/net v0.60.0.

## [0.1.1] - 2026-10-05

### Fixed

- **Zenodo identifiers:** record, deposit and `doi.org` URLs must now start
  the input. Before, a Zenodo URL embedded in another URL (for example in a
  query parameter) was accepted as a Zenodo identifier.

### Security

- GitHub Actions workflows default the `GITHUB_TOKEN` to read-only
  (`contents: read`); only the Release job gets `contents: write`.

## [0.1.0] - 2026-10-05

First tagged release. Earlier builds were published only as
`0.0.0.dev<sha>`.

### Added

- **SharePoint share links** in the `url` source. Anonymous
  `*.sharepoint.com` share links are now resolved to direct, Range-capable
  download URLs. Single-file (`:b:`) links resolve automatically; for folder
  (`:f:`) links, name the target file via the URL fragment, e.g.
  `https://tenant.sharepoint.com/:f:/s/Site/<token>#models.ckpt`.
- **Release versions in provenance.** Builds from a `vX.Y.Z` tag stamp `X.Y.Z`
  into the binary, so `hapiq version` and `hapiq_version` in `hapiq.json` name
  the release. Untagged builds still report `devel (<sha>, <time>)`. The conda
  package on `almost-conductor` is now published per tag as `X.Y.Z`; pushes to
  `main` keep publishing `0.0.0.dev<sha>`, which sorts below every release.
- `hapiq cache verify` now reports **orphan blob files**: files under
  `blobs/sha256/` with no index row, e.g. left by a crash or a recreated
  index. `--remove-orphans` deletes them, skipping files still hardlinked from
  output directories and files written in the last hour.

### Changed

- **`url` source: output filenames follow the server.** When the server sends
  `Content-Disposition` on the GET response, the file is saved under that name
  (e.g. `gene2go.pkl`) instead of the last URL segment (e.g. `6153417`). This
  matters for servers that refuse `HEAD` but name the file on `GET`, such as
  Dataverse's download API. Anything that expects the old URL-derived name
  must be updated. Cache hits reproduce the same name.

### Removed

- **`hapiq fetch`.** Use `hapiq download url <url> --out <dir>`, which takes
  the same flags (`--hash`, `--force`, `--skip-existing`, `--dry-run`, `-y`,
  `-t`) and goes through the same downloader and cache.

### Fixed

- `download` into an existing directory (figshare, GEO) honors `--force` and
  `--skip-existing`: both now download into the directory and apply the policy
  per file, instead of skipping the whole download. Without a terminal, or
  when stdin closes at the prompt, it falls back to the `-y` behaviour instead
  of failing with EOF.

[Unreleased]: https://github.com/btraven00/hapiq/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/btraven00/hapiq/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/btraven00/hapiq/releases/tag/v0.1.0
