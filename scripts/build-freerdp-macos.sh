#!/bin/bash
# Build only the embedded protocol library, without a separate desktop client.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
prefix="${FREERDP_PREFIX:-$root/.native/freerdp}"
work="$root/.native/build-freerdp"
version=3.33.0
archive="$work/FreeRDP-$version.tar.gz"
mkdir -p "$work" "$prefix"
if [[ ! -f "$archive" ]]; then
    curl --fail --location --retry 3 "https://github.com/FreeRDP/FreeRDP/archive/refs/tags/$version.tar.gz" -o "$archive"
fi
echo "3b5e7733e667ad66f54e0db51d952268c9d08b2e71a4fade44c716ab29dd5657  $archive" | shasum -a 256 -c -
if [[ ! -d "$work/source" ]]; then
    mkdir "$work/source"
    tar -xzf "$archive" -C "$work/source" --strip-components=1
fi
cmake -S "$work/source" -B "$work/build" \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$prefix" \
    -DCMAKE_INSTALL_LIBDIR=lib -DOPENSSL_ROOT_DIR="$(brew --prefix openssl)" \
    -DBUILD_SHARED_LIBS=ON -DWITH_CLIENT_COMMON=ON -DWITH_CLIENT=OFF \
    -DWITH_SERVER=OFF -DWITH_SERVER_CHANNELS=OFF -DWITH_SAMPLE=OFF \
    -DWITH_WINPR_TOOLS=OFF -DWITH_MANPAGES=OFF -DWITH_VERBOSE_WINPR_ASSERT=OFF \
    -DWITH_FFMPEG=OFF -DWITH_SWSCALE=OFF -DWITH_OPENH264=OFF \
    -DWITH_X11=OFF -DWITH_WAYLAND=OFF -DWITH_FUSE=OFF -DWITH_CUPS=OFF \
    -DWITH_PCSC=OFF -DWITH_SMARTCARD_PCSC=OFF -DWITH_PKCS11=OFF \
    -DWITH_AAD=OFF -DWITH_JSON_DISABLED=ON -DWITH_KRB5=OFF \
    -DCHANNEL_URBDRC=OFF -DCHANNEL_SMARTCARD=OFF -DCHANNEL_RDP2TCP=OFF \
    -DCHANNEL_AINPUT=OFF -DCHANNEL_RDPEAR=OFF -DCHANNEL_RDPEAR_CLIENT=OFF \
    -DCHANNEL_WEBAUTHN=OFF -DCHANNEL_TSMF=OFF -DWITH_GSM=OFF -DWITH_OPUS=OFF \
    -DWITH_LAME=OFF -DWITH_FAAD2=OFF -DWITH_FAAC=OFF -DWITH_SOXR=OFF
cmake --build "$work/build" --parallel "$(sysctl -n hw.ncpu)"
cmake --install "$work/build"
mkdir -p "$prefix/share/licenses/freerdp"
cp "$work/source/LICENSE" "$prefix/share/licenses/freerdp/LICENSE"
