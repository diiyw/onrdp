package app

import (
	"runtime"

	"onrdp/internal/i18n"
	"onrdp/internal/platform"
)

func (m *Mod) tr(key string, args ...any) string { return i18n.Text(m.language, key, args...) }

func (m *Mod) localize() {
	preference := ""
	if m.vault != nil {
		preference = m.vault.Settings().Language
	}
	m.language = i18n.Resolve(preference, platform.SystemLocale())
	m.node("shell").SetAttribute("lang", m.language)
	for _, binding := range textBindings {
		if binding.attribute == "" {
			m.text(binding.id, m.tr(binding.key))
			continue
		}
		m.node(binding.id).SetAttribute(binding.attribute, m.tr(binding.key))
	}
	modifier := "Ctrl+"
	if runtime.GOOS == "darwin" {
		modifier = "⌘"
	}
	for _, item := range []struct{ id, key, shortcut string }{
		{"add-host", "添加主机", "N"}, {"search", "搜索主机", "F"}, {"edit-save", "保存", "S"},
	} {
		m.node(item.id).SetAttribute("title", m.tr(item.key)+" ("+modifier+item.shortcut+")")
	}
	for _, item := range toolbarLabels {
		m.toolbarLabel(item.id, item.key)
	}
	m.node("rdp-toolbar").SetAttribute("aria-label", m.tr("远程工具条"))
	m.renderToolbar()
	if m.mirror != nil {
		m.renderDisplays()
	}
	m.hide("rdp-tooltip", true)
	if m.page == "edit" {
		if m.draft.Protocol == "android" {
			m.renderAndroidDevices()
		}
		m.renderHostProtocol()
	}
	m.listSignature = ""
	m.dirty = true
}

func (m *Mod) languageOptions() {
	list := m.node("language-list")
	list.Clear()
	preference := m.vault.Settings().Language
	options := append([]i18n.Language{{Code: "", Name: m.tr("跟随系统")}}, i18n.Languages()...)
	for _, language := range options {
		button := m.node("language-template").Clone()
		code := language.Code
		if code == "" {
			code = "system"
		}
		button.SetAttribute("id", "language:"+code)
		button.SetAttribute("lang", language.Code)
		class := "language-option"
		selected, mark := "false", ""
		if preference == language.Code {
			class += " selected"
			selected, mark = "true", "✓"
		}
		button.SetAttribute("class", class)
		button.SetAttribute("aria-pressed", selected)
		setNode(button, "language-name", language.Name)
		setNode(button, "language-mark", mark)
		list.Append(button)
	}
}

func (m *Mod) setLanguage(code string) {
	if code == "system" {
		code = ""
	}
	if code != "" && !i18n.Supported(code) {
		return
	}
	settings := m.vault.Settings()
	settings.Language = code
	if err := m.vault.SaveSettings(settings); err != nil {
		m.toast(err)
		return
	}
	m.hide("toast", true)
	m.localize()
	m.settings()
	m.renderLists()
	if m.page == "rdp" {
		m.renderSession()
	}
}

