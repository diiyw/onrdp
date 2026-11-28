# OnRDP 开发指南

安装和连接步骤见用户指南。[简体中文](../README.zh-CN.md) · [English](../README.md)

## 从源码构建与打包

GitHub Actions 在推送 `main` 或更新拉取请求时构建桌面安装包。在 Actions 页面手动运行 `Build desktop installers` 并填写 `release_tag`（如 `v0.2.0`），所有平台构建成功后会发布到 GitHub Releases。留空时可手动构建并从工作流运行页面下载产物。

需要 Go 1.27+、C 编译器、pkg-config，以及 FreeRDP 3.32+ 的开发库（`freerdp3`、`freerdp-client3`、`winpr3`）。所有协议调用直接链接原生库，构建必须启用 cgo。通过 Go Modules 获取 Onix v0.1.10，不依赖相邻的引擎目录。

### macOS 开发环境

```sh
brew install go pkg-config freerdp
pkg-config --modversion freerdp3 freerdp-client3 winpr3
go test ./...
go run .
```

### Ubuntu / Linux Mint 开发环境

```sh
sudo apt update
sudo apt install build-essential pkg-config freerdp3-dev libwinpr3-dev \
  libx11-dev libxrandr-dev libxext-dev libxrender-dev libxi-dev \
  libwayland-dev libxkbcommon-dev libgl-dev
go test ./...
go run .
```

从仓库根目录运行开发版。`ONRDP_DATA_DIR` 可覆盖数据目录，默认使用系统配置目录下的 `onrdp`。

### 使用带硬解的 FreeRDP

Ubuntu / Linux Mint 的系统 FreeRDP 可能未编入硬解。需要 VA-API 时，可从源码构建 FreeRDP 3.33.0 或更新版本。先安装 `build-essential`、`pkg-config`、`cmake`、`libssl-dev`、`zlib1g-dev`、`libavcodec-dev`、`libavutil-dev` 和 `libswscale-dev`，然后在 FreeRDP 源码目录执行以下命令。此示例使用 TLS 和基于 NTLM 的 NLA。

```sh
prefix="$PWD/_install"
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
  -DUSE_VERSION_FROM_GIT_TAG=OFF -DBUILD_SHARED_LIBS=ON \
  -DWITH_CLIENT=OFF -DWITH_SERVER=OFF -DWITH_SAMPLE=OFF \
  -DWITH_WINPR_TOOLS=OFF -DWITH_MANPAGES=OFF -DWITH_X11=OFF -DWITH_WAYLAND=OFF \
  -DWITH_KRB5=OFF -DWITH_AAD=OFF -DWITH_JSON_DISABLED=ON \
  -DWITH_FUSE=OFF -DWITH_CUPS=OFF -DWITH_PCSC=OFF -DWITH_SMARTCARD_PCSC=OFF \
  -DCHANNEL_URBDRC=OFF -DCHANNEL_TSMF=OFF -DCHANNEL_RDPECAM=OFF -DCHANNEL_WEBAUTHN=OFF \
  -DWITH_FFMPEG=ON -DWITH_VIDEO_FFMPEG=ON -DWITH_FFMPEG_HWACCEL=ON \
  -DWITH_DSP_FFMPEG=OFF -DWITH_SWSCALE=ON -DWITH_OPENH264=OFF
cmake --build build --parallel 8
cmake --install build
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
export LD_LIBRARY_PATH="$prefix/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
```

在同一终端切回 OnRDP 目录运行 `go run .`。Windows 上构建 FreeRDP 时也使用上述 FFmpeg 视频选项，并设置 `WITH_MEDIA_FOUNDATION=OFF`，使 FreeRDP 选用 FFmpeg 解码器。

### macOS 打包

在对应架构的 Mac 上运行以下命令。脚本从固定版本源码构建 FreeRDP 库，再用 Onix 打包资源、收集原生动态库并签名。

```sh
brew install go pkg-config cmake openssl@3 nasm
scripts/build-freerdp-macos.sh
scripts/build-macos.sh
```

输出为 `dist/macos/OnRDP.app` 和 `dist/macos/OnRDP-macos-arm64-FreeRDP.zip`（Intel Mac 为 `amd64`）。包内采用 ad-hoc 签名，Developer ID 签名及 Apple 公证仍需发行者的证书。

工具条运行时切换的图标在 `resources/app/mod.json` 中显式声明。macOS 构建会读取打包后的资源并解码全部 SVG 图标，缺失或无法解码时停止打包。

使用 `scripts/build-macos.sh` 生成 `.app`。脚本通过 `editor.BuildProject` 生成从源码目录外启动所需的资源挂载入口，并检查非系统 dylib 是否随包提供。

### Linux amd64 打包

