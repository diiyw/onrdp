package rdpclient

/*
#cgo pkg-config: freerdp3 freerdp-client3 winpr3
#include <stdlib.h>
#include "native.h"
void onrdp_init(void);
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"runtime/cgo"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"
)

var initializeFreeRDP sync.Once
var errSessionClosed = errors.New("RDP session closed")

type inputEvent struct {
	kind        int
	flags, code uint16
	x, y        int
	text        string
}

// One worker owns all FreeRDP transport/input calls. Only AbortConnect is called
// from another goroutine, under nativeMu so the context cannot be freed beneath it.
// The Go framebuffer belongs to paintMu and survives native disconnect teardown.
type nativeSession struct {
	owner         *Client
	quality       Quality // immutable after publication to Client
	nativeMu      sync.Mutex
	native        *C.onrdp_context
	queueMu       sync.Mutex
	queue         []inputEvent
	done          chan struct{}
	stopped       chan struct{}
	once          sync.Once
	pixels        []byte
	width, height int
	desktopWidth  int // requested size, guarded by queueMu, survives reconnect
	desktopHeight int
}

func newNativeSession(c *Client) *nativeSession {
	return &nativeSession{owner: c, done: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *nativeSession) run(cfg Config) {
	if cfg.Port == 0 {
		cfg.Port = 3389
	}
	defer close(s.stopped)
	// Callbacks and input dispatch stay on the worker's native thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initializeFreeRDP.Do(func() { C.onrdp_init() })
	delay := time.Second
	everConnected := false
	for {
		select {
		case <-s.done:
			return
		default:
		}
		err, connected, retryable := s.attempt(cfg)
		everConnected = everConnected || connected
		c := s.owner
		c.mu.Lock()
		if c.rdp != s || c.closed {
			c.mu.Unlock()
			return
		}
		retry := cfg.Reconnect && everConnected && retryable
		c.err = err
		if retry {
			c.state = StateConnecting
		} else {
			c.state = StateClosed
		}
		c.mu.Unlock()
		c.notify()
		if !retry {
			return
		}
		if connected {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.done:
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (s *nativeSession) attempt(cfg Config) (error, bool, bool) {
	s.queueMu.Lock()
	if s.desktopWidth > 0 && s.desktopHeight > 0 {
		cfg.Width, cfg.Height = s.desktopWidth, s.desktopHeight
	}
	s.queueMu.Unlock()
	cfg.Width, cfg.Height = cfg.Quality.Resolution(cfg.Width, cfg.Height)
	handle := cgo.NewHandle(s)
	defer handle.Delete()
	host, user, password, domain := C.CString(cfg.Host), C.CString(cfg.Username), C.CString(cfg.Password), C.CString(cfg.Domain)
	ctx := C.onrdp_new(C.uintptr_t(handle), host, C.int(clamp(cfg.Port, 1, 65535)), user, password, domain,
		C.int(clamp(cfg.Width, 200, 8192)&^1), C.int(clamp(cfg.Height, 200, 8192)), boolInt(cfg.Clipboard), boolInt(cfg.Console),
		C.int(cfg.Quality.colorDepth()), boolInt(cfg.Quality.Normalized() != QualityHigh))
	C.free(unsafe.Pointer(host))
	C.free(unsafe.Pointer(user))
	C.free(unsafe.Pointer(password))
	C.free(unsafe.Pointer(domain))
	if ctx == nil {
		return errors.New("FreeRDP: cannot create native context"), false, false
	}
	s.nativeMu.Lock()
	select {
	case <-s.done:
		s.nativeMu.Unlock()
		C.onrdp_free(ctx)
		return errSessionClosed, false, false
	default:
	}
	s.native = ctx
	s.nativeMu.Unlock()
	s.queueMu.Lock()
	s.queue = nil // Never replay key/button events from a disconnected session.
	s.queueMu.Unlock()
	retryable := C.onrdp_run(ctx) != 0
	err := fmt.Errorf("FreeRDP: %s", C.GoString(C.onrdp_error(ctx)))
	s.nativeMu.Lock()
	s.native = nil
	C.onrdp_free(ctx)
	s.nativeMu.Unlock()
	c := s.owner
	c.mu.Lock()
	connected := c.rdp == s && c.state == StateConnected
	c.mu.Unlock()
	return err, connected, retryable
}

func boolInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (s *nativeSession) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.nativeMu.Lock()
		if s.native != nil {
			C.onrdp_abort(s.native)
		}
		s.nativeMu.Unlock()
		s.queueMu.Lock()
		s.queue = nil
		s.queueMu.Unlock()
	})
	return nil
}

func (s *nativeSession) enqueue(e inputEvent) error {
	s.queueMu.Lock()
	select {
	case <-s.done:
		s.queueMu.Unlock()
		return errSessionClosed
	default:
	}
	// Consecutive pointer motion only needs its most recent position. Never
	// coalesce across a key, button or wheel transition.
	n := len(s.queue)
	if n > 0 && e.kind == 3 && e.flags == 0x0800 && s.queue[n-1].kind == 3 && s.queue[n-1].flags == 0x0800 {
		s.queue[n-1] = e
	} else {
		s.queue = append(s.queue, e)
	}
	s.queueMu.Unlock()
	s.nativeMu.Lock()
	if s.native != nil {
		C.onrdp_wake(s.native)
	}
	s.nativeMu.Unlock()
	return nil
}
func (s *nativeSession) SendKeyboardExtended(code uint16, down, extended bool) error {
	var flags uint16
	if !down {
		flags |= 0x8000
	}
	if extended {
		flags |= 0x0100
	}
	return s.enqueue(inputEvent{kind: 1, flags: flags, code: code})
}
func (s *nativeSession) SendUnicode(code uint16, down bool) error {
	var flags uint16
	if !down {
		flags = 0x8000
	}
	return s.enqueue(inputEvent{kind: 2, flags: flags, code: code})
}
func (s *nativeSession) SendMouse(x, y int, flags uint16) error {
	if flags == 0 {
		flags = 0x0800
	}
	return s.enqueue(inputEvent{kind: 3, flags: flags, x: x, y: y})
}
func (s *nativeSession) SendMouseWheel(x, y, notches int, horizontal bool) error {
	flags := uint16(0x0200)
	if horizontal {
		flags = 0x0400
	}
	// A protocol wheel event carries a signed nine-bit rotation (120/notch).
	for notches != 0 {
		step := clamp(notches, -2, 2)
		rotation := step * 120
		if rotation < 0 {
			flags |= 0x0100
		}
		if err := s.enqueue(inputEvent{kind: 3, flags: flags | uint16(rotation&0x01ff), x: x, y: y}); err != nil {
			return err
		}
		notches -= step
	}
	return nil
}
func (s *nativeSession) SetClipboard(text string) error {
	if len(text) > 8*1024*1024 {
		return errors.New("clipboard text exceeds 8 MiB")
	}
	return s.enqueue(inputEvent{kind: 4, text: text})
}

func (s *nativeSession) ResizeDesktop(width, height int) {
	width, height = s.quality.Resolution(width, height)
	s.queueMu.Lock()
	s.desktopWidth, s.desktopHeight = clamp(width, 200, 8192)&^1, clamp(height, 200, 8192)
	s.queueMu.Unlock()
}

func (s *nativeSession) FramebufferDims() (int, int) { return s.width, s.height }
func (s *nativeSession) FramebufferRegion(dst []byte, x, y, width, height int) bool {
	if x < 0 || y < 0 || width <= 0 || height <= 0 || x+width > s.width || y+height > s.height || len(dst) < width*height*4 {
		return false
	}
	for row := 0; row < height; row++ {
		offset := ((y+row)*s.width + x) * 4
		copy(dst[row*width*4:(row+1)*width*4], s.pixels[offset:offset+width*4])
	}
	return true
}

//export goRDPReady
func goRDPReady(handle C.uintptr_t) {
	s := cgo.Handle(handle).Value().(*nativeSession)
	c := s.owner
	c.mu.Lock()
	if c.rdp == s && !c.closed {
		c.state = StateConnected
		c.err = nil
		c.generation++
		c.full = true
	}
	c.mu.Unlock()
	c.notify()
}

//export goRDPFrame
func goRDPFrame(handle C.uintptr_t, buffer unsafe.Pointer, width, height, stride, x, y, w, h C.int) {
	s := cgo.Handle(handle).Value().(*nativeSession)
	fw, fh, sp := int(width), int(height), int(stride)
	if buffer == nil || fw <= 0 || fh <= 0 || fw > 8192 || fh > 8192 || sp < fw*4 {
		return
	}
	rect := Rect{X: max(int(x), 0), Y: max(int(y), 0)}
	rect.Width = min(int(x+w), fw) - rect.X
	rect.Height = min(int(y+h), fh) - rect.Y
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	c := s.owner
	c.paintMu.Lock()
	c.mu.Lock()
	if c.rdp != s || c.closed {
		c.mu.Unlock()
		c.paintMu.Unlock()
		return
	}
	if s.width != fw || s.height != fh {
		s.pixels = make([]byte, fw*fh*4)
		s.width = fw
		s.height = fh
		c.width = fw
		c.height = fh
		c.generation++
		c.full = true
		c.dirty = c.dirty[:0]
	}
	source := unsafe.Slice((*byte)(buffer), sp*fh)
	for row := rect.Y; row < rect.Y+rect.Height; row++ {
		dest := s.pixels[(row*fw+rect.X)*4 : (row*fw+rect.X+rect.Width)*4]
		copy(dest, source[row*sp+rect.X*4:row*sp+(rect.X+rect.Width)*4])
		// RDP has no framebuffer transparency; ensure opaque texture pixels.
		for i := 3; i < len(dest); i += 4 {
			dest[i] = 255
		}
	}
	c.addDirtyLocked(rect)
	c.mu.Unlock()
	c.paintMu.Unlock()
	c.notify()
}

//export goRDPInput
func goRDPInput(handle C.uintptr_t, ctx *C.onrdp_context) C.int {
	s := cgo.Handle(handle).Value().(*nativeSession)
	select {
	case <-s.done:
		return 0
	default:
	}
	s.queueMu.Lock()
	events := s.queue
	s.queue = nil
	width, height := s.desktopWidth, s.desktopHeight
	s.queueMu.Unlock()
	if width > 0 && height > 0 {
		// Display-control failure must not tear down an otherwise usable
		// session. The native bridge retains pending sizes until caps arrive.
		C.onrdp_resize(ctx, C.int(width), C.int(height))
	}
	for _, e := range events {
		var ok C.int
		if e.kind == 4 {
			text := strings.ReplaceAll(strings.ReplaceAll(e.text, "\r\n", "\n"), "\n", "\r\n")
			units := utf16.Encode([]rune(text))
			bytes := make([]byte, len(units)*2)
			for i, u := range units {
				binary.LittleEndian.PutUint16(bytes[i*2:], u)
			}
			var ptr *C.uint8_t
			if len(bytes) > 0 {
				ptr = (*C.uint8_t)(unsafe.Pointer(&bytes[0]))
			}
			ok = C.onrdp_clipboard(ctx, ptr, C.size_t(len(bytes)))
		} else {
			ok = C.onrdp_input(ctx, C.int(e.kind), C.uint16_t(e.flags), C.uint16_t(e.code), C.int(clamp(e.x, 0, 65535)), C.int(clamp(e.y, 0, 65535)))
		}
		if ok == 0 {
			return 0
		}
	}
	return 1
}

//export goRDPClipboard
func goRDPClipboard(handle C.uintptr_t, data unsafe.Pointer, length C.int) {
	s := cgo.Handle(handle).Value().(*nativeSession)
	bytes := unsafe.Slice((*byte)(data), int(length))
	units := make([]uint16, 0, len(bytes)/2)
	for i := 0; i+1 < len(bytes); i += 2 {
		u := binary.LittleEndian.Uint16(bytes[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	s.owner.onRemoteClipboard(s, strings.ReplaceAll(string(utf16.Decode(units)), "\r\n", "\n"))
}
