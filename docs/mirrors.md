# Remotes: datasets on stores other than upstream

Status: **proposed** — design record, nothing here is implemented yet.

## Context

omnibenchmark runs a whole benchmark (snakemake) inside one thoa VM. Its data
modules call hapiq to fetch their inputs by accession, so every run pulls the
same public datasets from GEO, Zenodo, … again, from inside the VM. thoa has
its own dataset store: files are content-addressed by md5 and a dataset is a
list of file ids. Fetching from thoa is closer to the VM and doesn't depend on
the upstream repository being up.

We want hapiq to treat thoa like a git remote for datasets:

1. **Fetch** an accession from upstream (or the local cache), as today.
2. **Push** its files to a configured remote, which creates a thoa dataset.
   Either explicitly or automatically after each upstream fetch.
3. **Record** "this accession and file set is dataset `X` on remote `R`" in
   the manifest, which is committed with the benchmark and works as a lockfile.
4. **Fetch transparently from the remote** next time: if a remote that ranks
   above upstream holds a verified copy, hapiq downloads from there. If it
   doesn't, hapiq falls back to upstream.

The store is one adaptor behind a small interface (as sources are behind
`Downloader`); thoa is the first, and the only one planned.

## Model

| git | hapiq |
|---|---|
| ref name | accession + options, i.e. one manifest entry |
| commit | the entry's file set (paths + sha256), identified by its digest |
| `origin`, upstream | the source repository (GEO, Zenodo, …) |
| other remotes, in priority order | `[remotes.<name>]` in config, e.g. `thoa` |
| `git push <remote>` | `hapiq push <remote>`: upload, create dataset, record its id |
| `refs/remotes/<r>/*`, committed | `remotes:` field in the manifest entry |
| `refs/remotes/<r>/*`, local only | `remote_refs` table in `index.db` |
| `git fetch` from a remote | download from the highest-priority remote that verifies |

**Identity vs. location.** A dataset is identified by its accession and its
file set. A remote copy is only a location. A remote record is used only when
the files it delivers match the hashes the manifest pins. A lost, stale or
unreadable remote copy costs time, never correctness.

## Config

```toml
[remotes]
order        = ["thoa"]   # tried in this order before upstream; upstream is always last
push_on_fetch = true      # after an upstream fetch, push to every remote in `order`

[remotes.thoa]
type = "thoa"
# endpoint defaults to https://api.thoa.io
# credentials: THOA_API_KEY, ~/.config/gothoa/config.yaml, or ~/.thoa/credentials
```

`--remote <name>` and `--no-remotes` override `order` per command;
`--no-push` overrides `push_on_fetch`. Credentials never go into the
manifest, `index.db`, or anything the cache server serves.

## Records

### In the manifest (shared, committed)

A manifest entry gets an optional `remotes` field. It needs a `files` list with
sha256 hashes, which `manifest gen` writes since every hapiq.json file now has
a sha256:

```yaml
- identifier: pbmc3k
  accession: geo:GSE123456
  files:
    - name: GSE123456_matrix.h5ad
      hash: sha256:9f86d0…
  remotes:
    - type: thoa
      endpoint: https://api.thoa.io
      dataset: 3fa85f64-…        # thoa dataset public id
      pushed_at: 2026-10-29T10:00:00Z
```

The record is keyed by `type` + `endpoint`, not by the local remote name.
`thoa` in one person's config and `thoa` in another's resolve to the same
records when they point at the same endpoint. This matters once thoa
shares datasets between accounts. Today the dataset only works with the
pusher's account. Later, another account can read it if it was shared, and
otherwise gets `ErrForbidden` and falls through (see *Sharing*).

`manifest gen` copies matching `remote_refs` rows into the entry, and
`hapiq push --manifest m.yaml` writes the field back in place.

### In `index.db` (local)

Written by every successful push. This is the record for plain
`hapiq download` runs that have no manifest, and the source `manifest gen`
reads from:

```sql
CREATE TABLE remote_refs (
  accession   TEXT NOT NULL,   -- canonical source:id
  digest      TEXT NOT NULL,   -- file set digest (below)
  type        TEXT NOT NULL,   -- adaptor type, "thoa"
  endpoint    TEXT NOT NULL,
  remote_id   TEXT NOT NULL,   -- thoa dataset public id
  pushed_at   INTEGER NOT NULL,
  verified_at INTEGER,
  PRIMARY KEY (accession, digest, type, endpoint)
);

CREATE TABLE remote_files (    -- file list behind each digest; survives blob gc
  digest TEXT NOT NULL, path TEXT NOT NULL,
  sha256 TEXT NOT NULL, md5 TEXT NOT NULL, size INTEGER NOT NULL,
  PRIMARY KEY (digest, path)
);

CREATE TABLE remote_pending (  -- pushes that failed or were deferred
  accession TEXT NOT NULL, digest TEXT NOT NULL, remote TEXT NOT NULL,
  dir TEXT NOT NULL,           -- folder holding hapiq.json
  last_error TEXT, queued_at INTEGER NOT NULL,
  PRIMARY KEY (accession, digest, remote)
);
```

`index.db` is created on first use even when `cache.mode = "off"`; only the
blob cache stays off.

## File set digest

The key for local records. Any implementation can recompute it from a file
list:

1. Per file: its path relative to the folder holding hapiq.json, slash-separated,
   clean (no `.`/`..`, no leading `/`), UTF-8 in NFC.
2. One line per file: `<sha256 lowercase hex><two spaces><path>\n`.
3. Sort lines bytewise by path, concatenate, sha256, lowercase hex.

Rejected, not encoded: an empty file set, duplicate paths, paths with `\n`
or `\r`, paths that collide case-insensitively. These all fail push with a
clear error. Empty files are included and directories are not.
`pkg/remote` ships test vectors.

## Fetch

For `hapiq download`, `manifest get` and `refetch`, per accession:

1. Find candidate records for the accession and options: the manifest
   entry's `remotes`, then `remote_refs` rows. Then walk `[remotes] order`.
   For each configured remote with a matching (type, endpoint) record, call
   `Fetch`.
2. Verify every delivered file against the pinned sha256: from the manifest
   `files`, or from `remote_files` for local records. Any mismatch, a missing
   file, `ErrNotFound` or `ErrForbidden` → log one line, move to the next
   remote.
3. Upstream is always last. With no records at all, hapiq fetches from
   upstream as today.
4. The witness records where the bytes came from: `fetched_from:
   thoa:<dataset>` or `upstream`.

Fetched bytes go through the local cache like any other download, so a second
fetch in the same VM is a cache hit.

An entry with no sha256 pins (no `files`, or only md5) is never served from a
remote; hapiq fetches it from upstream and warns once. thoa reports only md5,
so the sha256 pins are what make a thoa copy trustworthy.

## Push

`hapiq push <remote> <dir>...` or `hapiq push <remote> --manifest m.yaml
--parent-dir <dir>`:

1. Read `<dir>/hapiq.json`. Refuse a folder that holds more than one accession
   (`datasets` in hapiq.json); push those per entry from a manifest.
2. Build the file set from the witness: paths relative to `<dir>`, sha256 from
   the witness, md5 computed (thoa needs it). Refuse a file with an empty
   checksum or one missing on disk.
3. If a record (accession, digest, type, endpoint) exists and `Stat` verifies
   it, print it and stop.
4. `Push`, then `Stat` + verify paths, sizes and md5. Only then write
   `remote_refs` + `remote_files`, clear `remote_pending`, and update the
   manifest when given one.
5. Print `accession  digest  remote  dataset`.

`hapiq push <remote>` with no folder pushes everything in `remote_pending`.

**Push on fetch.** When `push_on_fetch` is on and a fetch came from upstream,
hapiq queues a `remote_pending` row and pushes right after the fetch. A failed
push warns and leaves the row queued; it never fails the fetch. Remote
fetches don't push.

**Orphans.** If hapiq dies between `Push` and the record write, the thoa
dataset is left without a record, and the next push creates another one.
Repeat file uploads are free (thoa dedupes by md5) and an orphan dataset is a
list of file ids, so this is accepted. Remote cleanup is a non-goal.

## Warm-up (later)

A thoa job takes one input dataset, staged into the VM before the script
runs. Warm-up builds that dataset from every manifest entry's thoa files
(md5-addressed, so no re-upload) under `<digest>/<path>`. It adds a `dir`
remote with `path = <staging dir>` at the head of `order` in the VM. hapiq
then links staged files instead of downloading them. It's the same
protocol, only one more adaptor. It waits until transparent fetch works.

## Sharing

