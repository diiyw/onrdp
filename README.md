# OnRDP

使用 [Onix v0.1.10](https://github.com/onix-engine/onix/releases/tag/v0.1.10) 引擎构建的 Windows 远程桌面（RDP）与 Android 投屏客户端。[FreeRDP](https://github.com/FreeRDP/FreeRDP) 负责 RDP 协议，Go/cgo 管理帧缓冲与连接生命周期，Onix 原生 HTML/CSS 渲染界面；没有 WebView 代理，也不依赖云端服务。

- 主机新增、编辑、删除、收藏，按名称、地址、分组搜索。
- 内置简体中文、繁体中文、英语、日语，支持跟随系统及即时切换，完全离线可用。
- 密码与主机信息保存在本机 AES-256-GCM 加密的凭据库中，密钥单独存放，文件损坏时拒绝覆盖。
- 图形帧只上传变化的矩形区域；窗口模式下等比缩放，全屏时按屏幕实际像素（含 Retina / 高 DPI）请求远程分辨率，并在服务端公布的像素上限内选择可用尺寸，避免仅放大低分辨率画面。
- 桌面宿主禁用按逻辑分辨率生成的 UI 合成缓存，远程画面始终直接绘制，避免 Retina / 高 DPI 全屏时因静止与更新切换而反复变清晰、变模糊。
- 画面始终读取最新像素，合并重叠更新并丢弃失效批次；临时像素缓冲最多一帧，区域列表与纹理内存重复使用，固定分辨率预热后的帧提取和纹理更新为零 Go 堆分配。首次创建、分辨率变化及连接重建仍可能分配内存。
- 鼠标移动、按键、滚轮、剪贴板双向同步、Ctrl+Alt+Del / Alt+Tab / Win / Esc / Tab / Enter / Backspace 浮动图标工具条；可拖动、收起/展开，快捷键和连接详情按需展开，远程桌面占满窗口内容区。
- 键盘走 PC/AT set-1 扫描码，方向键、导航键区和右侧 Ctrl/Alt 带 E0 前缀；无法用扫描码表达的字符（中文、带重音字母）走 Unicode 键盘事件。
- 自动重连、控制台会话、剪贴板开关等设置按主机或全局保存。网络断开后可自动重连；认证失败、远程注销、其他客户端接管会话时显示错误并停止重试。
- 主机可选择流畅、均衡、高清三档画质；键鼠输入入队后立即唤醒协议线程，减少等待发送的时间。

## 安装

在 [Releases](https://github.com/diiyw/onrdp/releases) 下载对应平台的安装包。Windows x64 运行 `.exe` 安装程序。macOS 按芯片选择 Apple Silicon（arm64）或 Intel（amd64）的 `.dmg`，打开后将 OnRDP 拖到「应用程序」。Ubuntu 24.04 / Linux Mint 22 x64 更新软件源后安装 `.deb`，APT 会安装所需的运行库。

```sh
sudo apt update
sudo apt install ./onrdp-linux-amd64.deb
```

发布页附带 `SHA256SUMS` 校验文件。macOS 应用使用临时签名，尚未经过 Apple 公证。

## 连接

默认端口 3389，使用 TLS 与网络级身份验证（NLA）。主机名、用户名、密码与域名在「添加主机」中填写，保存后可一键连接。会话使用主机表单中指定的桌面宽高建立连接；进入全屏时通过 RDP 显示控制通道请求屏幕实际像素尺寸，超出服务端能力时等比降低到支持的尺寸，退出全屏恢复主机配置的分辨率。服务端不支持动态分辨率时保留原有分辨率。远程画面始终保持宽高比并完整显示，鼠标坐标按同一比例映射回远程桌面。

桌面版每次连接都会打开独立窗口，可以在主窗口继续打开其他连接。RDP 窗口与 Android 投屏窗口都由主进程创建，macOS Dock 只显示一个 OnRDP 图标。「会话」页显示已打开的远程桌面，可单独断开或全部断开。关闭远程桌面窗口只断开对应会话，关闭主窗口会结束所有由它打开的会话。

画面或操作延迟明显时，在「编辑主机 → 显示与共享 → 画面质量」选择「流畅」，保存后重新连接。流畅最高请求 `1280 × 720`、16 位色；均衡最高 `1920 × 1080`、32 位色；前两档减少壁纸、窗口拖动内容和菜单动画等特效，分辨率上限仅用于窗口模式。竖屏使用对应的竖向尺寸，各档均保持比例且不放大小分辨率。高清保留主机配置分辨率，旧主机默认沿用此档。全屏时所有画质档均按显示器实际像素请求远程分辨率（含 Retina / 高 DPI），自动重连后保留该尺寸，退出全屏后恢复对应画质的窗口分辨率。服务端最终协商的分辨率、色深和特效可能受其能力或策略限制。

### H.264 硬件解码

「均衡」和「高清」在 FreeRDP 支持硬解时自动协商 RDPGFX 的 H.264 AVC420 / AVC444。macOS 发布构建使用系统 VideoToolbox。Linux 和 Windows 可用启用了 FFmpeg 硬件加速的 FreeRDP，默认分别尝试 VA-API 和 DXVA2。实际是否启用硬解取决于服务端发送的编码、客户端显卡及驱动。

硬件设备初始化失败时，FFmpeg 会尝试软件解码。未编入硬解的 FreeRDP、未协商 RDPGFX 的会话和「流畅」档使用传统 RDP 图形路径。遇到显示兼容性问题时，可在启动 OnRDP 前设置 `ONRDP_HW_DECODE=0`，让全部 RDP 会话使用传统路径。

从终端启动时可设置 `WLOG_FILTER=com.freerdp.codec:DEBUG` 查看解码器日志。macOS 的 `VideoToolbox H.264 decoder: hardware` 表示启用了硬解。Linux 可用 `FREERDP_HWCTX_DEVICE` 指定 VA-API 渲染节点。

### RustDesk

桌面版在进程内加载 RustDesk 1.5.3 的核心动态库，远程画面、键鼠操作和断开连接都在 OnRDP 会话窗口中。选择「主机 → 添加主机 → RustDesk」，填写远端设备 ID 和密码。远端启用了 IP 直连时，也可填写 IP 地址或 `IP:端口`。密码随主机一起保存在本地加密凭据库，使用远端确认的主机可留空。[RustDesk 1.5.3 发布包](https://github.com/rustdesk/rustdesk/releases/tag/1.5.3)

Linux、macOS 和 Windows 发布包已包含 RustDesk 核心库，OnRDP 会自动从包内加载。Apple Silicon 上的 RustDesk 核心需要 macOS 12.3 或更新版本。源码开发时可用 `ONRDP_RUSTDESK_LIB` 指定匹配架构的核心库绝对路径。Linux 开发版也可使用系统路径 `/usr/share/rustdesk/lib/librustdesk.so`。例如从 Linux 的官方 `.deb` 单独提取核心库后，可从仓库目录这样启动开发版。

```sh
dpkg-deb -x rustdesk-1.5.3-x86_64.deb /tmp/onrdp-rustdesk
ONRDP_RUSTDESK_LIB=/tmp/onrdp-rustdesk/usr/share/rustdesk/lib/librustdesk.so go run .
```

使用设备 ID 连接时，可在主机编辑页的「ID 与中继服务器」填写自建 ID 服务器（`hbbs`，默认端口 21116）和服务器公钥（`id_ed25519.pub` 的内容）。ID 服务器留空时沿用本机的 RustDesk 配置。勾选「始终使用中继」可让这台主机的会话通过中继连接。

RustDesk 由被控端通告中继地址。若要指定 `hbbr`，填写中继服务器（例如 `relay.example.com:21117`），并在被控端 RustDesk 的「设置 → 网络」填写同一地址。同时填写了 ID 服务器时，可点击「复制被控端配置」，在被控端一次导入并应用。若 ID 服务器已经通告所需的中继地址，此栏可留空。[RustDesk 客户端配置说明](https://rustdesk.com/docs/en/self-host/client-configuration/)

核心库首次加载会把本机 RustDesk 配置中的 `use-texture-render` 设为 `N`，以便 OnRDP 接收像素缓冲。目前提供单屏画面和键鼠输入。Linux 已验证实际连接，macOS 和 Windows 的原生运行尚待验证。

### Android 投屏

在桌面版打开「主机 → 添加主机 → Android」。USB 设备开启 USB 调试并授权后，可从 ADB 设备列表点选，或手动填写序列号。无线调试首次使用时，用 `adb pair IP:配对端口` 完成配对，然后选择「无线连接」并填写设备显示的**连接端口**。保存后可在主机列表中编辑、分组、搜索和收藏 Android 设备。

打开已保存的 Android 主机后，OnRDP 会在同一个会话窗口中跟随设备当前激活的显示屏。首次连接或切换屏幕时，投屏窗口会按新画面的宽高比调整大小，并在窗口所在显示器的工作区居中。旋转设备时只调整窗口大小。在悬浮工具条的「切换屏幕」中点选具体屏幕可固定投屏，选择「跟随当前激活屏幕」可恢复自动跟随。设备未报告焦点时，OnRDP 会尝试跟随新打开的应用屏幕。

把一个或多个 APK 文件拖到 Android 投屏窗口，OnRDP 会通过 ADB 将它们依次安装到该窗口对应的设备。安装更新时保留应用数据，窗口内会显示安装结果。

投屏显示独立的应用屏幕时，点击窗口标题栏的关闭按钮会退出当前应用，并在同一窗口回到 Android 主屏幕。投屏显示内置屏幕时，点击关闭按钮会结束本次连接。工具条的返回箭头执行安卓系统返回，键盘图标提供主屏幕、最近任务、音量加减和电源键。

点击投屏中的输入框，就能用电脑输入法选词。Android 7.0 及以上版本会通过设备剪贴板粘贴中文、表情和其他非 ASCII 文本。关闭「剪贴板同步」也能输入，设备剪贴板会保留最近一次粘贴的文字。此方式适用于允许粘贴的输入框。

桌面端通过 ADB 连接 scrcpy-server 5.0.1，使用进程内动态库解码 H.264 视频。Onix 管理会话窗口、图标、画面和键鼠输入。解码器优先尝试 Linux VA-API、Windows D3D11VA 或 macOS VideoToolbox，首帧到达后，「会话」显示实际解码方式。音频保留在手机上，文本剪贴板跟随全局设置。

发布脚本从校验过的 FFmpeg 源码构建 `libonrdp-scrcpy`，从 scrcpy 官方发行包提取配套的 `adb` 和 `scrcpy-server`。它们位于应用的 `tools/scrcpy` 目录，macOS 下该目录位于 `Contents/Resources`。解码库的构建需要 C 编译器、bash、make 和 pkg-config，x86_64 构建还需要 NASM，Linux 构建还需要 libva 和 libdrm 开发文件。

从源码运行时，可设置 `ONRDP_SCRCPY_LIBRARY` 为已构建的共享库路径，`adb` 和 `scrcpy-server` 默认从库所在目录取用。可分别用 `ADB` 和 `SCRCPY_SERVER_PATH` 指定路径。

## 键盘

进入远程桌面后轻点画面即可激活键盘输入。快捷条提供远程常用的组合键，其中 `Ctrl+Alt+Del` 在 RDP 中对应 `Ctrl+Alt+End` 语义以外的真实 SAS 序列，由服务端处理。输入法组合完成的文本以 Unicode 事件发送，不会与本机扫描码重复。

## 构建

GitHub Actions 在推送 `main` 或更新拉取请求时构建桌面安装包。在 Actions 页面手动运行 `Build desktop installers` 并填写 `release_tag`（如 `v0.2.0`），所有平台构建成功后会发布到 GitHub Releases。留空时可手动构建并从工作流运行页面下载产物。

需要 Go 1.27+、C 编译器、pkg-config，以及 FreeRDP 3.32+ 的开发库（`freerdp3`、`freerdp-client3`、`winpr3`）。所有协议调用直接链接原生库，构建必须启用 cgo。通过 Go Modules 获取 Onix v0.1.10，不依赖相邻的引擎目录。

macOS 开发环境：

```sh
brew install go pkg-config freerdp
pkg-config --modversion freerdp3 freerdp-client3 winpr3
go test ./...
go run .
```

Ubuntu / Linux Mint 开发环境：

```sh
sudo apt install build-essential pkg-config freerdp3-dev libwinpr3-dev libxrandr-dev
go test ./...
go run .
```

Ubuntu / Linux Mint 的系统 FreeRDP 可能未编入硬解。需要 VA-API 时，可使用 FreeRDP 3.33.0 或更新版本的本地构建。以 3.33.0 源码目录为例，安装 `build-essential`、`pkg-config`、`cmake`、`libssl-dev`、`zlib1g-dev`、`libavcodec-dev`、`libavutil-dev` 和 `libswscale-dev`，再执行以下命令。此示例使用 TLS 和基于 NTLM 的 NLA。

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

开发时从项目目录运行。默认窗口为可缩放的 `1024 × 700`，支持右键、中键、滚轮及 `F11` 全屏。`ONRDP_DATA_DIR` 可覆盖数据目录，默认使用系统配置目录下的 `onrdp`。

macOS 发布包需要在对应架构的 Mac 上生成。以下流程从固定版本源码构建 FreeRDP 库，使用 Onix 的资源打包入口，再收集原生动态库并签名；生成的 `.app` 不需要安装 Homebrew，也不依赖源码工作目录。

```sh
brew install go pkg-config cmake openssl
scripts/build-freerdp-macos.sh
scripts/build-macos.sh
```

输出为 `dist/macos/OnRDP.app` 和 `OnRDP-macos-arm64-FreeRDP.zip`（Intel Mac 为 `amd64`）。当前流程按本机架构构建，不生成 universal 包。包内采用 ad-hoc 签名；Developer ID 签名及 Apple 公证仍需发行者的证书。

工具条运行时切换的展开、窗口模式图标在 `resources/app/mod.json` 中显式声明，避免资源扫描只收集 HTML 初始图标而漏包。macOS 构建会读取打包后的资源并解码全部 SVG 图标，缺失或无法解码时停止打包。已有安装包需要重新构建，才能包含图标与高清画面修复。

**不能把普通 `go build` 的二进制直接放进 `.app`**：它缺少 Onix 生成的资源挂载入口，会在源码目录之外启动失败。`scripts/build-macos.sh` 会通过 `editor.BuildProject` 生成正确入口，并检查所有非系统 dylib 都在包内。

Linux amd64 和 Windows 便携包使用下列脚本。各平台的发布脚本会校验 RustDesk 1.5.3 官方发行包的 SHA-256，提取匹配架构的动态库及授权信息。macOS 会在签名时纳入动态库。

```sh
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.10 check .
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.10 generate .
scripts/build-linux.sh
python3 scripts/build-windows.py
```

Windows 打包需要目标架构的 MinGW-w64、pkg-config、Python 3.11+ 和 PATH 中的 7-Zip。普通的 Windows x64 发布包可在 MSYS2 UCRT64 环境中构建。FreeRDP 和 WinPR 由 pkg-config 提供，打包脚本会从同一 SDK 的 `bin` 收集可执行文件依赖的 DLL。SDK 布局不同时，可用 `FREERDP_RUNTIME_DIR` 指定包含这些 DLL 的目录。产物为 `dist/windows/onrdp-windows-<arch>.zip`。[MSYS2 FreeRDP 软件包](https://packages.msys2.org/base/mingw-w64-freerdp)

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

## 限制

- Linux、Windows、macOS 桌面宿主已接入；移动端保留引擎入口，但原生应用生命周期、系统键盘与安全存储宿主尚未完整接入。音频、摄像头、驱动器重定向尚未接入。
- 当前禁用音频、驱动器、打印机、智能卡和 USB 重定向。解码后的画面经 FreeRDP 的 GDI 帧缓冲交给 Onix 绘制。
- 沿用旧版本的证书策略：服务端证书尚未校验；请仅连接受信任网络中的主机。
