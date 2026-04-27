# Manifests

A manifest is a YAML file declaring datasets to download and (optionally) the
file hashes to verify after download. It is the batch entry point to hapiq:
one file describes a set of artifacts, and `hapiq manifest get` materializes
them into a folder tree.

## Schema

The top-level document is a **sequence of entries**. There is no wrapper key
and no global config. Each entry is a flat record:

| field        | required | type             | meaning                                                         |
|--------------|----------|------------------|-----------------------------------------------------------------|
| `identifier` | yes      | string           | folder name created under `--parent-dir`                        |
| `accession`  | yes      | string           | canonical ID in `source:id` form (e.g. `geo:GSE123456`)         |
| `hash`       | no       | string           | shorthand: `<algo>:<hex>` for the sole downloaded file          |
| `files`      | no       | list of `{name, hash}` | explicit list of expected files, paths relative to entry folder |
| `options`    | no       | object           | per-entry download filters (see below)                          |

Unknown fields are rejected at load time — the schema is closed on purpose.

### `hash` vs `files`

Pick one per entry:

- **`hash` (shorthand)** — the entry must resolve to exactly one downloaded
  file. If a filter (`options`) is needed to narrow the result and it still
  produces more than one file, the entry fails.
- **`files` (explicit)** — list each expected file by name (path relative to
  the entry folder) with its hash. Each named file must exist after the
  download; the hash on each file is verified independently. Unlisted extra
  files are tolerated.

Supported hash algorithms: `md5`, `sha1`, `sha256`. An empty hash skips
verification for that file.

### `options`

A flat per-entry subset of the `hapiq download` flags:

| key                    | type        | equivalent CLI flag       |
|------------------------|-------------|---------------------------|
| `include_ext`          | `[string]`  | `--include-ext`           |
| `exclude_ext`          | `[string]`  | `--exclude-ext`           |
| `max_file_size`        | string      | `--max-file-size`         |
| `filename_pattern`     | string      | `--filename-pattern`      |
| `subset`               | `[string]`  | `--subset`                |
| `organism`             | string      | `--organism`              |
| `exclude_raw`          | bool        | `--exclude-raw`           |
| `exclude_supplementary`| bool        | `--exclude-supplementary` |
| `include_sra`          | bool        | `--raw`                   |
| `limit_files`          | int         | `--limit-files`           |

## Example

```yaml
- identifier: pbmc3k
  accession: geo:GSE123456
  files:
    - name: GSE123456_matrix.mtx.gz
      hash: md5:d41d8cd98f00b204e9800998ecf8427e
    - name: GSE123456_barcodes.tsv.gz
      hash: md5:e2fc714c4727ee9395f324cd2e7f331f

- identifier: tabula-sapiens-liver
  accession: hca:cc95ff89-2e68-4a08-a234-480eca21ce79
  hash: sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  options:
    include_ext: [.h5ad]
```

## Commands

### `hapiq manifest gen <dir>`

Reads `<dir>/hapiq.json` (the witness file written by any successful
download) and prints a YAML entry to stdout. The entry's `identifier`
defaults to the basename of `<dir>`, `accession` is built from the witness's
`source` and `original_id`, and one `files` item is emitted per recorded
file with its checksum carried over verbatim.

Append the output to your manifest:

```sh
hapiq download geo GSE123456 --out ./scratch/pbmc3k
hapiq manifest gen ./scratch/pbmc3k >> manifest.yaml
```

### `hapiq manifest get <manifest.yaml> --parent-dir <dir>`

Iterates every entry in the manifest. For each one:

1. Splits `accession` into `source` and `id`.
2. Validates and fetches metadata via the matching downloader.
3. Downloads into `<parent-dir>/<identifier>` (created if absent),
   applying any per-entry `options` as filters.
4. Verifies hashes — `hash` shorthand requires exactly one resulting file;
   `files` verifies each named file by relative path.

Flags:

- `--parent-dir <dir>` (required) — base directory; each entry gets its own
  subfolder named after `identifier`.
- `--continue-on-error` — keep going past a failed entry instead of stopping.
- `--timeout <seconds>` — overall run timeout.

Exit status is non-zero if any entry failed.
