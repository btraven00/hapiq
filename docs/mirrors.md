# Mirrors: pushing fetched datasets to remote stores

Status: **proposed** — design record, nothing here is implemented yet.

## Context

hapiq fetches datasets from upstream repositories (GEO, Zenodo, …) by
canonical accession and keeps the bytes in a local content-addressed cache
([cache-server.md](cache-server.md)). Some consumers can't read that cache:
the motivating one is omnibenchmark running on a remote compute platform
(thoa), whose jobs take their inputs from the platform's own dataset store.
Today that means re-uploading the same public dataset by hand for every
benchmark, with nothing recording that "thoa dataset `ds-…`" *is*
`geo:GSE123456`. This pattern can easily be extended to any configurable
cluster-local cache.

We want hapiq to:

1. **Push** a fetched dataset to a configured remote store (a *mirror*).
2. **Record** the mapping "accession + exact file set → location on remote".
3. **Answer** "is there a verified copy of `geo:GSE123456` on `thoa`?" from
   the cache server, so a consumer (omnibenchmark's data stage) can use the
   mirrored copy and fall back to upstream when there is none.

Stores differ (thoa, S3, a lab NAS, …), so each one is an *adaptor* behind a
common interface, the same way sources are adaptors behind `Downloader`.

## Principle: identity vs. location

**A dataset's identity is its canonical accession plus the digest of its
file set. A mirror is only a location.**

- Benchmark plans and manifests reference `geo:GSE123456` (optionally pinned
  by digest), never a store-specific id like a thoa dataset id.
- A mirror record asserts: *remote `R` holds, under id `X`, exactly the file
  set with digest `D`, which is what accession `A` resolved to.* The digest
  makes the "same as" claim checkable instead of trusted.
- If the record is missing, stale, or its digest doesn't match what the
  consumer pinned, the consumer fetches upstream. A lost mirror costs time,
  never correctness or reproducibility.

## Terms

| term | meaning |
|---|---|
| **remote** | a configured, named store instance, e.g. `thoa` or `lab-s3` (like an rclone remote) |
| **adaptor** | the code for one store type (`thoa`, `s3`, …); one adaptor serves many remotes |
| **file set** | the files of one fetched dataset: dataset-relative paths with size, sha256 and md5 |
| **digest** | sha256 identifying a file set (definition below) |
| **mirror record** | (accession, digest, remote) → remote id, plus push/verify times |

## File set digest

Defined so that any implementation (Go, omnibenchmark's Python) can recompute
it from a file list:

1. For each file, take its dataset-relative path: slash-separated, clean
   (no `.`/`..` segments, no leading `/`), UTF-8.
2. Form one line per file: `<sha256 hex lowercase><two spaces><path>\n`
   (i.e. `sha256sum` output format).
3. Sort lines bytewise by path; concatenate.
4. digest = sha256 of the result, lowercase hex.

Empty files are included. Directory entries are not. The digest covers names
and content only; sizes and timestamps are implied by or irrelevant to it.

The witness `hapiq.json` stores whichever checksum the source provided (often
md5, sometimes none), so the push step computes sha256 itself — or takes it
from the cache index (`urls.sha256`) when the file came through the cache.

## Adaptor interface

New package `pkg/mirror`:

```go
package mirror

// File is one file of a set, as pushed.
type File struct {
	Path   string // dataset-relative, slash-separated, clean
	Local  string // local file to read (often a cache blob or its materialization)
	Size   int64
	SHA256 string // hex
	MD5    string // hex; many stores (thoa, S3 single-part ETag, Azure, GCS) verify with md5
}

// Fileset is what gets pushed: one accession's files.
type Fileset struct {
	Accession string // canonical "source:id"
	Files     []File
}

// Digest implements the definition in docs/mirrors.md.
func (fs Fileset) Digest() string

// Location is where a pushed file set landed.
type Location struct {
	RemoteID string // store-native id: thoa dataset id, S3 prefix, …
	URI      string // optional, human-readable: "thoa:ds-…", "s3://bucket/prefix"
}

// RemoteFile is a file as the store reports it.
type RemoteFile struct {
	Path string
	Size int64
	Hash string // "<algo>:<hex>" as the store keeps it (md5 for thoa), "" if none
}

// Mirror is implemented once per store type.
type Mirror interface {
	// Push uploads fs and returns its location. Must preserve Files[i].Path as
	// the path inside the remote dataset. Should skip bytes the store already
	// has (most stores dedupe by md5); must not fail because content exists.
	Push(ctx context.Context, fs Fileset) (Location, error)

	// Stat lists what the store holds at loc, for verification and liveness.
	// Returns ErrNotFound if loc no longer exists.
	Stat(ctx context.Context, loc Location) ([]RemoteFile, error)
}

var ErrNotFound = errors.New("mirror: location not found")
```

Deliberately small. Two methods cover push, verify-after-push, and the
consumer-side liveness check. Not in the interface (see Non-goals): delete,
list-all, pull.

Adaptors register by type name, mirroring `downloaders.Register`:

```go
// Factory builds a Mirror from its remote's config section.
type Factory func(name string, cfg map[string]any) (Mirror, error)

func Register(typ string, f Factory)
func Open(name string) (Mirror, error) // looks up [mirrors.<name>] in config
```

### Verification

`Verify(fs Fileset, remote []RemoteFile) error` is a shared helper, not
adaptor code: it compares path sets and sizes exactly, and compares hashes
whenever the store reports an algorithm the `File` carries (md5 or sha256).
It runs after every push and in `hapiq mirror verify`. An adaptor whose
store reports no hashes gets path+size verification only, and the record
says so (`verified = 'size'` vs `'hash'`).

## Config

Named remotes in the existing config (`~/.hapiqrc`), one table each; `type`
selects the adaptor, other keys belong to the adaptor:

```toml
[mirrors.thoa]
type = "thoa"
# credentials come from the adaptor's own resolution:
# THOA_API_KEY, ~/.config/gothoa/config.yaml, or ~/.thoa/credentials

[mirrors.lab-s3]          # future adaptor, shown for shape
type   = "s3"
bucket = "lab-datasets"
prefix = "hapiq/"
```

Credentials are never written into `index.db` or served by the cache server.

## Records

Two tables in the cache's `index.db`:

```sql
CREATE TABLE mirror_sets (
  digest      TEXT NOT NULL,      -- file set digest
  path        TEXT NOT NULL,      -- dataset-relative
  sha256      TEXT NOT NULL,
  md5         TEXT NOT NULL,
  size        INTEGER NOT NULL,
  PRIMARY KEY (digest, path)
);

CREATE TABLE mirrors (
  accession   TEXT NOT NULL,      -- canonical source:id
  digest      TEXT NOT NULL,
  remote      TEXT NOT NULL,      -- configured remote name, e.g. "thoa"
  remote_id   TEXT NOT NULL,
  uri         TEXT,
  pushed_at   INTEGER NOT NULL,
  verified_at INTEGER,            -- last successful Stat+Verify
  verified    TEXT,               -- 'hash' | 'size'
  PRIMARY KEY (accession, digest, remote)
);
```

`mirror_sets` stores the file list behind each digest, so the digest can be
recomputed and verification works even after the local blobs are gc'd. One
accession may have several digests: upstream revisions, or different
`options` filters (a filtered subset is a different file set, honestly
labelled by its digest).

## CLI

```
hapiq mirror push <remote> <dir>...       # dir = a download dir with hapiq.json
hapiq mirror push <remote> --manifest m.yaml --parent-dir <dir>
hapiq mirror ls [--remote R] [accession]
hapiq mirror verify [--remote R] [accession]
```

`push`:

1. Read `<dir>/hapiq.json`; accession = `source:original_id`; files =
   witness `files[].path` relative to `<dir>`.
2. Hash every file (sha256 + md5 in one read; reuse `urls.sha256` on cache
   hits) and build the `Fileset`.
3. If a record (accession, digest, remote) exists and `Stat` still verifies,
   print it and stop — pushing is idempotent.
4. `Push`, then `Stat` + `Verify`; write `mirror_sets` and `mirrors` rows
   only after verification passes.
5. Print `accession  digest  remote  remote_id`.

Manual push first. Automatic push-on-fetch (`[mirrors] auto_push = ["thoa"]`)
is a later, separate decision.

## Cache server endpoint

```
GET /v1/mirrors?accession=geo:GSE123456[&remote=thoa][&digest=<hex>]
```

Behind the existing bearer-token guard. Always `200` with a JSON list (empty
when nothing matches), newest `pushed_at` first:

```json
[{
  "accession": "geo:GSE123456",
  "digest": "9f86d0…",
  "remote": "thoa",
  "remote_id": "ds-…",
  "uri": "thoa:ds-…",
  "pushed_at": 1761200000,
  "verified_at": 1761200100,
  "verified": "hash",
  "files": [{"path": "GSE123456_matrix.h5ad", "size": 123, "sha256": "…", "md5": "…"}]
}]
```

`files` is included so the consumer can recompute the digest itself and
check it against its pin without trusting the server.

## Consumer protocol (omnibenchmark data stage)

1. The plan names the dataset by accession, pinned by digest (or by a
   `files` list with sha256, from which the digest is computed).
2. Query `GET /v1/mirrors?accession=…&remote=<platform>&digest=<pin>`.
3. Hit → recompute the digest from `files`, compare to the pin, then hand
   `remote_id` to the platform (for thoa: `thoa run --input-dataset <id>`).
   If `verified_at` is old, the consumer may ask the platform whether the id
   still exists before relying on it.
4. Miss, mismatch, or the id is gone → fetch upstream via hapiq as today
   (and optionally push, so the next run hits).

A record without a matching digest is never used. Unpinned plans may take the
newest record for the accession, but should log the digest they got so the
run is reproducible after the fact.

## First adaptor: thoa

`pkg/mirror/thoa`, a thin wrapper over the Go client
`github.com/btraven00/thoa-go` (a port of the MIT-licensed python `thoa`
dataset code; compatible with hapiq's GPL-3.0).

- `Push`: `Upload(map[path]local)` — the map form keeps dataset paths
  relative and stable instead of leaking local cache paths. Returns
  `RemoteID` = dataset public id, `URI` = `thoa:<id>`. thoa skips files it
  already holds by md5, so re-pushes are cheap.
- `Stat`: `Files(id)` for paths; sizes and md5 from `GET /files?dataset_public_id=`.
  `ErrNotFound` when the dataset lookup is empty.

Known blockers and questions for the thoa team, before this is useful:

- Imports of `provider: local` currently fail server-side with
  `Provider 'local' is not configured` (reproduced with the official python
  client 0.1.8).
- Datasets carry `remaining_downloads`. Does using a dataset as a job input
  decrement it? Benchmarks re-read inputs constantly; a read quota on job
  inputs would rule thoa out as a cache.
- Does `thoa run --input-dataset <id>` work as of 0.1.8?
- Are datasets permanent, or can they expire or be deleted?
- Does `GET /files` return each file's md5? If not, verification is
  size-only.
- `thoa-go` isn't published yet: hapiq needs it on GitHub (or a `replace`
  directive during development).

## Non-goals

- **General-purpose sync.** For mirroring a cache or download tree to S3,
  SFTP, Drive, etc., use rclone on a hapiq output dir. Adaptors exist only for
  stores rclone can't reach, or where the store's own dataset ids are what
  consumers need (thoa).
- **Delete / remote GC.** Records outlive remote data gracefully (consumers
  fall back), so deletion can wait. Add an optional `Deleter` interface when
  a store needs quota management.
- **Pulling from mirrors.** Using a mirror as a download source when upstream
  is down would be an optional `Fetcher` interface. It's a natural extension
  but a different feature.
- **Listing remotes.** Records are the source of truth for what hapiq
  pushed; adaptors don't need to enumerate stores.

## Implementation order

1. `pkg/mirror`: types, `Digest`, `Verify`, registry, with tests (digest
   test vectors so other implementations can check themselves).
2. Tables in `pkg/cache/sqlite.go`.
3. `pkg/mirror/thoa` adaptor; `hapiq mirror push` / `ls` / `verify`.
4. `GET /v1/mirrors` on the cache server; document it in
   [lan-sharing.md](lan-sharing.md).
5. omnibenchmark resolver (separate repo).

Steps 1, 2 and 4 don't depend on thoa and can land first; 3 can be built and
tested against a fake server, but real use waits on the blockers above.
