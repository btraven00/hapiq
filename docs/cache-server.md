# Minimal cache mode for hapiq

## Context

Today every downloader in `pkg/downloaders/*` performs its own `http.Get` +
`io.Copy(os.Create(...))`, so every new `hapiq download` re-fetches bytes from
the network even if the same URL was pulled five minutes ago. On a workstation
this is wasted bandwidth; across a lab of collaborators pulling the same public
datasets, it's wasted wall time and repeated load on public endpoints (GEO,
Zenodo, scPerturb, etc.).

We want an opt-in cache that:

1. **Dedupes bytes locally.** Cached files are materialized into the download
   target dir via reflink/hardlink, so repeated downloads are effectively free
   and consume no extra disk.
2. **Can be shared across nodes** later. A small REST server serves cached
   blobs by content hash to other hapiq instances, so a lab shares one copy.

First iteration is strictly local (cache + transparent lookup). The server is
specified but not implemented yet.

## Design decisions (user-confirmed)

- **Surface**: transparent via config only — no new subcommand for v1. When
  `cache.mode = "on"`, existing `hapiq download` consults the cache first.
- **Key model**: CAS blobs keyed by sha256 + a URL→hash index table. Enables
  dedup across mirror URLs and integrity by construction.
- **Materialization**: try `ioctl_ficlone` (reflink) → `link(2)` (hardlink) →
  `symlink` → copy. Cross-device falls through to symlink with a warning.
- **REST server**: sketched in this plan, implemented in a follow-up.

## Config

Extend the existing Viper setup (`cmd/root.go:54-76`). Viper already supports
TOML/YAML/JSON; add TOML to the list of recognized config names.

Search order (first match wins):

1. `--config <path>` flag
2. `$HOME/.hapiqrc` (TOML)
3. `$HOME/.hapiq.yaml` (existing)
4. `/etc/hapiq/config.toml`

Cache-related keys:

```toml
[cache]
mode = "on"                 # "on" | "off" (default off)
dir  = "~/.cache/hapiq"     # root of the CAS + sqlite db
link_strategy = "auto"      # "auto" | "hardlink" | "symlink" | "copy"
# auto = reflink > hardlink > symlink > copy

[cache.server]
enabled = false
listen  = "127.0.0.1:7777"
peers   = []                # list of base URLs for upstream cache peers
```

## Cache layout

```
~/.cache/hapiq/
├── index.db               # sqlite: urls, blobs, provenance
├── blobs/
│   └── sha256/
│       ├── ab/
│       │   └── ab12cd...  # full hash as filename, first 2 chars as shard dir
│       └── ...
└── tmp/                    # partial downloads, atomically moved into blobs/ on success
```

### sqlite schema (`index.db`)

```sql
CREATE TABLE blobs (
  sha256      TEXT PRIMARY KEY,      -- hex
  size        INTEGER NOT NULL,
  created_at  INTEGER NOT NULL,      -- unix seconds
  last_used   INTEGER NOT NULL,
  ref_count   INTEGER NOT NULL DEFAULT 0   -- for future GC
);

CREATE TABLE urls (
  url           TEXT PRIMARY KEY,    -- canonicalized
  sha256        TEXT NOT NULL REFERENCES blobs(sha256),
  etag          TEXT,
  last_modified TEXT,
  fetched_at    INTEGER NOT NULL
);

CREATE INDEX urls_by_hash ON urls(sha256);
```

URL canonicalization: lowercase scheme+host, strip default ports, drop
fragment, leave query string as-is (some endpoints rely on it).

## Code structure

New package: `pkg/cache/`

- `pkg/cache/cache.go` — `Cache` struct, `Open(dir string) (*Cache, error)`,
  `Get(ctx, url) (path string, hit bool, err error)`,
  `Put(ctx, url, srcPath, sha256) error`,
  `Materialize(hash, destPath string, strategy Strategy) error`.
- `pkg/cache/sqlite.go` — schema migration + prepared statements. Use
  `modernc.org/sqlite` (pure Go, no cgo — matches the project's current
  cgo-free profile; confirm when adding to `go.mod`).
- `pkg/cache/link.go` — `reflink`, `hardlink`, `symlink`, `copyFile`, and a
  `tryLink(src, dst, strategy) error` that walks the fallback chain. Reflink
  via `golang.org/x/sys/unix.IoctlFileClone` on Linux; stubbed on other OSes.
- `pkg/cache/url.go` — canonicalization helpers.
- `pkg/cache/config.go` — typed view over Viper keys.

