package app

import (
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onix-engine/onix/font"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	onixtext "github.com/onix-engine/onix/text"
	htmlui "github.com/onix-engine/onix/ui/html"

	"onrdp/internal/i18n"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/scrcpy"
	"onrdp/internal/store"
)

type deviceScan struct {
	devices []scrcpy.Device
	err     error
}

type androidPicker struct {
	devices []scrcpy.Device
	err     error
	pending chan deviceScan
	cancel  context.CancelFunc
}

func (m *Mod) refreshAndroidDevices() {
	if !m.sessionLauncher() || m.android.pending != nil {
		return
	}
	m.android = androidPicker{}
	tools, err := scrcpy.Resolve()
	if err != nil {
		m.android.err = err
		m.renderAndroidDevices()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	results := make(chan deviceScan, 1)
	m.android.pending, m.android.cancel = results, cancel
	m.renderAndroidDevices()
	wake := m.host.RequestRender
	go func() {
		devices, err := tools.Devices(ctx)
		cancel()
		results <- deviceScan{devices, err}
		wake()
	}()
}

func (m *Mod) syncAndroidDevices() {
	select {
	case result := <-m.android.pending:
		m.android = androidPicker{devices: result.devices, err: result.err}
		m.renderAndroidDevices()
	default:
	}
}

func (m *Mod) renderAndroidDevices() {
	list := m.node("android-devices")
	list.Clear()
	for _, device := range m.android.devices {
		label := device.Serial
		switch device.State {
		case "device":
		case "unauthorized":
			label += " · " + m.tr("请在设备上允许 USB 调试")
		case "offline":
			label += " · " + m.tr("离线")
		default:
			label += " · " + device.State
		}
		button := m.node("android-device-template").Clone()
		button.SetAttribute("id", "android-device:"+device.Serial)
		setNode(button, "device-name", device.Model)
		setNode(button, "device-serial", label)
		if device.State != "device" {
			button.SetAttribute("disabled", "")
		}
		list.Append(button)
	}
	message := ""
	switch {
	case m.android.pending != nil:
		message = m.tr("正在查找设备…")
	case m.android.err != nil:
		message = i18n.ErrorText(m.language, m.android.err)
	case len(m.android.devices) == 0:
		message = m.tr("未发现设备。请连接 USB 并允许调试，或输入无线调试地址。")
	}
	m.text("android-status", message)
	m.hide("android-status", message == "")
}

func (m *Mod) selectAndroidDevice(c *module.Context, serial string) {
	for _, device := range m.android.devices {
		if device.Serial != serial || device.State != "device" {
			continue
		}
		m.fields["adb"] = serial
		m.value(c, "field-adb", serial)
		if strings.TrimSpace(m.fields["name"]) == "" {
			m.fields["name"] = device.Model
			m.value(c, "field-name", device.Model)
		}
		// Network devices advertised by ADB can be reconnected by endpoint.
		// USB and mDNS serials must be passed to adb exactly as discovered.
		_, _, err := net.SplitHostPort(serial)
		m.draft.Wireless = err == nil
		m.renderHostProtocol()
		return
	}
}

func (m *Mod) androidDecoder(session *platform.DesktopWindow) string {
	decoder := session.Decoder
	if decoder == "" {
		return "scrcpy"
	}
	if decoder == "software" {
		return m.tr("软件解码")
	}
	return m.tr("%s 硬件解码", decoder)
}

// One Onix window owns the toolbar, frame texture, and active display.
type androidMirror struct {
	font          *onixtext.Font
	tools         scrcpy.Tools
	host          store.Host
	display       int // most recently requested display
	run           *mirrorRun
	displays      []scrcpy.Display
	scan          chan displayScan
	cancelScan    context.CancelFunc
	scans         sync.WaitGroup
	nextScan      time.Time
	indexed       bool
	lastFocus     int
	retries       int
	retryAfter    time.Time
	starting      bool
	pinned        bool
	returningHome bool
	fitPending    bool
	history       []int
	exitResult    chan error
	cancelExit    context.CancelFunc
	exitRun       *mirrorRun
	apkQueue      []string
	apkResult     chan string
	cancelAPK     context.CancelFunc
	message       string
	picker        bool
	pixels        []byte
}

type mirrorRun struct {
	cancelledForSwitch bool // UI thread only
	windowFitted       bool
	display            int
	width, height      int
	touching           bool
	keysDown           map[input.KeyCode]uint32
	native             *scrcpy.Session
	onTap              func()
	beforeTap          []scrcpy.Display
	launchUntil        time.Time
}

type displayScan struct {
	display    int
	displays   []scrcpy.Display
	err        error
	connectErr error
}

func (m *Mod) startAndroidChild(c *module.Context) {
	h, ok := m.findHost(m.window.Host.ID)
	if !ok || h.Protocol != store.Android {
		m.finishAndroid(i18n.NewError("找不到远程主机"))
		return
	}
	tools, err := scrcpy.Resolve()
	if err != nil {
		m.finishAndroid(err)
		return
	}
	m.host.SetWindowTitle(h.Name + " · OnRDP")
	m.selected = h.ID
	m.hide("rdp-back", true)
	m.mirror = &androidMirror{tools: tools, host: h, starting: true, displays: []scrcpy.Display{{ID: 0}}}
	// Each native window has its own graphics device. A process-wide glyph
	// atlas can lose letters when the launcher and toolbar upload to it.
	root := c.Content().Root()
	if surface, ok := c.Content().Get[htmlui.Surface](root); ok && surface.Font == nil {
		if face, err := font.LoadDefault(); err == nil {
			m.mirror.font = onixtext.NewFont(face)
			surface.Font = m.mirror.font
			if err := c.Content().Insert(root, surface); err != nil {
				m.finishAndroid(err)
				return
			}
		}
	}
	m.status = rdpclient.Snapshot{State: rdpclient.StateConnecting}
	m.route("rdp")
	m.renderToolbar()
	m.refreshDisplays()
}

func (m *Mod) finishAndroid(err error) {
	m.window.Err = err
	m.publishSessionState(rdpclient.StateClosed)
	m.host.RequestClose()
}

func (m *Mod) startMirrorRun() {
	mirror := m.mirror
	mirror.retryAfter = time.Time{}
	run := &mirrorRun{display: mirror.display, keysDown: make(map[input.KeyCode]uint32)}
	if run.display == 0 {
		run.onTap = func() {
			home, _ := displayByID(mirror.displays, 0)
			if home.Home {
				if time.Now().After(run.launchUntil) {
					run.beforeTap = mirror.displays
				}
				run.launchUntil = time.Now().Add(8 * time.Second)
			}
		}
	}
	mirror.run = run
	m.pressed = [3]bool{}
	m.status = rdpclient.Snapshot{State: rdpclient.StateConnecting}
	m.dirty = true
	mirror.message = m.tr("正在连接…")
	m.renderDisplays()
	m.publishSessionState(rdpclient.StateConnecting)
	target := scrcpy.Target{Address: mirror.host.Address, Wireless: mirror.host.Wireless && !mirror.starting, DisplayID: run.display}
	mirror.starting = false
	run.native = mirror.tools.Start(target, m.vault.Settings().Clipboard, m.host.RequestRender)
}

func (m *Mod) selectDisplay(value string) {
	mirror := m.mirror
	if mirror == nil || mirror.starting {
		return
	}
	if value == "follow" {
		mirror.returningHome = false
		mirror.pinned, mirror.picker = false, false
		if i := slices.IndexFunc(mirror.displays, func(d scrcpy.Display) bool { return d.Focused }); i >= 0 {
			m.switchAndroidDisplay(mirror.displays[i].ID)
		}
		m.refreshDisplays()
		m.renderDisplays()
		m.renderToolbar()
		return
	}
	id, err := strconv.Atoi(value)
	if err != nil || !slices.ContainsFunc(mirror.displays, func(d scrcpy.Display) bool { return d.ID == id }) {
		return
	}
	mirror.pinned, mirror.picker = true, false
	mirror.returningHome = false
	m.renderDisplays()
	m.renderToolbar()
	if mirror.display == id {
		if mirror.run == nil {
			mirror.retries = 0
			m.startMirrorRun()
		}
		return
	}
	m.switchAndroidDisplay(id)
}

func (m *Mod) restartMirror() {
	mirror := m.mirror
	if mirror.starting {
		return
	}
	if mirror.run == nil {
		m.startMirrorRun()
		return
	}
	mirror.run.cancelledForSwitch = true
	if mirror.cancelScan != nil {
		mirror.cancelScan()
	}
	m.status.State = rdpclient.StateConnecting
	m.publishSessionState(rdpclient.StateConnecting)
	m.dirty = true
	mirror.run.native.Close()
	mirror.message = m.tr("正在切换屏幕…")
	m.renderDisplays()
}

func (m *Mod) refreshDisplays() {
	mirror := m.mirror
	if mirror == nil || mirror.scan != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	scan := make(chan displayScan, 1)
	mirror.scan, mirror.cancelScan = scan, cancel
	display := mirror.display
	connect := mirror.starting && mirror.host.Wireless
	var beforeTap []scrcpy.Display
	if run := mirror.run; run != nil && !run.cancelledForSwitch && time.Now().Before(run.launchUntil) {
		beforeTap = run.beforeTap
	}
	wake := m.host.RequestRender
	mirror.scans.Go(func() {
		defer cancel()
		result := displayScan{display: display}
		if connect {
			result.connectErr = mirror.tools.Connect(ctx, mirror.host.Address)
		}
		if result.connectErr == nil {
			if beforeTap != nil {
				result.displays, result.err = mirror.tools.DisplaysAfterLauncherTap(ctx, mirror.host.Address, beforeTap)
			} else {
				result.displays, result.err = mirror.tools.Displays(ctx, mirror.host.Address)
			}
			if result.err == nil {
				focus := mirror.tools.FocusedDisplay(ctx, mirror.host.Address)
				for i := range result.displays {
					result.displays[i].Focused = result.displays[i].ID == focus
				}
			}
		}
		scan <- result
		wake()
	})
}

func (m *Mod) syncMirror() {
	mirror := m.mirror
	m.syncDisplayNavigation()
	m.syncAndroidAPKs()
	run := mirror.run
	if run == nil {
		return
	}
	done, err := run.native.State()
	if done {
		if run == mirror.exitRun && !run.cancelledForSwitch {
			// Removing an app display can end the stream before adb replies.
			return
		}
		run.native.Wait()
		mirror.run = nil
		if run.cancelledForSwitch {
			m.startMirrorRun()
		} else if err != nil && run.display != 0 {
			if mirror.retries < 2 {
				mirror.retries++
				mirror.retryAfter = time.Now().Add(time.Duration(mirror.retries) * time.Second)
				mirror.message = m.tr("正在连接…")
				m.status = rdpclient.Snapshot{State: rdpclient.StateConnecting}
			} else {
				mirror.message = i18n.ErrorText(m.language, err)
				m.status = rdpclient.Snapshot{State: rdpclient.StateClosed, Error: err}
				mirror.picker = true
				m.toolbar.collapsed = false
			}
			m.dirty = true
			m.publishSessionState(m.status.State)
			m.refreshDisplays()
			m.renderDisplays()
		} else {
			m.finishAndroid(err)
		}
		return
	}
	if run.cancelledForSwitch {
		return
	}
	if text, ok := run.native.TakeClipboard(); ok && m.host.SetClipboardText != nil {
		m.host.SetClipboardText(text)
	}
	pixels, width, height, decoder := run.native.Frame(mirror.pixels)
	if pixels == nil {
		return
	}
	mirror.pixels = pixels
	if width != run.width || height != run.height {
		m.dirty = true
		mirror.fitPending = true
		run.width, run.height = width, height
		m.fitAndroidWindow()
	}
	m.status.Width, m.status.Height = width, height
	region := rdpclient.Region{Width: width, Height: height, Pixels: pixels}
	if m.frame.Apply([]rdpclient.Region{region}, width, height, 0) {
		m.node("rdp-frame").SetAttribute("src", m.frame.Source())
	}
	if m.window.State != rdpclient.StateConnected {
		mirror.retries = 0
		mirror.message = ""
		m.status.State = rdpclient.StateConnected
		m.dirty = true
		m.window.Decoder = decoder
		m.publishSessionState(rdpclient.StateConnected)
		if mirror.returningHome && run.display == 0 {
			m.pressAndroidButton("home")
		}
		// A wireless connection has an addressable ADB transport after startup.
		m.refreshDisplays()
		m.renderDisplays()
	}
}

func (m *Mod) fitAndroidWindow() {
	mirror := m.mirror
	run := mirror.run
	if mirror.fitPending && !m.fullscreen && run != nil && !run.cancelledForSwitch && run.width > 0 && m.window.FitFrame != nil {
		if m.window.FitFrame(run.width, run.height, !run.windowFitted) {
			run.windowFitted = true
			mirror.fitPending = false
		}
	}
}

func (m *Mod) renderDisplays() {
	mirror := m.mirror
	list := m.node("screens-list")
	list.Clear()
	m.appendDisplayOption("follow", m.tr("跟随当前激活屏幕"), !mirror.pinned)
	for _, display := range mirror.displays {
		name := m.tr("显示屏 %d", display.ID)
		if display.Name != "" {
			name += " · " + display.Name
		}
		if display.App != "" {
			name += " · " + display.App
		}
		if display.Focused {
			name += " · " + m.tr("当前激活")
		}
		m.appendDisplayOption(strconv.Itoa(display.ID), name, mirror.pinned && mirror.display == display.ID)
	}
	m.text("screens-status", mirror.message)
	m.hide("screens-status", mirror.message == "")
}

func (m *Mod) appendDisplayOption(value, name string, selected bool) {
	button := m.node("language-template").Clone()
	button.SetAttribute("id", "display:"+value)
	setNode(button, "language-name", name)
	button.SetAttribute("aria-pressed", strconv.FormatBool(selected))
	if selected {
		button.SetAttribute("class", "language-option selected")
		setNode(button, "language-mark", "✓")
	}
	m.node("screens-list").Append(button)
}

func (mirror *androidMirror) close() {
	if mirror.cancelAPK != nil {
		mirror.cancelAPK()
	}
	if mirror.cancelExit != nil {
		mirror.cancelExit()
	}
	if mirror.cancelScan != nil {
		mirror.cancelScan()
	}
	if mirror.run != nil {
		mirror.run.native.Close()
		mirror.run.native.Wait()
	}
	mirror.scans.Wait()
	if mirror.font != nil {
		_ = mirror.font.Dispose()
	}
}
