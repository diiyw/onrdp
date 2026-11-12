//go:build !android && !ios

package main

import (
	"os"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/render"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/window"
)

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
