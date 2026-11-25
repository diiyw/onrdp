#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd -P)"
version="${1#v}"
out="$root/dist/linux"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

package="$stage/package"
install -d "$package"/{DEBIAN,opt,usr/bin,usr/share/applications,usr/share/icons/hicolor/scalable/apps}
tar -xzf "$out/onrdp-linux-amd64.tar.gz" -C "$package/opt"
ln -s /opt/onrdp/onrdp "$package/usr/bin/onrdp"
mv "$package/opt/onrdp/onrdp.svg" "$package/usr/share/icons/hicolor/scalable/apps/"
rm "$package/opt/onrdp/onrdp.desktop"

cat > "$package/usr/share/applications/onrdp.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=OnRDP
Exec=onrdp
Icon=onrdp
Terminal=false
Categories=Network;RemoteAccess;
EOF

cat > "$package/DEBIAN/control" <<EOF
Package: onrdp
Version: $version
Section: net
Priority: optional
Architecture: amd64
Maintainer: OnRDP <diiyw@users.noreply.github.com>
Depends: libc6 (>= 2.39), libfreerdp3-3 (>= 3.32), libfreerdp-client3-3 (>= 3.32), libwinpr3-3 (>= 3.32), libx11-6, libxrandr2, libxext6, libxrender1, libxi6, libwayland-client0, libwayland-cursor0, libxkbcommon0, libgl1, libgtk-3-0t64, libxfixes3, libxtst6, libxcb-randr0, libxcb-shm0, libpulse0, libgstreamer-plugins-base1.0-0
Recommends: libva2, libva-drm2
Homepage: https://github.com/diiyw/onrdp
Description: Remote desktop and Android mirroring client
 OnRDP supports RDP, RustDesk, and Android screen mirroring over ADB.
EOF

dpkg-deb --root-owner-group --build "$package" "$out/onrdp-linux-amd64.deb"
