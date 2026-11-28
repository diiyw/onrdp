package app

import (
	"slices"
	"testing"

	"github.com/onix-engine/onix/ecs"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	htmlui "github.com/onix-engine/onix/ui/html"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/store"
)

type keyTransition struct {
	key  rdpclient.Key
	down bool
}

type recordingKeyboard struct {
	remoteClient
	events []keyTransition
}

type mouseTransition struct {
	button int
	down   bool
}

type recordingSessionInput struct {
	recordingKeyboard
	buttons      []mouseTransition
	moves, wheel int
}

func (*recordingSessionInput) Status() rdpclient.Snapshot {
	return rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1600, Height: 900}
}

func (r *recordingSessionInput) MouseButton(button int, down bool, _, _ int) {
	r.buttons = append(r.buttons, mouseTransition{button, down})
}

func (r *recordingSessionInput) MouseMove(_, _ int) { r.moves++ }

func (r *recordingSessionInput) Wheel(_, _, notches int, _ bool) { r.wheel += notches }

func (*recordingKeyboard) Status() rdpclient.Snapshot {
	return rdpclient.Snapshot{State: rdpclient.StateConnected}
}

func (r *recordingKeyboard) KeyDown(key rdpclient.Key) {
	r.events = append(r.events, keyTransition{key, true})
}

func (r *recordingKeyboard) KeyUp(key rdpclient.Key) {
	r.events = append(r.events, keyTransition{key, false})
}

func TestSessionForwardsFullscreenKeys(t *testing.T) {
	toggles := 0
	host := &module.Host{ToggleFullscreen: func() { toggles++ }, RequestRender: func() {}}
	c, mod, runtime, world := mountRuntimeWorldSizeWithHost(t, 1024, 700, host)
	keyboard := &recordingKeyboard{remoteClient: mod.client}
	mod.client = keyboard
	mod.route("rdp")
	if err := c.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	if err := ui.SyncHTMLSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	state, _ := world.GetResource[*input.State]()
	frame := func() {
		t.Helper()
		if err := ui.UpdateSystem("").Run(world); err != nil {
			t.Fatal(err)
		}
		state.EndFrame()
	}
	for _, fullscreen := range []bool{false, true} {
		if fullscreen {
			mod.click(c, "rdp-fullscreen")
		}
		keyboard.events = nil
		state.PressKey(input.KeyEscape)
		frame()
		state.ReleaseKey(input.KeyEscape)
		frame()
		// A quick tap can arrive entirely in one frame.
		state.PressKey(input.KeyF11)
		state.ReleaseKey(input.KeyF11)
		frame()
		esc, f11 := rdpclient.Key{Scancode: 0x01}, rdpclient.Key{Scancode: 0x57}
		want := []keyTransition{{esc, true}, {esc, false}, {f11, true}, {f11, false}}
		if !slices.Equal(keyboard.events, want) || mod.fullscreen != fullscreen {
			t.Fatalf("fullscreen=%t: remote keys %v, local fullscreen %t", fullscreen, keyboard.events, mod.fullscreen)
		}
	}
	mod.click(c, "rdp-fullscreen")
	if mod.fullscreen || toggles != 2 {
		t.Fatalf("toolbar exit: fullscreen=%t, native toggles=%d", mod.fullscreen, toggles)
	}
}

func TestRemoteInputStopsAtToolbarAndFrameEdges(t *testing.T) {
	c, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
	client := &recordingSessionInput{recordingKeyboard: recordingKeyboard{remoteClient: mod.client}}
	mod.client = client
	mod.window = &platform.DesktopWindow{}
	mod.bindDesktopInput()
	capture := &mod.window.Keyboard
	var x, y float32
	capture.Active = func() bool { return capture.Ready() && capture.OverFrame(x, y) }
	capture.Blur = func() { ui.FocusControl(world, 0) }
	mod.route("rdp")
	frame := func() {
		t.Helper()
		if err := c.Content().SyncHTML(); err != nil {
			t.Fatal(err)
		}
		runtime.SyncScopes()
		if err := ui.SyncHTMLSystem("").Run(world); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Update(1.0 / 60); err != nil {
			t.Fatal(err)
		}
	}
	frame()
	toolbar, _ := mod.node("rdp-toolbar").Bounds()
	stage, _ := mod.node("rdp-stage").Bounds()
	scale, _, offsetY, _ := mod.frameLayout()
	control, ok := c.Content().HTMLControl(c.Content().Root(), "rdp-keyboard")
	if !ok || toolbar.Width <= 0 || scale <= 0 || offsetY <= 0 {
		t.Fatal("fixture requires a focused input, toolbar and letterboxed image")
	}
	press := func(button input.MouseButton, kind scene.InputEventKind) {
		mod.pointer(module.Input{Event: scene.InputEvent{
			Kind: kind, Device: input.PointerDeviceMouse, Button: button, X: x, Y: y,
		}})
	}
	moveAndScroll := func() {
		press(input.MouseLeft, scene.InputEventMove)
		mod.pointer(module.Input{Event: scene.InputEvent{
			Kind: scene.InputEventWheel, Device: input.PointerDeviceMouse, X: x, Y: y, WheelY: -1,
		}})
	}
	for _, target := range []struct {
		name string
		x, y float32
	}{
		{"toolbar", toolbar.X + toolbar.Width/2, toolbar.Y + toolbar.Height/2},
		{"black bar edge", stage.X + 1, stage.Y + offsetY - scale/2},
		{"outside", stage.X + stage.Width + 1, stage.Y + stage.Height/2},
	} {
		x, y = stage.X+stage.Width/2, stage.Y+stage.Height/2
		frame()
		if !capture.Active() || c.Content().FocusedControl() != control {
			t.Fatalf("%s: remote input did not resume in the image", target.name)
		}
		state, _ := world.GetResource[*input.State]()
		if state.TextInputConfig().Active || world.Has[ui.TextField](control) {
			t.Fatal("desktop keyboard focus activated the local input method")
		}
		client.events, client.buttons = nil, nil
		client.moves, client.wheel = 0, 0
		capture.Key(input.KeyLeftShift, true)
		capture.Key(input.KeyNum2, true)
		capture.Key(input.KeyNum2, false)
		capture.Key(input.KeyLeftShift, false)
		moveAndScroll()
		capture.Key(input.KeyLeftAlt, true)
		press(input.MouseLeft, scene.InputEventPress)
		press(input.MouseRight, scene.InputEventPress)
		x, y = target.x, target.y
		if capture.Active() {
			t.Fatalf("%s still belongs to the remote desktop", target.name)
		}
		capture.Release()
		frame()
		press(input.MouseRight, scene.InputEventRelease)
		press(input.MouseMiddle, scene.InputEventPress)
		moveAndScroll()
		key, _ := rdpclient.Scancode(input.KeyLeftAlt)
		shift, two := rdpclient.Key{Scancode: 0x2a}, rdpclient.Key{Scancode: 0x03}
		if !slices.Equal(client.events, []keyTransition{{shift, true}, {two, true}, {two, false}, {shift, false}, {key, true}, {key, false}}) ||
			!slices.Equal(client.buttons, []mouseTransition{{0, true}, {1, true}, {0, false}, {1, false}}) ||
			client.moves != 1 || client.wheel != 1 ||
			c.Content().FocusedControl() == control {
			t.Fatalf("%s: keys %v, mouse %v, moves %d, wheel %d, focus %v",
				target.name, client.events, client.buttons, client.moves, client.wheel, c.Content().FocusedControl())
		}
	}
}

