package rdpclient

import (
	"sync"
	"unicode/utf16"
)

// State is the lifecycle position of one connection. It is intentionally
// coarser than the protocol's own states: the UI only distinguishes these.
type State uint8

const (
	StateIdle State = iota
	StateConnecting
	StateConnected
	StateClosed
)

// Config is everything a connection needs. Reconnect retains credentials until
// the session closes; the vault keeps the durable copy.
type Config struct {
	Host      string
	Username  string
	Password  string
	Domain    string
	Port      int
	Width     int
	Height    int
	Quality   Quality
	Clipboard bool
	// Reconnect keeps retrying after an unexpected network drop instead of
	// surfacing the disconnect immediately.
	Reconnect bool
	// Console connects to the physical console session, which some servers
	// require while another user is logged in.
	Console bool
}

// Region is a rectangle of RGBA pixels that changed since the previous flush.
// Pixels is borrowed from the client's scratch buffer and stays valid until the
// next Flush call.
type Region struct {
	Pixels []byte
	X      int
	Y      int
	Width  int
	Height int
}

// Snapshot is a copy of the connection status taken without blocking callbacks.
type Snapshot struct {
	Error      error
	Width      int
	Height     int
	Generation uint64
	State      State
}

// Client owns one RDP session. Protocol callbacks arrive on the library's
// receive goroutine; everything they touch is mutex-guarded, and the loop
// thread learns about changes through wake and the dirty list.
type Client struct {
	wake       func()
	rdp        session
	err        error
	scratch    []byte
	dirty      []Rect
	dirtyStore [maxDirtyRects]Rect
	regions    [maxDirtyRects]Region
	pending    string
	clipboard  func(string)
	generation uint64
	width      int
	height     int
	mu         sync.Mutex
	paintMu    sync.Mutex // excludes protocol pixel writes while Flush reads
	state      State
	full       bool
	closed     bool
	hasPending bool
}

// session is the protocol surface used by the UI. Implementations serialize
// input on the protocol worker and lend framebuffer pixels through copying reads.
type session interface {
	FramebufferDims() (int, int)
	FramebufferRegion([]byte, int, int, int, int) bool
	Close() error
	SendKeyboardExtended(uint16, bool, bool) error
	SendUnicode(uint16, bool) error
	SendMouse(int, int, uint16) error
	SendMouseWheel(int, int, int, bool) error
	SetClipboard(string) error
	ResizeDesktop(int, int)
}

// Pointer flags from MS-RDPBCGR 2.2.8.1.1.3.1.1. The button bit stays set on
// release; PTRFLAGS_DOWN is what distinguishes a press from a release.
const (
	pduButton1 uint16 = 0x1000
	pduButton2 uint16 = 0x2000
	pduButton3 uint16 = 0x4000
	pduDown    uint16 = 0x8000
)

// Rect is a clipped desktop rectangle in top-down coordinates.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// MaxFrameRegions bounds per-flush region bookkeeping and texture upload lists.
const MaxFrameRegions = 48

// maxDirtyRects bounds per-flush region bookkeeping. Past it the accumulated
// rects collapse into their bounding box, which is cheaper than many uploads.
const maxDirtyRects = MaxFrameRegions

// New returns an idle client. wake is called from protocol goroutines whenever
// the UI has something new to render; it must be goroutine-safe.
func New(wake func()) *Client {
	return &Client{wake: wake}
}

// OnClipboardText installs the receiver for remote clipboard text. It runs on
// the protocol goroutine, so implementations must not touch the UI.
func (c *Client) OnClipboardText(fn func(string)) {
	c.mu.Lock()
	c.clipboard = fn
	c.mu.Unlock()
}

// Connect starts a session in the background and returns immediately. Progress
// is observable through Status; failures surface as StateClosed with an error.
func (c *Client) Connect(cfg Config) {
	c.mu.Lock()
	if c.state == StateConnecting || c.state == StateConnected {
		c.mu.Unlock()
		return
	}
	c.state = StateConnecting
	c.err = nil
	c.closed = false
	c.dirty = c.dirty[:0]
	c.full = true
	s := newNativeSession(c)
	s.quality = cfg.Quality.Normalized()
	c.rdp = s
	c.mu.Unlock()
	go s.run(cfg)
}

func (c *Client) onDirty(rect Rect) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.addDirtyLocked(rect)
	c.mu.Unlock()
	c.notify()
}

func (c *Client) onResize(width, height int) {
	c.mu.Lock()
	c.width, c.height = width, height
	c.generation++
	c.full = true
	c.dirty = c.dirty[:0]
	c.mu.Unlock()
	c.notify()
}

