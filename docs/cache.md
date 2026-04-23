# Local download cache

Hapiq can cache downloaded files locally so that re-running a download — into a
fresh output directory, on a different machine in the same lab, or after
deleting the original data — does not hit the network again.

The cache is **opt-in** and transparent: enable it in your config file and
existing `hapiq download` commands work unchanged.

## Enabling the cache

Create `~/.hapiqrc` (TOML):

```toml
[cache]
mode = "on"
```

That is the minimal config. On first use hapiq creates `~/.cache/hapiq/` and
starts populating it.

## How it works

Files are stored in a content-addressable store keyed by their SHA-256 hash,
so identical files fetched from different mirror URLs are stored only once.

When `mode = "on"`:

1. Before downloading a file, hapiq looks up its URL in a local index.
2. **Hit** — the file is materialized into the output directory via a hardlink
   (or reflink on Btrfs/XFS, symlink if cross-device, plain copy as last
   resort). No network traffic.
3. **Miss** — hapiq streams the file, computes SHA-256 on the fly, stores the
   blob, then materializes it.

The `hapiq.json` witness file records `"cache_hit": true` for each file served
from cache, so provenance remains accurate.

## Full config reference

Place in `~/.hapiqrc`:

```toml
[cache]
mode          = "on"              # "on" | "off"  (default: off)
dir           = "~/.cache/hapiq" # where blobs and the index live
link_strategy = "auto"           # "auto" | "hardlink" | "symlink" | "copy"
                                 # auto = reflink > hardlink > symlink > copy
max_size      = "50GB"           # "" or 0 disables quota
min_free_disk = "5GB"            # refuse new blobs if disk would drop below this
quota_policy  = "lru"            # only "lru" supported for now
```

`link_strategy = "auto"` is almost always the right choice. Set it to
`"copy"` if you need the output directory to be fully self-contained (e.g. for
archiving to tape or sharing on a network share).

## Cache layout

```
~/.cache/hapiq/
├── index.db          # SQLite: URL→hash index and blob metadata
├── blobs/
│   └── sha256/
│       └── ab/
│           └── ab12cd...  # blob file, first 2 chars of hash as shard dir
└── tmp/              # partial downloads, cleaned up on success or failure
```

The index holds per-blob size, creation time, and last-used timestamp (for LRU
eviction). It also records the canonical URL each blob was fetched from, so the
same file accessible from multiple mirrors is still only downloaded once.

## Managing the cache

```bash
hapiq cache info               # size, blob count, quota usage
hapiq cache list               # tabular view: hash, size, last used, URL
hapiq cache list --url '*/zenodo*'   # filter by URL glob
hapiq cache list --json        # JSON output for scripting

hapiq cache verify             # re-hash all blobs; evict corrupt ones
hapiq cache verify <sha256>    # check a single blob

hapiq cache gc                 # evict LRU blobs until under quota
hapiq cache gc --dry-run       # show what would be removed
hapiq cache gc --keep 7d       # spare blobs used in the last 7 days

hapiq cache evict <sha256>     # remove a specific blob and its URL mappings
hapiq cache prune-urls         # clean up index entries whose blobs are missing
```

All commands accept `--cache-dir <path>` to target a non-default store.

## Quota enforcement

If `max_size` is set, hapiq refuses to admit a new blob that would push the
total over the limit. It does **not** silently evict anything on your behalf
during a download — it just skips caching that file and downloads it normally.

Run `hapiq cache gc` manually (or from a cron job) to bring the cache back
under quota.

`min_free_disk` is a separate safety net: regardless of `max_size`, hapiq
will not store a blob if the filesystem would drop below this threshold.

## Verifying a known hash

If you know the expected hash of a file in advance (from a published checksum,
a previous trusted download, or a YAML spec), pass it with `--hash`:

```bash
hapiq download scperturb NormanWeissman2019 --out ./data \
  --hash sha256:4f23bc...

hapiq download zenodo 10.5281/zenodo.3242074 --out ./data \
  --hash md5:d41d8c...
```

Supported algorithms: `sha256`, `md5`.

`--hash` is only valid for **single-file downloads**. If the download resolves
to more than one file, hapiq exits with an error before writing anything.

On a mismatch hapiq removes the file from the output directory and reports both
the expected and actual hash. On a match the hash is logged alongside the file
path.

For sha256, hapiq reuses the hash already computed during streaming — no
second read of the file. For md5, one additional read is required.

## Supported sources

Caching is active for **Zenodo**, **Figshare**, and **scPerturb** downloads.
GEO and SRA (which involve many small files and complex path structures) are
not yet cached and always hit the network.
