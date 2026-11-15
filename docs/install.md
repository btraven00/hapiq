# Installation

## go install (recommended)

```bash
go install github.com/btraven00/hapiq@latest
```

Requires Go 1.24+. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`).

## Nix

With flakes enabled:

```bash
nix run github:btraven00/hapiq -- download geo GSE133344 --out ./data --dry-run   # run without installing
nix profile install github:btraven00/hapiq                                        # install into your profile
```

Append `/<commit>` to pin a commit (releases up to v0.1.4 have no flake). In another flake, use
`inputs.hapiq.url = "github:btraven00/hapiq";` and `hapiq.packages.${system}.default`.
The binary reports its version as `devel-<commit>`.

## Nightly binary

Pre-built binaries are published nightly for Linux, macOS, and Windows:

```bash
# Linux amd64
curl -L https://github.com/btraven00/hapiq/releases/download/nightly/hapiq-linux-amd64.tar.gz \
  | tar -xz
sudo mv hapiq-linux-amd64 /usr/local/bin/hapiq

# macOS arm64 (Apple Silicon)
curl -L https://github.com/btraven00/hapiq/releases/download/nightly/hapiq-darwin-arm64.tar.gz \
  | tar -xz
sudo mv hapiq-darwin-arm64 /usr/local/bin/hapiq
```

Windows: download `hapiq-windows-amd64.zip` from the [nightly release](https://github.com/btraven00/hapiq/releases/tag/nightly), extract, and add to `PATH`.

## From source

```bash
git clone https://github.com/btraven00/hapiq.git
cd hapiq
go build -o hapiq .
```
