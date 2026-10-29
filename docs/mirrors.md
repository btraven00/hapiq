# Remotes: datasets on stores other than upstream

Status: **proposed**. This is a design record; nothing here is implemented yet.

## Context

omnibenchmark runs a whole benchmark (snakemake) inside one thoa VM. Its data
modules call hapiq to fetch their inputs by accession, so every run pulls the
same public datasets from GEO, Zenodo, … again, from inside the VM. thoa has
its own dataset store: files are content-addressed by md5 and a dataset is a
list of file ids. That store is the platform's internal data lake. The point
of this design is that thoa executors get their inputs from the lake, not
from public repositories and not from a hapiq server. The hapiq server only
maps accession + filters to lake datasets and fills the lake on a miss. It
never serves dataset bytes to VMs.

We want hapiq to treat thoa like a git remote for datasets, without the
benchmark plan ever naming a thoa id:

1. The plan names an accession and its filters, as today.
2. The data module's hapiq asks a **hapiq server** whether a verified copy
   exists on thoa. If one does, hapiq downloads from thoa.
3. If none exists, the VM fetches from upstream itself. The miss also
   makes the server fetch the same key, push it to thoa with its own key and
   record the dataset id: a pull-through cache. The next run gets a hit.

The data module needs only environment variables and never writes records.
Nothing in the plan changes after the first upload.

**MVP: lock mode, no server.** Until a public server exists, the records
travel in a `hapiq.lock` next to the benchmark plan. You warm it from a
laptop (`hapiq lock add`), and it's committed with the plan. The VM reads it
like the server's lookup answer. Both modes use the same record format,
fetch path, hash checks and hard failure. See *Lockfile*.

## Model

| git | hapiq |
|---|---|
| ref name | lookup key: accession + canonical filters |
| commit | the file set (paths + sha256), identified by its digest |
| `origin`, upstream | the source repository (GEO, Zenodo, …) |
| other remotes, in priority order | `[remotes.<name>]`, e.g. `thoa` |
| `git push` | upload to thoa, create the dataset, record its id |
| refs | records in the server's `index.db` (or the local one) |
| `git fetch` from a remote | download from the highest-priority remote that verifies |

**Identity vs. location.** A dataset is identified by its key and file set.
A remote copy is only a location. hapiq checks the files a remote delivers
against the sha256 values recorded at push time, so a lost or unreadable copy
costs time, never correctness.

## Data module environment

```sh
HAPIQ_SERVER=https://hapiq.example.org   # the records server
HAPIQ_TOKEN=…                            # read token
HAPIQ_REMOTES=thoa                       # priority order, upstream always last
# thoa: the job's own credentials, used only to download
```

`HAPIQ_LOCK=<path>` names the lockfile. Without it, hapiq searches for
`hapiq.lock` upward from the working directory, which finds it in a monorepo
with the plan and modules in one tree.

With no lockfile and no `HAPIQ_SERVER`, hapiq keeps the records in its local
`index.db` and pushes them itself (`push_on_fetch`). This is the laptop mode. Fetch
resolution, the hash check and the hard failure are the same code in both
modes. Only who pushes differs.
Config-file equivalent:

```toml
[remotes]
server        = "https://hapiq.example.org"
order         = ["thoa"]
push_on_fetch = true       # on by default; --no-push turns it off

[remotes.thoa]
type = "thoa"              # endpoint defaults to https://api.thoa.io
```

## Lookup key

`key = accession + "\x00" + canonical(filters)`:

- **accession:** canonical `source:id`.
- **canonical(filters):** the `DownloadOptions` fields that change which files
  are fetched (the same set `manifest gen` writes: extensions, pattern,
  subset, organism, max size, raw/supplementary/sra, limit), as JSON with
  sorted keys, defaults dropped and lists sorted. Fields that don't select
  files, such as concurrency or skip-existing, are excluded. No filters give
  `{}`.

A key has **one** recorded digest. There are no revisions to pick from.

## Upstream changes: hard failure

The digest recorded for a key is the pinned version. hapiq fails loudly
whenever it sees different content for that key:

- **Upstream fetch with an existing record:** this happens when the remote
  copy failed or was skipped. If the upstream file set has a different digest,
  the command exits nonzero with `upstream content for geo:GSE123456 changed:
  recorded <d1>, got <d2>; run hapiq remote invalidate … to accept`. hapiq
  doesn't push and doesn't overwrite the record.
- **Remote fetch:** a file whose sha256 doesn't match the record means a
  corrupt copy, not an upstream change. hapiq falls back to upstream, and
  the rule above applies.
- A remote hit never contacts upstream, so an upstream change goes unnoticed
  until something fetches upstream. That is intended: runs stay reproducible
  against the pinned digest.

`hapiq remote invalidate <accession> [filters] [--remote thoa]` deletes the
key's record on the server (it needs the write token). The next fetch goes
upstream and records the new digest. `hapiq remote ls [accession]` shows the
records.

## Lockfile

`hapiq.lock` sits next to the benchmark plan and is committed with it. It
holds one record per key, in the format the server's `GET` returns:

```yaml
- accession: geo:GSE123456
  filters: {include_ext: [.h5ad]}   # canonical
  digest: 9f86d0…
  files:
    - {path: GSE123456_matrix.h5ad, size: 123, sha256: …, md5: …}
  remotes:
    - {type: thoa, endpoint: https://api.thoa.io, dataset: 3fa85f64-…}
```

- **`hapiq lock add <source> <id> [filters]`** runs on the laptop, which has
  the thoa key. It fetches from upstream or the cache, pushes to every
  remote in `order`, verifies, and writes or refreshes the entry. This is
  the warm-up and the pin in one step, like `go get`.
- **`hapiq lock`** checks that every entry's remote copy still exists (`Stat`)
  and reports any that are gone.
- **`hapiq lock update <source> <id> [filters]`** re-fetches from upstream and
  accepts a new digest. It replaces `invalidate` in lock mode.
- **In the VM**, the lock is read-only. A key missing from the lock is fetched
  from upstream, with a warning naming the `lock add` command to run. The VM
  never pushes, because it can't update the committed lock.
- **Entries are sorted by key** and written deterministically, so diffs stay
  readable.

Records are looked up in this order: `hapiq.lock`, then `HAPIQ_SERVER`, then
the local `index.db`.

## Server

The same `hapiq cache serve`, with `[remotes.thoa]` configured, so it holds
a thoa key. It is the only writer of records:

```
GET    /v1/remotes?accession=…&filters=<canonical, urlencoded>   read token
DELETE /v1/remotes?accession=…&filters=…[&type=…]                 write token
```

- **`GET`:** returns `200` with `{"status": "ready", "records": […]}` (at most
  one per (type, endpoint), each with its `files`: path, size, sha256, md5),
  or `{"status": "pending"}` on a miss.
- **A miss** queues the key for the server's fill worker and returns at once.
  The VM doesn't wait; it fetches upstream itself.
- **The fill worker** fetches the key through the server's own cache, pushes
  to every remote in its `order`, runs `Stat` and the hash check, and only
  then writes the record. It is single-flight per key, so ten VMs missing at
  once cause one fill. Upstream content that differs from an existing record
  fails the fill, and the failure shows in `GET` as
  `{"status": "failed", "error": …}`, with no new record.
- **The queue lives in memory.** A restart loses it, and the next miss queues
  the key again.
- **The server pays the bandwidth.** It downloads every missed dataset once
  and uploads it to thoa once. Its blob cache is subject to the usual
  `hapiq cache gc`; the records outlive the blobs.
- **Tokens:** the read token (`cache.server.token`) covers lookups, and hence
  fills, plus the existing blob routes. The new `cache.server.write_token`
  covers `DELETE`. VMs get only the read token.
- **Public serving:** thoa VMs reach the server over the internet.
  `cache.server.autotls = "hapiq.example.org"` serves HTTPS with Let's
  Encrypt (`golang.org/x/crypto/acme/autocert`). `cache.server.routes =
  ["remotes"]` turns off the blob and peer routes on a public instance.
  A reverse proxy such as Caddy in front of a plain `serve` works too, with no
  code.

### Tables (server `index.db`, or local)

```sql
CREATE TABLE remote_refs (
  key         TEXT NOT NULL,      -- accession \0 canonical filters
  accession   TEXT NOT NULL,
  filters     TEXT NOT NULL,      -- canonical JSON
  digest      TEXT NOT NULL,
  type        TEXT NOT NULL,      -- "thoa"
  endpoint    TEXT NOT NULL,
  remote_id   TEXT NOT NULL,      -- thoa dataset public id
  pushed_at   INTEGER NOT NULL,
  PRIMARY KEY (key, type, endpoint)
);

CREATE TABLE remote_files (       -- file list behind each digest
  digest TEXT NOT NULL, path TEXT NOT NULL,
  sha256 TEXT NOT NULL, md5 TEXT NOT NULL, size INTEGER NOT NULL,
  PRIMARY KEY (digest, path)
);
```

In laptop mode, pushes that fail are retried by `hapiq push` from a local
`remote_pending` table. The server needs no such table: a failed fill
is retried on the next miss.

## File set digest

1. Per file: its path relative to the folder holding hapiq.json, slash-separated,
   clean (no `.`/`..`, no leading `/`), UTF-8 in NFC.
2. One line per file: `<sha256 lowercase hex><two spaces><path>\n`.
3. Sort lines bytewise by path, concatenate, sha256, lowercase hex.

Rejected, not encoded: an empty file set, duplicate paths, paths containing
`\n` or `\r`, and paths that collide case-insensitively. `pkg/remote` ships
test vectors.

## Fetch

Each of `hapiq download`, `manifest get` and `refetch` handles an accession
in five steps:

1. Compute the key and look it up: `hapiq.lock`, then `GET /v1/remotes`,
   then the local `index.db`. If the server can't be reached, warn and go
   upstream.
   `pending` means there is no record yet, and the server is filling it.
2. For each remote in `order` with a record: `Fetch` it into the cache, then
   check every file against the record's sha256. On a mismatch,
   `ErrNotFound`, `ErrForbidden` or a provider error (quota, for example),
   log one line and try the next remote.
