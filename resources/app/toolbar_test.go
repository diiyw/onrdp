package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/onix-engine/onix/ecs"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/scene"
	"onrdp/internal/rdpclient"
)

func TestFloatingToolbarKeepsDesktopFullSize(t *testing.T) {
	for _, size := range [][2]int{{320, 640}, {420, 780}, {1024, 700}, {1800, 600}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			context, mod := mountFixtureSize(t, size[0], size[1])
			mod.route("rdp")
			sync := func() {
				t.Helper()
				if err := context.Content().SyncHTML(); err != nil {
					t.Fatal(err)
				}
				mod.layoutToolbar()
				if err := context.Content().SyncHTML(); err != nil {
					t.Fatal(err)
				}
			}
			for _, action := range []string{"", "rdp-keys-toggle", "rdp-info-toggle", "rdp-toolbar-toggle", "rdp-toolbar-toggle"} {
				mod.click(context, action)
				sync()
				stage, _ := mod.node("rdp-stage").Bounds()
				if stage.X != 0 || stage.Y != 0 || stage.Width != float32(size[0]) || stage.Height != float32(size[1]) {
					t.Fatalf("%s: desktop no longer fills the content area: %+v", action, stage)
				}
				overlay, _ := mod.node("rdp-overlay").Bounds()
				if overlay != stage {
					t.Fatalf("connection overlay does not fill stage: %+v in %+v", overlay, stage)
				}
				bar, _ := mod.node("rdp-toolbar").Bounds()
				if bar.X < 0 || bar.Y < 0 || bar.X+bar.Width > stage.Width+.01 || bar.Y+bar.Height > stage.Height+.01 {
					t.Fatalf("%s: toolbar outside desktop: %+v", action, bar)
				}
				if visible(mod.node("rdp-tools")) == mod.toolbar.collapsed || visible(mod.node("l10n-43")) != (mod.toolbar.keys && !mod.toolbar.collapsed) {
					t.Fatalf("%s: incorrect collapsed/expanded state", action)
				}
				for _, item := range toolbarLabels {
					if !visible(mod.node(item.id)) {
						continue
					}
					button, _ := mod.node(item.id).Bounds()
					if button.X < bar.X || button.Y < bar.Y || button.X+button.Width > bar.X+bar.Width || button.Y+button.Height > bar.Y+bar.Height {
						t.Fatalf("%s: button %s escaped toolbar: %+v in %+v", action, item.id, button, bar)
					}
				}
			}
		})
	}
}

func TestToolbarDragUsesRuntimePointerCapture(t *testing.T) {
	context, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
	mod.route("rdp")
	mod.hide("rdp-overlay", true)
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1024, Height: 700}
	var reader ecs.MessageReader[scene.InputEvent]
	state, _ := world.GetResource[*input.State]()
	sync := func() {
		t.Helper()
		if err := context.Content().SyncHTML(); err != nil {
			t.Fatal(err)
		}
		runtime.SyncScopes()
		for _, system := range []ecs.SystemConfig{scene.SyncGlobalTransformSystem(""), scene.SyncUIGlobalTransformSystem("")} {
			if err := system.Run(world); err != nil {
				t.Fatal(err)
			}
		}
	}
	dispatch := func() {
		t.Helper()
		if err := scene.DispatchInputEvents2DSystem("").Run(world); err != nil {
			t.Fatal(err)
		}
		for event := range reader.All(world) {
			if err := runtime.HandleInput(*event); err != nil {
				t.Fatal(err)
			}
		}
		state.EndFrame()
		sync()
	}
	sync()
	mod.layoutToolbar()
	sync()
	handle, _ := mod.node("rdp-drag").Bounds()
	state.PressMouse(input.MouseLeft, float64(handle.X+12), float64(handle.Y+18))
	dispatch()
	if !mod.toolbar.dragging || mod.pressed[0] {
		t.Fatal("drag handle press reached remote desktop or failed to capture")
	}
	state.MoveMouse(1023, 699)
	dispatch()
	bar, _ := mod.node("rdp-toolbar").Bounds()
	if bar.X+bar.Width != 1024 || bar.Y+bar.Height != 700 {
		t.Fatalf("drag did not clamp to content edge: %+v", bar)
	}
	state.ReleaseMouse(input.MouseLeft, 1023, 699)
	dispatch()
	if mod.toolbar.dragging || mod.pressed[0] {
		t.Fatal("drag release did not end local capture cleanly")
	}
	mod.click(context, "rdp-keys-toggle")
	mod.click(context, "rdp-info-toggle")
	sync()
	mod.layoutToolbar()
	sync()
	bar, _ = mod.node("rdp-toolbar").Bounds()
	if bar.Y+bar.Height > 700 {
		t.Fatalf("expanding at the bottom stranded toolbar: %+v", bar)
	}
	// Simulate a window shrink through the same viewport resource as the host.
	world.InsertResource(scene.ViewportSize{Width: 420, Height: 400})
	sync()
	mod.layoutToolbar()
	sync()
	bar, _ = mod.node("rdp-toolbar").Bounds()
	if bar.X+bar.Width > 420 || bar.Y+bar.Height > 400 {
		t.Fatalf("toolbar stranded after window resize: %+v", bar)
	}
}

