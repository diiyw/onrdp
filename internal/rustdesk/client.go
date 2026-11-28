//go:build (linux && !android && (amd64 || arm64)) || (darwin && !ios) || (windows && (amd64 || arm64))

package rustdesk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"onrdp/internal/i18n"
	"onrdp/internal/rdpclient"
)

type Client struct {
	wake      func()
	network   Network
	quality   VideoQuality
	mu        sync.Mutex
	active    *session
	previous  <-chan struct{}
	status    rdpclient.Snapshot
	region    [1]rdpclient.Region
	displayed []byte
}

type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	signal chan struct{}
	id     [16]byte
	uuid   string

	mu               sync.Mutex
	events           []wireEvent
	inputs           []inputOperation
	originX, originY int
	pixels           []byte
	fresh            bool
	width, height    int
	mappedKeyboard   bool
	keyboardLocks    int32
	heldModifiers    uint8
}

type wireEvent struct {
	text    string
	display int
	frame   bool
}

type event struct {
	Name                string
	Type                string
	Title               string
	Text                string
	Displays            string
	CurrentDisplay      string `json:"current_display"`
	Display             string
	X, Y, Width, Height string
}

type displayGeometry struct {
	X, Y, Width, Height int
}

// The RustDesk core shares one transport and frame buffer between sessions for
// a peer. Each OnRDP window owns its transport until it finishes closing.
var activePeers sync.Map

func New(wake func(), network Network, quality VideoQuality) *Client {
	return &Client{wake: wake, network: network, quality: quality.Normalized()}
}

func (c *Client) Connect(config rdpclient.Config) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{ctx: ctx, cancel: cancel, signal: make(chan struct{}, 1)}
	_, _ = rand.Read(s.id[:])
	s.id[6] = s.id[6]&0x0f | 0x40
	s.id[8] = s.id[8]&0x3f | 0x80
	id := hex.EncodeToString(s.id[:])
	s.uuid = id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	c.mu.Lock()
	if c.active != nil {
		c.active.cancel()
	}
	c.active = s
	previous := c.previous
	done := make(chan struct{})
	c.previous = done
	c.status = rdpclient.Snapshot{State: rdpclient.StateConnecting, Generation: c.status.Generation}
	c.mu.Unlock()
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		var err error
		if s.ctx.Err() == nil {
			err = c.run(s, config)
		}
		c.mu.Lock()
		isCurrent := c.active == s
		if isCurrent {
			c.status.State, c.status.Error = rdpclient.StateClosed, err
			c.active = nil
		}
		c.mu.Unlock()
		if isCurrent {
			c.notify()
		}
	}()
}

func (c *Client) run(s *session, config rdpclient.Config) error {
	target, err := c.network.Target(config.Host)
	if err != nil {
		return err
	}
	core, err := loadCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "RustDesk:", err)
		return i18n.NewError("无法加载 RustDesk 1.5.3 核心库")
	}
	if s.ctx.Err() != nil {
		return nil
	}
	// RustDesk indexes transports by ID@server, independent of the public key.
	peer, _, _ := strings.Cut(target, "?")
	if _, busy := activePeers.LoadOrStore(peer, struct{}{}); busy {
		return i18n.NewError("此 RustDesk 主机已有打开的会话")
	}
	defer activePeers.Delete(peer)
	// The preset password is passed directly to the in-process library.
	result := core.add(core.bytes(s.id[:]), core.str(target), false, false, false, false, false,
		core.str(""), c.network.ForceRelay, core.str(config.Password), false, nil, core.str(""))
	message := "Could not create RustDesk session"
	core.syncValue(result, func(o *dartObject) { message = o.text() })
	if message != "" {
		return fmt.Errorf("RustDesk: %s", message)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = call(ctx, func(port int64) { core.close(port, core.bytes(s.id[:])) })
	}()
	for _, option := range []struct {
		name  string
		value bool
	}{
		{"disable-clipboard", !config.Clipboard},
		{"enable-file-copy-paste", false},
		{"disable-audio", true},
	} {
		current := false
		core.syncValue(core.getToggle(core.bytes(s.id[:]), core.str(option.name)), func(o *dartObject) { current = o.boolean() })
		if current != option.value {
			if _, err := s.call(func(port int64) { core.toggle(port, core.bytes(s.id[:]), core.str(option.name)) }); err != nil {
				return err
			}
		}
	}
	if err := s.applyQuality(core, c.quality); err != nil {
		return err
	}
	port, unregister := registerPort(s.receive)
	defer unregister()
	core.start(port, core.bytes(s.id[:]), core.str(target))
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	startupTimeout := deadline.C
	var displays []displayGeometry
	currentDisplay := 0
	keyboardConfigured := false
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-startupTimeout:
			return i18n.NewError("RustDesk 连接超时")
		case <-s.signal:
			s.mu.Lock()
			events, inputs := s.events, s.inputs
			s.events, s.inputs = nil, nil
			s.mu.Unlock()
			for _, e := range events {
				if e.frame {
					if e.display == currentDisplay && e.display < len(displays) {
						if c.readFrame(core, s, e.display, displays[e.display]) {
							deadline.Stop()
							startupTimeout = nil
						}
					} else {
						core.free(core.next(core.bytes(s.id[:]), uintptr(e.display)))
					}
					continue
				}
				if e.text == "close" {
					return nil
				}
				var msg event
				if json.Unmarshal([]byte(e.text), &msg) != nil {
					continue
				}
				switch msg.Name {
				case "peer_info", "sync_peer_info":
					if !keyboardConfigured {
						if err := s.configureKeyboard(core); err != nil {
							return err
						}
						keyboardConfigured = true
					}
					if msg.CurrentDisplay != "" {
						currentDisplay, _ = strconv.Atoi(msg.CurrentDisplay)
					}
					_ = json.Unmarshal([]byte(msg.Displays), &displays)
					if startupTimeout != nil {
						deadline.Reset(45 * time.Second)
					}
				case "switch_display":
					index, _ := strconv.Atoi(msg.Display)
					if index >= 0 && index < len(displays) {
						display := &displays[index]
						display.X, _ = strconv.Atoi(msg.X)
						display.Y, _ = strconv.Atoi(msg.Y)
						display.Width, _ = strconv.Atoi(msg.Width)
						display.Height, _ = strconv.Atoi(msg.Height)
						currentDisplay = index
					}
				case "msgbox":
					switch msg.Type {
					case "input-password", "re-input-password":
						return i18n.NewError("请在主机设置中填写正确的 RustDesk 密码")
					case "wait-remote-accept", "wait-remote-accept-nook":
						deadline.Reset(60 * time.Second)
					case "input-2fa":
						return i18n.NewError("此 RustDesk 主机要求双重认证")
					case "error":
						if msg.Text == "" {
							msg.Text = msg.Title
						}
						return fmt.Errorf("RustDesk: %s", msg.Text)
					}
				}
			}
			for _, input := range inputs {
				if _, err := s.call(func(port int64) { input(core, s, port, core.bytes(s.id[:])) }); err != nil {
					return err
				}
			}
		}
	}
}

