#!/bin/sh
# Node entrypoint for the peer cloud. Usage: entrypoint.sh origin|cache|peer
set -eu

role=$1

# Cap this node's upload, so that several sources are measurably faster than one.
if [ -n "${RATE:-}" ]; then
    tc qdisc add dev eth0 root tbf rate "$RATE" burst 256kb latency 400ms
fi

seed() {
    exec hapiq-fetch-magnet seed --cache-dir /cache --listen 0.0.0.0:6881 \
        --advertise "$(hostname -i):6881" --interval 2s
}

case "$role" in
origin)
    mkdir -p /srv
    head -c "$SIZE" /dev/urandom > /srv/data.bin
    exec busybox httpd -f -p 8080 -h /srv
    ;;
cache | peer)
    mkdir -p /etc/hapiq /cache
    printf '[cache]\nmode = "on"\ndir = "/cache"\n' > /etc/hapiq/config.toml
    if [ "$role" = peer ]; then
        # A peer knows the public cache and nothing else about the cloud.
        printf '[cache.server]\npeers = ["http://cache:7777"]\n' >> /etc/hapiq/config.toml
    fi
    (seed) &
    echo $! > /run/seed.pid
    if [ "$role" = cache ]; then
        # The tracker runs next to the cache: hapiq follows only trackers on
        # the host of the cache that handed out the magnet.
        hapiq-fetch-magnet tracker --listen 0.0.0.0:6969 --interval 30s &
        exec hapiq cache serve --listen 0.0.0.0:7777
    fi
    exec sleep infinity
    ;;
*)
    echo "unknown role: $role" >&2
    exit 2
    ;;
esac