func (c *Client) onRemoteClipboard(source session, text string) {
	c.mu.Lock()
	if c.rdp != source || c.closed {
		c.mu.Unlock()
		return
	}
	fn := c.clipboard
	c.pending, c.hasPending = text, true
	c.mu.Unlock()
	if fn != nil {
		fn(text)
	}
	c.notify()
}

// TakeClipboard returns remote clipboard text once, so the UI can mirror it
// into the host clipboard on the loop thread.
func (c *Client) TakeClipboard() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasPending {
		return "", false
	}
	c.hasPending = false
	return c.pending, true
}

func (c *Client) notify() {
	if c.wake != nil {
		c.wake()
	}
}

func (c *Client) addDirtyLocked(rect Rect) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	if c.width > 0 && c.height > 0 {
		if rect.X < 0 {
			rect.Width += rect.X
			rect.X = 0
		}
		if rect.Y < 0 {
			rect.Height += rect.Y
			rect.Y = 0
		}
		rect.Width = min(rect.Width, c.width-rect.X)
		rect.Height = min(rect.Height, c.height-rect.Y)
		if rect.Width <= 0 || rect.Height <= 0 {
			return
		}
	}
	if c.dirty == nil {
		c.dirty = c.dirtyStore[:0]
	}
	// Keep only damage, never historical pixels. Overlapping updates collapse
	// before copying so repeated frames cannot multiply the pixel storage.
	for i := 0; i < len(c.dirty); {
		other := c.dirty[i]
		if rect.X < other.X+other.Width && other.X < rect.X+rect.Width &&
			rect.Y < other.Y+other.Height && other.Y < rect.Y+rect.Height {
			rect = unionRect(rect, other)
			last := len(c.dirty) - 1
			c.dirty[i] = c.dirty[last]
			c.dirty = c.dirty[:last]
			i = 0 // the larger bounds may now intersect earlier rectangles
		} else {
			i++
		}
	}
	if len(c.dirty) == maxDirtyRects {
		// Collapse into the bounding box: past this point one large upload
		// beats tracking dozens of small ones.
		first := c.dirty[0]
		for _, other := range c.dirty[1:] {
			first = unionRect(first, other)
		}
		c.dirty = append(c.dirty[:0], unionRect(first, rect))
		return
	}
	c.dirty = append(c.dirty, rect)
}

func unionRect(a, b Rect) Rect {
	x := min(a.X, b.X)
	y := min(a.Y, b.Y)
	return Rect{X: x, Y: y, Width: max(a.X+a.Width, b.X+b.Width) - x, Height: max(a.Y+a.Height, b.Y+b.Height) - y}
}

// Status reports the current lifecycle position and framebuffer geometry.
func (c *Client) Status() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Snapshot{State: c.state, Error: c.err, Width: c.width, Height: c.height, Generation: c.generation}
}