在开发环境的基础上，安装 `nasm`、`libva-dev` 和 `libdrm-dev`，再生成便携包和 `.deb` 安装包。

```sh
scripts/build-linux.sh
bash scripts/package-linux-deb.sh 0.2.0
```

将 `0.2.0` 替换为要打包的版本号。产物位于 `dist/linux/`。

### Windows 打包

Windows x64 便携包可在 MSYS2 UCRT64 环境中构建，需要 MinGW-w64、pkg-config、Python 3.11+、PATH 中的 7-Zip，以及用于 Android 解码器构建的 `make` 和 NASM。FreeRDP 和 WinPR 由 pkg-config 提供，打包脚本会从同一 SDK 的 `bin` 收集可执行文件依赖的 DLL。若 SDK 布局不同，可用 `FREERDP_RUNTIME_DIR` 指定 DLL 目录。[Windows 构建工作流](../.github/workflows/build.yml)

```sh
python3 scripts/build-windows.py
```

产物为 `dist/windows/onrdp-windows-<arch>.zip`。构建工作流使用 Inno Setup 将便携包制作成 `.exe` 安装程序。

## 目录

| 路径 | 内容 |
| --- | --- |
| `main.go` | 应用入口与场景路由，只负责把输入转发给 UI 模块 |
| `resources/app` | Onix 模块：`app.html` 界面、`mod.go` 交互逻辑、帧纹理与本地化绑定 |
| `resources/scenes/main.json` | 单场景，挂载 `resources/app` 模块 |
| `internal/rdpclient` | FreeRDP 原生桥接、连接取消与重连、RGBA 脏矩形、输入和剪贴板 |
| `internal/store` | 加密凭据库与设置 |
| `internal/i18n` | 翻译目录与回退规则 |
| `internal/platform` | 桌面存储路径、系统语言、宿主钩子 |
| `scripts` | FreeRDP 依赖构建、Onix 资源打包、macOS 动态库收集与签名 |

## RDP 硬件解码与日志

「均衡」和「高清」在 FreeRDP 支持硬解时自动协商 RDPGFX 的 H.264 AVC420 / AVC444。macOS 发布构建使用系统 VideoToolbox。Linux 和 Windows 可用启用了 FFmpeg 硬件加速的 FreeRDP，默认分别尝试 VA-API 和 DXVA2。实际是否启用硬解取决于服务端发送的编码、客户端显卡及驱动。

硬件设备初始化失败时，FFmpeg 会尝试软件解码。未编入硬解的 FreeRDP、未协商 RDPGFX 的会话和「流畅」档使用传统 RDP 图形路径。遇到显示兼容性问题时，可在启动 OnRDP 前设置 `ONRDP_HW_DECODE=0`，让全部 RDP 会话使用传统路径。

从终端启动时可设置 `WLOG_FILTER=com.freerdp.codec:DEBUG` 查看解码器日志。macOS 的 `VideoToolbox H.264 decoder: hardware` 表示启用了硬解。Linux 可用 `FREERDP_HWCTX_DEVICE` 指定 VA-API 渲染节点。

## RustDesk 核心库

打包脚本校验 RustDesk 1.5.3 官方发行包的 SHA-256，并将匹配架构的核心库和授权信息一并打包。源码开发时可用 `ONRDP_RUSTDESK_LIB` 指定核心库绝对路径。Linux 开发版也可使用系统路径 `/usr/share/rustdesk/lib/librustdesk.so`。使用 RustDesk 官方 `.deb` 时，可先提取核心库，再从仓库目录启动。[RustDesk 1.5.3 发行包](https://github.com/rustdesk/rustdesk/releases/tag/1.5.3)

```sh
dpkg-deb -x rustdesk-1.5.3-x86_64.deb /tmp/onrdp-rustdesk
ONRDP_RUSTDESK_LIB=/tmp/onrdp-rustdesk/usr/share/rustdesk/lib/librustdesk.so go run .
```

OnRDP 将 RustDesk 的 `use-texture-render` 设为 `N`，以便从核心库接收像素缓冲。

## Android 投屏依赖

发布脚本从校验过的 FFmpeg 源码构建 `libonrdp-scrcpy`，从 scrcpy 官方发行包提取配套的 `adb` 和 `scrcpy-server`。它们位于应用的 `tools/scrcpy` 目录，macOS 下该目录位于 `Contents/Resources`。解码库的构建需要 C 编译器、bash、make 和 pkg-config，x86_64 构建还需要 NASM，Linux 构建还需要 libva 和 libdrm 开发文件。

从源码运行时，可设置 `ONRDP_SCRCPY_LIBRARY` 为已构建的共享库路径，`adb` 和 `scrcpy-server` 默认从库所在目录取用。可分别用 `ADB` 和 `SCRCPY_SERVER_PATH` 指定路径。