### Integrating with downloaders

Today each downloader calls `http.Get` directly
(e.g. `pkg/downloaders/scperturb/downloader.go:231-258`,
`pkg/downloaders/geo/downloader.go:526-543`). Introduce a shared helper in
`pkg/downloaders/common/`:

```go
// Fetch downloads url to destPath, consulting the cache if one is configured.
// On cache hit, materializes via link/copy and returns (bytes, true, nil).
// On miss, streams the response to tmp, hashes-as-it-goes, promotes to CAS,
// records the URL→hash mapping, then materializes to destPath.
func Fetch(ctx context.Context, url, destPath string, opts FetchOptions) (n int64, hit bool, err error)
```

The cache handle is resolved once at startup in `cmd/download.go` and passed
through `FetchOptions` (or stashed on a `context.Context` key). When
`cache.mode = "off"`, `Fetch` degenerates to the current behavior.

**Migration scope for v1**: wire `Fetch` into the three downloaders most
likely to hit repeat URLs — `scperturb`, `zenodo`, `figshare`. GEO/SRA have
many tiny files with complex paths and can migrate in a follow-up. The helper
is designed so migration is mechanical.

Integrity: `Fetch` always verifies `sha256(destPath) == index.sha256` after
materialization (cheap for hardlink/reflink; for copy it's already been
computed during streaming). Mismatch → evict blob, re-fetch once, error out
on repeat.

### Witness file

`hapiq.json` gains a `cache_hit: true` flag per `FileWitness` (extend
`pkg/downloaders/interface.go:158`) so provenance records whether the bytes
came from network or cache. The stored sha256 is unchanged — the witness
still reflects ground truth.

## REST server (sketched, not implemented)

Single-binary daemon spawned by `hapiq cache serve` (the one subcommand we
*do* add later — out of scope for this PR but reserving the name).

Endpoints:

| Method | Path                    | Purpose                                       |
|--------|-------------------------|-----------------------------------------------|
| GET    | `/v1/blob/{sha256}`     | Stream blob bytes; `ETag: "sha256:<hex>"`.    |
| HEAD   | `/v1/blob/{sha256}`     | Existence check; returns size + etag.         |
| GET    | `/v1/resolve?url=...`   | `{sha256, size}` for a canonical URL.         |
| GET    | `/v1/healthz`           | Liveness.                                     |

Wire format: raw bytes for blobs; JSON for metadata. Clients verify sha256
while streaming — untrusted server is fine because the hash is the key.

Auth model for v1 daemon: bind to LAN interface + optional shared bearer
token (`cache.server.token` in config). No TLS in-process; expect a reverse
proxy if exposed beyond the lab.

Peer fallback on the *client* side: before hitting the origin URL, if
`cache.server.peers` is set, `GET /v1/resolve?url=...` against each peer; on
hit, stream `/v1/blob/{sha256}` into the local CAS. Peers are just other
hapiq caches — no special role.

## Files to create / modify

- **new** `pkg/cache/cache.go`
- **new** `pkg/cache/sqlite.go`
- **new** `pkg/cache/link.go`
- **new** `pkg/cache/link_linux.go` (reflink via ioctl; build tag)
- **new** `pkg/cache/link_other.go` (stub; build tag)
- **new** `pkg/cache/url.go`
- **new** `pkg/cache/config.go`
- **new** `pkg/cache/size.go` (human-readable size parsing)
- **new** `pkg/cache/gc.go` (LRU eviction, quota enforcement)
- **new** `cmd/cache.go` (`info`, `list`, `verify`, `gc`, `evict`, `prune-urls`)
- **new** `pkg/cache/cache_test.go`, `link_test.go`, `gc_test.go`
- **modify** `cmd/root.go:54-76` — add TOML + `/etc/hapiq/config.toml` +
  `~/.hapiqrc` to Viper search path.
- **modify** `cmd/download.go:424-509` — open the cache once, thread it to
  downloaders via a context key or a new field on `DownloadRequest`.
- **modify** `pkg/downloaders/common/filesystem.go` — add `Fetch` helper
  alongside existing file utils; reuse `CalculateFileChecksum` pattern
  (lines 231-245).
- **modify** `pkg/downloaders/interface.go:158` — add `CacheHit bool` to
  `FileWitness`.
- **modify** `pkg/downloaders/scperturb/downloader.go:231-258`,
  `pkg/downloaders/zenodo/...`, `pkg/downloaders/figshare/...` — route
  through `common.Fetch`.
- **modify** `go.mod` — add `modernc.org/sqlite`.

