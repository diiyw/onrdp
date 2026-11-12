#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
export FREERDP_PREFIX="${FREERDP_PREFIX:-$root/.native/freerdp}"
export PKG_CONFIG_PATH="$FREERDP_PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
export CGO_ENABLED=1
if [[ "$(uname -s)" != Darwin ]]; then
    echo 'Run this packaging script on macOS with the native architecture.' >&2
    exit 1
fi
pkg-config --atleast-version=3.32 freerdp3
pkg-config --exists freerdp-client3 winpr3
go run ./scripts/package-macos.go
go run ./scripts/check-resources.go dist/macos/OnRDP.app/Contents/Resources
/usr/bin/python3 scripts/bundle-macos.py dist/macos/OnRDP.app
arch="${GOARCH:-$(go env GOARCH)}"
archive="$root/dist/macos/OnRDP-macos-$arch-FreeRDP.zip"
ditto -c -k --keepParent "$root/dist/macos/OnRDP.app" "$archive"
echo "$archive"