// textBindings lists the static text targets. Leaf nodes only: controls and
// icons keep their structure so their own attributes stay intact.
var textBindings = []struct{ id, attribute, key string }{
	{"rustdesk-id-label", "", "RustDesk 设备 ID 或 IP 地址"},
	{"rustdesk-password-label", "", "RustDesk 密码（可选）"},
	{"rustdesk-help", "", "IP 直连需在远端启用 RustDesk 的直连功能。"},
	{"rustdesk-network-title", "", "ID 与中继服务器（可选）"},
	{"rustdesk-id-server-label", "", "ID 服务器"},
	{"rustdesk-key-label", "", "服务器公钥"},
	{"rustdesk-relay-server-label", "", "中继服务器"},
	{"rustdesk-force-relay-label", "", "始终使用中继"},
	{"rustdesk-relay-help", "", "中继地址由被控端通告。请在被控端的 RustDesk 网络设置中填写此地址，或导入服务器配置。"},
	{"android-keys", "aria-label", "安卓按键"},
	{"screens-title", "", "切换屏幕"},
	{"android-serial", "", "设备序列号"},
	{"android-refresh", "", "刷新"},
	{"android-detected", "", "ADB 设备"},
	{"android-wireless", "", "无线连接"},

	{"android-wireless-help", "", "使用设备显示的连接端口。首次使用无线调试时，请先用 adb pair 配对。"},
	{"add-host-label", "", "添加主机"},
	{"sidebar", "aria-label", "主导航"},
	{"library-title", "", "连接库"},
	{"groups-title", "", "分组"},
	{"favorites-label", "", "已收藏"},
	{"nav-favorites", "title", "已收藏"},
	{"nav-favorites", "aria-label", "已收藏"},
	{"hosts-col-display", "", "连接信息"},
	{"hosts-col-name", "", "名称"},
	{"hosts-col-address", "", "主机地址"},
	{"hosts-col-actions", "", "操作"},
	{"sessions-col-name", "", "名称"},
	{"sessions-col-address", "", "主机地址"},
	{"sessions-col-actions", "", "操作"},
	{"hosts-col-group", "", "分组"},
	{"sessions-col-state", "", "状态"},
	{"nav-hosts", "title", "全部主机"},
	{"nav-sessions", "title", "会话"},
	{"nav-settings", "title", "设置"},
	{"add-host", "aria-label", "添加主机"},
	{"search", "placeholder", "搜索主机、地址或分组"},
	{"search", "aria-label", "搜索主机"},
	{"host-count", "", "0 台主机"},
	{"active-count", "", "暂无连接"},
	{"empty-add", "", "添加主机"},
	{"l10n-4", "", "会话"},
	{"close-all", "", "全部断开"},
	{"session-summary", "", "连接过的桌面会显示在这里"},
	{"l10n-5", "", "还没有会话"},
	{"l10n-6", "", "选择一台主机，打开远程桌面。\n切换页面时，会话会继续保留。"},
	{"go-hosts", "", "选择主机"},
	{"l10n-7", "", "设置"},
	{"appearance-title", "", "外观"},
	{"theme-light", "", "亮色"},
	{"theme-dark", "", "暗色"},
	{"language-title", "", "语言"},
	{"l10n-8", "", "连接"},
	{"l10n-10", "", "自动重连"},
	{"l10n-11", "", "网络恢复后重新连接服务器"},
	{"setting-reconnect", "aria-label", "自动重连"},
	{"l10n-30", "", "剪贴板同步"},
	{"l10n-31", "", "允许在本机与远程桌面之间复制文本"},
	{"setting-clipboard", "aria-label", "剪贴板同步"},
	{"l10n-32", "", "适应窗口"},
	{"l10n-33", "", "按窗口大小缩放画面，保持宽高比"},
	{"l10n-17", "", "安全"},
	{"l10n-18", "", "凭据保存在本机"},
	{"background-note", "", "主机密码经 AES-GCM 加密后写入本地凭据库，密钥由操作系统保管。删除主机时同时删除其凭据。"},
	{"l10n-21", "", "全部主机"},
	{"l10n-22", "", "会话"},
	{"l10n-23", "", "设置"},
	{"nav-hosts", "aria-label", "全部主机"},
	{"nav-sessions", "aria-label", "会话"},
	{"nav-settings", "aria-label", "设置"},
	{"edit-back", "", "主机"},
	{"edit-cancel", "", "取消"},
	{"edit-save", "", "保存"},
	{"l10n-24", "", "连接信息"},
	{"l10n-25", "", "名称"},
	{"field-name", "placeholder", "例如：办公室电脑"},
	{"l10n-26", "", "主机地址"},
	{"field-address", "placeholder", "IP 地址或域名"},
	{"l10n-27", "", "端口"},
	{"l10n-28", "", "用户名"},
	{"field-user", "placeholder", "Administrator"},
	{"l10n-29", "", "域（可选）"},
	{"field-domain", "placeholder", "例如：CORP"},
	{"l10n-40", "", "分组（可选）"},
	{"field-group", "placeholder", "例如：办公环境"},
	{"l10n-30b", "", "凭据"},
	{"l10n-31b", "", "登录密码"},
	{"field-password", "placeholder", "输入远程桌面密码"},
	{"l10n-34", "", "显示与共享"},
	{"quality-label", "", "画面质量"},
	{"quality-smooth", "", "流畅"},
	{"quality-balanced", "", "均衡"},
	{"quality-high", "", "高清"},
	{"quality-help", "", "流畅：窗口最高 1280 × 720、16 位色；均衡：窗口最高 1920 × 1080、32 位色；高清：原始分辨率。全屏均适配显示器实际像素，前两档减少桌面特效。保存后重新连接生效。"},
	{"l10n-35", "", "桌面宽度"},
	{"l10n-36", "", "桌面高度"},
	{"l10n-37", "", "剪贴板"},
	{"field-clipboard", "aria-label", "剪贴板重定向"},
	{"l10n-45", "", "控制台会话"},
	{"field-console", "aria-label", "连接控制台会话"},
	{"l10n-38", "", "连接使用 TLS 与网络级身份验证（NLA）。密码仅在本机加密保存。"},
	{"edit-delete", "", "删除主机"},
	{"rdp-back", "aria-label", "返回会话"},
	{"rdp-fullscreen", "aria-label", "全屏"},
	{"rdp-reconnect", "aria-label", "重连"},
	{"rdp-close", "aria-label", "断开"},
	{"rdp-input", "aria-label", "远程桌面输入"},
	{"rdp-overlay-retry", "", "重新连接"},
	{"l10n-43", "aria-label", "远程快捷键"},
	{"key-cad", "aria-label", "Ctrl+Alt+Del"},
	{"key-alt-tab", "aria-label", "Alt+Tab"},
	{"key-win", "aria-label", "Win"},
	{"key-esc", "aria-label", "Esc"},
	{"key-tab", "aria-label", "Tab"},
	{"key-enter", "aria-label", "Enter"},
	{"key-backspace", "aria-label", "退格"},
	{"confirm-cancel", "", "取消"},
	{"confirm-ok", "", "确认"},
	{"l10n-40b", "", "无法打开安全存储"},
	{"language-mark", "", ""},
}