func TestToolbarBlocksRemoteButtonsAndReleasesExistingDrags(t *testing.T) {
	context, mod := mountFixtureSize(t, 1024, 700)
	mod.route("rdp")
	mod.hide("rdp-overlay", true)
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1024, Height: 700}
	bar, _ := mod.node("rdp-toolbar").Bounds()
	x, y := bar.X+bar.Width-20, bar.Y+20
	for _, button := range []input.MouseButton{input.MouseLeft, input.MouseRight, input.MouseMiddle} {
		mod.pointer(module.Input{Event: scene.InputEvent{Kind: scene.InputEventPress, Button: button, X: x, Y: y}})
		if mod.pressed != [3]bool{} {
			t.Fatalf("toolbar sent button %d to remote desktop", button)
		}
		mod.pointer(module.Input{Event: scene.InputEvent{Kind: scene.InputEventPress, Button: button, X: 700, Y: 500}})
		if !mod.pressed[int(button)] {
			t.Fatal("desktop press stopped working")
		}
		mod.pointer(module.Input{Event: scene.InputEvent{Kind: scene.InputEventRelease, Button: button, X: x, Y: y}})
		if mod.pressed != [3]bool{} {
			t.Fatal("release over toolbar left remote mouse button held")
		}
	}
}

func TestToolbarIconsSurviveLocalizationAndFullscreen(t *testing.T) {
	context, mod := mountFixture(t)
	mod.host = &module.Host{ToggleFullscreen: func() {}}
	for _, language := range []string{"en", "ja", "zh-TW", "zh-CN"} {
		mod.setLanguage(language)
		mod.toggleFullscreen()
		label, icon := "全屏", "fullscreen"
		if mod.fullscreen {
			label, icon = "窗口模式", "window"
		}
		if got, _ := attribute(context, "rdp-fullscreen", "aria-label"); got != mod.tr(label) {
			t.Fatalf("fullscreen label = %q, want %q", got, mod.tr(label))
		}
		if got, _ := attribute(context, "rdp-fullscreen-icon", "src"); got != "icons/toolbar-"+icon+".svg" {
			t.Fatalf("fullscreen icon = %q", got)
		}
		root, err := context.Content().HTMLRoot(context.Content().Root())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range toolbarLabels {
			node, _ := root.FindByID(item.id)
			markup, _ := node.Serialize()
			if !strings.Contains(markup, "<img") {
				t.Fatalf("%s: %s icon erased by text binding", language, item.id)
			}
		}
	}
}
