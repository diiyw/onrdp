//go:build windows && !onix_drm

package main

/*
#cgo LDFLAGS: -limm32
#include <windows.h>
#include <imm.h>

static HWND sessionWindow;
static HHOOK keyboardHook;
static BYTE intercepted[256];

static LRESULT CALLBACK sessionShortcut(int code, WPARAM message, LPARAM data) {
	if (code != HC_ACTION || GetForegroundWindow() != sessionWindow)
		return CallNextHookEx(NULL, code, message, data);
	const KBDLLHOOKSTRUCT *key = (const KBDLLHOOKSTRUCT *)data;
	DWORD vk = key->vkCode;
	if (vk >= 256) return CallNextHookEx(NULL, code, message, data);
	BOOL up = key->flags & LLKHF_UP;
	BOOL ctrl = GetAsyncKeyState(VK_CONTROL) & 0x8000;
	BOOL alt = key->flags & LLKHF_ALTDOWN;
	// Intercept the Windows keys themselves so Win chords never activate the shell.
	BOOL modifier = (vk >= VK_LSHIFT && vk <= VK_RMENU);
	BOOL shortcut = vk == VK_LWIN || vk == VK_RWIN || vk == VK_SNAPSHOT ||
	                (vk >= VK_F1 && vk <= VK_F24) || ((alt || ctrl) && !modifier);
	if (up ? !intercepted[vk] : !shortcut)
		return CallNextHookEx(NULL, code, message, data);
	DWORD bits = 1 | (key->scanCode << 16);
	if (key->flags & LLKHF_EXTENDED) bits |= 1u << 24;
	if (intercepted[vk]) bits |= 1u << 30;
	if (up) bits |= 1u << 31;
	if (!PostMessageW(sessionWindow, up ? WM_KEYUP : WM_KEYDOWN, vk, (LPARAM)bits))
		return CallNextHookEx(NULL, code, message, data);
	intercepted[vk] = !up;
	return 1;
}

static int grabKeyboard(HWND window) {
	if (!window || GetForegroundWindow() != window) return 0;
	keyboardHook = SetWindowsHookExW(WH_KEYBOARD_LL, sessionShortcut, GetModuleHandleW(NULL), 0);
	if (!keyboardHook) return 0;
	sessionWindow = window;
	return 1;
}

static void releaseKeyboard(void) {
	UnhookWindowsHookEx(keyboardHook);
	keyboardHook = NULL;
	sessionWindow = NULL;
	ZeroMemory(intercepted, sizeof(intercepted));
}

static int clientPointer(HWND window, POINT *point) {
	RECT client;
	return GetCursorPos(point) && WindowFromPoint(*point) == window &&
	       ScreenToClient(window, point) && GetClientRect(window, &client) &&
	       PtInRect(&client, *point);
}
*/
import "C"

import (
	"unsafe"

	"github.com/onix-engine/onix/window"
)

func nativeKeyboard(ctx *window.Context) (func() bool, func() (float32, float32, bool), func() func()) {
	handle := (C.HWND)(unsafe.Pointer(ctx.NativeHandle()))
	// This session window uses key events and has no local editable fields.
	C.ImmAssociateContextEx(handle, nil, 0)
	return ctx.IsFocused, func() (float32, float32, bool) {
			var point C.POINT
			if C.clientPointer(handle, &point) == 0 {
				return 0, 0, false
			}
			x, y := ctx.GetLogicalXY(float64(point.x), float64(point.y))
			return float32(x), float32(y), true
		}, func() func() {
			if C.grabKeyboard(handle) == 0 {
				return nil
			}
			return func() { C.releaseKeyboard() }
		}
}
