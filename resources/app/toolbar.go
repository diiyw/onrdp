package app

import (
	"fmt"
	"strconv"

	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/scene"
	"onrdp/internal/rdpclient"
)

type floatingToolbar struct {
	x, y, grabX, grabY float32
	positioned         bool
	dragging           bool
	collapsed          bool
	keys, info         bool
}

var toolbarLabels = []struct{ id, key string }{
	{"rdp-drag", "移动工具条"},
	{"rdp-toolbar-toggle", "收起工具条"},
	{"rdp-back", "返回会话"},
	{"rdp-info-toggle", "连接信息"},
	{"rdp-fullscreen", "全屏"},
	{"rdp-reconnect", "重连"},
	{"rdp-keys-toggle", "远程快捷键"},
	{"rdp-close", "断开"},
	{"key-cad", "Ctrl+Alt+Del"},
	{"key-alt-tab", "Alt+Tab"},
	{"key-win", "Win"},
	{"key-esc", "Esc"},
	{"key-tab", "Tab"},
	{"key-enter", "Enter"},
	{"key-backspace", "退格"},
}

func (m *Mod) toolbarLabel(id, key string) {
	label := m.tr(key)
	m.node(id).SetAttribute("aria-label", label)
	m.node(id).SetAttribute("title", label)
}

func (m *Mod) renderToolbar() {
	class := "rdp-toolbar"
	label, icon := "收起工具条", "collapse"
	if m.toolbar.collapsed {
		class += " collapsed"
		label, icon = "展开工具条", "expand"
	}
	if m.toolbar.dragging {
		class += " dragging"
	}
	m.class("rdp-toolbar", class)
	m.toolbarLabel("rdp-toolbar-toggle", label)
	m.node("rdp-toolbar-toggle").SetAttribute("aria-expanded", strconv.FormatBool(!m.toolbar.collapsed))
	m.node("rdp-toolbar-toggle-icon").SetAttribute("src", "icons/toolbar-"+icon+".svg")
	m.hide("rdp-tools", m.toolbar.collapsed)
	m.hide("l10n-43", m.toolbar.collapsed || !m.toolbar.keys)
	m.hide("rdp-info", m.toolbar.collapsed || !m.toolbar.info)
	m.node("rdp-keys-toggle").SetAttribute("aria-pressed", strconv.FormatBool(m.toolbar.keys))
	m.node("rdp-info-toggle").SetAttribute("aria-pressed", strconv.FormatBool(m.toolbar.info))
	dot := "rdp-dot"
	if m.status.State == rdpclient.StateConnected {
		dot += " live"
	} else if m.status.State == rdpclient.StateClosed {
		dot += " closed"
	}
	m.class("rdp-dot", dot)
}

// Read actual layout sizes: the keys and details can change the toolbar's
// height, and resizing/fullscreen must never strand it outside the window.
func (m *Mod) layoutToolbar() {
	page, ok := m.node("rdp-page").Bounds()
	bar, found := m.node("rdp-toolbar").Bounds()
	if !ok || !found || bar.Width <= 0 || bar.Height <= 0 {
		return
	}
	if !m.toolbar.positioned {
		m.positionToolbar((page.Width-bar.Width)/2, 12)
		return
	}
	m.positionToolbar(m.toolbar.x, m.toolbar.y)
}

func (m *Mod) positionToolbar(x, y float32) {
	page, ok := m.node("rdp-page").Bounds()
	bar, found := m.node("rdp-toolbar").Bounds()
	if !ok || !found {
		return
	}
	x = max(0, min(x, page.Width-bar.Width))
	y = max(0, min(y, page.Height-bar.Height))
	if m.toolbar.positioned && x == m.toolbar.x && y == m.toolbar.y {
		return
	}
	m.toolbar.x, m.toolbar.y = x, y
	m.toolbar.positioned = true
	m.node("rdp-toolbar").SetAttribute("style", fmt.Sprintf("left:%.2fpx;top:%.2fpx", x, y))
}

func containsPoint(rect module.Rect, x, y float32) bool {
	return rect.Width > 0 && rect.Height > 0 && x >= rect.X && y >= rect.Y && x < rect.X+rect.Width && y < rect.Y+rect.Height
}

func (m *Mod) overLocalUI(x, y float32) bool {
	if m.confirm != "" {
		return true
	}
	bar, ok := m.node("rdp-toolbar").Bounds()
	return ok && containsPoint(bar, x, y)
}

// Intercept local gestures before the remote session sees them. The engine
// captures the drag handle, so moves/releases still arrive outside its box.
func (m *Mod) toolbarPointer(event scene.InputEvent) bool {
	if m.toolbar.dragging {
		switch event.Kind {
		case scene.InputEventMove:
			page, _ := m.node("rdp-page").Bounds()
			m.positionToolbar(event.X-page.X-m.toolbar.grabX, event.Y-page.Y-m.toolbar.grabY)
		case scene.InputEventRelease, scene.InputEventCancel:
			m.toolbar.dragging = false
			m.renderToolbar()
			m.focusInput = true
		}
		return true
	}
	if m.confirm != "" {
		m.hide("rdp-tooltip", true)
		return false // pointer() still releases any remote button held before the dialog.
	}
	if event.Kind == scene.InputEventPress && event.Button == input.MouseLeft {
		handle, ok := m.node("rdp-drag").Bounds()
		if ok && containsPoint(handle, event.X, event.Y) {
			bar, _ := m.node("rdp-toolbar").Bounds()
			m.toolbar.grabX, m.toolbar.grabY = event.X-bar.X, event.Y-bar.Y
			m.toolbar.dragging = true
			m.renderToolbar()
			m.hide("rdp-tooltip", true)
			return true
		}
	}
	if event.Kind == scene.InputEventMove || event.Kind == scene.InputEventEnter {
		m.toolbarTooltip(event.X, event.Y)
	} else {
		m.hide("rdp-tooltip", true)
	}
	// Releasing a remote drag over the toolbar must release its remote button.
	if event.Kind == scene.InputEventRelease || event.Kind == scene.InputEventCancel {
		for _, pressed := range m.pressed {
			if pressed {
				return false
			}
		}
	}
	return m.overLocalUI(event.X, event.Y)
}

// Onix treats title as metadata, so provide a rendered hover hint as well.
func (m *Mod) toolbarTooltip(x, y float32) {
	if !m.overLocalUI(x, y) {
		m.hide("rdp-tooltip", true)
		return
	}
	for _, item := range toolbarLabels {
		bounds, ok := m.node(item.id).Bounds()
		if !ok || !containsPoint(bounds, x, y) {
			continue
		}
		label := item.key
		if item.id == "rdp-toolbar-toggle" && m.toolbar.collapsed {
			label = "展开工具条"
		} else if item.id == "rdp-fullscreen" && m.fullscreen {
			label = "窗口模式"
		}
		page, _ := m.node("rdp-page").Bounds()
		bar, _ := m.node("rdp-toolbar").Bounds()
		tipX := max(0, min(bounds.X-page.X+bounds.Width/2-95, page.Width-190))
		tipY := bar.Y - page.Y + bar.Height + 6
		if tipY+32 > page.Height {
			tipY = max(0, bar.Y-page.Y-38)
		}
		m.text("rdp-tooltip", m.tr(label))
		m.node("rdp-tooltip").SetAttribute("style", fmt.Sprintf("left:%.2fpx;top:%.2fpx", tipX, tipY))
		m.hide("rdp-tooltip", false)
		return
	}
	m.hide("rdp-tooltip", true)
}
