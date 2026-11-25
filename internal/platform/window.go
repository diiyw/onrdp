package platform

import (
	"github.com/onix-engine/onix/module"

	"onrdp/internal/rdpclient"
	"onrdp/internal/store"
)

// DesktopWindow is shared by a native window and the launcher on the UI thread.
type DesktopWindow struct {
	ID           string
	Host         store.Host // Session metadata, with the password cleared.
	State        rdpclient.State
	Decoder      string
	Err          error
	OpenSession  func(*DesktopWindow)
	Activate     func()
	Center       func()
	FitFrame     func(width, height int, center bool) bool
	OnChange     func()
	OnClosed     func(error)
	PendingTheme *bool
	Closing      bool
	native       *module.Host
}

// The generated registry constructs each module. Bind its window by host while
// the module mounts, then let the module retain the binding for its lifetime.
var mountingWindows = make(map[*module.Host]*DesktopWindow)

func (w *DesktopWindow) Bind(host *module.Host) func() {
	mountingWindows[host] = w
	w.native = host
	return func() { delete(mountingWindows, host) }
}

func WindowFor(host *module.Host) *DesktopWindow { return mountingWindows[host] }

func (w *DesktopWindow) SetTheme(dark bool) {
	w.PendingTheme = &dark
	if w.native != nil {
		w.native.RequestRender()
	}
}

func (w *DesktopWindow) Close() {
	if w.Closing {
		return
	}
	w.Closing = true
	if w.native != nil {
		w.native.RequestClose()
	}
}

func (w *DesktopWindow) DidClose(err error) {
	w.native = nil
	w.Activate = nil
	w.Center = nil
	w.FitFrame = nil
	if w.OnClosed != nil {
		w.OnClosed(err)
	}
	w.OnChange, w.OnClosed = nil, nil
}
