package app

import (
	"fmt"
	"os"
	"slices"

	"onrdp/internal/i18n"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/store"
)

func (m *Mod) sessionChild() bool    { return m.window != nil && m.window.Host.ID != "" }
func (m *Mod) sessionLauncher() bool { return m.window != nil && m.window.OpenSession != nil }

func (m *Mod) startSessionChild() {
	h, ok := m.findHost(m.window.Host.ID)
	if !ok {
		m.text("l10n-40b", m.tr("无法打开远程桌面窗口"))
		m.showFatal(i18n.NewError("找不到远程主机"))
		m.publishSessionState(rdpclient.StateClosed)
		return
	}
	m.hide("rdp-back", true)
	m.host.SetWindowTitle(h.Name + " · OnRDP")
	m.openHost(h.ID, true)
}

func (m *Mod) openSessionWindow(h store.Host) {
	h.Password = ""
	session := &platform.DesktopWindow{ID: store.NewID(), Host: h, State: rdpclient.StateConnecting}
	session.OnChange = func() {
		m.dirty = true
		m.host.RequestRender()
	}
	session.OnClosed = func(err error) {
		if index := slices.Index(m.sessionWindows, session); index >= 0 {
			m.sessionWindows = slices.Delete(m.sessionWindows, index, index+1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			m.toast(i18n.NewError("无法打开远程桌面窗口"))
		} else if !session.Closing {
			m.toast(session.Err)
		}
		session.OnChange()
	}
	m.sessionWindows = append(m.sessionWindows, session)
	m.dirty = true
	m.window.OpenSession(session)
}

func (m *Mod) publishSessionState(state rdpclient.State) {
	if !m.sessionChild() || state == m.window.State {
		return
	}
	m.window.State = state
	if state == rdpclient.StateConnecting {
		m.window.Decoder = ""
	}
	if state == rdpclient.StateConnected && !m.fullscreen && m.window.Center != nil {
		m.window.Center()
	}
	if m.window.OnChange != nil {
		m.window.OnChange()
	}
}
