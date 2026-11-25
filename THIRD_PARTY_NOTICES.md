# Third-party notices

OnRDP uses [Onix v0.1.10](https://github.com/onix-engine/onix/releases/tag/v0.1.10) (MIT) through Go modules. Its source and version, and those of the other Go dependencies, are recorded in `go.mod` and `go.sum`.

The RDP protocol engine is [FreeRDP](https://github.com/FreeRDP/FreeRDP), including WinPR, under Apache-2.0. OnRDP links directly to its libraries through cgo. The former gopher-rdp implementation has been removed. The native bridge in `internal/rdpclient` implements software framebuffer delivery, input, text clipboard integration and connection lifecycle; no external FreeRDP desktop client is launched.

RustDesk connections use the RustDesk 1.5.3 core library under AGPL-3.0. OnRDP loads the bundled library at runtime and uses the upstream Flutter Rust Bridge 1.80 C ABI for session events, pixels, and input. The package contains the license text and a manifest with the exact source revision, build recipe, and verified upstream archive digest under `NativeLicenses/rustdesk`. [RustDesk 1.5.3 release](https://github.com/rustdesk/rustdesk/releases/tag/1.5.3)

The macOS dependency build script pins FreeRDP 3.33.0 and verifies the source archive SHA-256. The release package contains the native libraries required by that build, including OpenSSL (Apache-2.0), together with their upstream license texts and a version/source manifest under `OnRDP.app/Contents/Resources/NativeLicenses`. FreeRDP's upstream source is available at <https://github.com/FreeRDP/FreeRDP/tree/3.33.0>; OpenSSL's source URL is recorded in the manifest. System libraries and frameworks are supplied by macOS.

The bundled SVG icons in `resources/app/icons` are drawn for OnRDP and released under the same license as the rest of the project; the notice is in `resources/app/icons/LICENSE`.

The floating toolbar uses [Phosphor Icons](https://github.com/phosphor-icons/core) (MIT). The upstream copyright and license are included in `resources/app/icons/PHOSPHOR-LICENSE`.

Android mirroring uses the Android server from [scrcpy 5.0.1](https://github.com/Genymobile/scrcpy/tree/v5.0.1) (Apache-2.0) and its host protocol. The Linux VA-API loader in `native/scrcpy/vaapi_shim.c` is adapted from the same release. The OnRDP decoder links [FFmpeg 9.0.2](https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz) (LGPL-2.1-or-later). The build recipe in `scripts/bundle-scrcpy.py` verifies both the FFmpeg source and the [scrcpy release archive](https://github.com/Genymobile/scrcpy/releases/tag/v5.0.1). The decoder, adb, server, and license texts are packaged in `tools/scrcpy`.
