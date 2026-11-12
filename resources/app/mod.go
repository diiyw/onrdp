package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	htmlui "github.com/onix-engine/onix/ui/html"
	"onrdp/internal/i18n"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/store"
)

// Mod is the single screen of the app: an address book, a session list and the
// remote desktop surface itself.
type Mod struct {
	toastUntil     time.Time
	fields         map[string]string
	nodes          map[string]module.HTMLNode
	vault          *store.Vault
	client         *rdpclient.Client
	frame          *frameTexture
	root           module.HTMLNode
	host           *module.Host
	confirm        string
	language       string
	selected       string
	query          string
	page           string
	draft          store.Host
	listSignature  string
	statusLine     string
	status         rdpclient.Snapshot
	sessionQuality rdpclient.Quality
	remoteX        int
	remoteY        int
	elapsed        float32
	favoriteOnly   bool
	connected      bool
	pressed        [3]bool
	dragging       bool
	dirty          bool
	focusInput     bool
	clipboard      bool
	fullscreen     bool
	toolbar        floatingToolbar
}

func (m *Mod) node(id string) module.HTMLNode {
	if n, ok := m.nodes[id]; ok {
		return n
	}
	n, _ := m.root.FindByID(id)
	m.nodes[id] = n
	return n
}

func (m *Mod) text(id, value string)       { m.node(id).SetText(value) }
func (m *Mod) hide(id string, hidden bool) { m.node(id).SetHidden(hidden) }
func (m *Mod) class(id, value string)      { m.node(id).SetAttribute("class", value) }

func (m *Mod) value(c *module.Context, id, value string) {
	m.node(id).SetAttribute("value", value)
	if control, ok := c.Content().HTMLControl(c.Content().Root(), id); ok {
		c.Content().SetControlValue(control, value)
	}
}

// Install wires the HTML page to the vault. A failure to open storage keeps the
// app usable and shows the fatal panel instead of crashing.
func (m *Mod) Install(c *module.Context) error {
	var err error
	m.root, err = module.HTML(c, c.Root())
	if err != nil {
		return err
	}
	m.host = c.Host()
	m.nodes = make(map[string]module.HTMLNode)
	if m.host != nil {
		m.hide("rdp-fullscreen", m.host.ToggleFullscreen == nil)
		if m.host.OnCloseRequested != nil {
			m.host.OnCloseRequested(m.closeRequested)
		}
	}
	m.fields = make(map[string]string)
	m.language = i18n.Resolve("", platform.SystemLocale())
	m.localize()

	dir, key, keyErr := platform.Storage()
	if keyErr != nil {
		m.showFatal(keyErr)
		return nil
	}
	m.vault, err = store.Open(dir, key)
	clear(key)
	if err != nil {
		m.showFatal(err)
		return nil
	}
	m.client = rdpclient.New(func() {
		if host := c.Host(); host != nil && host.RequestRender != nil {
			host.RequestRender()
		}
	})
	// The protocol delivers remote clipboard text on its own goroutine; the
	// callback only marks it as pending, and the loop thread mirrors it.
	m.client.OnClipboardText(func(string) { m.clipboard = true })
	m.frame = &frameTexture{}
	if surface, ok := c.Content().Get[htmlui.Surface](c.Content().Root()); ok && surface.Images != nil {
		// Reuse the project provider for every non-framebuffer source so the
		// bundled icons keep resolving through the scoped mod filesystem.
		if previous, installed := surface.Images.Provider.(*frameTexture); installed {
			m.frame = previous
		} else {
			m.frame.previous = surface.Images.Provider
			surface.Images.Provider = m.frame
		}
	}
	platform.Register(platform.Hooks{
		Language: func() string { return m.vault.Settings().Language },
		Active: func() int {
			if m.connected {
				return 1
			}
			return 0
		},
		Resume: func() {},
		Close:  func() { m.client.Close() },
	})
	m.localize()
	m.page = "hosts"
	m.dirty = true
	m.settings()
	m.renderLists()
	return nil
}

func (m *Mod) showFatal(err error) {
	m.hide("fatal", false)
	m.text("fatal-message", i18n.ErrorText(m.language, err))
}

