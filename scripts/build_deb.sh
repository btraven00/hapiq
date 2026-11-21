#!/bin/sh
# Build dist/<pkg>_<version>_<arch>.deb from a static binary.
# Usage: scripts/build_deb.sh <pkg> <version> <goarch>
#   pkg:    hapiq | hapiq-fetch-magnet
#   goarch: amd64 | arm64 (Go and Debian agree on these names)
set -eu
PKG=$1 VERSION=$2 ARCH=$3
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

case $PKG in
  hapiq)
    SRC=. REL=Suggests: REL_PKG=hapiq-fetch-magnet
    LDFLAGS="-s -w -X github.com/btraven00/hapiq/internal/version.Version=$VERSION"
    DESC="download datasets from scientific repositories
 Hapiq downloads datasets from scientific repositories (GEO, SRA/ENA,
 Zenodo, Figshare, Ensembl and others) with provenance tracking." ;;
  hapiq-fetch-magnet)
    SRC=helpers/hapiq-fetch-magnet REL=Enhances: REL_PKG=hapiq
    LDFLAGS="-s -w"
    DESC="BitTorrent transfer helper for hapiq
 Lets hapiq fetch and seed datasets over BitTorrent through its peer cache.
 hapiq finds it on PATH; without it, hapiq works as before." ;;
  *) echo "unknown package: $PKG" >&2; exit 2 ;;
esac

mkdir -p "$ROOT/DEBIAN" "$ROOT/usr/bin" "$ROOT/usr/share/doc/$PKG" dist
OUT=$(pwd)/dist
(cd "$SRC" && CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH \
  go build -trimpath -ldflags="$LDFLAGS" -o "$ROOT/usr/bin/$PKG" .)
cp "$SRC/README.md" "$ROOT/usr/share/doc/$PKG/"
cat > "$ROOT/usr/share/doc/$PKG/copyright" <<EOC
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Source: https://github.com/btraven00/hapiq
License: GPL-3.0-or-later
 See /usr/share/common-licenses/GPL-3.
EOC

cat > "$ROOT/DEBIAN/control" <<EOC
Package: $PKG
Version: $VERSION
Architecture: $ARCH
Maintainer: ben <ben.uzh@proton.me>
Section: science
Priority: optional
Homepage: https://github.com/btraven00/hapiq
$REL $REL_PKG
Description: $DESC
EOC

dpkg-deb --root-owner-group --build "$ROOT" "$OUT/${PKG}_${VERSION}_${ARCH}.deb"
