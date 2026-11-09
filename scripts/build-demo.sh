#!/usr/bin/env bash
# Builds the public demo from invented people only. The script takes no input
# file: the fixture generator is the only source of the data.
set -euo pipefail

out=${1:-dist-demo}
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cd "$root"
go build -trimpath -o "$work/heirfold" ./cmd/heirfold

(cd web && npm ci --ignore-scripts && npm run build:demo)

rm -rf "$out"
mkdir -p "$out"
cp -R web/dist-demo/. "$out/"
mkdir -p "$out/snapshot"

for locale in ru en; do
  "$work/heirfold" fixture --locale "$locale" --out "$work/$locale.gramps"
  "$work/heirfold" import --data "$work/$locale" "$work/$locale.gramps" >/dev/null
  for role in guest member; do
    "$work/heirfold" snapshot --data "$work/$locale" --role "$role" --out "$out/snapshot/$locale-$role.json"
    chmod 644 "$out/snapshot/$locale-$role.json"
  done
done

echo "Demo written to $out"
