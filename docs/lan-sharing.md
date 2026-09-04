# Sharing a cache across a network

Thirty people at a workshop running the same `hapiq download` pull the same
multi-GB dataset thirty times through one venue uplink. A lab fetches the same
public dataset once per machine. In both cases the bytes are already on a box a
few metres away.

`hapiq cache serve` exposes the [local cache](cache.md) over HTTP so other
hapiq instances pull from it instead of from the origin repository.

## Why no trust is needed

Blobs are addressed by their SHA-256, and the client verifies the hash while
streaming. A peer that serves the wrong bytes is detected and skipped, and the
download falls back to the origin URL. That is why peer selection is a plain
"try them in order" loop with no reputation, signatures, or key exchange — the
worst a hostile peer can do is waste one round-trip.

The witness file still records the **origin** URL as `source_url`, so
provenance is unaffected by where the bytes physically came from.

## Serving

```bash
hapiq cache serve                          # binds cache.server.listen
hapiq cache serve --listen 0.0.0.0:7777
```

Endpoints:

| Method     | Path                  | Purpose |
|------------|-----------------------|---------|
| `GET/HEAD` | `/v1/blob/{sha256}`   | Stream a blob. Supports `Range`, so interrupted transfers resume. |
| `GET`      | `/v1/resolve?url=`    | `{sha256, size, filename}` for an origin URL, or 404. |
| `GET`      | `/v1/peers`           | Peers seen recently (the introducer role). |
| `GET`      | `/v1/healthz`         | Liveness. Never requires the token. |

## Finding peers

Three bootstrap mechanisms, which all feed the same peer list. Use whichever
fits; they combine freely.

### 1. Static peers

```toml
[cache.server]
peers = ["http://cache01:7777", "http://cache02:7777", "http://cache03:7777"]
```

List as many as you like. Resolves rotate their starting peer round-robin, so
load spreads across the set instead of always landing on the first entry; within
one resolve the list is a fallback chain, and the first peer that answers wins.
A peer that is unreachable is dropped for the rest of the session, so a stale
entry costs one dial timeout, not one per file.

Always works, including across a VPN. **This is the right answer for a lab or
datacenter**: hosts have stable names, and config management already pushes
`/etc/hapiq/config.toml`, so "one config edit per machine" costs nothing.

If those machines share a filesystem, you may not need a server at all — point
them at one cache directory instead, see [Shared group cache](cache.md#shared-group-cache).

### 2. Introducer

An introducer is **not a separate role or daemon** — it is an ordinary peer
that also answers `/v1/peers`. Every hapiq request carries an `X-Hapiq-Self`
header, so a node that *fetches* from the introducer has thereby announced
itself. There is nothing to register or unregister.

```toml
[cache.server]
enabled     = true                              # share this node's cache
introducers = ["http://cache.lab.example:7777"] # one hostname, gossips the rest
```

`enabled = true` is what makes a node announce itself: it derives its own
address from `listen` (set `advertise` explicitly if that guess is wrong, e.g.
behind a reverse proxy). A node that only consumes stays silent.

Use an introducer when the peer addresses **cannot be known in advance**:
ephemeral batch or autoscaled nodes whose hostnames do not exist when the
config is written, or a room full of laptops nobody wants to configure
individually. It also spreads load — nodes learn about each other, so one
static host does not serve everybody.

With stable, known hosts, a static `peers` list is simpler and does the same
job. Reach for an introducer when that list would be wrong by tomorrow.

### 3. mDNS

```toml
[cache.server]
discover = "mdns"
```

Advertises and browses `_hapiq._tcp.local`, so nodes on the same network
segment find each other with no addresses configured at all. Verify with
`avahi-browse -r _hapiq._tcp`.

Limits worth knowing before relying on it:

- **One broadcast domain only.** mDNS packets carry TTL 255 and are dropped by
  routers. It will not reach another subnet — use an introducer for that.
- **Conference wifi often blocks it.** Many APs rate-limit or drop
  client-to-client multicast, and AP client isolation blocks peering entirely.
- **Every interface is advertised**, so a host with docker bridges offers
  several addresses of which only one works. Unreachable addresses are dropped
  from the peer set after the first failed request, so the cost is one dial
  timeout per session, not per file.

## Crossing NAT

hapiq does no NAT traversal and has no hole punching — it deliberately knows
nothing about NAT. Peers behind separate NATs need an overlay network, which
existing tools already do well:

```bash
tailscale up          # or wg-quick up wg0, or: ssh -L 7777:localhost:7777 host
```

Then point `peers` or `introducers` at the resulting hostname. An introducer
reachable over the overlay bootstraps the whole set.

## Timeouts

```toml
[cache.server]
timeout = "2s"        # or a bare number of seconds
```

One knob sets the base peer deadline: the TCP dial and each `/v1/resolve` or
`/v1/peers` round-trip get exactly this, the TLS handshake gets 2×, and the wait
for response headers gets 3×. Blob transfers have **no** overall deadline —
they are arbitrarily large.

The 2s default is tuned for a LAN. Raise it when peers are reached over a
high-latency link such as a cross-continent VPN, where the default is too tight
and peers get silently pruned as unreachable.

Known ceiling: these deadlines catch a peer that never answers. A peer that
starts sending and then trickles — a laptop that slept mid-transfer — is only
caught by TCP retransmission, roughly 15 minutes, after which the download falls
back to the origin.

## Authentication

```toml
[cache.server]
token = "a-shared-secret"
```

When set, every endpoint except `/v1/healthz` requires
`Authorization: Bearer <token>`, and clients send it automatically. There is no
in-process TLS: put a reverse proxy in front if the port is exposed beyond a
trusted network.

Without a token, anyone who can reach the port can read every blob in the
cache and enumerate the URLs it was fetched from. `hapiq cache serve` warns
about this at startup.

## Full config

```toml
[cache]
mode = "on"          # a peer cache without a local cache still works, but
                     # nothing is retained for the next person

[cache.server]
enabled     = false                # announce this node to introducers
listen      = "0.0.0.0:7777"
advertise   = ""                   # default: derived from listen
token       = ""
peers       = []
introducers = []
discover    = ""                   # "" or "mdns"
timeout     = "2s"                 # base peer deadline
```

## Workshop recipe

Presenter, on ethernet, pre-warms the dataset and serves it:

```bash
hapiq download geo GSE133344 --out ./data      # with cache.mode = "on"
hapiq cache serve --listen 0.0.0.0:7777
```

Everyone else needs one config block in `~/.hapiqrc`:

```toml
[cache]
mode = "on"

[cache.server]
enabled     = true
introducers = ["http://<presenter-ip>:7777"]
```

Then the ordinary command, unchanged:

```bash
hapiq download geo GSE133344 --out ./data
```

Because each attendee sets `enabled = true`, they announce themselves to the
introducer and start serving each other — the presenter's laptop stops being
the bottleneck after the first few downloads.

If the venue's wifi has client isolation, none of this works and no amount of
networking cleverness fixes it. Bring a switch, or USB sticks.