func (m *Mod) Update(c *module.Context, tick module.Tick) error {
	if m.vault == nil {
		return nil
	}
	host := c.Host()
	if m.page == "rdp" && m.confirm == "" {
		if control, ok := c.Content().HTMLControl(c.Content().Root(), "rdp-input"); ok && c.Content().Has[ui.TextField](control) {
			// HTML reconciliation can replace this control, and UI pointer
			// dispatch runs after module updates, clearing focus on frame clicks.
			// Rebind the handler and restore focus instead of trusting the last
			// focus request to survive the rest of the engine's frame.
			if err := c.Content().Insert(control, ui.TextFieldKeyHandler{Handle: func(state *input.State) bool { return m.remoteKeys(state) }}); err != nil {
				return err
			}
			if (m.focusInput || c.Content().FocusedControl() != control) && c.Content().Focus(control) {
				m.focusInput = false
			}
		}
	}
	// Remote pixels must reach the screen on the frame they arrive, so the
	// throttle below deliberately does not gate the session flush: a dropped
	// flush would leave the app idle with a stale picture.
	if m.page == "rdp" {
		m.syncSession(c)
		m.syncResolution()
		m.flushFrame()
		m.layoutToolbar()
	}

	m.elapsed += tick.Delta
	if m.elapsed >= .08 {
		m.elapsed = 0
		if preference := i18n.Resolve(m.vault.Settings().Language, platform.SystemLocale()); preference != m.language {
			m.localize()
			m.settings()
		}
		if !m.toastUntil.IsZero() && time.Now().After(m.toastUntil) {
			m.hide("toast", true)
			m.toastUntil = time.Time{}
			m.dirty = true
		}
	}
	if m.dirty {
		m.dirty = false
		m.renderLists()
		if m.page == "rdp" {
			m.renderSession()
		}
		if host != nil && host.RequestRender != nil {
			host.RequestRender()
		}
	}
	return nil
}

// syncSession mirrors the connection status into the page and adopts remote
// clipboard text, which must happen on the loop thread.
func (m *Mod) syncSession(c *module.Context) {
	if m.client == nil {
		return
	}
	if text, ok := m.client.TakeClipboard(); ok {
		if m.clipboardShared() && c.Host() != nil && c.Host().SetClipboardText != nil {
			c.Host().SetClipboardText(text)
		}
		m.clipboard = false
		m.dirty = true
	}
	status := m.client.Status()
	if status.Width != m.status.Width || status.Height != m.status.Height {
		m.dirty = true
	}
	m.status = status
	line := m.statusText(status)
	if line != m.statusLine {
		m.statusLine = line
		m.dirty = true
	}
}

// clipboardShared reports whether the current session redirects the clipboard.
func (m *Mod) clipboardShared() bool {
	if m.client == nil || !m.vault.Settings().Clipboard {
		return false
	}
	h, ok := m.findHost(m.selected)
	return ok && h.Clipboard
}

// pushClipboard publishes the local clipboard so a paste inside the remote
// session is served from it instead of an empty remote clipboard.
func (m *Mod) pushClipboard() {
	if !m.clipboardShared() || m.host == nil || m.host.Context == nil {
		return
	}
	if text, ok := m.host.Context.ClipboardText(); ok && text != "" {
		m.client.SetClipboard(text)
	}
}

func (m *Mod) statusText(status rdpclient.Snapshot) string {
	switch status.State {
	case rdpclient.StateConnecting:
		return m.tr("正在连接…")
	case rdpclient.StateConnected:
		return m.tr("已连接")
	default:
		if status.Error != nil {
			return m.tr("已断开")
		}
		return m.tr("未连接")
	}
}

func (m *Mod) route(page string) {
	m.toolbar.dragging = false
	m.hide("rdp-tooltip", true)
	if m.page == "rdp" && page != "rdp" && m.fullscreen {
		m.toggleFullscreen()
	}
	m.page = page
	shellClass := "shell"
	if page == "rdp" {
		shellClass += " remote"
	}
	m.class("shell", shellClass)
	m.focusInput = page == "rdp"
	for _, p := range []string{"hosts", "sessions", "settings", "edit", "rdp"} {
		m.hide(p+"-page", p != page)
	}
	m.hide("bottom-nav", page == "edit" || page == "rdp")
	for _, p := range []string{"hosts", "sessions", "settings"} {
		class := "nav-item"
		if page == p {
			class += " selected"
		}
		m.class("nav-"+p, class)
		current := "false"
		if page == p {
			current = "page"
		}
		m.node("nav-"+p).SetAttribute("aria-current", current)
	}
	m.dirty = true
}

func (m *Mod) toast(err error) {
	if err == nil {
		return
	}
	m.text("toast", i18n.ErrorText(m.language, err))
	m.hide("toast", false)
	m.toastUntil = time.Now().Add(4 * time.Second)
}

