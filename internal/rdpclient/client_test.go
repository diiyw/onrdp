package rdpclient

import (
	"bytes"
	"sync"
	"testing"

	"github.com/onix-engine/onix/input"
)

// Every key the UI can emit must land in the PC/AT set-1 space, and the
// navigation cluster must carry the E0 prefix or servers treat it as the
// numeric keypad.
func TestScancodeMapping(t *testing.T) {
	for _, tt := range []struct {
		code input.KeyCode
		want Key
	}{
		{input.KeyEscape, Key{Scancode: 0x01}},
		{input.KeyNum1, Key{Scancode: 0x02}},
		{input.KeyEnter, Key{Scancode: 0x1C}},
		{input.KeyLeftControl, Key{Scancode: 0x1D}},
		{input.KeyA, Key{Scancode: 0x1E}},
		{input.KeySpace, Key{Scancode: 0x39}},
		{input.KeyF12, Key{Scancode: 0x58}},
		{input.KeyUp, Key{Scancode: 0x48, Extended: true}},
		{input.KeyLeft, Key{Scancode: 0x4B, Extended: true}},
		{input.KeyDelete, Key{Scancode: 0x53, Extended: true}},
		{input.KeyKPEnter, Key{Scancode: 0x1C, Extended: true}},
		{input.KeyRightControl, Key{Scancode: 0x1D, Extended: true}},
		{input.KeyRightAlt, Key{Scancode: 0x38, Extended: true}},
		{input.KeyLeftSuper, Key{Scancode: 0x5B, Extended: true}},
	} {
		got, ok := Scancode(tt.code)
		if !ok || got != tt.want {
			t.Fatalf("Scancode(%d) = %+v, %t; want %+v", tt.code, got, ok, tt.want)
		}
	}
}

// Unmapped keys must report false so callers fall back to Unicode input rather
// than sending a wrong scancode.
func TestScancodeRejectsUnknownKeys(t *testing.T) {
	for _, code := range []input.KeyCode{input.KeyWorld1, input.KeyWorld2, input.KeyPause, input.KeyKPEqual} {
		if key, ok := Scancode(code); ok {
			t.Fatalf("Scancode(%d) unexpectedly mapped to %+v", code, key)
		}
	}
}

// Every mapped key must be unique: two key codes sharing one scancode means one
// of them types the wrong character.
func TestScancodeMappingIsInjective(t *testing.T) {
	seen := make(map[Key]input.KeyCode, len(scancodes))
	for code, key := range scancodes {
		if previous, ok := seen[key]; ok {
			t.Fatalf("keys %d and %d share scancode %+v", previous, code, key)
		}
		seen[key] = code
	}
}

// Shortcut chords are pressed in order and must name only real keys.
func TestShortcutSequences(t *testing.T) {
	for name, sequence := range map[string][]Key{
		"CtrlAltDelete": CtrlAltDelete,
		"CtrlAltEnd":    CtrlAltEnd,
		"AltTab":        AltTab,
		"Win":           Win,
	} {
		if len(sequence) == 0 {
			t.Fatalf("%s is empty", name)
		}
		for _, key := range sequence {
			if key.Scancode == 0 {
				t.Fatalf("%s contains an empty scancode", name)
			}
		}
	}
	if CtrlAltDelete[2] != (Key{Scancode: 0x53, Extended: true}) {
		t.Fatal("Ctrl+Alt+Del must use the extended Delete scancode")
	}
}

func TestDirtyRectsCollapseIntoBounds(t *testing.T) {
	client := &Client{width: 1000, height: 800}
	for i := 0; i < maxDirtyRects; i++ {
		client.addDirtyLocked(Rect{X: i * 3, Y: i * 3, Width: 2, Height: 2})
	}
	if len(client.dirty) != maxDirtyRects {
		t.Fatalf("kept %d rects, want %d", len(client.dirty), maxDirtyRects)
	}
	client.addDirtyLocked(Rect{X: 500, Y: 500, Width: 10, Height: 10})
	if len(client.dirty) != 1 {
		t.Fatalf("overflow kept %d rects, want a single bounding box", len(client.dirty))
	}
	got := client.dirty[0]
	want := Rect{X: 0, Y: 0, Width: 510, Height: 510}
	if got != want {
		t.Fatalf("bounding box = %+v, want %+v", got, want)
	}
}

