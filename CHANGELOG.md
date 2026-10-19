# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

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
