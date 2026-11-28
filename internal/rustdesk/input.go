//go:build (linux && !android && (amd64 || arm64)) || (darwin && !ios) || (windows && (amd64 || arm64))

package rustdesk

import (
	"encoding/json"
	"slices"
	"strconv"

	"onrdp/internal/rdpclient"
)

type inputOperation func(*core, *session, int64, *wireBytes)

// Each mask includes the left and right USB keyboard modifier usages.
const (
	controlKeys = 0x11
	shiftKeys   = 0x22
	altKeys     = 0x44
	commandKeys = 0x88
)

func (s *session) configureKeyboard(c *core) error {
	const mode = "map"
	c.syncValue(c.supportsKeyMode(c.bytes(s.id[:]), c.str(mode)), func(value *dartObject) {
		s.mappedKeyboard = value.boolean()
	})
	if !s.mappedKeyboard {
		return nil
	}
	_, err := s.call(func(port int64) { c.setKeyMode(port, c.bytes(s.id[:]), c.str(mode)) })
	return err
}

func (c *Client) queue(input inputOperation) {
	c.mu.Lock()
	s := c.active
	connected := c.status.State == rdpclient.StateConnected
	c.mu.Unlock()
	if s == nil || !connected {
		return
	}
	s.mu.Lock()
	s.inputs = append(s.inputs, input)
	s.mu.Unlock()
	s.signalWorker()
}

func (c *Client) KeyDown(key rdpclient.Key) { c.sendKey(key, true) }
func (c *Client) KeyUp(key rdpclient.Key)   { c.sendKey(key, false) }

func (c *Client) sendKey(key rdpclient.Key, down bool) {
	c.queue(func(core *core, s *session, port int64, id *wireBytes) {
		s.sendKey(core, port, id, key, down)
	})
}

func (s *session) sendKey(c *core, port int64, id *wireBytes, key rdpclient.Key, down bool) {
	usage := keyboardUsage(key)
	mods := s.heldModifiers
	if usage >= 0xe0 && usage <= 0xe7 {
		mask := uint8(1) << (usage - 0xe0)
		if down {
			s.heldModifiers |= mask
		} else {
			s.heldModifiers &^= mask
		}
	}
	if s.mappedKeyboard && usage != 0 {
		if down {
			switch usage {
			case 0x39: // Caps Lock
				s.keyboardLocks ^= 1 << 1
			case 0x53: // Num Lock
				s.keyboardLocks ^= 1 << 2
			}
		}
		c.mappedKey(port, id, c.str(""), int32(usage), s.keyboardLocks, down)
		return
	}
	keys := legacyKeys[:]
	if key.Extended {
		keys = extendedKeys[:]
	}
	if int(key.Scancode) >= len(keys) {
		return
	}
	if name := keys[key.Scancode]; name != "" {
		// Legacy peers include the modifier state in each event.
		c.key(port, id, c.str(name), down, false,
			mods&altKeys != 0, mods&controlKeys != 0, mods&shiftKeys != 0, mods&commandKeys != 0)
	}
}

func (c *Client) SendSequence(keys []rdpclient.Key) {
	if slices.Equal(keys, rdpclient.CtrlAltDelete) {
		c.queue(func(core *core, _ *session, port int64, id *wireBytes) { core.ctrlAltDel(port, id) })
		return
	}
	for _, key := range keys {
		c.KeyDown(key)
	}
	for i := len(keys) - 1; i >= 0; i-- {
		c.KeyUp(keys[i])
	}
}

func (c *Client) SendText(text string) {
	c.queue(func(core *core, _ *session, port int64, id *wireBytes) {
		core.text(port, id, core.str(text))
	})
}

func (c *Client) mousePosition(x, y int) (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		x += c.active.originX
		y += c.active.originY
	}
	return x, y
}

func (c *Client) MouseMove(x, y int) {
	x, y = c.mousePosition(x, y)
	c.sendMouse("move", "", x, y)
}