// Close cancels connection, reconnect and input immediately. Native resources
// are released by the protocol worker after it exits. It is safe to call
// repeatedly and from any goroutine.
func (c *Client) Close() {
	c.mu.Lock()
	client := c.rdp
	c.rdp = nil
	c.state = StateClosed
	c.closed = true
	c.dirty = c.dirty[:0]
	c.full = false
	c.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

// Flush returns the regions changed since the previous call together with the
// current framebuffer size. A changed generation means the caller must rebuild
// its texture; regions then describe the full frame. The returned slice and its
// pixels are borrowed until the next Flush; only one goroutine may call Flush.
// At a fixed resolution this path allocates only its initial one-frame scratch.
func (c *Client) Flush() (regions []Region, width, height int, generation uint64) {
	c.paintMu.Lock()
	defer c.paintMu.Unlock()
	clear(c.regions[:])
	c.mu.Lock()
	client := c.rdp
	state := c.state
	generation = c.generation
	c.mu.Unlock()
	if client == nil || state != StateConnected {
		return nil, 0, 0, 0
	}
	fbWidth, fbHeight := client.FramebufferDims()
	if fbWidth <= 0 || fbHeight <= 0 {
		return nil, 0, 0, 0
	}

	c.mu.Lock()
	if c.rdp != client || c.state != StateConnected || c.generation != generation {
		c.mu.Unlock()
		return nil, 0, 0, 0
	}
	if c.width != fbWidth || c.height != fbHeight {
		c.width, c.height = fbWidth, fbHeight
		c.generation++
		c.full = true
	}
	if c.full {
		c.dirty = append(c.dirtyStore[:0], Rect{Width: fbWidth, Height: fbHeight})
		c.full = false
	}
	var pendingStore [maxDirtyRects]Rect
	pending := pendingStore[:copy(pendingStore[:], c.dirty)]
	c.dirty = c.dirty[:0]
	generation = c.generation
	c.mu.Unlock()

	if len(pending) == 0 {
		return nil, fbWidth, fbHeight, generation
	}

	// Non-overlapping, clipped damage fits in one screen, even when callbacks
	// arrive much faster than rendering. Shrinking also releases oversized RAM.
	size := fbWidth * fbHeight * 4
	if cap(c.scratch) != size {
		c.scratch = make([]byte, size)
	}
	buffer := c.scratch[:size]

	regions = c.regions[:0]
	offset := 0
	for _, rect := range pending {
		size := rect.Width * rect.Height * 4
		pixels := buffer[offset : offset+size]
		if !client.FramebufferRegion(pixels, rect.X, rect.Y, rect.Width, rect.Height) {
			// A resize/reset invalidates the entire batch. Repaint fully on the
			// next flush instead of publishing a partially read old frame.
			c.mu.Lock()
			if c.rdp == client {
				c.full = true
			}
			c.mu.Unlock()
			clear(c.regions[:])
			return nil, 0, 0, 0
		}
		regions = append(regions, Region{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height, Pixels: pixels})
		offset += size
	}
	c.mu.Lock()
	current := c.rdp == client && c.state == StateConnected && c.generation == generation
	c.mu.Unlock()
	if !current {
		clear(c.regions[:])
		return nil, 0, 0, 0
	}
	return regions, fbWidth, fbHeight, generation
}

// Generation returns the current framebuffer generation, which changes on
// resize and reconnect.
func (c *Client) Generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

// KeyDown and KeyUp send one physical scancode, including the E0 prefix that
// navigation and right-hand modifier keys require.
func (c *Client) KeyDown(key Key) { c.sendKey(key, true) }
func (c *Client) KeyUp(key Key)   { c.sendKey(key, false) }

func (c *Client) sendKey(key Key, pressed bool) {
	c.mu.Lock()
	client := c.rdp
	c.mu.Unlock()
	if client == nil {
		return
	}
	_ = client.SendKeyboardExtended(key.Scancode, pressed, key.Extended)
}

// SendSequence presses every key in order and releases them in reverse, so
// modifier chords such as Ctrl+Alt+Del stay well formed.
func (c *Client) SendSequence(keys []Key) {
	for _, key := range keys {
		c.KeyDown(key)
	}
	for i := len(keys) - 1; i >= 0; i-- {
		c.KeyUp(keys[i])
	}
}

// SendText types a string through Unicode input events. This is how characters
// with no set-1 scancode, including CJK, reach the remote desktop.
func (c *Client) SendText(text string) {
	c.mu.Lock()
	client := c.rdp
	c.mu.Unlock()
	if client == nil {
		return
	}
	for _, r := range text {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			_ = client.SendKeyboardExtended(0x1C, true, false)
			_ = client.SendKeyboardExtended(0x1C, false, false)
			continue
		}
		for _, unit := range utf16.Encode([]rune{r}) {
			_ = client.SendUnicode(unit, true)
			_ = client.SendUnicode(unit, false)
		}
	}
}

// MouseMove reports a pointer position without any button transition.
func (c *Client) MouseMove(x, y int) { c.sendMouse(x, y, 0) }

// MouseButton sends a button transition. down selects press or release; the
// button flag stays set on release, which is what the protocol expects.
func (c *Client) MouseButton(button int, down bool, x, y int) {
	var flag uint16
	switch button {
	case 0:
		flag = pduButton1
	case 1:
		flag = pduButton2
	case 2:
		flag = pduButton3
	default:
		return
	}
	if down {
		flag |= pduDown
	}
	c.sendMouse(x, y, flag)
}

// Wheel sends wheel rotation in notches; positive scrolls up or right.
func (c *Client) Wheel(x, y, notches int, horizontal bool) {
	c.mu.Lock()
	client := c.rdp
	c.mu.Unlock()
	if client == nil || notches == 0 {
		return
	}
	_ = client.SendMouseWheel(x, y, notches, horizontal)
}

func (c *Client) sendMouse(x, y int, flags uint16) {
	c.mu.Lock()
	client := c.rdp
	c.mu.Unlock()
	if client == nil {
		return
	}
	_ = client.SendMouse(x, y, flags)
}

// SetClipboard publishes local clipboard text to the remote session.
func (c *Client) SetClipboard(text string) {
	c.mu.Lock()
	client := c.rdp
	c.mu.Unlock()
	if client == nil {
		return
	}
	_ = client.SetClipboard(text)
}

// ResizeDesktop requests real remote pixels through the display-control
// channel. Servers without that channel retain their negotiated resolution.
// Requests made while connecting are retained until the channel is ready.
func (c *Client) ResizeDesktop(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rdp != nil && !c.closed {
		c.rdp.ResizeDesktop(width, height)
	}
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}
