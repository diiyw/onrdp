//go:build linux && !android && !onix_drm

package main

import (
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/render"
	"github.com/onix-engine/onix/window"
	"onrdp/internal/platform"
)

// Run under xvfb-run with ONRDP_TEST_NATIVE_INPUT=1. A second X client owns
// the desktop shortcuts, just as a window manager would.
func TestX11SessionKeyboardCapture(t *testing.T) {
	if os.Getenv("ONRDP_TEST_NATIVE_INPUT") != "1" {
		t.Skip("requires an isolated X11 display with XTEST")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := render.ActiveDevice()
	defer previous.Activate()
	session := window.NewContext(320, 200, "session", window.DefaultOptions())
	defer session.Destroy()
	focused, pointer, grab := nativeKeyboard(session)
	other := window.NewContext(320, 200, "launcher", window.DefaultOptions())
	defer other.Destroy()

	open := func(name string) uintptr {
		t.Helper()
		lib, err := purego.Dlopen(name, purego.RTLD_NOW)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = purego.Dlclose(lib) })
		return lib
	}
	x11, xtst := open("libX11.so.6"), open("libXtst.so.6")
	var openDisplay func(uintptr) uintptr
	var closeDisplay func(uintptr) int32
	var rootWindow func(uintptr) uintptr
	var setFocus func(uintptr, uintptr, int32, uintptr) int32
	var keycode func(uintptr, uintptr) uint8
	var grabKey func(uintptr, int32, uint32, uintptr, int32, int32, int32) int32
	var fakeKey func(uintptr, uint32, int32, uintptr) int32
	var warpPointer func(uintptr, uintptr, uintptr, int32, int32, uint32, uint32, int32, int32) int32
	var syncX func(uintptr, int32) int32
	var pending func(uintptr) int32
	var nextEvent func(uintptr, *[24]uintptr) int32
	purego.RegisterLibFunc(&openDisplay, x11, "XOpenDisplay")
	purego.RegisterLibFunc(&closeDisplay, x11, "XCloseDisplay")
	purego.RegisterLibFunc(&rootWindow, x11, "XDefaultRootWindow")
	purego.RegisterLibFunc(&setFocus, x11, "XSetInputFocus")
	purego.RegisterLibFunc(&warpPointer, x11, "XWarpPointer")
	purego.RegisterLibFunc(&keycode, x11, "XKeysymToKeycode")
	purego.RegisterLibFunc(&grabKey, x11, "XGrabKey")
	purego.RegisterLibFunc(&syncX, x11, "XSync")
	purego.RegisterLibFunc(&pending, x11, "XPending")
	purego.RegisterLibFunc(&nextEvent, x11, "XNextEvent")
	purego.RegisterLibFunc(&fakeKey, xtst, "XTestFakeKeyEvent")
	display := openDisplay(0)
	if display == 0 {
		t.Fatal("could not connect the window manager to X11")
	}
	defer closeDisplay(display)
	alt, tab, win := keycode(display, 0xffe9), keycode(display, 0xff09), keycode(display, 0xffeb)
	root := rootWindow(display)
	grabKey(display, int32(tab), 1<<3, root, 0, 1, 1)
	grabKey(display, int32(win), 0, root, 0, 1, 1)
	setFocus(display, session.NativeX11Window(), 1, 0)
	warpPointer(display, 0, session.NativeX11Window(), 0, 0, 0, 0, 160, 100)
	syncX(display, 0)
	if !focused() {
		t.Fatal("session did not acquire focus")
	}
	var events []input.Event
	releases := 0
	keyboard := &keyboardCapture{focused: focused, pointer: pointer, grab: grab, session: &platform.SessionKeyboard{
		OverFrame: func(x, y float32) bool { return x >= 0 && x < 320 && y >= 40 && y < 200 },
		Ready:     func() bool { return true },
		Release:   func() { releases++ },
		Key: func(key input.KeyCode, down bool) {
			kind := input.EventKeyUp
			if down {
				kind = input.EventKeyDown
			}
			events = append(events, input.Event{Kind: kind, Key: key})
		},
	}}
	keyboards := desktopKeyboards{windows: map[*keyboardCapture]struct{}{keyboard: {}}}
	keyboards.update()
	if keyboards.release == nil {
		t.Fatal("session did not acquire the native keyboard")
	}
	sendShortcuts := func() int {
		for _, key := range []struct {
			code uint8
			down int32
		}{{alt, 1}, {tab, 1}, {tab, 0}, {alt, 0}, {win, 1}, {win, 0}} {
			fakeKey(display, uint32(key.code), key.down, 0)
		}
		syncX(display, 0)
		presses := 0
		for pending(display) > 0 {
			var event [24]uintptr
			nextEvent(display, &event)
			if int32(event[0]) == 2 { // KeyPress
				presses++
			}
		}
		return presses
	}
	if n := sendShortcuts(); n != 0 {
		t.Fatalf("window manager received %d shortcuts during capture", n)
	}
	two := keycode(display, '2')
	for _, shift := range []uint8{keycode(display, 0xffe1), keycode(display, 0xffe2)} {
		fakeKey(display, uint32(shift), 1, 0)
		fakeKey(display, uint32(two), 1, 0)
		fakeKey(display, uint32(two), 0, 0)
		fakeKey(display, uint32(shift), 0, 0)
	}
	syncX(display, 0)

	dispatch := keyboards.handler(keyboard)
	session.OnKeydown(func(key input.KeyCode) {
		dispatch(&input.Dispatch{Event: input.Event{Kind: input.EventKeyDown, Key: key}})
	})
	session.OnKeyup(func(key input.KeyCode) {
		dispatch(&input.Dispatch{Event: input.Event{Kind: input.EventKeyUp, Key: key}})
	})
	loop := window.EventLoop{}
	loop.Add(session, func() (bool, error) { return false, nil }, nil)
	loop.BetweenFrames = func() error {
		session.RequestClose()
		return nil
	}
	if err := loop.Run(); err != nil {
		t.Fatal(err)
	}
	want := []input.Event{
		{Kind: input.EventKeyDown, Key: input.KeyLeftAlt}, {Kind: input.EventKeyDown, Key: input.KeyTab},
		{Kind: input.EventKeyUp, Key: input.KeyTab}, {Kind: input.EventKeyUp, Key: input.KeyLeftAlt},
		{Kind: input.EventKeyDown, Key: input.KeyLeftSuper}, {Kind: input.EventKeyUp, Key: input.KeyLeftSuper},
		{Kind: input.EventKeyDown, Key: input.KeyLeftShift}, {Kind: input.EventKeyDown, Key: input.KeyNum2},
		{Kind: input.EventKeyUp, Key: input.KeyNum2}, {Kind: input.EventKeyUp, Key: input.KeyLeftShift},
		{Kind: input.EventKeyDown, Key: input.KeyRightShift}, {Kind: input.EventKeyDown, Key: input.KeyNum2},
		{Kind: input.EventKeyUp, Key: input.KeyNum2}, {Kind: input.EventKeyUp, Key: input.KeyRightShift},
	}
	if !slices.Equal(events, want) {
		t.Fatalf("session received key transitions %v", events)
	}
	// The top of the window stands in for the toolbar, with focus unchanged.
	warpPointer(display, 0, session.NativeX11Window(), 0, 0, 0, 0, 160, 20)
	syncX(display, 0)
	keyboards.poll()
	if !focused() || releases != 1 || keyboards.active != nil || sendShortcuts() != 2 {
		t.Fatal("keyboard remained grabbed over local controls")
	}
	warpPointer(display, 0, session.NativeX11Window(), 0, 0, 0, 0, 160, 100)
	syncX(display, 0)
	keyboards.poll()
	if keyboards.active == nil || sendShortcuts() != 0 {
		t.Fatal("keyboard was not recaptured inside the remote frame")
	}
	warpPointer(display, 0, session.NativeX11Window(), 0, 0, 0, 0, 160, 220)
	syncX(display, 0)
	keyboards.poll()
	if !focused() || releases != 2 || keyboards.active != nil || sendShortcuts() != 2 {
		t.Fatal("keyboard remained grabbed outside the session window")
	}
	warpPointer(display, 0, session.NativeX11Window(), 0, 0, 0, 0, 160, 100)
	syncX(display, 0)
	keyboards.poll()
	setFocus(display, other.NativeX11Window(), 1, 0)
	syncX(display, 0)
	if focused() {
		t.Fatal("session still reports focus while the launcher is focused")
	}
	keyboards.update()
	if releases != 3 {
		t.Fatalf("capture release notified the remote session %d times, want 3", releases)
	}
	if n := sendShortcuts(); n != 2 {
		t.Fatalf("window manager received %d shortcuts after release, want 2", n)
	}
}
