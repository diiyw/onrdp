//go:build darwin && !ios && !onix_drm

package main

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdbool.h>
static void activateMac(uintptr_t handle) {
	NSWindow *window = (NSWindow *)handle;
	if (window.miniaturized) [window deminiaturize:nil];
	[window makeKeyAndOrderFront:nil];
}

static int placeWindowMac(uintptr_t handle, int width, int height, bool center) {
	NSWindow *window = (NSWindow *)handle;
	if (!window || window.zoomed || window.miniaturized || (window.styleMask & NSWindowStyleMaskFullScreen)) return 0;
	NSRect frame = window.frame;
	NSRect next = frame;
	if (width > 0 && height > 0) next = [window frameRectForContentRect:NSMakeRect(0, 0, width, height)];
	next.origin = NSMakePoint(NSMinX(frame), NSMaxY(frame) - NSHeight(next));
	if (center) {
		NSScreen *screen = window.screen ?: NSScreen.mainScreen;
		if (!screen) return 0;
		NSRect work = screen.visibleFrame;
		next.origin.x = NSMinX(work) + MAX(0, floor((NSWidth(work) - NSWidth(next)) / 2));
		next.origin.y = NSMaxY(work) - NSHeight(next) - MAX(0, floor((NSHeight(work) - NSHeight(next)) / 2));
	}
	[window setFrame:next display:YES];
	return 1;
}
*/
import "C"

import "github.com/onix-engine/onix/window"

func activateWindow(ctx *window.Context) {
	C.activateMac(C.uintptr_t(ctx.NativeHandle()))
}

func placeWindow(ctx *window.Context, width, height int, center bool) bool {
	return C.placeWindowMac(C.uintptr_t(ctx.NativeHandle()), C.int(width), C.int(height), C.bool(center)) != 0
}