Records name the endpoint, not the account. When thoa ships sharing, a
consumer on another account can use a shared dataset id from the committed
manifest unchanged. Without access, `ErrForbidden` falls through to the next
remote or upstream. With `push_on_fetch`, the consumer then pushes its own
copy and appends a second record. One entry may hold several thoa records,
which are tried newest first. No account names are stored.

## Adaptor interface

New package `pkg/remote`:

```go
type File struct {
	Path   string // relative, clean, NFC
	Local  string // file to read when pushing
	Size   int64
	SHA256 string
	MD5    string
}

type Fileset struct {
	Accession string
	Files     []File
}

func (fs Fileset) Digest() (string, error) // rejects the cases listed above

type Location struct{ Type, Endpoint, ID string }

type RemoteFile struct {
	Path string
	Size int64
	Hash string // "md5:<hex>", "" if the store keeps none
}

type Remote interface {
	Push(ctx context.Context, fs Fileset) (Location, error)
	Stat(ctx context.Context, loc Location) ([]RemoteFile, error)
	// Fetch downloads loc into dest, preserving paths; the caller verifies.
	Fetch(ctx context.Context, loc Location, dest string) error
}

var (
	ErrNotFound  = errors.New("remote: location not found")
	ErrForbidden = errors.New("remote: no access")
)

type Factory func(name string, cfg map[string]any) (Remote, error)
func Register(typ string, f Factory)
func Open(name string) (Remote, error) // [remotes.<name>]
```

`ctx` carries no run-wide deadline. Transfers use the same per-request stall
limit as downloads (`common.SetIdleTimeout`), so a 25 GB push isn't killed
partway through.

### thoa adaptor

`pkg/remote/thoa`, a thin wrapper over `github.com/btraven00/thoa-go`:

- `Push`: `Upload(map[path]local)`, which returns the dataset public id.
- `Stat`: `Files(id)` plus sizes and md5. HTTP 404 → `ErrNotFound`,
  401/403 → `ErrForbidden`.
- `Fetch`: `Download(id, dest, nil, nil)`. The download quota
  (`remaining_downloads`) is the provider's concern; its error goes up
  as an ordinary fetch failure, and hapiq falls through to the next remote.

## Prerequisites (bugs in hapiq today)

- hapiq.json paths must be relative to their folder for every source:
  manifest-001, branch `fix/witness-relative-paths`.
- `--skip-existing` drops already-present files from hapiq.json `files`
  (`FilesSkipped`), so a re-run gives an incomplete file set. They must be
  listed, or push sends a partial dataset.
- `manifest gen` must keep `options`: manifest-002, `fix/manifest-gen-options`.
- `--timeout` must be a stall limit everywhere: `fix/manifest-stall-timeout`.

## Open questions for thoa

1. Uploads fail with `Provider 'local' is not configured` (python client
   0.1.8 too). This blocks everything.
2. Can a job VM authenticate to the API (an API key in the job environment),
   so hapiq inside the VM can call `Download`?
3. Does `GET /files` return each file's md5 and size, so `Stat` can verify by
   hash?
4. Are datasets permanent, or can they expire?
5. Sharing: which status does an unshared dataset id return to another
   account (404 or 403)? And will shared datasets be readable through the
   same `Download` path?
6. Warm-up: can a dataset be built from existing file ids with new paths?
7. `thoa-go` has to be published on GitHub, or use a `replace` directive
   until it is.

## Non-goals

- General sync to S3, SFTP and the like: use rclone on a hapiq output folder.
- Remote delete or garbage collection.
- Listing or discovering datasets on a remote. Records are the only index,
  because thoa datasets carry no name.
- Serving remote records from the cache server. Add a `GET /v1/remotes`
  endpoint if something outside hapiq ever needs it.

## Implementation order

1. Prerequisite fixes above.
2. `pkg/remote`: types, `Digest` with test vectors, registry, verify helper,
   tested against a fake `Remote`.
3. `index.db` tables; `hapiq push` (manual + pending); the manifest
   `remotes` field in `manifest gen`, `manifest get` and `push --manifest`.
4. Fetch resolution in `download`, `manifest get` and `refetch`;
   `push_on_fetch`.
5. `pkg/remote/thoa`. Steps 2–4 can be built against the fake now; real use
   waits on thoa questions 1 and 2.
6. Warm-up with the `dir` adaptor.
