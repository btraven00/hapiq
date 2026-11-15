#!/bin/bash
# Peer-cloud test for magnet transfers: one public cache, N user nodes, each
# with its own IP on a private docker network and a capped upload rate.
#
#   test/cloud/run.sh            run the scenarios, tear the cloud down
#   KEEP=1 test/cloud/run.sh     leave the cloud up afterwards for poking at
#   SIZE=64000000 RATE=32mbit    the defaults (file size in bytes, upload cap)
#
# Needs docker with compose, and a Go toolchain to build the two binaries.
set -euo pipefail
cd "$(dirname "$0")"

export SIZE=${SIZE:-64000000} RATE=${RATE:-32mbit}
URL=http://origin:8080/data.bin
dc() { docker compose "$@"; }

fail=0
check() { # check <description> <command...>
    local what=$1
    shift
    if "$@"; then echo "  ok    $what"; else
        echo "  FAIL  $what"
        fail=1
    fi
}

echo "== build"
repo=$(cd ../.. && pwd)
CGO_ENABLED=0 go -C "$repo" build -o "$PWD/bin/hapiq" .
CGO_ENABLED=0 go -C "$repo/helpers/hapiq-fetch-magnet" build -o "$PWD/bin/hapiq-fetch-magnet" .
dc build -q

[ -n "${KEEP:-}" ] || trap 'dc down -v -t 1 --remove-orphans >/dev/null 2>&1' EXIT
dc down -v -t 1 --remove-orphans >/dev/null 2>&1 || true
dc up -d --scale peer=6 >/dev/null 2>&1

CACHE_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$(dc ps -q cache)")
logs=$(mktemp -d)

# download <name> <compose exec args...>: run hapiq download on a node. Sets
# $secs (wall time), $got (sha256 of the result) and $sources (one
# "ip:port=bytes" line per torrent peer that supplied data; empty for HTTP).
download() {
    local log=$logs/$1.log start end
    shift
    start=$(date +%s.%N)
    dc exec -T "$@" sh -c "rm -rf /out; hapiq download url $URL --out /out" >"$log" 2>&1 || true
    end=$(date +%s.%N)
    secs=$(echo "$end $start" | awk '{printf "%.1f", $1-$2}')
    sources=$(sed -n 's/.*sources=//p' "$log" | tr ',' '\n' | sed '/^$/d')
    got=$(sha_on "${@: -3}")
}
sha_on() { dc exec -T "$@" sh -c 'sha256sum /out/data.bin 2>/dev/null | cut -d" " -f1' || true; }
nsources() { echo "$sources" | sed '/^$/d' | cut -d: -f1 | sort -u | wc -l; }
from_cache() { echo "$sources" | grep -q "^$CACHE_IP:"; }
only_from_cache() { [ "$(nsources)" = 1 ] && from_cache; }
only_from_swarm() { [ "$(nsources)" -ge 1 ] && ! from_cache; }
resolve() { dc exec -T cache curl -s "http://localhost:7777/v1/resolve?url=$URL"; }
has_magnet() { resolve | grep -q magnet; }
show_sources() { echo "$sources" | sed 's/^/          /'; }

echo "== 1. the cache fills from the origin"
for _ in $(seq 100); do dc exec -T origin test -s /srv/data.bin 2>/dev/null && break; sleep 0.2; done
sleep 1
WANT=$(dc exec -T origin sha256sum /srv/data.bin | cut -d' ' -f1)
download cache --index 1 cache
check "cache has the file (${secs}s)" [ "$got" = "$WANT" ]
for _ in $(seq 60); do has_magnet && break; sleep 0.5; done
check "cache hands out a magnet" has_magnet

echo "== 2. origin goes away; from here on every byte must come from the cloud"
dc stop -t 1 origin >/dev/null 2>&1

echo "== 3. first user: the cache's seeder is the only source"
download p1 --index 1 peer
t_single=$secs
check "peer 1 got the file (${secs}s)" [ "$got" = "$WANT" ]
check "from exactly one source, the cache" only_from_cache

echo "== 4. two more users at once"
download p2 --index 2 peer &
download p3 --index 3 peer
wait
for i in 2 3; do
    check "peer $i got the file" [ "$(sha_on --index $i peer)" = "$WANT" ]
done

# Users 1-3 now hold the blob; wait for their seeders to pick it up and announce.
for i in 1 2 3; do
    for _ in $(seq 60); do dc exec -T --index $i peer sh -c 'ls /cache/magnets/* >/dev/null 2>&1' && break; sleep 0.5; done
done
sleep 2

echo "== 5. a later user: should be served by several nodes together"
download p4 --index 4 peer
t_swarm=$secs
check "peer 4 got the file (${secs}s)" [ "$got" = "$WANT" ]
check "from more than one node ($(nsources) sources)" [ "$(nsources)" -gt 1 ]
show_sources

echo "== 6. the cache's seeder is stopped cleanly (its magnet stays; the swarm lives on)"
dc exec -T cache sh -c 'pid=$(cat /run/seed.pid); kill "$pid"; while grep -q . "/proc/$pid/cmdline" 2>/dev/null; do sleep 0.2; done' # nobody reaps it: a zombie has no cmdline
check "cache still resolves with a magnet" has_magnet
download p5 --index 5 peer
check "peer 5 got the file (${secs}s)" [ "$got" = "$WANT" ]
check "from the swarm, not the cache ($(nsources) sources)" only_from_swarm
show_sources

echo "== 7. a user without the helper: plain HTTP from the cache, as before"
download p6 -e PATH=/usr/local/bin:/usr/bin:/bin --index 6 peer
t_http=$secs
check "peer 6 got the file (${secs}s)" [ "$got" = "$WANT" ]
check "without any torrent source" [ "$(nsources)" = 0 ]

echo
echo "== timings for $((SIZE / 1000000)) MB with each node's upload capped at $RATE"
printf '  %-22s %6ss\n' "HTTP from the cache" "$t_http" "magnet, one seeder" "$t_single" "magnet, swarm" "$t_swarm"
echo "  logs: $logs"
[ "$fail" = 0 ] && echo "PASS" || echo "FAIL"
exit "$fail"
