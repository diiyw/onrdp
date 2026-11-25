package app

import (
	"context"
	"slices"
	"time"

	"onrdp/internal/rdpclient"
	"onrdp/internal/scrcpy"
)

func displayByID(displays []scrcpy.Display, id int) (scrcpy.Display, bool) {
	for _, d := range displays {
		if d.ID == id {
			return d, true
		}
	}
	return scrcpy.Display{}, false
}

// Consume the native close request while the app task exits and the same
// window switches back to the device's main display.
func (m *Mod) closeAndroidAppScreen() bool {
	mirror := m.mirror
	if mirror == nil {
		return false
	}
	if mirror.exitResult != nil || (mirror.returningHome && m.status.State == rdpclient.StateConnecting) {
		return true
	}
	if mirror.display == 0 || m.status.State != rdpclient.StateConnected || mirror.run == nil {
		return false
	}
	current, _ := displayByID(mirror.displays, mirror.display)
	if current.Home || (current.TasksKnown && current.App == "") {
		return false
	}
	if current.TaskID == 0 {
		m.showToast(m.tr("无法识别可单独关闭的应用任务"))
		m.refreshDisplays()
		return true
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	mirror.cancelExit, mirror.exitResult, mirror.exitRun = cancel, result, mirror.run
	wake := m.host.RequestRender
	mirror.scans.Go(func() {
		defer cancel()
		result <- mirror.tools.ExitApp(ctx, mirror.host.Address, current)
		wake()
	})
	return true
}

func (mirror *androidMirror) previousDisplay() int {
	for _, id := range slices.Backward(mirror.history) {
		if _, ok := displayByID(mirror.displays, id); ok {
			return id
		}
	}
	return 0
}

func (m *Mod) switchAndroidDisplay(id int) {
	mirror := m.mirror
	if id == mirror.display {
		return
	}
	if i := slices.Index(mirror.history, id); i >= 0 {
		mirror.history = mirror.history[:i]
	} else {
		mirror.history = append(mirror.history, mirror.display)
	}
	mirror.display = id
	mirror.retries = 0
	mirror.retryAfter = time.Time{}
	m.restartMirror()
}

func (mirror *androidMirror) updateDisplays(displays []scrcpy.Display) int {
	before := mirror.displays
	mirror.displays = displays
	first := !mirror.indexed
	mirror.indexed = true
	current, exists := displayByID(displays, mirror.display)
	old, _ := displayByID(before, mirror.display)
	focused := slices.IndexFunc(displays, func(d scrcpy.Display) bool { return d.Focused })
	focusChanged := focused >= 0 && (first || mirror.lastFocus != displays[focused].ID)
	if focused >= 0 {
		mirror.lastFocus = displays[focused].ID
	} else if first {
		mirror.lastFocus = -1
	}
	if mirror.pinned && exists {
		return mirror.display
	}
	mirror.pinned = false
	if focused >= 0 {
		d := displays[focused]
		previous, _ := displayByID(before, d.ID)
		if focusChanged || !exists || (previous.TasksKnown && previous.App == "" && d.App != "") {
			return d.ID
		}
	}
	if first {
		return mirror.display
	}
	// An injected launcher tap can leave global input focus on the launcher
	// even after the emulator opens the app on a separate display.
	for _, d := range slices.Backward(displays) {
		if d.ID == 0 || d.ID == mirror.display {
			continue
		}
		old, exists := displayByID(before, d.ID)
		newDisplay := !exists && (d.App != "" || (!d.TasksKnown && focused < 0))
		openedApp := exists && old.TasksKnown && old.App == "" && d.App != ""
		if newDisplay || openedApp {
			return d.ID
		}
	}
	if !exists || (mirror.display != 0 && current.TasksKnown && current.App == "" && old.App != "") {
		return mirror.previousDisplay()
	}
	return mirror.display
}

func (m *Mod) syncDisplayNavigation() {
	mirror := m.mirror
	select {
	case err := <-mirror.exitResult:
		run := mirror.exitRun
		mirror.exitResult, mirror.cancelExit, mirror.exitRun = nil, nil, nil
		if run == mirror.run && !run.cancelledForSwitch {
			if err != nil {
				m.toast(err)
			} else {
				mirror.pinned = false
				mirror.returningHome = true
				m.switchAndroidDisplay(0)
			}
		}
		m.refreshDisplays()
	default:
		if mirror.exitResult != nil {
			// Removing the task may change focus before adb returns.
			return
		}
	}
	select {
	case result := <-mirror.scan:
		mirror.scan, mirror.cancelScan = nil, nil
		if result.connectErr != nil {
			m.finishAndroid(result.connectErr)
			return
		}
		if result.display != mirror.display {
			m.refreshDisplays()
			return
		}
		oldMessage := mirror.message
		if result.err == nil {
			changed := !slices.Equal(mirror.displays, result.displays)
			next := mirror.updateDisplays(result.displays)
			if mirror.returningHome {
				// Adopt the post-exit focus as the new baseline. Android may
				// briefly focus another open app when its sibling disappears.
				next = 0
				mirror.returningHome = m.window.State != rdpclient.StateConnected
			}
			if m.window.State == rdpclient.StateConnected {
				mirror.message = ""
			}
			m.switchAndroidDisplay(next)
			if changed || oldMessage != mirror.message {
				m.renderDisplays()
				m.renderToolbar()
			}
		} else if m.window.State == rdpclient.StateConnected {
			mirror.message = m.tr("无法读取显示屏列表")
			if oldMessage != mirror.message {
				m.renderDisplays()
			}
		}
		if mirror.starting || (!mirror.retryAfter.IsZero() && !time.Now().Before(mirror.retryAfter)) {
			m.startMirrorRun()
		}
		m.fitAndroidWindow()
		mirror.nextScan = time.Now().Add(time.Second)
		if m.host.WakeAt != nil {
			m.host.WakeAt(mirror.nextScan)
		}
	default:
	}
	if !mirror.nextScan.IsZero() && !time.Now().Before(mirror.nextScan) {
		m.refreshDisplays()
	}
}
