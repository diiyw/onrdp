//go:build !android && !ios

package main

import (
	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/scene"
	htmlui "github.com/onix-engine/onix/ui/html"
	"github.com/onix-engine/onix/window"
)

// desktopHost exposes native-window capabilities without coupling the UI to
// the application object. It also owns the additional desktop input forwarding.
func desktopHost(app *onix.App, spec project.ProjectSpec) *module.Host {
	app.SetViewportMode(window.ViewportModeExpand)
	// Onix composites UI at logical viewport resolution, then scales
	// that cache to the native surface. Dense frame updates bypass the cache,
	// so a HiDPI remote desktop otherwise alternates between blurred cached
	// output and sharp direct output. Keep both idle and busy frames direct.
	app.World.InsertResource(scene.CompositeSettings2D{MaxCacheBytes: -1})
	installDesktopInput(app)
	return &module.Host{
		Context:          app.Context,
		RequestRender:    app.RequestRender,
		WakeAt:           app.WakeAt,
		RequestClose:     func() { app.RequestClose() },
		Minimize:         func() { app.Minimize() },
		ToggleMaximize:   func() { app.ToggleMaximize() },
		ToggleFullscreen: app.ToggleFullscreen,
		SetWindowTitle:   app.SetWindowTitle,
		SetViewport: func(w, h int) {
			if app.Context != nil {
				app.SetViewport(w, h)
			}
		},
		GetViewport: func() (int, int) {
			if app.Context == nil {
				return spec.Viewport.Width, spec.Viewport.Height
			}
			return app.GetViewport()
		},
		SetWindowDragRegion:  app.SetWindowDragRegion,
		SetWindowInputRegion: app.SetWindowInputRegion,
		OnCloseRequested:     func(f func() bool) { app.OnCloseRequested(f) },
		OpenFolder:           app.OpenFolder,
		OpenFile:             app.OpenFile,
		OpenFiles:            app.OpenFiles,
		SetClipboardText: func(s string) bool {
			return app.Context != nil && app.SetClipboardText(s)
		},
		SetMouseCaptured: func(v bool) bool {
			return app.Context != nil && app.SetMouseCaptured(v)
		},
		MouseCaptured: func() bool {
			return app.Context != nil && app.MouseCaptured()
		},
		TakeMouseDelta: func() (float64, float64) {
			if app.Context == nil {
				return 0, 0
			}
			return app.TakeMouseDelta()
		},
	}
}

// installDesktopInput forwards mouse buttons other than the left one. The
// engine's 2D pointer dispatcher only emits left-button transitions, while a
// remote desktop needs right and middle buttons too. Wheel events already pass
// through the 2D dispatcher and must not be duplicated here.
func installDesktopInput(app *onix.App) {
	app.AddInputHandler(input.PhaseGlobal, func(d *input.Dispatch) {
		forwardDesktopButton(app, d.Event)
	})
}

func forwardDesktopButton(app *onix.App, event input.Event) {
	if event.Kind != input.EventMouseDown && event.Kind != input.EventMouseUp {
		return
	}
	if event.Button != input.MouseRight && event.Button != input.MouseMiddle {
		return
	}
	runtime := app.Modules()
	if runtime == nil {
		return
	}
	// Window callbacks already map native coordinates into the current logical
	// viewport before enqueueing input.Event. Converting again shifts right and
	// middle clicks away from pointer motion after resizing/fullscreen or HiDPI.
	x, y := float32(event.X), float32(event.Y)
	kind := scene.InputEventPress
	if event.Kind == input.EventMouseUp {
		kind = scene.InputEventRelease
	}
	event2D := scene.InputEvent{
		Kind:   kind,
		Button: event.Button,
		Device: input.PointerDeviceMouse,
		X:      x,
		Y:      y,
	}
	// HandleInput drops pointer events without an entity. Resolve the
	// HTML surface here; the module checks the remote stage's bounds.
	for entity := range app.World.Query[htmlui.Surface]().All {
		event2D.Entity = entity
		break
	}
	// Deliver outside the query: module delivery flushes structural edits.
	if event2D.Entity != 0 {
		_ = runtime.HandleInput(event2D)
	}
}
