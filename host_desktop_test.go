//go:build !android && !ios

package main

import (
	"os"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/ecs"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/render"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	"github.com/onix-engine/onix/window"
)

func TestDesktopCloseVetoRestoresInput(t *testing.T) {
	app := onix.NewApp()
	world := app.World
	state := input.NewState()
	world.InsertResource(state)
	camera := scene.SpawnCamera2D(world)
	world.GetMut[scene.Camera](camera).UpdateViewport(1024, 700)
	spawn := func(x, y, width, height float32) ecs.Entity {
		bundle := ecs.NewBundle(8)
		bundle.Put(scene.UITransformFromXY(x, y))
		bundle.Put(scene.UIGlobalTransform{})
		bundle.Put(scene.Visibility{Visible: true})
		bundle.Put(scene.InputTarget2D{Width: width, Height: height, Cursor: input.CursorPointer})
		bundle.Put(scene.InputState2D{})
		bundle.Put(scene.AnchorTopLeft)
		bundle.Put(ui.Control{})
		bundle.Put(ui.TextField{})
		return world.Spawn(bundle)
	}
	remote := spawn(500, 300, 200, 200)
	local := spawn(20, 20, 100, 50)
	dispatch := func() {
		t.Helper()
		for _, system := range []ecs.SystemConfig{scene.SyncGlobalTransformSystem(""), scene.SyncUIGlobalTransformSystem(""), scene.DispatchInputEvents2DSystem("")} {
			if err := system.Run(world); err != nil {
				t.Fatal(err)
			}
		}
		state.EndFrame()
	}
	state.PressMouse(input.MouseLeft, 600, 400)
	state.PressMouse(input.MouseRight, 600, 400)
	state.PressMouse(input.MouseMiddle, 600, 400)
	dispatch()
	if !ui.FocusControl(world, remote) || !state.TextInputConfig().Active {
		t.Fatal("remote input did not receive native text focus")
	}
	// The native title-bar takes the release, then the application vetoes the
	// window close after hiding the remote surface.
	if desktopCloseRequested(app, func() bool {
		world.GetMut[scene.Visibility](remote).Visible = false
		return false
	}) {
		t.Fatal("session close must keep the application open")
	}
	if state.PointerHeld() || state.MouseHeld(input.MouseRight) || state.MouseHeld(input.MouseMiddle) || state.TextInputConfig().Active || state.Cursor() != input.CursorDefault {
		t.Fatal("vetoed close left pointer buttons, text focus or cursor active")
	}
	dispatch()
	if capture, ok := world.GetResource[*scene.InputCapture2D](); !ok || capture.Target != 0 || capture.Receiver != 0 {
		t.Fatal("vetoed close left a pointer capture on the hidden remote surface")
	}
	var events ecs.MessageReader[scene.InputEvent]
	events.Discard(world)
	state.PressMouse(input.MouseLeft, 50, 40)
	dispatch()
	state.ReleaseMouse(input.MouseLeft, 50, 40)
	dispatch()
	for event := range events.All(world) {
		if event.Kind == scene.InputEventTap && event.Entity == local {
			return
		}
	}
	t.Fatal("first local click after closing the remote session did not activate")
}

func TestDesktopAcceptedClosePreservesInput(t *testing.T) {
	app := onix.NewApp()
	state := input.NewState()
	app.World.InsertResource(state)
	state.PressMouse(input.MouseLeft, 50, 40)
	state.SetCursor(input.CursorPointer)
	if !desktopCloseRequested(app, func() bool { return true }) {
		t.Fatal("accepted close was vetoed")
	}
	if !state.PointerHeld() || state.Cursor() != input.CursorPointer {
		t.Fatal("accepted close unexpectedly ran the session input reset")
	}
}