func (c *Client) MouseButton(button int, down bool, x, y int) {
	buttons := [...]string{"left", "right", "wheel"}
	if button < 0 || button >= len(buttons) {
		return
	}
	kind := "up"
	if down {
		kind = "down"
	}
	x, y = c.mousePosition(x, y)
	c.sendMouse(kind, buttons[button], x, y)
}

func (c *Client) Wheel(_, _ int, notches int, horizontal bool) {
	if horizontal {
		c.sendMouse("wheel", "", notches, 0)
	} else {
		c.sendMouse("wheel", "", 0, notches)
	}
}

func (c *Client) sendMouse(kind, button string, x, y int) {
	c.queue(func(core *core, s *session, port int64, id *wireBytes) {
		event := map[string]string{
			"type": kind, "buttons": button, "x": strconv.Itoa(x), "y": strconv.Itoa(y),
		}
		// RustDesk reconciles the peer's held modifiers when a button goes down.
		for name, mask := range map[string]uint8{"alt": altKeys, "ctrl": controlKeys, "shift": shiftKeys, "command": commandKeys} {
			if s.heldModifiers&mask != 0 {
				event[name] = "true"
			}
		}
		msg, _ := json.Marshal(event)
		core.mouse(port, id, core.bytes(msg))
	})
}

// USB keyboard usages identify physical positions for RustDesk's Map mode.
// The bridge supplies the keyboard page itself and takes only the usage ID.
func keyboardUsage(key rdpclient.Key) uint8 {
	usages := set1Usages[:]
	if key.Extended {
		usages = extendedUsages[:]
	}
	if int(key.Scancode) >= len(usages) {
		return 0
	}
	return usages[key.Scancode]
}

var set1Usages = [...]uint8{
	0x01: 0x29, 0x02: 0x1e, 0x03: 0x1f, 0x04: 0x20, 0x05: 0x21, 0x06: 0x22, 0x07: 0x23,
	0x08: 0x24, 0x09: 0x25, 0x0a: 0x26, 0x0b: 0x27, 0x0c: 0x2d, 0x0d: 0x2e, 0x0e: 0x2a, 0x0f: 0x2b,
	0x10: 0x14, 0x11: 0x1a, 0x12: 0x08, 0x13: 0x15, 0x14: 0x17, 0x15: 0x1c, 0x16: 0x18, 0x17: 0x0c,
	0x18: 0x12, 0x19: 0x13, 0x1a: 0x2f, 0x1b: 0x30, 0x1c: 0x28, 0x1d: 0xe0, 0x1e: 0x04, 0x1f: 0x16,
	0x20: 0x07, 0x21: 0x09, 0x22: 0x0a, 0x23: 0x0b, 0x24: 0x0d, 0x25: 0x0e, 0x26: 0x0f, 0x27: 0x33,
	0x28: 0x34, 0x29: 0x35, 0x2a: 0xe1, 0x2b: 0x31, 0x2c: 0x1d, 0x2d: 0x1b, 0x2e: 0x06, 0x2f: 0x19,
	0x30: 0x05, 0x31: 0x11, 0x32: 0x10, 0x33: 0x36, 0x34: 0x37, 0x35: 0x38, 0x36: 0xe5, 0x37: 0x55,
	0x38: 0xe2, 0x39: 0x2c, 0x3a: 0x39, 0x3b: 0x3a, 0x3c: 0x3b, 0x3d: 0x3c, 0x3e: 0x3d, 0x3f: 0x3e,
	0x40: 0x3f, 0x41: 0x40, 0x42: 0x41, 0x43: 0x42, 0x44: 0x43, 0x45: 0x53, 0x46: 0x47, 0x47: 0x5f,
	0x48: 0x60, 0x49: 0x61, 0x4a: 0x56, 0x4b: 0x5c, 0x4c: 0x5d, 0x4d: 0x5e, 0x4e: 0x57, 0x4f: 0x59,
	0x50: 0x5a, 0x51: 0x5b, 0x52: 0x62, 0x53: 0x63, 0x57: 0x44, 0x58: 0x45,
}

