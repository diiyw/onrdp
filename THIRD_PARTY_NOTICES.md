# Third-party notices

OnRDP uses [Onix v0.1.5](https://github.com/onix-engine/onix/releases/tag/v0.1.5) (MIT) through Go modules. Its source and version, and those of the other Go dependencies, are recorded in `go.mod` and `go.sum`.

The RDP protocol engine is [FreeRDP](https://github.com/FreeRDP/FreeRDP), including WinPR, under Apache-2.0. OnRDP links directly to its libraries through cgo. The former gopher-rdp implementation has been removed. The native bridge in `internal/rdpclient` implements software framebuffer delivery, input, text clipboard integration and connection lifecycle; no external FreeRDP desktop client is launched.

The macOS dependency build script pins FreeRDP 3.33.0 and verifies the source archive SHA-256. The release package contains the native libraries required by that build, including OpenSSL (Apache-2.0), together with their upstream license texts and a version/source manifest under `OnRDP.app/Contents/Resources/NativeLicenses`. FreeRDP's upstream source is available at <https://github.com/FreeRDP/FreeRDP/tree/3.33.0>; OpenSSL's source URL is recorded in the manifest. System libraries and frameworks are supplied by macOS.

The bundled SVG icons in `resources/app/icons` are drawn for OnRDP and released under the same license as the rest of the project; the notice is in `resources/app/icons/LICENSE`.

The floating toolbar uses [Phosphor Icons](https://github.com/phosphor-icons/core) (MIT). The upstream copyright and license are included in `resources/app/icons/PHOSPHOR-LICENSE`.