3. If no remote delivers, fetch upstream. If a record exists, compare digests
   and fail hard on a difference (see above).
4. Laptop mode only (no server): if there was no record and `push_on_fetch`
   is set, `Push`, then `Stat` and check paths, sizes and md5, then write
   the record. A failed push gives a warning and queues the key in
   `remote_pending`; it never fails the fetch. With a server, the client never
   pushes; the miss in step 1 already queued the fill.
5. hapiq.json records `fetched_from` (`thoa:<dataset>` or `upstream`) and the
   digest, so a run can be traced to the exact files afterwards.

## Push (manual)

`hapiq push <remote> <dir>...` is laptop mode: it pushes folders that came
from upstream and records them locally, as in step 4. With no folder, it
retries `remote_pending`. To warm up a server, run `hapiq remote warm
<manifest.yaml>` instead: it sends one lookup per entry, which queues the
fills. A folder that holds more than one
accession (`datasets` in hapiq.json) is refused. A file with an empty
checksum or missing on disk is refused too.

## Sharing

Records name the thoa endpoint, not the account. Once thoa supports sharing
datasets, a record pushed by one account works for any account it is shared
with. Without access, `ErrForbidden` sends hapiq to the next remote or
upstream, and the key's record stays as it is. A per-account second copy is
a later extension: add `account` to the primary key when it's needed.

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
	// Fetch downloads loc into dest, preserving paths; the caller checks hashes.
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
limit as downloads (`common.SetIdleTimeout`).

### thoa adaptor

`pkg/remote/thoa` is a thin wrapper over `github.com/btraven00/thoa-go`:

- **`Push`:** `Upload(map[path]local)` returns the dataset public id.
- **`Stat`:** `Files(id)` plus sizes and md5. A 404 maps to `ErrNotFound`;
  401 and 403 map to `ErrForbidden`.
- **`Fetch`:** `Download(id, dest, nil, nil)`. The download quota
  (`remaining_downloads`) is the provider's concern; its errors pass through as
  fetch failures.

## Warm-up (later)

A thoa job takes one input dataset, staged into the VM before the script
runs. Warm-up builds that dataset from the records' thoa files (they are
md5-addressed, so nothing is uploaded again), laid out as `<digest>/<path>`.
In the VM, a `dir` remote at the head of `order` points at the staging
directory. It is the same protocol with one more adaptor, and it waits until
the transparent fetch works.

## Prerequisites (bugs in hapiq today)

- **Relative paths in hapiq.json:** every source must write them relative to
  the folder that holds hapiq.json (manifest-001, `fix/witness-relative-paths`).
- **`--skip-existing`:** it leaves files already present out of hapiq.json
  `files` (`FilesSkipped`), so the file set is incomplete. They must be
  listed.
- **Filters in `manifest gen`:** gen must keep them (manifest-002,
  `fix/manifest-gen-options`). The canonical filters for the lookup key reuse
  that mapping.
- **`--timeout`:** it must be a stall limit everywhere
  (`fix/manifest-stall-timeout`).

## Open questions for thoa

1. ~~Uploads fail with `Provider 'local' is not configured`~~: hotfixed on
   2026-10-29.
2. Can a job download datasets that a different credential (the server's
   key) uploaded? This is trivial with one account; with sharing, it is
   question 5. And how do the job's credentials appear in the VM?
3. Does `GET /files` return each file's md5 and size?
4. Are datasets permanent, or can they expire?
5. Sharing: does another account get 404 or 403 for an unshared id? Will
   shared datasets download through the same API?
6. Warm-up: can a dataset be built from existing file ids with new paths?
7. Publish `thoa-go` on GitHub; until then, use a `replace` directive.

## Non-goals

- General sync to S3, SFTP and the like: use rclone on a hapiq output folder.
- Remote delete or garbage collection. `invalidate` removes records only, not
  thoa datasets.
- Listing or discovering datasets on thoa: the records are the only index,
  since thoa datasets carry no name.
- Several digests per key, or picking between revisions: a key has one
  pinned version, and you change it with `invalidate`.

## Implementation order

1. The prerequisite fixes above.
2. `pkg/remote`: types, `Digest` with test vectors, canonical filters,
   registry, and the hash check, tested against a fake `Remote`.
3. Lock mode, the MVP: `hapiq.lock` read and write; fetch resolution with
   the hard failure in `download`, `manifest get` and `refetch`;
   `hapiq lock add`, `lock` and `lock update`.
4. `pkg/remote/thoa`, then a beta run in a monorepo (plan and modules in
   one tree).
5. Local `index.db` records, `push_on_fetch`, `hapiq push`,
   `hapiq remote ls` and `invalidate`.
6. Server: `/v1/remotes` with the fill worker, `write_token`, `routes`,
   `autotls`, and `hapiq remote warm`; document them in
   [lan-sharing.md](lan-sharing.md).
7. Warm-up with the `dir` adaptor.

Steps 2 and 3 can be built against the fake remote. The upload blocker
(question 1) has been hotfixed. For the MVP, the job's thoa credentials come
from environment variables (question 2).
