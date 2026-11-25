//go:build windows && !onix_drm

package main

/*
#include <windows.h>
#include <stdbool.h>
static void activateWin(HWND window) {
	if (IsIconic(window)) ShowWindow(window, SW_RESTORE);
	SetForegroundWindow(window);
}

static int placeWindowWin(HWND window, int width, int height, bool center) {
	RECT frame, client;
	if (IsZoomed(window) || IsIconic(window) || !GetWindowRect(window, &frame) || !GetClientRect(window, &client)) return 0;
	int frameW = frame.right - frame.left;
	int frameH = frame.bottom - frame.top;
	UINT flags = SWP_NOZORDER | SWP_NOACTIVATE | SWP_NOOWNERZORDER;
	if (width > 0 && height > 0) {
		frameW += width - client.right;
		frameH += height - client.bottom;
	} else {
		flags |= SWP_NOSIZE;
	}
	int x = 0, y = 0;
	if (center) {
		MONITORINFO info = {.cbSize = sizeof(info)};
		if (!GetMonitorInfoW(MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST), &info)) return 0;
		RECT work = info.rcWork;
		x = work.left + max(0, (work.right - work.left - frameW) / 2);
		y = work.top + max(0, (work.bottom - work.top - frameH) / 2);
	} else {
		flags |= SWP_NOMOVE;
	}
	return SetWindowPos(window, NULL, x, y, frameW, frameH, flags);
}
*/
import "C"

import (
	"github.com/onix-engine/onix/window"
	"unsafe"
)

func activateWindow(ctx *window.Context) {
	C.activateWin((C.HWND)(unsafe.Pointer(ctx.NativeHandle())))
}

func placeWindow(ctx *window.Context, width, height int, center bool) bool {
	return C.placeWindowWin((C.HWND)(unsafe.Pointer(ctx.NativeHandle())), C.int(width), C.int(height), C.bool(center)) != 0
}
