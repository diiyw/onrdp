//go:build !android && !ios && !onix_drm

package main

import (
	"time"

	"github.com/onix-engine/onix/input"
	"onrdp/internal/platform"
)

type keyboardCapture struct {
	session *platform.SessionKeyboard
	focused func() bool
	pointer func() (x, y float32, inside bool)
	grab    func() func()
	wakeAt  func(time.Time)
}

func (k *keyboardCapture) ready() bool {
	if !k.session.Ready() || !k.focused() {
		return false
	}
	x, y, inside := k.pointer()
	return inside && k.session.OverFrame(x, y)
}

type desktopKeyboards struct {
	windows   map[*keyboardCapture]struct{}
	active    *keyboardCapture
	release   func()
	nextCheck time.Time
}

func (d *desktopKeyboards) handler(k *keyboardCapture) func(*input.Dispatch) {
	return func(dispatch *input.Dispatch) {
		switch dispatch.Event.Kind {
		case input.EventMouseMove, input.EventMouseDown, input.EventMouseUp:
			if d.active == k && !k.session.OverFrame(float32(dispatch.Event.X), float32(dispatch.Event.Y)) {
				d.stop()
			}
		case input.EventKeyDown, input.EventKeyUp:
			// Pointer and window focus can change before the next frame.
			d.update()
			if d.active == k {
				k.session.Key(dispatch.Event.Key, dispatch.Event.Kind == input.EventKeyDown)
				dispatch.Consume()
			}
		}
	}
}

func (d *desktopKeyboards) stop() {
	if d.release != nil {
		d.release()
	}
	d.active.session.Release()
	d.active, d.release = nil, nil
	d.nextCheck = time.Time{}
}

func (d *desktopKeyboards) poll() {
	d.update()
	if d.active == nil || d.active.wakeAt == nil {
		return
	}
	// A move directly outside the client area need not send a motion event
	// to this window. Keep checking even when the remote desktop is idle.
	now := time.Now()
	if !now.Before(d.nextCheck) {
		d.nextCheck = now.Add(25 * time.Millisecond)
		d.active.wakeAt(d.nextCheck)
	}
}

// Run even when the old window is idle or minimized. Release its grab before
// attempting a grab on the newly focused window.
func (d *desktopKeyboards) update() {
	if d.active != nil {
		if d.active.ready() {
			if d.release == nil {
				d.release = d.active.grab()
			}
			return
		}
		d.stop()
	}
	for k := range d.windows {
		if k.ready() {
			d.active, d.release = k, k.grab()
			return
		}
	}
}

func (d *desktopKeyboards) remove(k *keyboardCapture) {
	if k != nil && k == d.active {
		d.stop()
	}
	delete(d.windows, k)
}
