//go:build darwin && !ios && !onix_drm

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

static BOOL keyboardFocused(NSWindow *window) {
	return NSApp.active && window.isKeyWindow && !window.attachedSheet;
}

static uintptr_t grabKeyboard(uintptr_t handle) {
	NSWindow *window = (NSWindow *)handle;
	if (!keyboardFocused(window)) return 0;
	// An app-local monitor delivers window events before menu equivalents.
	NSEventMask mask = NSEventMaskKeyDown | NSEventMaskKeyUp | NSEventMaskFlagsChanged;
	id monitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *key) {
		if ((key.window ?: NSApp.keyWindow) != window || !keyboardFocused(window))
			return key;
		NSView *view = window.contentView;
		switch (key.type) {
		case NSEventTypeKeyDown: [view keyDown:key]; break;
		case NSEventTypeKeyUp: [view keyUp:key]; break;
		case NSEventTypeFlagsChanged: [view flagsChanged:key]; break;
		default: return key;
		}
		return nil;
	}];
	return (uintptr_t)monitor;
}

static void releaseKeyboard(uintptr_t monitor) {
	[NSEvent removeMonitor:(id)monitor];
}

static int focusedHandle(uintptr_t handle) { return keyboardFocused((NSWindow *)handle); }

static int clientPointer(uintptr_t handle, double *x, double *y) {
	NSWindow *window = (NSWindow *)handle;
	NSPoint screen = NSEvent.mouseLocation;
	if ([NSWindow windowNumberAtPoint:screen belowWindowWithWindowNumber:0] != window.windowNumber)
		return 0;
	NSView *view = window.contentView;
	NSPoint point = [view convertPoint:[window convertPointFromScreen:screen] fromView:nil];
	if (!NSPointInRect(point, view.bounds)) return 0;
	*x = point.x;
	*y = point.y;
	return 1;
}
*/
import "C"

import "github.com/onix-engine/onix/window"

func nativeKeyboard(ctx *window.Context) (func() bool, func() (float32, float32, bool), func() func()) {
	id := C.uintptr_t(ctx.NativeHandle())
	return func() bool { return C.focusedHandle(id) != 0 },
		func() (float32, float32, bool) {
			var x, y C.double
			if C.clientPointer(id, &x, &y) == 0 {
				return 0, 0, false
			}
			logicalX, logicalY := ctx.GetLogicalXY(float64(x), float64(y))
			return float32(logicalX), float32(logicalY), true
		}, func() func() {
			grab := C.grabKeyboard(id)
			if grab == 0 {
				return nil
			}
			return func() { C.releaseKeyboard(grab) }
		}
}