func desktopButtonFixture(t *testing.T) (*onix.App, *[]scene.InputEvent) {
	t.Helper()
	source := fstest.MapFS{}
	for name, data := range map[string]string{
		"resources/project.json":     `{"entry":"main.json","scenes":["main.json"],"viewport":{"width":1024,"height":700}}`,
		"resources/scenes/main.json": `{"modules":[{"id":"app","mod":"page"}]}`,
		"page/mod.json":              `{"space":"ui","components":{"html":{"resource":"page"}},"resources":{"page":{"kind":"html","path":"index.html"}}}`,
		"page/index.html":            `<div id="rdp-stage">Desktop</div>`,
	} {
		source[name] = &fstest.MapFile{Data: []byte(data)}
	}
	plan, err := project.CompileProject(source, ".")
	if err != nil {
		t.Fatal(err)
	}
	var events []scene.InputEvent
	registry := module.Registry{}
	if err := registry.RegisterScene("main.json", func() Main { return Main{} }, module.Lifecycle[Main]{
		OnMessage: func(c *module.Context, m *Main, message module.Message) error { return m.OnMessage(c, message) },
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterMod("page", func() struct{} { return struct{}{} }, module.Lifecycle[struct{}]{
		OnMessage: func(_ *module.Context, _ *struct{}, message module.Message) error {
			if value, ok := message.Value[module.Input](); ok {
				events = append(events, value.Event)
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	app := onix.NewApp()
	if _, err := app.LoadModules[OnixContentProfile](plan, registry, module.Options{Source: source}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Modules().Close() })
	return app, &events
}

func TestDesktopButtonsReachModule(t *testing.T) {
	app, received := desktopButtonFixture(t)
	for _, button := range []input.MouseButton{input.MouseRight, input.MouseMiddle} {
		for _, kind := range []input.EventKind{input.EventMouseDown, input.EventMouseUp} {
			forwardDesktopButton(app, input.Event{Kind: kind, Button: button, X: 400, Y: 300})
		}
	}
	events := *received
	if len(events) != 4 {
		t.Fatalf("module received %d button transitions, want 4", len(events))
	}
	for i, event := range events {
		wantKind := scene.InputEventPress
		if i%2 != 0 {
			wantKind = scene.InputEventRelease
		}
		wantButton := input.MouseRight
		if i >= 2 {
			wantButton = input.MouseMiddle
		}
		if event.Kind != wantKind || event.Button != wantButton || event.X != 400 || event.Y != 300 {
			t.Fatalf("button transition %d = %+v", i, event)
		}
	}
	for _, kind := range []input.EventKind{input.EventMouseDown, input.EventMouseUp, input.EventMouseWheel} {
		forwardDesktopButton(app, input.Event{Kind: kind, Button: input.MouseLeft})
	}
	if len(*received) != 4 {
		t.Fatal("native forwarding duplicated left-button or wheel events")
	}
}

// Run with ONRDP_TEST_NATIVE_INPUT=1 (under xvfb-run on headless Linux).
// A real context initializes the platform's input viewport, so this catches
// accidental conversion of already-logical input coordinates a second time.
func TestDesktopButtonsKeepLogicalCoordinates(t *testing.T) {
	if os.Getenv("ONRDP_TEST_NATIVE_INPUT") != "1" {
		t.Skip("requires a native window and renderer")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := render.ActiveDevice()
	surface := window.NewContext(1920, 1080, "OnRDP input regression", window.DefaultOptions())
	defer func() { surface.Destroy(); previous.Activate() }()
	app, events := desktopButtonFixture(t)
	app.Context = surface
	surface.SetViewportMode(window.ViewportModeExpand)
	for _, size := range [][2]int{{1024, 700}, {1728, 1117}, {1024, 700}} {
		surface.SetViewport(size[0], size[1])
		// Simulate the platform boundary mapping a click into the current
		// logical viewport before it reaches the global input handler.
		x, y := surface.GetLogicalXY(900, 600)
		doubleX, doubleY := surface.GetLogicalXY(x, y)
		if x == doubleX && y == doubleY {
			t.Fatal("fixture must exercise a non-identity coordinate transform")
		}
		for _, button := range []input.MouseButton{input.MouseRight, input.MouseMiddle} {
			for _, kind := range []input.EventKind{input.EventMouseDown, input.EventMouseUp} {
				forwardDesktopButton(app, input.Event{Kind: kind, Button: button, X: x, Y: y})
				got := (*events)[len(*events)-1]
				if got.X != float32(x) || got.Y != float32(y) {
					t.Fatalf("viewport %v button %v kind %v: got (%g,%g), want (%g,%g)", size, button, kind, got.X, got.Y, float32(x), float32(y))
				}
			}
		}
	}
}