func (m *Mod) findHost(id string) (store.Host, bool) {
	for _, h := range m.vault.Snapshot().Hosts {
		if h.ID == id {
			return h, true
		}
	}
	return store.Host{}, false
}

func setNode(root module.HTMLNode, id, text string) {
	if n, ok := root.FindByID(id); ok {
		n.SetText(text)
	}
}

func setID(root module.HTMLNode, id, next string) {
	if n, ok := root.FindByID(id); ok {
		n.SetAttribute("id", next)
	}
}

func (m *Mod) renderLists() {
	if m.vault == nil {
		return
	}
	data := m.vault.Snapshot()
	var signature strings.Builder
	fmt.Fprintf(&signature, "%s/%t/", m.query, m.favoriteOnly)
	for _, h := range data.Hosts {
		fmt.Fprintf(&signature, "%s:%s:%s:%s:%s:%d:%t/", h.ID, h.Name, h.Address, h.User, h.Domain, h.Port, h.Favorite)
	}
	fmt.Fprintf(&signature, "%s:%s:%t/", m.statusLine, m.selected, m.connected)
	next := signature.String()
	if next == m.listSignature {
		return
	}
	m.listSignature = next

	connected := 0
	if m.connected {
		connected = 1
	}
	m.text("active-count", m.tr("暂无连接"))
	m.class("active-dot", "status-dot")
	if connected > 0 {
		m.text("active-count", m.tr("%d 个连接中", connected))
		m.class("active-dot", "status-dot live")
	}
	m.text("session-count", strconv.Itoa(connected))
	m.hide("session-count", connected == 0)
	m.text("session-summary", m.tr("连接过的桌面会显示在这里"))
	if m.selected != "" {
		if h, ok := m.findHost(m.selected); ok {
			m.text("session-summary", m.tr("%d 个连接中 · %s", connected, h.Name))
		}
	}

	list := m.node("host-list")
	list.Clear()
	hosts := data.Hosts
	slices.SortStableFunc(hosts, func(a, b store.Host) int {
		if a.Favorite != b.Favorite {
			if a.Favorite {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	count := 0
	for _, h := range hosts {
		if m.favoriteOnly && !h.Favorite {
			continue
		}
		if m.query != "" && !strings.Contains(strings.ToLower(h.Name+" "+h.Address+" "+h.Group), strings.ToLower(m.query)) {
			continue
		}
		count++
		card := m.node("host-template").Clone()
		card.SetAttribute("id", "host-"+h.ID)
		setNode(card, "host-name", h.Name)
		setNode(card, "host-address", h.User+"@"+h.Endpoint())
		meta := h.Group
		if meta == "" {
			meta = m.tr("未分组")
		}
		width, height := h.Resolution()
		meta = fmt.Sprintf("%s · %d × %d", meta, width, height)
		setNode(card, "host-meta", meta)
		setNode(card, "host-edit", m.tr("编辑"))
		setNode(card, "host-favorite", m.tr("收藏"))
		if h.ID == m.selected && m.connected {
			setNode(card, "host-state", m.tr("已连接"))
		}
		if h.Favorite {
			setNode(card, "host-favorite", m.tr("已收藏"))
		}
		setID(card, "host-connect", "connect:"+h.ID)
		if n, ok := card.FindByID("connect:" + h.ID); ok {
			n.SetAttribute("aria-label", m.tr("连接 %s", h.Name))
		}
		setID(card, "host-edit", "edit:"+h.ID)
		setID(card, "host-favorite", "favorite:"+h.ID)
		list.Append(card)
	}
	m.text("host-count", m.tr("%d 台主机", count))
	m.hide("hosts-empty", count != 0)
	filtered := m.query != "" || m.favoriteOnly
	m.hide("empty-add", filtered)
	if filtered {
		m.text("empty-title", m.tr("没有找到主机"))
		m.text("empty-description", m.tr("试试其他关键词，或切换到全部主机。"))
	} else {
		m.text("empty-title", m.tr("你的桌面，从这里开始"))
		m.text("empty-description", m.tr("保存一台 Windows 主机，轻点即可连接。\n密码仅在本机加密保存。"))
	}

	sessions := m.node("session-list")
	sessions.Clear()
	h, hasSession := m.findHost(m.selected)
	m.hide("sessions-empty", hasSession)
	m.hide("close-all", !m.connected)
	if hasSession {
		card := m.node("session-template").Clone()
		card.SetAttribute("id", "session-"+h.ID)
		setNode(card, "session-name", h.Name)
		setNode(card, "session-address", h.User+"@"+h.Endpoint())
		state := m.statusLine
		if state == "" {
			state = m.tr("未连接")
		}
		setNode(card, "session-state", state)
		width, height := h.Resolution()
		setNode(card, "session-meta", fmt.Sprintf("%s · %d × %d", h.Address, width, height))
		setNode(card, "session-close", m.tr("断开"))
		setID(card, "session-open", "session:"+h.ID)
		setID(card, "session-close", "close:"+h.ID)
		sessions.Append(card)
	}
}

func (m *Mod) openHost(id string, connect bool) {
	h, ok := m.findHost(id)
	if !ok {
		return
	}
	m.selected = id
	m.statusLine = ""
	if connect {
		m.connect(h)
		m.route("rdp")
		m.dirty = true
		return
	}
	m.route("sessions")
}

func (m *Mod) connect(h store.Host) {
	if m.client == nil {
		return
	}
	m.client.Close()
	m.pressed = [3]bool{}
	settings := m.vault.Settings()
	width, height := h.Resolution()
	m.sessionQuality = rdpclient.Quality(h.Quality).Normalized()
	width, height = m.sessionQuality.Resolution(width, height)
	m.client.Connect(rdpclient.Config{
		Host:      h.Address,
		Port:      h.Port,
		Username:  h.User,
		Password:  h.Password,
		Domain:    h.Domain,
		Width:     width,
		Height:    height,
		Quality:   m.sessionQuality,
		Clipboard: h.Clipboard && settings.Clipboard,
		Reconnect: settings.Reconnect,
		Console:   h.ConsoleMode,
	})
	m.connected = true
	m.statusLine = m.tr("正在连接…")
	m.status = rdpclient.Snapshot{State: rdpclient.StateConnecting, Width: width, Height: height}
	m.hide("rdp-error", true)
	m.hide("rdp-overlay", false)
	m.text("rdp-overlay-text", m.tr("正在连接…"))
	m.hide("rdp-overlay-retry", true)
}

// flushFrame publishes pending remote pixels into the page texture. The upload
// must run on the loop thread, and re-pointing the image source is only needed
// when the texture object itself was replaced.
func (m *Mod) flushFrame() {
	if m.client == nil || m.frame == nil {
		return
	}
	regions, width, height, generation := m.client.Flush()
	if width <= 0 || height <= 0 {
		return
	}
	if m.frame.Apply(regions, width, height, generation) {
		m.node("rdp-frame").SetAttribute("src", m.frame.Source())
	}
	if len(regions) > 0 {
		m.dirty = true
	}
}

func (m *Mod) edit(c *module.Context, id string) {
	h := store.Host{ID: store.NewID(), Port: 3389, Width: 1600, Height: 900, Clipboard: true}
	if id != "" {
		var ok bool
		h, ok = m.findHost(id)
		if !ok {
			return
		}
	}
	m.draft = h
	m.fields = map[string]string{
		"name": h.Name, "address": h.Address, "port": strconv.Itoa(h.Port),
		"user": h.User, "domain": h.Domain, "group": h.Group, "password": h.Password,
		"width": strconv.Itoa(h.Width), "height": strconv.Itoa(h.Height),
	}
	for name, value := range m.fields {
		m.value(c, "field-"+name, value)
	}
	m.text("edit-title", m.tr("添加主机"))
	if id != "" {
		m.text("edit-title", m.tr("编辑主机"))
	}
	m.hide("edit-delete", id == "")
	m.toggle("field-clipboard", h.Clipboard)
	m.toggle("field-console", h.ConsoleMode)
	m.renderQuality()
	m.route("edit")
}

func (m *Mod) renderQuality() {
	selected := rdpclient.Quality(m.draft.Quality).Normalized()
	for _, quality := range []rdpclient.Quality{rdpclient.QualitySmooth, rdpclient.QualityBalanced, rdpclient.QualityHigh} {
		id := "quality-" + string(quality)
		class := "quality-option"
		if quality == selected {
			class += " selected"
		}
		m.class(id, class)
		m.node(id).SetAttribute("aria-pressed", strconv.FormatBool(quality == selected))
	}
}

func (m *Mod) save(c *module.Context) {
	h := m.draft
	h.Name = strings.TrimSpace(m.fields["name"])
	h.Address = strings.TrimSpace(strings.Trim(m.fields["address"], "[]"))
	h.User = strings.TrimSpace(m.fields["user"])
	h.Domain = strings.TrimSpace(m.fields["domain"])
	h.Group = strings.TrimSpace(m.fields["group"])
	h.Password = m.fields["password"]
	h.Port, _ = strconv.Atoi(m.fields["port"])
	h.Width, _ = strconv.Atoi(m.fields["width"])
	h.Height, _ = strconv.Atoi(m.fields["height"])
	if h.Width < 640 || h.Width > 4096 {
		h.Width = 1600
	}
	if h.Height < 480 || h.Height > 4096 {
		h.Height = 900
	}
	if err := m.vault.SaveHost(h); err != nil {
		m.toast(err)
		return
	}
	m.clearSecrets(c)
	m.listSignature = ""
	m.route("hosts")
}

func (m *Mod) clearSecrets(c *module.Context) {
	m.fields["password"] = ""
	m.value(c, "field-password", "")
	m.draft = store.Host{}
}

func (m *Mod) toggle(id string, on bool) {
	class := "toggle"
	if on {
		class += " on"
	}
	m.class(id, class)
	m.node(id).SetAttribute("aria-checked", strconv.FormatBool(on))
}

func (m *Mod) settings() {
	if m.vault == nil {
		return
	}
	settings := m.vault.Settings()
	m.languageOptions()
	m.toggle("setting-reconnect", settings.Reconnect)
	m.toggle("setting-clipboard", settings.Clipboard)
}

func (m *Mod) setting(id string) {
	settings := m.vault.Settings()
	switch id {
	case "setting-reconnect":
		settings.Reconnect = !settings.Reconnect
	case "setting-clipboard":
		settings.Clipboard = !settings.Clipboard
	}
	m.toast(m.vault.SaveSettings(settings))
	m.settings()
	m.dirty = true
}

func (m *Mod) renderSession() {
	h, ok := m.findHost(m.selected)
	if !ok {
		return
	}
	m.text("rdp-title", h.Name)
	m.text("rdp-address", h.User+"@"+h.Endpoint())
	fullscreenLabel := "全屏"
	if m.fullscreen {
		fullscreenLabel = "窗口模式"
	}
	m.toolbarLabel("rdp-fullscreen", fullscreenLabel)
	m.renderToolbar()
	status := m.status
	m.text("rdp-status", m.statusText(status))
	if status.Width > 0 && status.Height > 0 {
		m.text("rdp-size", fmt.Sprintf("%d × %d", status.Width, status.Height))
	}
	m.hide("rdp-error", status.Error == nil)
	if status.Error != nil {
		m.text("rdp-error", i18n.ErrorText(m.language, status.Error))
	}
	m.class("rdp-frame", "rdp-frame")
	connecting := status.State == rdpclient.StateConnecting
	closed := status.State == rdpclient.StateClosed
	m.hide("rdp-overlay", !connecting && !closed)
	if connecting {
		m.text("rdp-overlay-text", m.tr("正在连接…"))
		m.hide("rdp-overlay-retry", true)
	} else if closed {
		m.text("rdp-overlay-text", m.tr("连接已断开"))
		m.hide("rdp-overlay-retry", false)
	}
}

// remoteKeys translates keyboard input into remote scancodes. Returning true
// keeps the local text field from also editing its own buffer.
func (m *Mod) remoteKeys(state *input.State) bool {
	if m.page != "rdp" || m.client == nil {
		return false
	}
	if m.confirm != "" {
		return true
	}
	status := m.client.Status()
	if status.State != rdpclient.StateConnected {
		return true
	}
	if state.KeyPressed(input.KeyF11) && m.host != nil && m.host.ToggleFullscreen != nil {
		m.toggleFullscreen()
		return true
	}
	if m.fullscreen && state.KeyPressed(input.KeyEscape) {
		m.toggleFullscreen()
		return true
	}
	handled := false
	for _, key := range rdpclient.Keycodes() {
		pressed, released := state.KeyPressed(key), state.KeyReleased(key)
		if !pressed && !released {
			continue
		}
		mapped, _ := rdpclient.Scancode(key)
		if pressed {
			m.client.KeyDown(mapped)
		} else {
			m.client.KeyUp(mapped)
		}
		handled = true
	}
	if (state.KeyHeld(input.KeyLeftControl) || state.KeyHeld(input.KeyRightControl)) && state.KeyPressed(input.KeyV) {
		m.pushClipboard()
	}
	// Printable ASCII already travelled as a scancode, so re-sending it as
	// Unicode would type every character twice. Only characters outside that
	// range (CJK, accented letters, symbols from a composed layout) go out as
	// Unicode input.
	for _, edit := range state.TextEdits() {
		text := unicodeOnly(edit.Text)
		if text == "" {
			continue
		}
		m.client.SendText(text)
		handled = true
	}
	return handled
}

// unicodeOnly drops the characters a scancode already delivered.
func unicodeOnly(text string) string {
	var out strings.Builder
	for _, r := range text {
		if r < 0x20 || r > 0x7E {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (m *Mod) requestConfirm(kind, title, description string, args ...any) {
	m.confirm = kind
	m.text("confirm-title", m.tr(title))
	m.text("confirm-description", m.tr(description, args...))
	m.hide("confirm-modal", false)
}

func (m *Mod) OnMessage(c *module.Context, message module.Message) error {
	if m.vault == nil || message.Topic != module.InputTopic {
		return nil
	}
	event, ok := message.Value[module.Input]()
	if !ok {
		return module.ErrType
	}
	switch event.HTMLKind {
	case htmlui.EventInput:
		m.input(event.HTMLID, event.Value)
		return nil
	case htmlui.EventSubmit:
		if event.HTMLID == "rdp-input" {
			m.client.SendSequence([]rdpclient.Key{{Scancode: 0x1C}})
			m.resetInput(c)
			return nil
		}
	case htmlui.EventClick:
		m.click(c, event.HTMLID)
		if host := c.Host(); host != nil && host.RequestRender != nil {
			host.RequestRender()
		}
		return nil
	}
	if m.page == "rdp" {
		m.pointer(event)
	}
	return nil
}

func (m *Mod) input(id, value string) {
	if strings.HasPrefix(id, "field-") {
		m.fields[strings.TrimPrefix(id, "field-")] = value
		return
	}
	if id == "search" {
		m.query = value
		m.dirty = true
	}
}

// pointer forwards desktop mouse input. Coordinates are mapped from the CSS
// stage box onto remote pixels, so the pointer lands where it looks.
func (m *Mod) pointer(event module.Input) {
	if m.toolbarPointer(event.Event) {
		return
	}
	if m.client == nil || m.status.State != rdpclient.StateConnected {
		return
	}
	x, y, ok := m.remotePoint(event.Event.X, event.Event.Y)
	if !ok && (event.Event.Kind == scene.InputEventRelease || event.Event.Kind == scene.InputEventCancel) {
		// Releasing outside a scaled frame must not leave a remote button down.
		x, y, ok = m.remoteX, m.remoteY, true
	}
	if !ok {
		return
	}
	m.remoteX, m.remoteY = x, y
	switch event.Event.Kind {
	case scene.InputEventMove:
		m.client.MouseMove(x, y)
	case scene.InputEventPress:
		button := int(event.Event.Button)
		if button < 0 || button >= len(m.pressed) {
			return
		}
		m.pressed[button] = true
		// A tap on the frame is also how a phone gets its keyboard back; the
		// invisible text field only receives keys once it holds focus.
		m.focusInput = true
		m.client.MouseButton(button, true, x, y)
	case scene.InputEventRelease:
		button := int(event.Event.Button)
		// The engine's left-button events carry MouseLeft. Other desktop
		// buttons retain their identity in the direct native-event path.
		if button < 0 || button >= len(m.pressed) || !m.pressed[button] {
			for candidate, pressed := range m.pressed {
				if pressed {
					button = candidate
					break
				}
			}
		}
		if m.pressed[button] {
			m.client.MouseButton(button, false, x, y)
			m.pressed[button] = false
		}
	case scene.InputEventCancel:
		for button, pressed := range m.pressed {
			if pressed {
				m.client.MouseButton(button, false, x, y)
				m.pressed[button] = false
			}
		}
	case scene.InputEventWheel:
		notches := int(-event.Event.WheelY)
		if notches == 0 {
			notches = int(-event.Event.WheelX)
			m.client.Wheel(x, y, notches, true)
			return
		}
		m.client.Wheel(x, y, notches, false)
	}
}

// frameLayout reports how the remote framebuffer is drawn inside the stage:
// the scale factor and the top-left offset of the drawn area. It must match the
// CSS the page applies, otherwise the pointer lands away from the cursor.
func (m *Mod) frameLayout() (scale, offsetX, offsetY float32, ok bool) {
	bounds, ok := m.node("rdp-stage").Bounds()
	if !ok || bounds.Width <= 0 || bounds.Height <= 0 {
		return 0, 0, 0, false
	}
	status := m.status
	if status.Width <= 0 || status.Height <= 0 {
		return 0, 0, 0, false
	}
	// object-fit:contain letterboxes the frame, centred on the smaller ratio.
	scale = min(bounds.Width/float32(status.Width), bounds.Height/float32(status.Height))
	drawnWidth, drawnHeight := float32(status.Width)*scale, float32(status.Height)*scale
	return scale, (bounds.Width - drawnWidth) * .5, (bounds.Height - drawnHeight) * .5, true
}

// remotePoint converts a point in the HTML surface into remote framebuffer
// coordinates, or reports false when it lies outside the drawn frame.
func (m *Mod) remotePoint(x, y float32) (int, int, bool) {
	if m.overLocalUI(x, y) {
		return 0, 0, false
	}
	bounds, ok := m.node("rdp-stage").Bounds()
	if !ok {
		return 0, 0, false
	}
	scale, offsetX, offsetY, ok := m.frameLayout()
	if !ok {
		return 0, 0, false
	}
	status := m.status
	localX, localY := x-bounds.X-offsetX, y-bounds.Y-offsetY
	remoteX := int(localX / scale)
	remoteY := int(localY / scale)
	if remoteX < 0 || remoteY < 0 || remoteX >= status.Width || remoteY >= status.Height {
		return 0, 0, false
	}
	return remoteX, remoteY, true
}

func (m *Mod) resetInput(c *module.Context) { m.value(c, "rdp-input", "") }

func (m *Mod) click(c *module.Context, id string) {
	if prefix, value, ok := strings.Cut(id, ":"); ok {
		switch prefix {
		case "language":
			m.setLanguage(value)
		case "connect":
			m.openHost(value, true)
			m.resetInput(c)
		case "session":
			m.openHost(value, false)
			m.resetInput(c)
		case "edit":
			m.edit(c, value)
		case "favorite":
			if h, ok := m.findHost(value); ok {
				h.Favorite = !h.Favorite
				m.toast(m.vault.SaveHost(h))
				m.dirty = true
			}
		case "close":
			m.selected = value
			m.requestConfirm("disconnect", "断开连接？", "远程桌面会话将关闭，未保存的工作可能丢失。")
		}
		return
	}
	switch id {
	case "nav-hosts", "go-hosts":
		m.route("hosts")
	case "nav-sessions", "rdp-back":
		m.route("sessions")
	case "nav-settings":
		m.settings()
		m.route("settings")
	case "add-host", "empty-add":
		m.edit(c, "")
	case "filter-all":
		m.favoriteOnly = false
		m.class("filter-all", "filter selected")
		m.class("filter-favorite", "filter")
		m.node("filter-all").SetAttribute("aria-pressed", "true")
		m.node("filter-favorite").SetAttribute("aria-pressed", "false")
		m.dirty = true
	case "filter-favorite":
		m.favoriteOnly = true
		m.class("filter-all", "filter")
		m.class("filter-favorite", "filter selected")
		m.node("filter-all").SetAttribute("aria-pressed", "false")
		m.node("filter-favorite").SetAttribute("aria-pressed", "true")
		m.dirty = true
	case "field-clipboard":
		m.draft.Clipboard = !m.draft.Clipboard
		m.toggle("field-clipboard", m.draft.Clipboard)
	case "field-console":
		m.draft.ConsoleMode = !m.draft.ConsoleMode
		m.toggle("field-console", m.draft.ConsoleMode)
	case "quality-smooth", "quality-balanced", "quality-high":
		m.draft.Quality = strings.TrimPrefix(id, "quality-")
		m.renderQuality()
		m.dirty = true
	case "edit-save":
		m.save(c)
	case "edit-cancel":
		m.clearSecrets(c)
		m.route("hosts")
	case "edit-delete":
		m.requestConfirm("delete", "删除这台主机？", "本机会移除主机及其登录凭据。")
	case "close-all":
		m.requestConfirm("all", "断开全部连接？", "所有远程桌面会话将关闭。")
	case "rdp-fullscreen":
		m.toggleFullscreen()
		m.focusInput = true
	case "rdp-toolbar-toggle":
		m.hide("rdp-tooltip", true)
		m.toolbar.collapsed = !m.toolbar.collapsed
		m.renderToolbar()
		m.focusInput = true
	case "rdp-keys-toggle":
		m.hide("rdp-tooltip", true)
		m.toolbar.keys = !m.toolbar.keys
		m.renderToolbar()
		m.focusInput = true
	case "rdp-info-toggle":
		m.hide("rdp-tooltip", true)
		m.toolbar.info = !m.toolbar.info
		m.renderToolbar()
		m.focusInput = true
	case "rdp-close":
		m.requestConfirm("disconnect", "断开连接？", "远程桌面会话将关闭，未保存的工作可能丢失。")
	case "rdp-reconnect", "rdp-overlay-retry":
		if h, ok := m.findHost(m.selected); ok {
			m.connect(h)
			m.dirty = true
		}
	case "confirm-cancel":
		m.hide("confirm-modal", true)
		m.confirm = ""
	case "confirm-ok":
		m.confirmed(c)
	case "setting-reconnect", "setting-clipboard":
		m.setting(id)
	case "key-cad":
		m.client.SendSequence(rdpclient.CtrlAltDelete)
		m.focusInput = true
	case "key-alt-tab":
		m.client.SendSequence(rdpclient.AltTab)
		m.focusInput = true
	case "key-win":
		m.client.SendSequence(rdpclient.Win)
		m.focusInput = true
	case "key-esc":
		m.client.SendSequence(rdpclient.Escape)
		m.focusInput = true
	case "key-tab":
		m.client.SendSequence([]rdpclient.Key{{Scancode: 0x0F}})
		m.focusInput = true
	case "key-enter":
		m.client.SendSequence([]rdpclient.Key{{Scancode: 0x1C}})
		m.focusInput = true
	case "key-backspace":
		m.client.SendSequence([]rdpclient.Key{{Scancode: 0x0E}})
		m.focusInput = true
	}
}

func (m *Mod) toggleFullscreen() {
	if m.host == nil || m.host.ToggleFullscreen == nil {
		return
	}
	m.host.ToggleFullscreen()
	m.fullscreen = !m.fullscreen
	label := "全屏"
	if m.fullscreen {
		label = "窗口模式"
	}
	m.toolbarLabel("rdp-fullscreen", label)
	icon := "fullscreen"
	if m.fullscreen {
		icon = "window"
	}
	m.node("rdp-fullscreen-icon").SetAttribute("src", "icons/toolbar-"+icon+".svg")
	m.syncResolution()
	m.dirty = true
}

// Native surface dimensions include Retina/HiDPI pixels. The fullscreen
// transition may finish asynchronously, so Update keeps the request current.
func (m *Mod) syncResolution() {
	if m.client == nil {
		return
	}
	width, height := m.desktopResolution()
	if width > 0 && height > 0 {
		m.client.ResizeDesktop(width, height)
	}
}

func (m *Mod) desktopResolution() (int, int) {
	if m.fullscreen && m.host != nil {
		if m.host.Context != nil {
			return m.sessionQuality.Resolution(m.host.Context.Width, m.host.Context.Height)
		}
		if m.host.GetViewport != nil {
			return m.sessionQuality.Resolution(m.host.GetViewport())
		}
	}
	if h, ok := m.findHost(m.selected); ok {
		return m.sessionQuality.Resolution(h.Resolution())
	}
	return 0, 0
}

// Closing a session window returns to the address book's session list. Only
// a subsequent close with no session open should terminate the application.
func (m *Mod) closeRequested() bool {
	if !m.connected && m.page != "rdp" {
		return true
	}
	m.disconnect()
	if m.host != nil && m.host.RequestRender != nil {
		m.host.RequestRender()
	}
	return false
}

func (m *Mod) disconnect() {
	if m.client != nil {
		m.client.Close()
		m.status = m.client.Status()
	}
	m.connected = false
	m.pressed = [3]bool{}
	m.confirm = ""
	m.hide("confirm-modal", true)
	m.route("sessions")
}

func (m *Mod) confirmed(c *module.Context) {
	switch m.confirm {
	case "delete":
		id := m.draft.ID
		if err := m.vault.DeleteHost(id); err != nil {
			m.toast(err)
			break
		}
		if m.selected == id {
			m.client.Close()
			m.connected = false
			m.selected = ""
		}
		m.clearSecrets(c)
		m.listSignature = ""
		m.route("hosts")
	case "all", "disconnect":
		m.disconnect()
	}
	m.confirm = ""
	m.hide("confirm-modal", true)
	m.dirty = true
}

// Destroy releases the session and the GPU texture. The frame provider is put
// back so a remount starts from the project provider instead of a disposed one.
func (m *Mod) Destroy(c *module.Context) error {
	if m.fullscreen && m.host != nil && m.host.ToggleFullscreen != nil {
		m.host.ToggleFullscreen()
		m.fullscreen = false
	}
	if m.client != nil {
		m.client.Close()
	}
	if m.frame != nil {
		m.frame.Dispose()
		if surface, ok := c.Content().Get[htmlui.Surface](c.Content().Root()); ok && surface.Images != nil {
			if installed, ok := surface.Images.Provider.(*frameTexture); ok && installed == m.frame {
				surface.Images.Provider = m.frame.previous
			}
		}
	}
	return nil
}