// memorySession supplies changing top-down pixels without a network server.
// Input methods are unused by the framebuffer tests.
type memorySession struct {
	pixels     []byte
	beforeDims func()
	beforeRead func()
	width      int
	height     int
	failRead   bool
}

func (s *memorySession) FramebufferDims() (int, int) {
	width, height := s.width, s.height
	if fn := s.beforeDims; fn != nil {
		s.beforeDims = nil
		fn()
	}
	return width, height
}
func (s *memorySession) Close() error { return nil }
func (s *memorySession) FramebufferRegion(dst []byte, x, y, width, height int) bool {
	if fn := s.beforeRead; fn != nil {
		s.beforeRead = nil
		fn()
	}
	if s.failRead || x < 0 || y < 0 || x+width > s.width || y+height > s.height {
		return false
	}
	for row := range height {
		offset := ((y+row)*s.width + x) * 4
		copy(dst[row*width*4:(row+1)*width*4], s.pixels[offset:offset+width*4])
	}
	return true
}

func newMemoryClient(width, height int) (*Client, *memorySession) {
	s := &memorySession{width: width, height: height, pixels: make([]byte, width*height*4)}
	return &Client{rdp: s, state: StateConnected, width: width, height: height, generation: 1, full: true}, s
}

func TestFlushReusesOneFrameAndPublishesLatestPixels(t *testing.T) {
	c, s := newMemoryClient(32, 24)
	regions, _, _, _ := c.Flush()
	pixels, region := &c.scratch[0], &regions[0]
	for i := range 10000 {
		// A slow consumer only sees the final contents, without losing damage.
		value := byte(i)
		for p := range s.pixels {
			s.pixels[p] = value
		}
		c.onDirty(Rect{Width: 32, Height: 24})
	}
	regions, width, height, generation := c.Flush()
	if len(regions) != 1 || width != 32 || height != 24 || generation != 1 {
		t.Fatalf("flush = %d regions, %dx%d generation %d", len(regions), width, height, generation)
	}
	if &c.scratch[0] != pixels || &regions[0] != region || cap(c.scratch) != 32*24*4 {
		t.Fatal("frame storage was replaced or grew beyond one frame")
	}
	if !bytes.Equal(regions[0].Pixels, s.pixels) {
		t.Fatal("flush published historical pixels")
	}
	regions, width, height, _ = c.Flush()
	if len(regions) != 0 || width != 32 || height != 24 {
		t.Fatal("unchanged frame was republished")
	}
}

func TestFlushSteadyStateHasZeroAllocations(t *testing.T) {
	c, _ := newMemoryClient(64, 64)
	c.Flush()
	allocs := testing.AllocsPerRun(1000, func() {
		for i := range maxDirtyRects {
			c.onDirty(Rect{X: i, Y: i, Width: 1, Height: 1})
		}
		regions, _, _, _ := c.Flush()
		if len(regions) != maxDirtyRects {
			t.Fatal("disjoint damage lost")
		}
	})
	if allocs != 0 {
		t.Fatalf("steady flush allocated %g objects", allocs)
	}
}

func TestFlushDiscardsInvalidatedBatch(t *testing.T) {
	for _, reason := range []string{"resize", "reconnect", "close", "read failure"} {
		t.Run(reason, func(t *testing.T) {
			c, s := newMemoryClient(32, 24)
			s.beforeRead = func() {
				switch reason {
				case "resize":
					s.width, s.height = 8, 8
					c.onResize(8, 8)
				case "reconnect":
					c.onResize(32, 24)
				case "close":
					c.Close()
				case "read failure":
					s.failRead = true
				}
			}
			regions, width, height, generation := c.Flush()
			if len(regions) != 0 || width != 0 || height != 0 || generation != 0 {
				t.Fatal("invalidated batch was published")
			}
			if reason == "close" {
				c.onDirty(Rect{Width: 32, Height: 24})
				if len(c.dirty) != 0 {
					t.Fatal("closed session accepted late damage")
				}
				return
			}
			s.failRead = false
			regions, width, height, _ = c.Flush()
			if len(regions) != 1 || regions[0].Width != width || regions[0].Height != height {
				t.Fatal("invalidated batch did not recover with a full repaint")
			}
			if cap(c.scratch) != width*height*4 {
				t.Fatal("resize retained more than one frame of scratch")
			}
		})
	}
}

