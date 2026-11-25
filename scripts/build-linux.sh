#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
if [[ "$(go env GOOS)/$(go env GOARCH)" != linux/amd64 ]]; then
    echo 'The official Linux scrcpy package requires linux/amd64.' >&2
    exit 1
fi
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.10 build -platform linux -mode release "$root"
out="$root/dist/linux"
archive=onrdp-linux-amd64.tar.gz
stage="$(mktemp -d "$out/.bundle.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
mv "$out/$archive" "$stage/$archive"
tar -xzf "$stage/$archive" -C "$stage"
python3 scripts/bundle-scrcpy.py linux-x86_64 "$stage/onrdp"
python3 scripts/bundle-rustdesk.py linux-amd64 "$stage/onrdp"
tar -C "$stage" -czf "$stage/$archive" onrdp
mv "$stage/$archive" "$out/$archive"
(cd "$out" && sha256sum "$archive" > SHA256SUMS)
echo "$out/$archive"