// Set the options before session startup so RustDesk includes them at login.
// The custom quality setter also selects custom mode.
func (s *session) applyQuality(core *core, quality VideoQuality) error {
	mode := quality.Mode
	switch mode {
	case QualityCustom:
		if _, err := s.call(func(port int64) {
			core.setCustomQuality(port, core.bytes(s.id[:]), int32(quality.ImageQuality))
		}); err != nil {
			return err
		}
		_, err := s.call(func(port int64) {
			core.setCustomFPS(port, core.bytes(s.id[:]), int32(quality.FPS))
		})
		return err
	case "smooth":
		mode = "low"
	case "high":
		mode = "best"
	}
	_, err := s.call(func(port int64) { core.setQuality(port, core.bytes(s.id[:]), core.str(mode)) })
	return err
}

func (s *session) call(fn func(int64)) (string, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	return call(ctx, fn)
}

func (s *session) signalWorker() {
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

func (s *session) receive(object *dartObject) {
	fields := object.array()
	if len(fields) == 0 {
		return
	}
	var e wireEvent
	switch fields[0].integer() {
	case 0:
		if len(fields) < 2 {
			return
		}
		payload := fields[1].array()
		if len(payload) < 2 {
			return
		}
		switch payload[0].integer() {
		case 0:
			e.text = payload[1].text()
		case 1:
			e.display = payload[1].integer()
			if e.display < 0 {
				return
			}
			e.frame = true
		default:
			return
		}
	case 1:
		if len(fields) > 2 {
			msg, _ := json.Marshal(event{Name: "msgbox", Type: "error", Text: fields[2].text()})
			e.text = string(msg)
		}
	case 2:
		e.text = "close"
	default:
		return
	}
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	s.signalWorker()
}

func (c *Client) readFrame(core *core, s *session, index int, display displayGeometry) bool {
	defer func() { core.free(core.next(core.bytes(s.id[:]), uintptr(index))) }()
	w, h := display.Width, display.Height
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 || w*h > 1<<26 {
		return false
	}
	size := 0
	core.syncValue(core.getSize(core.bytes(s.id[:]), uintptr(index)), func(o *dartObject) { size = o.integer() })
	if size != w*h*4 {
		return false
	}
	source := core.rgba(s.uuid, uintptr(index))
	if source == nil {
		return false
	}
	c.mu.Lock()
	if c.active != s {
		c.mu.Unlock()
		return false
	}
	if cap(s.pixels) < size {
		s.pixels = make([]byte, size)
	}
	s.pixels = s.pixels[:size]
	copy(s.pixels, unsafe.Slice(source, size))
	if runtime.GOOS == "darwin" {
		for i := 0; i < len(s.pixels); i += 4 {
			s.pixels[i], s.pixels[i+2] = s.pixels[i+2], s.pixels[i]
		}
	}
	s.originX, s.originY, s.width, s.height = display.X, display.Y, w, h
	s.fresh = true
	if c.status.State != rdpclient.StateConnected || c.status.Width != w || c.status.Height != h {
		c.status.Generation++
	}
	c.status.State, c.status.Width, c.status.Height = rdpclient.StateConnected, w, h
	c.mu.Unlock()
	c.notify()
	return true
}

func (c *Client) notify() {
	if c.wake != nil {
		c.wake()
	}
}

func (c *Client) Status() rdpclient.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Client) Flush() ([]rdpclient.Region, int, int, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.active; s != nil && s.fresh {
		c.displayed, s.pixels = s.pixels, c.displayed
		s.fresh = false
		c.region[0] = rdpclient.Region{Pixels: c.displayed, Width: s.width, Height: s.height}
		return c.region[:], s.width, s.height, c.status.Generation
	}
	return nil, c.status.Width, c.status.Height, c.status.Generation
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.active != nil {
		c.active.cancel()
		c.active = nil
	}
	c.status.State, c.status.Error = rdpclient.StateClosed, nil
	c.mu.Unlock()
}

// The core reads and writes the system clipboard directly on desktop.
func (c *Client) TakeClipboard() (string, bool) { return "", false }
func (c *Client) SetClipboard(string)           {}
func (c *Client) ResizeDesktop(int, int)        {}