func TestFlushDiscardsDimensionsFromOldGeneration(t *testing.T) {
	c, s := newMemoryClient(32, 24)
	s.beforeDims = func() {
		s.width, s.height = 8, 8
		c.onResize(8, 8)
	}
	regions, width, height, _ := c.Flush()
	if len(regions) != 0 || width != 0 || height != 0 || c.width != 8 || c.height != 8 {
		t.Fatal("flush overwrote current geometry with an expired dimensions read")
	}
	regions, width, height, _ = c.Flush()
	if len(regions) != 1 || width != 8 || height != 8 {
		t.Fatal("current generation did not recover")
	}
}

func TestFlushReadsCompletedPaintsDuringConcurrentUpdates(t *testing.T) {
	c, s := newMemoryClient(32, 24)
	var producer sync.WaitGroup
	producer.Go(func() {
		for i := range 1000 {
			c.paintMu.Lock()
			for p := range s.pixels {
				s.pixels[p] = byte(i)
			}
			c.onDirty(Rect{Width: 32, Height: 24})
			c.paintMu.Unlock()
		}
	})
	defer producer.Wait()
	for range 1000 {
		regions, _, _, _ := c.Flush()
		for _, region := range regions {
			for _, pixel := range region.Pixels {
				if pixel != region.Pixels[0] {
					t.Fatal("flush observed an incomplete paint batch")
				}
			}
		}
	}
}

func TestDirtyUnionDoesNotLoseOrDuplicateDamage(t *testing.T) {
	c := &Client{width: 64, height: 64}
	var changed [64][64]bool
	for i := range 1000 {
		x, y := (i*17)%64, (i*29)%64
		w, h := min(1+i%7, 64-x), min(1+i%11, 64-y)
		c.addDirtyLocked(Rect{X: x, Y: y, Width: w, Height: h})
		for py := y; py < y+h; py++ {
			for px := x; px < x+w; px++ {
				changed[py][px] = true
			}
		}
		var covered [64][64]bool
		for _, rect := range c.dirty {
			for py := rect.Y; py < rect.Y+rect.Height; py++ {
				for px := rect.X; px < rect.X+rect.Width; px++ {
					if covered[py][px] {
						t.Fatal("overlapping regions can exceed one frame of pixel storage")
					}
					covered[py][px] = true
				}
			}
		}
		for py := range changed {
			for px, dirty := range changed[py] {
				if dirty && !covered[py][px] {
					t.Fatal("merging discarded pixels that still need repainting")
				}
			}
		}
	}
}

// A rect that starts outside the desktop is clipped, and one entirely outside
// is dropped, so the uploader never addresses pixels that do not exist.
func TestDirtyRectsClipToDesktop(t *testing.T) {
	client := &Client{width: 100, height: 50}
	client.addDirtyLocked(Rect{X: -10, Y: -5, Width: 30, Height: 20})
	if len(client.dirty) != 1 || client.dirty[0] != (Rect{X: 0, Y: 0, Width: 20, Height: 15}) {
		t.Fatalf("clipped rect = %+v", client.dirty)
	}
	client.dirty = client.dirty[:0]
	client.addDirtyLocked(Rect{X: 200, Y: 200, Width: 10, Height: 10})
	if len(client.dirty) != 0 {
		t.Fatalf("off-screen rect was kept: %+v", client.dirty)
	}
}

func (*memorySession) SendKeyboardExtended(uint16, bool, bool) error { return nil }
func (*memorySession) SendUnicode(uint16, bool) error                { return nil }
func (*memorySession) SendMouse(int, int, uint16) error              { return nil }
func (*memorySession) SendMouseWheel(int, int, int, bool) error      { return nil }
func (*memorySession) SetClipboard(string) error                     { return nil }
func (*memorySession) ResizeDesktop(int, int)                        {}

func TestClipboardRejectsRetiredSession(t *testing.T) {
	c, old := newMemoryClient(2, 2)
	current := &memorySession{}
	c.rdp = current
	c.onRemoteClipboard(old, "stale")
	if _, ok := c.TakeClipboard(); ok {
		t.Fatal("retired session changed the clipboard")
	}
	c.onRemoteClipboard(current, "current")
	if text, ok := c.TakeClipboard(); !ok || text != "current" {
		t.Fatal("active clipboard text lost")
	}
	c.Close()
	c.onRemoteClipboard(current, "late")
	if _, ok := c.TakeClipboard(); ok {
		t.Fatal("closed session changed the clipboard")
	}
}
