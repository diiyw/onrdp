//go:build linux && !android && !onix_drm

package main

/*
#cgo pkg-config: x11 xrandr
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/extensions/Xrandr.h>
#include <stdbool.h>
#include <string.h>
#include <sys/param.h>

static void activateX11(Window window) {
	Display *d = XOpenDisplay(NULL);
	if (!d) return;
	XMapRaised(d, window);
	XEvent event = {0};
	event.xclient.type = ClientMessage;
	event.xclient.window = window;
	event.xclient.message_type = XInternAtom(d, "_NET_ACTIVE_WINDOW", False);
	event.xclient.format = 32;
	event.xclient.data.l[0] = 1; // Application-initiated activation.
	event.xclient.data.l[1] = CurrentTime;
	XSendEvent(d, DefaultRootWindow(d), False, SubstructureRedirectMask | SubstructureNotifyMask, &event);
	XCloseDisplay(d);
}

static unsigned long readProperty(Display *d, Window w, const char *name, Atom expected,
                                  long offset, long limit, unsigned long *values) {
	Atom type;
	int format;
	unsigned long count = 0, remaining;
	unsigned char *data = NULL;
	if (XGetWindowProperty(d, w, XInternAtom(d, name, False), offset, limit, False, expected,
	                       &type, &format, &count, &remaining, &data) == Success && type == expected && format == 32 && data) {
		memcpy(values, data, count * sizeof(*values));
	} else {
		count = 0;
	}
	if (data) XFree(data);
	return count;
}

typedef struct { int x, y, w, h; } Rect;

static Rect intersection(Rect a, Rect b) {
	int x = MAX(a.x, b.x), y = MAX(a.y, b.y);
	return (Rect){x, y, MAX(0, MIN(a.x+a.w, b.x+b.w)-x), MAX(0, MIN(a.y+a.h, b.y+b.h)-y)};
}

static Rect monitorWorkArea(Display *d, XWindowAttributes *attr, Rect frame) {
	Rect area = {0, 0, WidthOfScreen(attr->screen), HeightOfScreen(attr->screen)};
	int major, minor, count;
	if (XRRQueryVersion(d, &major, &minor) && (major > 1 || (major == 1 && minor >= 5))) {
		XRRMonitorInfo *monitors = XRRGetMonitors(d, attr->root, True, &count);
		long best = -1;
		for (int i = 0; monitors && i < count; i++) {
			Rect monitor = {monitors[i].x, monitors[i].y, monitors[i].width, monitors[i].height};
			Rect overlap = intersection(frame, monitor);
			long coverage = (long)overlap.w * overlap.h;
			if (coverage > best) {
				area = monitor;
				best = coverage;
			}
		}
		if (monitors) XRRFreeMonitors(monitors);
	}
	unsigned long desktop = 0, work[4];
	readProperty(d, attr->root, "_NET_CURRENT_DESKTOP", XA_CARDINAL, 0, 1, &desktop);
	if (readProperty(d, attr->root, "_NET_WORKAREA", XA_CARDINAL, desktop * 4, 4, work) == 4) {
		Rect available = intersection(area, (Rect){work[0], work[1], work[2], work[3]});
		if (available.w && available.h) area = available;
	}
	return area;
}

static bool placeWindowX11(Display *d, Window window, int width, int height, bool center) {
	Atom states[64];
	unsigned long count = readProperty(d, window, "_NET_WM_STATE", XA_ATOM, 0, 64, states);
	Atom fullscreen = XInternAtom(d, "_NET_WM_STATE_FULLSCREEN", False);
	Atom zoomX = XInternAtom(d, "_NET_WM_STATE_MAXIMIZED_HORZ", False);
	Atom zoomY = XInternAtom(d, "_NET_WM_STATE_MAXIMIZED_VERT", False);
	for (unsigned long i = 0; i < count; i++) {
		if (states[i] == fullscreen || states[i] == zoomX || states[i] == zoomY) return false;
	}
	if (!center) return XResizeWindow(d, window, width, height);

	XWindowAttributes attr;
	Window child;
	int x, y;
	if (!XGetWindowAttributes(d, window, &attr) || attr.map_state != IsViewable ||
	    !XTranslateCoordinates(d, window, attr.root, 0, 0, &x, &y, &child)) return false;
	unsigned long edges[4] = {0}; // left, right, top, bottom
	readProperty(d, window, "_NET_FRAME_EXTENTS", XA_CARDINAL, 0, 4, edges);
	int borderW = edges[0] + edges[1], borderH = edges[2] + edges[3];
	Rect frame = {x - (int)edges[0], y - (int)edges[2], attr.width + borderW, attr.height + borderH};
	Rect work = monitorWorkArea(d, &attr, frame);
	if (width <= 0 || height <= 0) {
		width = attr.width;
		height = attr.height;
	}
	x = work.x + MAX(0, (work.w - width - borderW) / 2);
	y = work.y + MAX(0, (work.h - height - borderH) / 2);
	return XMoveResizeWindow(d, window, x, y, width, height);
}

static bool placeX11(Window window, int width, int height, bool center) {
	Display *d = XOpenDisplay(NULL);
	if (!d) return false;
	bool placed = placeWindowX11(d, window, width, height, center);
	XCloseDisplay(d);
	return placed;
}
*/
import "C"

import "github.com/onix-engine/onix/window"

func activateWindow(ctx *window.Context) {
	if id := ctx.NativeX11Window(); id != 0 {
		C.activateX11(C.Window(id))
	}
}

func placeWindow(ctx *window.Context, width, height int, center bool) bool {
	// The packaged Onix OpenGL backend uses X11 (or XWayland).
	id := ctx.NativeX11Window()
	return id != 0 && bool(C.placeX11(C.Window(id), C.int(width), C.int(height), C.bool(center)))
}
