package app

import (
	"strings"

	"github.com/onix-engine/onix/input"
	"onrdp/internal/rdpclient"
)

const androidBackKey = 4

var androidButtonCodes = map[string]uint32{
	"back": androidBackKey, "home": 3, "recents": 187,
	"volume-up": 24, "volume-down": 25, "power": 26,
}

func (m *Mod) pressAndroidButton(name string) {
	code, ok := androidButtonCodes[name]
	if !ok || m.mirror == nil || m.mirror.run == nil || m.status.State != rdpclient.StateConnected {
		return
	}
	m.mirror.run.native.Key(code, 0, true)
	m.mirror.run.native.Key(code, 0, false)
	m.focusInput = true
}

func (r *mirrorRun) MouseMove(x, y int) {
	action := byte(7) // hover
	if r.touching {
		action = 2
	}
	r.native.Touch(action, x, y, r.width, r.height)
}

func (r *mirrorRun) MouseButton(button int, down bool, x, y int) {
	switch input.MouseButton(button) {
	case input.MouseLeft:
		action := byte(0)
		if !down {
			action = 1
		}
		r.touching = down
		r.native.Touch(action, x, y, r.width, r.height)
		if !down && r.onTap != nil {
			r.onTap()
		}
	case input.MouseRight:
		r.native.Key(androidBackKey, 0, down) // Back
	case input.MouseMiddle:
		r.native.Key(3, 0, down) // Home
	}
}

func (r *mirrorRun) Wheel(x, y, steps int, horizontal bool) {
	r.native.Scroll(x, y, r.width, r.height, steps, horizontal)
}

// Android KeyEvent key codes for non-text keys and shortcut chords.
var androidSpecialKeys = map[input.KeyCode]uint32{
	input.KeyEscape: androidBackKey, input.KeyEnter: 66, input.KeyKPEnter: 66, input.KeyTab: 61, input.KeyBackspace: 67,
	input.KeyInsert: 124, input.KeyDelete: 112, input.KeyRight: 22, input.KeyLeft: 21, input.KeyDown: 20, input.KeyUp: 19,
	input.KeyPageUp: 92, input.KeyPageDown: 93, input.KeyHome: 122, input.KeyEnd: 123,
	input.KeyLeftShift: 59, input.KeyRightShift: 60, input.KeyLeftControl: 113, input.KeyRightControl: 114,
	input.KeyLeftAlt: 57, input.KeyRightAlt: 58, input.KeyLeftSuper: 117, input.KeyRightSuper: 118,
	input.KeyCapsLock: 115, input.KeyNumLock: 143, input.KeyScrollLock: 116, input.KeyMenu: 82,
}

func androidChordKey(key input.KeyCode) uint32 {
	switch {
	case key >= input.KeyA && key <= input.KeyZ:
		return 29 + uint32(key-input.KeyA)
	case key >= input.KeyNum0 && key <= input.KeyNum9:
		return 7 + uint32(key-input.KeyNum0)
	case key >= input.KeyF1 && key <= input.KeyF12:
		return 131 + uint32(key-input.KeyF1)
	default:
		return androidSpecialKeys[key]
	}
}

func (m *Mod) androidKeys(state *input.State) bool {
	run := m.mirror.run
	if run == nil || run.cancelledForSwitch {
		return true
	}
	for _, key := range rdpclient.Keycodes() {
		down, up := state.KeyPressed(key), state.KeyReleased(key)
		if !down && !up {
			continue
		}
		mods := state.KeyModifiers(key)
		command := mods&(input.ModifierControl|input.ModifierSuper) != 0
		// Printable input comes from the host's text/IME events. A shortcut
		// needs its physical key and the Android metastate instead.
		code := androidSpecialKeys[key]
		if command || (key >= input.KeyF1 && key <= input.KeyF12) {
			code = androidChordKey(key)
		}
		var meta uint32
		if mods&input.ModifierShift != 0 {
			meta |= 1
		}
		if mods&input.ModifierAlt != 0 {
			meta |= 2
		}
		if command {
			meta |= 0x1000
		}
		if down && !state.KeyPressedDuringComposition(key) {
			if command && key == input.KeyV && m.vault.Settings().Clipboard && m.host.Context != nil {
				if text, ok := m.host.Context.ClipboardText(); ok {
					run.native.Paste(text)
				}
			} else if code != 0 {
				run.native.Key(code, meta, true)
				run.keysDown[key] = code
			}
		}
		// The IME may have consumed the press in an earlier frame.
		if pressed := run.keysDown[key]; up && pressed != 0 {
			run.native.Key(pressed, meta, false)
			delete(run.keysDown, key)
		}
	}
	var committed strings.Builder
	for _, edit := range state.TextEdits() {
		committed.WriteString(edit.Text)
	}
	run.native.TypeText(committed.String())
	return true
}