// Exercise hit testing and native controls: sending EventClick directly skips
// the pointer state that can survive hiding the remote page and its dialog.
func TestSessionExitRestoresPointerInput(t *testing.T) {
	for _, exit := range []string{"confirm", "window", "back"} {
		t.Run(exit, func(t *testing.T) {
			_, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
			host := store.Host{ID: "desktop", Name: "Windows", Address: "10.0.0.1", Port: 3389, User: "alice", Password: "secret", Width: 1920, Height: 1080}
			if err := mod.vault.SaveHost(host); err != nil {
				t.Fatal(err)
			}
			mod.selected = host.ID
			state, _ := world.GetResource[*input.State]()
			var pointerEvents ecs.MessageReader[scene.InputEvent]
			var htmlEvents ecs.MessageReader[htmlui.Event]
			frame := func() {
				t.Helper()
				runtime.SyncScopes()
				for _, system := range []ecs.SystemConfig{scene.SyncGlobalTransformSystem(""), scene.SyncUIGlobalTransformSystem(""), scene.DispatchInputEvents2DSystem("")} {
					if err := system.Run(world); err != nil {
						t.Fatal(err)
					}
				}
				for event := range pointerEvents.All(world) {
					if err := runtime.HandleInput(*event); err != nil {
						t.Fatal(err)
					}
				}
				for event := range htmlEvents.All(world) {
					if err := runtime.HandleHTMLEvent(*event); err != nil {
						t.Fatal(err)
					}
				}
				if err := runtime.Update(1.0 / 60); err != nil {
					t.Fatal(err)
				}
				for _, system := range []ecs.SystemConfig{ui.UpdateSystem(""), ui.SyncSystem(""), scene.SyncUIGlobalTransformSystem(""), ui.SyncAfterLayoutSystem("")} {
					if err := system.Run(world); err != nil {
						t.Fatal(err)
					}
				}
				state.EndFrame()
				world.AdvanceMessages()
			}
			click := func(id string) {
				t.Helper()
				frame()
				bounds, ok := mod.node(id).Bounds()
				if !ok || bounds.Width <= 0 || bounds.Height <= 0 {
					t.Fatalf("%s has no clickable bounds: %+v", id, bounds)
				}
				x, y := float64(bounds.X+bounds.Width/2), float64(bounds.Y+bounds.Height/2)
				state.PressMouse(input.MouseLeft, x, y)
				frame()
				state.ReleaseMouse(input.MouseLeft, x, y)
				frame()
				frame()
			}
			click("nav-sessions")
			if mod.page != "sessions" {
				t.Fatal("initial navigation click did not work")
			}
			mod.route("rdp")
			mod.connected = true
			frame()
			mod.layoutToolbar()
			switch exit {
			case "confirm":
				click("rdp-close")
				if mod.confirm != "disconnect" {
					t.Fatal("disconnect click did not open confirmation")
				}
				click("confirm-ok")
			case "window":
				if mod.closeRequested() {
					t.Fatal("session window close should keep the app open")
				}
			case "back":
				click("rdp-back")
			}
			if mod.page != "sessions" {
				t.Fatalf("page after exit = %q", mod.page)
			}
			frame()
			state.MoveMouse(980, 650)
			frame()
			if state.Cursor() != input.CursorDefault {
				t.Errorf("empty session area has cursor %v, want default", state.Cursor())
			}
			click("nav-settings")
			if mod.page != "settings" {
				t.Fatal("settings click stopped working after session exit")
			}
			click("nav-hosts")
			if mod.page != "hosts" {
				t.Fatal("hosts click stopped working after session exit")
			}
		})
	}
}
