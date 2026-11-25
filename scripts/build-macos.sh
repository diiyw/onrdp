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
# The build-only Onix editor has dependencies outside the application graph.
build_deps="$(mktemp -d)"
trap 'rm -rf "$build_deps"' EXIT
cp go.mod "$build_deps/build.mod"
cp go.sum "$build_deps/build.sum"
go run -mod=mod -modfile="$build_deps/build.mod" ./scripts/package-macos.go
if [[ -n "${ONRDP_RELEASE_VERSION:-}" ]]; then
    plist=dist/macos/OnRDP.app/Contents/Info.plist
    /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $ONRDP_RELEASE_VERSION" "$plist"
    /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $ONRDP_RELEASE_VERSION" "$plist"
fi
go run ./scripts/check-resources.go dist/macos/OnRDP.app/Contents/Resources
arch="${GOARCH:-$(go env GOARCH)}"
case "$arch" in
    arm64) scrcpy_target=macos-aarch64 ;;
    amd64) scrcpy_target=macos-x86_64 ;;
    *) echo "Unsupported scrcpy architecture: $arch" >&2; exit 1 ;;
esac
/usr/bin/python3 scripts/bundle-scrcpy.py "$scrcpy_target" dist/macos/OnRDP.app/Contents/Resources
/usr/bin/python3 scripts/bundle-rustdesk.py "macos-$arch" dist/macos/OnRDP.app
/usr/bin/python3 scripts/bundle-macos.py dist/macos/OnRDP.app
archive="$root/dist/macos/OnRDP-macos-$arch-FreeRDP.zip"
ditto -c -k --keepParent "$root/dist/macos/OnRDP.app" "$archive"
echo "$archive"
