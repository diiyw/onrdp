//go:build linux && !android && !onix_drm

package main

/*
#cgo pkg-config: x11 gl
#include <GL/glx.h>

static int keyboardFocused(Display *display, Window window) {
	Window focus;
	int revert;
	XGetInputFocus(display, &focus, &revert);
	return focus == window;
}

static int clientPointer(Display *display, Window window, int *x, int *y) {
	Window root, child;
	int rootX, rootY;
	unsigned int mask;
	return XQueryPointer(display, window, &root, &child, &rootX, &rootY, x, y, &mask);
}

static int grabKeyboard(Display *display, Window window) {
	Atom hint = XInternAtom(display, "_XWAYLAND_MAY_GRAB_KEYBOARD", True);
	if (hint != None) {
		XEvent event = {0};
		event.xclient.type = ClientMessage;
		event.xclient.window = window;
		event.xclient.message_type = hint;
		event.xclient.format = 32;
		event.xclient.data.l[0] = 1;
		XSendEvent(display, DefaultRootWindow(display), False,
		           SubstructureRedirectMask | SubstructureNotifyMask, &event);
	}
	return XGrabKeyboard(display, window, False, GrabModeAsync, GrabModeAsync, CurrentTime);
}
*/
import "C"

import "github.com/onix-engine/onix/window"

func nativeKeyboard(ctx *window.Context) (func() bool, func() (float32, float32, bool), func() func()) {
	// A grab delivers keys to the connection that requested it. The current
	// GLX context uses the same X connection as this window's input queue.
	display := C.glXGetCurrentDisplay()
	id := C.Window(ctx.NativeX11Window())
	if display == nil || id == 0 || C.glXGetCurrentDrawable() != C.GLXDrawable(id) {
		return ctx.IsFocused, nil, func() func() { return nil }
	}
	return func() bool {
			return C.keyboardFocused(display, id) != 0
		}, func() (float32, float32, bool) {
			var x, y C.int
			if C.clientPointer(display, id, &x, &y) == 0 {
				return 0, 0, false
			}
			logicalX, logicalY := ctx.GetLogicalXY(float64(x), float64(y))
			return float32(logicalX), float32(logicalY), true
		}, func() func() {
			if C.grabKeyboard(display, id) != C.GrabSuccess {
				return nil
			}
			return func() {
				// Closing the native window also closes its X connection.
				if ctx.NativeX11Window() != 0 {
					C.XUngrabKeyboard(display, C.CurrentTime)
					C.XSync(display, C.False)
				}
			}
		}
}
