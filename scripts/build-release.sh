#!/usr/bin/env bash
# Builds static binaries for all release platforms.
set -euo pipefail

version=${1:?usage: build-release.sh VERSION}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

(cd web && npm ci --ignore-scripts && npm run build)

rm -rf dist
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  goos=${target%/*}
  goarch=${target#*/}
  suffix=""
  if [ "$goos" = "windows" ]; then suffix=".exe"; fi
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath \
    -ldflags "-s -w -X main.version=$version" \
    -o "dist/heirfold-$goos-$goarch$suffix" ./cmd/heirfold
done
(cd dist && sha256sum heirfold-* >SHA256SUMS)