var extendedUsages = [...]uint8{
	0x1c: 0x58, 0x1d: 0xe4, 0x35: 0x54, 0x37: 0x46, 0x38: 0xe6, 0x47: 0x4a, 0x48: 0x52, 0x49: 0x4b,
	0x4b: 0x50, 0x4d: 0x4f, 0x4f: 0x4d, 0x50: 0x51, 0x51: 0x4e, 0x52: 0x49, 0x53: 0x4c, 0x5b: 0xe3,
	0x5c: 0xe7, 0x5d: 0x65,
}

// RustDesk's legacy input accepts virtual-key names. The indices are the same
// set-1 scancodes that the OnRDP toolbar and the RDP session already use.
var legacyKeys = [...]string{
	0x01: "VK_ESCAPE",
	0x02: "VK_1", 0x03: "VK_2", 0x04: "VK_3", 0x05: "VK_4", 0x06: "VK_5",
	0x07: "VK_6", 0x08: "VK_7", 0x09: "VK_8", 0x0a: "VK_9", 0x0b: "VK_0",
	0x0c: "VK_MINUS", 0x0d: "VK_PLUS", 0x0e: "VK_BACK", 0x0f: "VK_TAB",
	0x10: "VK_Q", 0x11: "VK_W", 0x12: "VK_E", 0x13: "VK_R", 0x14: "VK_T",
	0x15: "VK_Y", 0x16: "VK_U", 0x17: "VK_I", 0x18: "VK_O", 0x19: "VK_P",
	0x1a: "VK_LBRACKET", 0x1b: "VK_RBRACKET", 0x1c: "VK_RETURN", 0x1d: "VK_CONTROL",
	0x1e: "VK_A", 0x1f: "VK_S", 0x20: "VK_D", 0x21: "VK_F", 0x22: "VK_G",
	0x23: "VK_H", 0x24: "VK_J", 0x25: "VK_K", 0x26: "VK_L",
	0x27: "VK_SEMICOLON", 0x28: "VK_QUOTE", 0x29: "`", 0x2a: "VK_SHIFT", 0x2b: "VK_BACKSLASH",
	0x2c: "VK_Z", 0x2d: "VK_X", 0x2e: "VK_C", 0x2f: "VK_V", 0x30: "VK_B",
	0x31: "VK_N", 0x32: "VK_M", 0x33: "VK_COMMA", 0x34: ".", 0x35: "VK_SLASH",
	0x36: "RShift", 0x37: "VK_MULTIPLY", 0x38: "VK_MENU", 0x39: "VK_SPACE", 0x3a: "VK_CAPITAL",
	0x3b: "VK_F1", 0x3c: "VK_F2", 0x3d: "VK_F3", 0x3e: "VK_F4", 0x3f: "VK_F5",
	0x40: "VK_F6", 0x41: "VK_F7", 0x42: "VK_F8", 0x43: "VK_F9", 0x44: "VK_F10",
	0x46: "VK_SCROLL",
	0x47: "VK_NUMPAD7", 0x48: "VK_NUMPAD8", 0x49: "VK_NUMPAD9", 0x4a: "VK_SUBTRACT",
	0x4b: "VK_NUMPAD4", 0x4c: "VK_NUMPAD5", 0x4d: "VK_NUMPAD6", 0x4e: "VK_ADD",
	0x4f: "VK_NUMPAD1", 0x50: "VK_NUMPAD2", 0x51: "VK_NUMPAD3",
	0x52: "VK_NUMPAD0", 0x53: "VK_DECIMAL", 0x57: "VK_F11", 0x58: "VK_F12",
}

var extendedKeys = [...]string{
	0x1c: "VK_RETURN", 0x1d: "RControl", 0x35: "VK_DIVIDE", 0x37: "VK_SNAPSHOT", 0x38: "RAlt",
	0x47: "VK_HOME", 0x48: "VK_UP", 0x49: "VK_PRIOR", 0x4b: "VK_LEFT", 0x4d: "VK_RIGHT",
	0x4f: "VK_END", 0x50: "VK_DOWN", 0x51: "VK_NEXT", 0x52: "VK_INSERT", 0x53: "VK_DELETE",
	0x5b: "Meta", 0x5c: "RWin", 0x5d: "Apps",
}
