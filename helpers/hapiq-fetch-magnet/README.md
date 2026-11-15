# hapiq-fetch-magnet

BitTorrent transfer helper for hapiq's peer cache. Prototype.

It is a separate binary and a separate Go module on purpose: the torrent
library (about 30 MB linked) never appears in hapiq's `go.mod`. hapiq finds the
helper on `PATH` and talks to it over a pipe. Without the helper, hapiq behaves
exactly as before.

```bash
make magnet-helper     # builds bin/hapiq-fetch-magnet
make magnet-e2e        # end-to-end suite, loopback only
```

## How the pieces fit

```
node A                                        node B
  hapiq cache serve  ── /v1/resolve ────────▶  hapiq download …
        │ reads                                    │ spawns, JSON lines on stdin/stdout
  <cache>/magnets/<sha256>                         ▼
        ▲ writes                               hapiq-fetch-magnet
  hapiq-fetch-magnet seed  ◀── BitTorrent ──────────┘
```

1. `hapiq-fetch-magnet seed --cache-dir DIR --listen HOST:PORT --advertise HOST:PORT`
   serves every blob in a hapiq cache and writes one magnet per blob to
   `DIR/magnets/<sha256>`. It removes them on exit. `--advertise` (default:
   `--listen`) goes into every magnet, so the seeder refuses to start when it
   is `0.0.0.0` or `[::]`.
2. `hapiq cache serve` adds that magnet to its `/v1/resolve` answer. It never
   does so when `cache.server.token` is set.
3. A downloading hapiq that gets a magnet and has the helper on `PATH` fetches
   through it, then verifies the sha256 itself. Any failure falls back to the
   peer's `/v1/blob`, then to the origin URL.

The magnet comes from an untrusted peer. hapiq keeps only its infohash and
the trackers (`tr`) and peer addresses (`x.pe`) whose host is the host of the
peer URL that answered; everything else (other hosts, `xs`/`as`/`ws`) is
dropped, so a peer cannot make the helper contact, or announce the user to,
anybody the user did not configure. Run the tracker on the cache's host, and
name the cache by the same host the seeders' `--advertise` and tracker URL
use, or `x.pe` is dropped and only the tracker finds the seeder.

## Fetch protocol (version 1)

One JSON object per line. The helper starts with no arguments, announces its
version, and exits when stdin closes.

```
helper → {"protocol":1}
hapiq  → {"op":"fetch","id":1,"magnet":"magnet:?xt=…","dest":"/abs/path","size":5000123}
helper → {"id":1,"done":true}          or  {"id":1,"error":"…"}
                                       or  {"id":1,"error":"timeout: no metadata after 30s","timeout":true}
hapiq  → {"op":"cancel","id":1}        (optional)
```

- `dest` is the exact file to write. The name inside the torrent is ignored.
- `size` is the expected length; a torrent of any other size, or with more
  than one file, is refused before any bytes are written.
- Fetches run concurrently; replies carry the request `id`. Two fetches of
  the same infohash run one after the other.
- `HAPIQ_MAGNET_TIMEOUT` (default `30s`) bounds the wait for metadata and any
  stretch without progress; such a failure carries `"timeout":true`, and hapiq
  then ignores that peer's magnets for the rest of the run.
- A cancelled fetch still gets its one reply, sent once the helper has let go
  of `dest`. hapiq waits up to 5 s for it, then kills the helper, so nothing
  writes the file after hapiq removes it.
- hapiq hands out no magnets (in `Resolve`) when no helper is installed or the
  helper has exited; the rest of the run uses HTTP.

With `HAPIQ_MAGNET_VERBOSE` set, the helper prints one line on stderr per
file saying where the bytes came from:
`hapiq-fetch-magnet: fetched "<name>" sources=<ip:port>=<bytes>,…`.

## Finding more than one seeder

A magnet always names the seeder that published it (`x.pe`). To let every node
holding the same blob serve it, run a tracker next to the public cache and
point the seeders at it:

```bash
hapiq-fetch-magnet tracker --listen :6969                 # one, next to the cache
HAPIQ_MAGNET_TRACKERS=http://cache.example:6969/announce \
  hapiq-fetch-magnet seed --cache-dir DIR --listen …       # every seeding node
```

The tracker URL is written into the magnets, so a downloading node needs no
configuration; it has to be on the cache's host (see above). The tracker is
in-memory and records only which addresses announced which infohash, under
the address the announce came from. A client on the tracker's own host (the
cache's seeder) may name its address in the announce's `ip`, which the seeder
sets from `--advertise`; a request carrying `X-Forwarded-For` never gets
that privilege. Behind a reverse proxy (the way to serve the tracker over
https), the proxy must set `X-Forwarded-For` and the tracker runs with
`--trust-proxy`, taking the address from the last entry and refusing an
announce whose entry is not a plain IP address. Never use `--trust-proxy`
without a proxy: clients could then name any address. Peers that stop
announcing, and swarms left empty, are forgotten after about two intervals.
Peer exchange is on; DHT is off (see the comment on `newClient` for why).

`make magnet-cloud` runs this as a docker compose cloud (a cache with its
tracker and six rate-limited user nodes on a private network; see
`test/cloud/`).

## Environment

| Variable | Used by | Meaning |
|---|---|---|
| `HAPIQ_MAGNET_TRACKERS` | `seed` | comma-separated tracker announce URLs |
| `HAPIQ_MAGNET_TIMEOUT` | fetch | stall and metadata timeout, default `30s` |
| `HAPIQ_MAGNET_LISTEN` | fetch | BitTorrent listen address, default `0.0.0.0:0` |
| `HAPIQ_MAGNET_VERBOSE` | fetch | print the per-file `sources=` line |
| `HAPIQ_MAGNET_DEBUG` | both | dump the torrent client's status to stderr (implies verbose) |

## Prototype limits

- A fetcher uploads only while it downloads. A node keeps serving a blob only
  if it runs the seed daemon.
- A seeder with a tracker leaves its magnets behind when it stops, so the cache
  keeps pointing at the swarm. If nobody else seeds the blob, a fetcher waits
  out `HAPIQ_MAGNET_TIMEOUT` once, then skips that peer's magnets for the rest
  of the run. (Without a tracker the magnets are withdrawn on exit.)
- The tracker has no access control and no cap: an announce flood holds
  memory for about two intervals. Behind a same-host proxy that sets no
  `X-Forwarded-For` (a plain TCP relay) every client looks local and may
  name any address.
- A verified download the local cache refuses (quota full) is moved to the
  destination; if the two are on different filesystems it is downloaded
  again instead.
- Two fetches of the same blob in one run download it twice, one after the
  other.
- IPv6: the tracker serves both families. A seeder or fetcher listening on an
  IPv4 address (the default `0.0.0.0`) is IPv4 only; listen on `[::]:PORT` for
  IPv6.
- No progress reporting to hapiq.
- A blob removed by cache GC keeps its magnet until the seeder restarts.
