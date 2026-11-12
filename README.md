# OnRDP

使用 [Onix v0.1.5](https://github.com/onix-engine/onix/releases/tag/v0.1.5) 引擎构建的 Windows 远程桌面（RDP）客户端。[FreeRDP](https://github.com/FreeRDP/FreeRDP) 负责 RDP 协议，Go/cgo 管理帧缓冲与连接生命周期，Onix 原生 HTML/CSS 渲染界面；没有 WebView 代理，也不依赖云端服务。

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

在 [Releases](https://github.com/diiyw/onrdp/releases) 下载发布包。Linux amd64 包使用系统 FreeRDP 3.32+ 运行库；Ubuntu 24.04 / Linux Mint 22 请先更新软件源并安装依赖，再解压运行：

```sh
sudo apt update
sudo apt install libfreerdp3-3 libfreerdp-client3-3 libwinpr3-3 libx11-6 libxext6 libxrender1 libxi6 libwayland-client0 libwayland-cursor0 libxkbcommon0 libgl1
tar -xzf onrdp-linux-amd64.tar.gz
./onrdp/onrdp
```

保留解压目录中的 `resources` 文件夹。macOS 和 Windows 的源码构建要求见下文。

## 连接

默认端口 3389，使用 TLS 与网络级身份验证（NLA）。主机名、用户名、密码与域名在「添加主机」中填写，保存后可一键连接。会话使用主机表单中指定的桌面宽高建立连接；进入全屏时通过 RDP 显示控制通道请求屏幕实际像素尺寸，超出服务端能力时等比降低到支持的尺寸，退出全屏恢复主机配置的分辨率。服务端不支持动态分辨率时保留原有分辨率。远程画面始终保持宽高比并完整显示，鼠标坐标按同一比例映射回远程桌面。确认断开连接后直接返回会话列表。

画面或操作延迟明显时，在「编辑主机 → 显示与共享 → 画面质量」选择「流畅」，保存后重新连接。流畅最高请求 `1280 × 720`、16 位色；均衡最高 `1920 × 1080`、32 位色；前两档减少壁纸、窗口拖动内容和菜单动画等特效，全屏也遵守上限。竖屏使用对应的竖向尺寸，各档均保持比例且不放大小分辨率。高清保留主机配置及全屏原始像素分辨率，旧主机默认沿用此档。服务端最终协商的分辨率、色深和特效可能受其能力或策略限制。

## 键盘

进入远程桌面后轻点画面即可激活键盘输入。快捷条提供远程常用的组合键，其中 `Ctrl+Alt+Del` 在 RDP 中对应 `Ctrl+Alt+End` 语义以外的真实 SAS 序列，由服务端处理。输入法组合完成的文本以 Unicode 事件发送，不会与本机扫描码重复。

## 构建

需要 Go 1.27+、C 编译器、pkg-config，以及 FreeRDP 3.32+ 的开发库（`freerdp3`、`freerdp-client3`、`winpr3`）。所有协议调用直接链接原生库，构建必须启用 cgo。通过 Go Modules 获取 Onix v0.1.5，不依赖相邻的引擎目录。

macOS 开发环境：

```sh
brew install go pkg-config freerdp
pkg-config --modversion freerdp3 freerdp-client3 winpr3
go test ./...
go run .
```

Onix v0.1.5 已包含 macOS 取消关闭窗口后的原生关闭标记修复，开发和打包均直接使用 Go Modules 中的引擎，不再需要私有副本或本地补丁。

Ubuntu / Linux Mint 开发环境：

```sh
sudo apt install build-essential pkg-config freerdp3-dev libwinpr3-dev
go test ./...
go run .
```

开发时从项目目录运行。默认窗口为可缩放的 `1024 × 700`，支持右键、中键、滚轮及 `F11` 全屏。有 RDP 会话或正处于远程桌面页面时，点击窗口关闭按钮会断开当前会话并返回会话列表，保留 OnRDP；没有打开的会话时再次关闭窗口才退出程序。`ONRDP_DATA_DIR` 可覆盖数据目录，默认使用系统配置目录下的 `onrdp`。

macOS 发布包需要在对应架构的 Mac 上生成。以下流程从固定版本源码构建 FreeRDP 库，使用 Onix 的资源打包入口，再收集原生动态库并签名；生成的 `.app` 不需要安装 Homebrew，也不依赖源码工作目录。

```sh
brew install go pkg-config cmake openssl
scripts/build-freerdp-macos.sh
scripts/build-macos.sh
```

输出为 `dist/macos/OnRDP.app` 和 `OnRDP-macos-arm64-FreeRDP.zip`（Intel Mac 为 `amd64`）。当前流程按本机架构构建，不生成 universal 包。包内采用 ad-hoc 签名；Developer ID 签名及 Apple 公证仍需发行者的证书。

工具条运行时切换的展开、窗口模式图标在 `resources/app/mod.json` 中显式声明，避免资源扫描只收集 HTML 初始图标而漏包。macOS 构建会读取打包后的资源并解码全部 SVG 图标，缺失或无法解码时停止打包。已有安装包需要重新构建，才能包含图标与高清画面修复。

**不能把普通 `go build` 的二进制直接放进 `.app`**：它缺少 Onix 生成的资源挂载入口，会在源码目录之外启动失败。`scripts/build-macos.sh` 会通过 `editor.BuildProject` 生成正确入口，并检查所有非系统 dylib 都在包内。

Linux / Windows 可通过 Onix 构建器打包，但必须准备目标架构的 FreeRDP 及 WinPR 库，并提供其运行时依赖或相应安装说明；不再支持无 C 工具链的纯 Go 交叉构建。

```sh
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.5 check .
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.5 generate .
go run github.com/onix-engine/onix/editor/cmd/onix@v0.1.5 build -platform linux -mode release "$PWD"
```

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
- 当前禁用音频、驱动器、打印机、智能卡和 USB 重定向，使用软件 GDI 图形路径。
- 沿用旧版本的证书策略：服务端证书尚未校验；请仅连接受信任网络中的主机。
