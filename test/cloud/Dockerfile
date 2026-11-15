# Test image for the peer cloud (test/cloud/run.sh). Not a release image.
#
# The binaries are built on the host by run.sh and copied in: the cloud is
# rebuilt on every run, and compiling the torrent library inside the image
# would add minutes each time.
FROM debian:trixie-slim

# iproute2: tc, to rate-limit each node. busybox: httpd for the origin.
# hadolint ignore=DL3008
RUN apt-get update \
 && apt-get install -y --no-install-recommends busybox curl iproute2 \
 && rm -rf /var/lib/apt/lists/*

COPY bin/hapiq /usr/local/bin/hapiq
# The helper sits in its own directory so a test can take it off PATH.
COPY bin/hapiq-fetch-magnet /opt/magnet/hapiq-fetch-magnet
COPY entrypoint.sh /entrypoint.sh
ENV PATH=/opt/magnet:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Runs as root on purpose: tc needs it (with CAP_NET_ADMIN) to shape eth0.
ENTRYPOINT ["/entrypoint.sh"]