## Verification

1. **Unit tests**
   - `link_test.go`: create two tempdirs on the same fs, verify hardlink path;
     simulate cross-device by pointing at `/tmp` vs a tmpfs mount (skipped if
     unavailable); verify copy fallback.
   - `cache_test.go`: `Put` a synthetic blob, `Get` returns hit; sha256
     mismatch triggers eviction; URL canonicalization round-trips.
2. **End-to-end**
   - `cache.mode = off`: `hapiq download GSE12345` behaves identically to
     today; no `~/.cache/hapiq` created.
   - `cache.mode = on`, first run: populates `~/.cache/hapiq/blobs/sha256/…`
     and `index.db`; witness records `cache_hit: false`.
   - Same command second run into a fresh `--output` dir: no network traffic
     (verify with `strace -e trace=connect` or by pointing at an unreachable
     mirror); files materialized as hardlinks (`stat -c '%h' file` shows link
     count ≥ 2); witness records `cache_hit: true`.
   - Output on a different filesystem (e.g. tmpfs): falls back to symlink
     with a logged warning; `readlink` resolves into the CAS.
3. **Integrity**
   - Corrupt a blob by hand (`printf x >> ~/.cache/hapiq/blobs/sha256/ab/…`);
     next download detects mismatch, evicts, re-fetches, succeeds.
4. **Existing test suite** — `go test ./...` remains green after the
   interface additions.

## Cache management commands

Even though lookup is transparent, operators need a way to inspect and control
the store. A `hapiq cache` subcommand is added in v1 (limited surface; `serve`
lands with the server follow-up).

| Command                          | Behavior                                                                 |
|----------------------------------|--------------------------------------------------------------------------|
| `hapiq cache info`               | Print cache dir, total size, blob count, quota, % full, sqlite version.  |
| `hapiq cache list [--url GLOB]`  | Tabular listing: sha256, size, last_used, url(s). Supports `--json`.     |
| `hapiq cache verify [--all\|sha]`| Re-hash blobs, compare against index; report/evict corrupt entries.      |
| `hapiq cache gc`                 | Evict until under quota (LRU by `last_used`). `--dry-run`, `--keep <dur>`. |
| `hapiq cache evict <sha\|--url>` | Remove a specific blob and its URL mappings.                             |
| `hapiq cache prune-urls`         | Drop URL rows whose blobs are missing (index hygiene).                   |
| `hapiq cache serve`              | *(follow-up)* Start the REST server.                                     |

All commands operate against the cache dir resolved from config/flags; a
`--cache-dir` override is accepted for scripting.

## Quota and eviction

Config keys:

```toml
[cache]
max_size      = "50GB"   # "" or 0 disables the quota
quota_policy  = "lru"    # "lru" | "fifo" (default lru)
min_free_disk = "5GB"    # refuse to admit new blobs if the fs would drop below this
```

Parsing: `pkg/cache/size.go` — accept `B/KB/MB/GB/TB`, binary
(`KiB/MiB/...`), and raw byte counts.

Enforcement points:

1. **On `Put`** — before promoting a tmp blob into `blobs/`, check
   `current_size + blob_size > max_size`. If so, run inline LRU eviction
   until it fits; if the single blob exceeds `max_size`, refuse with a clear
   error (the user's quota is too small for the dataset).
2. **On `Put`** — refuse if the fs would dip below `min_free_disk`,
   regardless of quota. Prevents filling the disk even when the user set a
   generous quota.
3. **Opportunistic `gc`** — `hapiq cache gc` is the explicit path. No
   background daemon in v1 (keep the code simple; revisit if inline eviction
   causes tail latency).

Eviction touches both tables transactionally: delete rows from `blobs`,
cascade-delete from `urls` via foreign key, then `unlink(2)` the file.
Orphan files (crash between sqlite commit and unlink) are cleaned by
`hapiq cache prune-urls` + a filesystem scan in `verify`.

The schema adds one column to support LRU:

```sql
ALTER TABLE blobs ADD COLUMN last_used INTEGER NOT NULL;  -- already in v1 schema above
```

`last_used` is updated on every cache hit (`UPDATE blobs SET last_used=?
WHERE sha256=?`). Kept cheap by batching updates if hit rates get high
(follow-up, only if profiling shows it).

## Out of scope (follow-ups)

- REST server implementation + peer resolution (`hapiq cache serve`).
- Background eviction daemon / scheduled gc.
- GEO/SRA downloader migration to `common.Fetch`.
- Per-blob pinning (manual protection from gc).
