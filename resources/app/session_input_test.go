package app

import (
	"testing"

	"github.com/onix-engine/onix/ecs"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	htmlui "github.com/onix-engine/onix/ui/html"
	"onrdp/internal/store"
)

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
