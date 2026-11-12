package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/onix-engine/onix/ecs"
	"github.com/onix-engine/onix/graphics"
	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	htmlui "github.com/onix-engine/onix/ui/html"
	"github.com/onix-engine/onix/window"
	"onrdp/internal/i18n"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/store"
)

// testScene mirrors the production scene behavior: it owns nothing and only
// forwards platform input to the UI module.
type testScene struct{}

// fixtureWidth and fixtureHeight are the phone-sized viewport the layout tests
// use, matching the project's adaptive default.
const (
	fixtureWidth  = 420
	fixtureHeight = 780
)

// mountFixture builds the real page and module inside an engine runtime, with a
// temporary vault directory so the test never touches the user's data.
func mountFixture(t *testing.T) (*module.Context, *Mod) {
	return mountFixtureSize(t, fixtureWidth, fixtureHeight)
}

func mountFixtureSize(t *testing.T, width, height int) (*module.Context, *Mod) {
	context, mod, _ := mountRuntimeSize(t, width, height)
	return context, mod
}

// mountRuntime also returns the runtime, which is what delivers real HTML
// events the way the app receives them at run time.
func mountRuntime(t *testing.T) (*module.Context, *Mod, *module.Runtime) {
	return mountRuntimeSize(t, fixtureWidth, fixtureHeight)
}

func mountRuntimeSize(t *testing.T, width, height int) (*module.Context, *Mod, *module.Runtime) {
	c, m, r, _ := mountRuntimeWorldSize(t, width, height)
	return c, m, r
}

func mountRuntimeWorldSize(t *testing.T, width, height int) (*module.Context, *Mod, *module.Runtime, *ecs.World) {
	return mountRuntimeWorldSizeWithHost(t, width, height, nil)
}

func mountRuntimeWorldSizeWithHost(t *testing.T, width, height int, host *module.Host) (*module.Context, *Mod, *module.Runtime, *ecs.World) {
	t.Helper()
	t.Setenv("ONRDP_DATA_DIR", t.TempDir())
	t.Setenv("ONRDP_LOCALE", "zh-CN")

	source := projectFiles(t)
	plan, err := project.CompileProject(source, ".")
	if err != nil {
		t.Fatalf("compile project: %v", err)
	}
	var context *module.Context
	var mod *Mod
	registry := module.Registry{}
	if err := registry.RegisterMod("resources/app", func() Mod { return Mod{} }, module.Lifecycle[Mod]{
		Install: func(c *module.Context, m *Mod) error {
			context, mod = c, m
			return m.Install(c)
		},
		Update:    func(c *module.Context, m *Mod, tick module.Tick) error { return m.Update(c, tick) },
		OnMessage: func(c *module.Context, m *Mod, message module.Message) error { return m.OnMessage(c, message) },
		Destroy:   func(c *module.Context, m *Mod) error { return m.Destroy(c) },
	}); err != nil {
		t.Fatalf("register mod: %v", err)
	}
	// The scene behavior forwards input to the module, exactly like main.go.
	if err := registry.RegisterScene("main.json", func() testScene { return testScene{} }, module.Lifecycle[testScene]{
		OnMessage: func(c *module.Context, _ *testScene, message module.Message) error {
			if message.Topic == module.InputTopic {
				return c.Forward("app", message)
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("register scene: %v", err)
	}
	// A camera and a viewport size are what the layout uses to give the page a
	// concrete box; without them every node reports empty bounds.
	world := ecs.NewWorld()
	ui.RegisterRuntimeFeature[ui.HTMLRenderer](world)
	world.InsertResource(input.NewState())
	camera := scene.SpawnCamera2D(world)
	world.GetMut[scene.Camera](camera).UpdateViewport(width, height)
	world.InsertResource(scene.ViewportSize{Width: float32(width), Height: float32(height)})
	runtime, err := module.New[module.WithContent[module.WithContent[module.Render2D, module.LayoutContent], module.HTMLContent]](
		world, plan, registry, module.Options{Source: source, Resources: project.ImageFeature[testImageDecoder]{}, Host: host},
	)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if _, err := runtime.MountEntry(); err != nil {
		t.Fatalf("mount entry: %v", err)
	}
	if mod == nil || context == nil {
		t.Fatal("module Install did not run")
	}
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync html: %v", err)
	}
	return context, mod, runtime, world
}

// The module must reach a working state: the vault opens, the page binds and the
// frame provider replaces the project provider without losing icon lookups.
func TestInstallBindsPageAndVault(t *testing.T) {
	context, mod := mountFixture(t)
	if mod.vault == nil {
		t.Fatal("vault did not open")
	}
	if mod.frame == nil {
		t.Fatal("frame provider was not installed")
	}
	if mod.language != "zh-CN" {
		t.Fatalf("language = %q", mod.language)
	}
	surface, ok := context.Content().Get[htmlui.Surface](context.Content().Root())
	if !ok {
		t.Fatal("root has no HTML surface")
	}
	if _, installed := surface.Images.Provider.(*frameTexture); !installed {
		t.Fatal("surface still uses the project provider")
	}
	// Icons must still resolve through the frame provider's fallback.
	if texture, ok := surface.Images.Provider.ResolveHTMLImage("icons/plus.svg", 0, 0); !ok || texture == nil {
		t.Fatal("bundled icon no longer resolves")
	}
	if mod.page != "hosts" {
		t.Fatalf("initial page = %q", mod.page)
	}
	if !visible(mod.node("hosts-page")) {
		t.Fatal("host list page was not laid out")
	}
}

// Routing must hide every other page, otherwise the screens stack on top of
// each other and the visible one is whichever renders last.
func TestRouteHidesOtherPages(t *testing.T) {
	context, mod := mountFixture(t)
	for _, page := range []string{"sessions", "settings", "edit", "rdp", "hosts"} {
		mod.route(page)
		// The engine re-lays out the document on the next sync, which is what
		// makes a hidden section actually disappear from the screen.
		if err := context.Content().SyncHTML(); err != nil {
			t.Fatalf("sync html: %v", err)
		}
		for _, other := range []string{"hosts", "sessions", "settings", "edit", "rdp"} {
			shown := visible(mod.node(other + "-page"))
			if other == page && !shown {
				t.Fatalf("page %q is not visible after routing to it", other)
			}
			if other != page && shown {
				t.Fatalf("page %q is still visible after routing to %q", other, page)
			}
		}
	}
}

// Desktop windows must have a persistent side navigation instead of the mobile
// bottom bar, while the remote page still fills the entire content surface.
func TestDesktopLayoutUsesSideNavigation(t *testing.T) {
	context, mod := mountFixtureSize(t, 1024, 700)
	nav, ok := mod.node("bottom-nav").Bounds()
	if !ok || nav.X != 0 || nav.Y != 0 || nav.Width != 180 || nav.Height != 700 {
		t.Fatalf("desktop navigation bounds = %+v, ok=%t", nav, ok)
	}
	page, ok := mod.node("hosts-page").Bounds()
	if !ok || page.X != 180 || page.Y != 0 || page.Width != 844 || page.Height != 700 {
		t.Fatalf("desktop content bounds = %+v, ok=%t", page, ok)
	}
	mod.route("rdp")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync remote page: %v", err)
	}
	remote, ok := mod.node("rdp-page").Bounds()
	if !ok || remote.X != 0 || remote.Width != 1024 || remote.Height != 700 {
		t.Fatalf("remote desktop bounds = %+v, ok=%t", remote, ok)
	}
}

func TestSessionListVisibilityTracksAvailableSession(t *testing.T) {
	for _, width := range []int{420, 840, 1024} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			context, mod := mountFixtureSize(t, width, 700)
			host := store.Host{ID: "office", Name: "Office", Address: "10.0.0.7", Port: 3389, User: "alice", Password: "secret"}
			if err := mod.vault.SaveHost(host); err != nil {
				t.Fatal(err)
			}
			mod.route("sessions")
			for _, state := range []struct {
				name, selected string
				connected      bool
				wantCard       bool
			}{
				{name: "empty"},
				{name: "connected", selected: host.ID, connected: true, wantCard: true},
				{name: "disconnected", selected: host.ID, wantCard: true},
				{name: "missing host", selected: "missing"},
				{name: "cleared"},
			} {
				mod.selected, mod.connected = state.selected, state.connected
				mod.renderLists()
				if err := context.Content().SyncHTML(); err != nil {
					t.Fatal(err)
				}
				card, _ := mod.root.FindByID("session-" + host.ID)
				if visible(card) != state.wantCard {
					t.Fatalf("%s: session card visible = %t", state.name, visible(card))
				}
				if visible(mod.node("sessions-empty")) == state.wantCard {
					t.Fatalf("%s: empty state must appear only when no card exists", state.name)
				}
				for _, id := range []string{"close-all", "session-count"} {
					if visible(mod.node(id)) != state.connected {
						t.Fatalf("%s: %s visible = %t", state.name, id, visible(mod.node(id)))
					}
				}
			}
		})
	}
}

func TestCardActionsFollowSelectedLanguage(t *testing.T) {
	context, mod := mountFixture(t)
	host := store.Host{ID: "office", Name: "Office", Address: "10.0.0.7", Port: 3389, User: "alice", Password: "secret"}
	if err := mod.vault.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	mod.selected = host.ID
	for _, language := range []string{"en", "ja", "zh-TW", "zh-CN"} {
		mod.setLanguage(language)
		root, err := context.Content().HTMLRoot(context.Content().Root())
		if err != nil {
			t.Fatal(err)
		}
		for id, key := range map[string]string{"close:office": "断开", "edit:office": "编辑", "favorite:office": "收藏"} {
			node, ok := root.FindByID(id)
			if !ok {
				t.Fatalf("missing action %s", id)
			}
			markup, err := node.Serialize()
			if err != nil || !strings.Contains(markup, ">"+mod.tr(key)+"</button>") {
				t.Fatalf("%s: action %s = %q, err=%v", language, id, markup, err)
			}
		}
	}
}

// Saving a host through the UI must persist it and return to the host list.
func TestSaveHostThroughUI(t *testing.T) {
	context, mod := mountFixture(t)
	mod.edit(context, "")
	mod.fields["name"] = "Office"
	mod.fields["address"] = "10.0.0.7"
	mod.fields["user"] = "alice"
	mod.fields["password"] = "secret"
	mod.fields["port"] = "3389"
	mod.click(context, "quality-smooth")
	mod.save(context)

	hosts := mod.vault.Snapshot().Hosts
	if len(hosts) != 1 {
		t.Fatalf("saved %d hosts, want 1", len(hosts))
	}
	if hosts[0].Name != "Office" || hosts[0].Address != "10.0.0.7" || hosts[0].Port != 3389 {
		t.Fatalf("saved host = %+v", hosts[0])
	}
	if mod.page != "hosts" {
		t.Fatalf("page after save = %q", mod.page)
	}
	if mod.fields["password"] != "" {
		t.Fatal("password stayed in memory after saving")
	}
	if hosts[0].Quality != "smooth" {
		t.Fatal("selected picture quality was not saved")
	}
	mod.edit(context, hosts[0].ID)
	if selected, _ := attribute(context, "quality-smooth", "aria-pressed"); selected != "true" {
		t.Fatal("reopening host did not restore selected picture quality")
	}
}

func TestQualityLimitsFullscreenAndWindowedResolution(t *testing.T) {
	_, mod := mountFixture(t)
	host := store.Host{ID: "quality", Name: "Office", Address: "10.0.0.7", Port: 3389, User: "alice", Password: "secret", Width: 2560, Height: 1440, Quality: "smooth"}
	if err := mod.vault.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	mod.selected = host.ID
	mod.sessionQuality = rdpclient.QualitySmooth
	mod.host = &module.Host{Context: &window.Context{Width: 3840, Height: 2160}, ToggleFullscreen: func() {}}
	for _, fullscreen := range []bool{false, true, false} {
		mod.fullscreen = fullscreen
		if w, h := mod.desktopResolution(); w != 1280 || h != 720 {
			t.Fatalf("fullscreen=%v requested %dx%d", fullscreen, w, h)
		}
	}
	// Editing a saved preference takes effect on reconnect, so resize updates
	// must continue using the active session's profile.
	host.Quality = "high"
	if err := mod.vault.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	if w, h := mod.desktopResolution(); w != 1280 || h != 720 {
		t.Fatalf("saved edit changed active quality to %dx%d", w, h)
	}
}

// An incomplete record must be rejected with a message instead of written.
func TestSaveRejectsIncompleteHost(t *testing.T) {
	context, mod := mountFixture(t)
	mod.edit(context, "")
	mod.fields["name"] = "Office"
	mod.fields["address"] = ""
	mod.fields["user"] = "alice"
	mod.fields["password"] = "secret"
	mod.save(context)
	if hosts := mod.vault.Snapshot().Hosts; len(hosts) != 0 {
		t.Fatalf("incomplete host was saved: %+v", hosts)
	}
	if mod.toastUntil.IsZero() {
		t.Fatal("no error was shown")
	}
	if mod.page != "edit" {
		t.Fatalf("page after a failed save = %q", mod.page)
	}
}

// Deleting a host must remove it from the vault and clear the selection.
func TestDeleteSelectedHost(t *testing.T) {
	context, mod := mountFixture(t)
	mod.edit(context, "")
	mod.fields["name"] = "Office"
	mod.fields["address"] = "10.0.0.7"
	mod.fields["user"] = "alice"
	mod.fields["password"] = "secret"
	mod.save(context)
	host := mod.vault.Snapshot().Hosts[0]

	mod.edit(context, host.ID)
	mod.selected = host.ID
	mod.requestConfirm("delete", "删除这台主机？", "本机会移除主机及其登录凭据。")
	mod.confirmed(context)
	if hosts := mod.vault.Snapshot().Hosts; len(hosts) != 0 {
		t.Fatalf("host survived deletion: %+v", hosts)
	}
	if mod.selected != "" {
		t.Fatalf("selection survived deletion: %q", mod.selected)
	}
}

func TestDisconnectReturnsToSessionsAndLeavesFullscreen(t *testing.T) {
	context, mod := mountFixture(t)
	toggles := 0
	mod.host = &module.Host{ToggleFullscreen: func() { toggles++ }}
	mod.route("rdp")
	mod.fullscreen = true
	mod.connected = true
	mod.pressed[0] = true
	mod.click(context, "rdp-close")
	mod.click(context, "confirm-ok")
	if mod.page != "sessions" || mod.fullscreen || mod.connected || mod.pressed[0] {
		t.Fatalf("disconnect left page=%q fullscreen=%t connected=%t pressed=%v", mod.page, mod.fullscreen, mod.connected, mod.pressed)
	}
	if toggles != 1 || visible(mod.node("rdp-page")) || visible(mod.node("confirm-modal")) {
		t.Fatal("disconnect did not leave fullscreen and close the remote page/modal")
	}
	if mod.client.Status().State != rdpclient.StateClosed {
		t.Fatal("disconnect left client open")
	}
}

func TestWindowCloseDisconnectsSessionAndKeepsAppOpen(t *testing.T) {
	for _, tc := range []struct {
		name       string
		page       string
		connected  bool
		fullscreen bool
	}{
		{name: "remote desktop", page: "rdp", connected: true},
		{name: "fullscreen", page: "rdp", connected: true, fullscreen: true},
		{name: "connection overlay", page: "rdp"},
		{name: "background session", page: "hosts", connected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var closeRequested func() bool
			renders, toggles := 0, 0
			host := &module.Host{
				OnCloseRequested: func(f func() bool) { closeRequested = f },
				RequestRender:    func() { renders++ },
				ToggleFullscreen: func() { toggles++ },
			}
			context, mod, runtime, _ := mountRuntimeWorldSizeWithHost(t, 1024, 700, host)
			if closeRequested == nil {
				t.Fatal("window close handler was not installed")
			}
			if !closeRequested() || mod.client.Status().State != rdpclient.StateIdle {
				t.Fatal("window close without a session must allow normal application exit")
			}
			mod.route(tc.page)
			mod.connected, mod.fullscreen = tc.connected, tc.fullscreen
			mod.pressed = [3]bool{true, true, true}
			mod.requestConfirm("disconnect", "断开连接？", "远程桌面会话将关闭，未保存的工作可能丢失。")
			renders = 0
			if closeRequested() {
				t.Fatal("closing a session allowed the application to exit")
			}
			if mod.client.Status().State != rdpclient.StateClosed || mod.connected || mod.pressed != [3]bool{} {
				t.Fatal("window close did not release the session and its input state")
			}
			if mod.page != "sessions" || mod.fullscreen || mod.confirm != "" || visible(mod.node("confirm-modal")) {
				t.Fatal("window close did not return to the session list and dismiss overlays")
			}
			if renders == 0 || (toggles == 1) != tc.fullscreen {
				t.Fatalf("window close: renders=%d fullscreen toggles=%d", renders, toggles)
			}
			if err := runtime.Update(1.0 / 60); err != nil {
				t.Fatal(err)
			}
			if err := context.Content().SyncHTML(); err != nil {
				t.Fatal(err)
			}
			if !visible(mod.node("sessions-page")) || visible(mod.node("rdp-page")) {
				t.Fatal("session list was not shown after window close")
			}
			if !closeRequested() {
				t.Fatal("second window close without a session must allow application exit")
			}
		})
	}
}

func TestFullscreenResolutionUsesNativePixelsAndRestoresHostSize(t *testing.T) {
	_, mod := mountFixture(t)
	host := store.Host{ID: "office", Name: "Office", Address: "10.0.0.7", Port: 3389, User: "alice", Password: "secret", Width: 1600, Height: 900}
	if err := mod.vault.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	mod.selected = host.ID
	surface := &window.Context{Width: 3456, Height: 2234}
	mod.host = &module.Host{
		Context:          surface,
		ToggleFullscreen: func() {},
		GetViewport:      func() (int, int) { return 1728, 1117 },
	}
	mod.toggleFullscreen()
	if w, h := mod.desktopResolution(); w != 3456 || h != 2234 {
		t.Fatalf("fullscreen resolution=%dx%d; expected native Retina pixels", w, h)
	}
	// Fullscreen animation and moving between monitors can change the surface
	// after the toggle; each update must use the latest native dimensions.
	surface.Width, surface.Height = 3840, 2160
	if w, h := mod.desktopResolution(); w != 3840 || h != 2160 {
		t.Fatalf("resolution after surface resize=%dx%d", w, h)
	}
	mod.toggleFullscreen()
	if w, h := mod.desktopResolution(); w != host.Width || h != host.Height {
		t.Fatalf("windowed resolution=%dx%d; expected saved host size", w, h)
	}
}

// Language changes must retranslate the page and persist the preference.
func TestLanguageSwitchPersists(t *testing.T) {
	_, mod := mountFixture(t)
	mod.setLanguage("ja")
	if mod.language != "ja" {
		t.Fatalf("language = %q", mod.language)
	}
	if mod.vault.Settings().Language != "ja" {
		t.Fatalf("saved language = %q", mod.vault.Settings().Language)
	}
	if got := mod.tr("设置"); got != "設定" {
		t.Fatalf("translated settings heading = %q", got)
	}
}

// The remote point mapping must report failure rather than a bogus coordinate
// when there is nothing to map onto.
func TestRemotePointRejectsUnmappedPoints(t *testing.T) {
	_, mod := mountFixture(t)
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1600, Height: 900}
	if _, _, ok := mod.remotePoint(10, 10); ok {
		t.Fatal("mapped a point outside the stage")
	}
}

// With no session geometry the pointer must be ignored instead of dividing by
// a zero framebuffer.
func TestRemotePointNeedsFramebufferGeometry(t *testing.T) {
	context, mod := mountFixture(t)
	mod.route("rdp")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync html: %v", err)
	}
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected}
	bounds, ok := mod.node("rdp-stage").Bounds()
	if !ok {
		t.Skip("stage was not laid out")
	}
	if _, _, mapped := mod.remotePoint(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); mapped {
		t.Fatal("mapped a point without framebuffer geometry")
	}
}

// The pointer must land on the remote pixel under the cursor: the letterboxed
// frame is centred, so the stage centre maps to the framebuffer centre and the
// stage corner maps outside it.
func TestRemotePointFollowsLetterbox(t *testing.T) {
	context, mod := mountFixture(t)
	mod.route("rdp")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync html: %v", err)
	}
	bounds, ok := mod.node("rdp-stage").Bounds()
	if !ok || bounds.Width < 40 || bounds.Height < 40 {
		t.Skip("stage was not laid out")
	}
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 800, Height: 600}

	x, y, mapped := mod.remotePoint(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2)
	if !mapped {
		t.Fatal("stage centre was not mapped")
	}
	if abs(x-400) > 1 || abs(y-300) > 1 {
		t.Fatalf("stage centre mapped to %d,%d, want 400,300", x, y)
	}
	if _, _, mapped := mod.remotePoint(bounds.X, bounds.Y); mapped {
		t.Fatal("letterbox corner mapped onto the framebuffer")
	}

	// Legacy preferences cannot crop the remote frame or change pointer mapping.
	if err := mod.vault.SaveSettings(store.Settings{FitToWindow: false}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	x, y, mapped = mod.remotePoint(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2)
	if !mapped || abs(x-400) > 1 || abs(y-300) > 1 {
		t.Fatalf("legacy preference changed mapping = %d,%d (%t)", x, y, mapped)
	}
	if _, _, mapped := mod.remotePoint(bounds.X, bounds.Y); mapped {
		t.Fatal("legacy preference removed letterboxing")
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func TestFullDesktopFitsDifferentWindowShapes(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{"portrait", 420, 780},
		{"laptop", 1024, 700},
		{"wide", 1800, 600},
	} {
		t.Run(size.name, func(t *testing.T) {
			context, mod := mountFixtureSize(t, size.width, size.height)
			mod.route("rdp")
			if err := context.Content().SyncHTML(); err != nil {
				t.Fatal(err)
			}
			mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1920, Height: 1080}
			if err := mod.vault.SaveSettings(store.Settings{FitToWindow: false}); err != nil {
				t.Fatal(err)
			}
			stage, _ := mod.node("rdp-stage").Bounds()
			scale, x, y, ok := mod.frameLayout()
			if !ok || scale <= 0 || x < -.01 || y < -.01 ||
				1920*scale > stage.Width+.01 || 1080*scale > stage.Height+.01 {
				t.Fatalf("remote desktop clipped: scale=%f offset=%f,%f stage=%+v", scale, x, y, stage)
			}
			// Both far corners remain accessible after local scaling, including
			// windows whose aspect ratio differs from the remote desktop.
			for _, point := range [][2]int{{1, 1}, {1918, 1078}} {
				px := stage.X + x + (float32(point[0])+.5)*scale
				py := stage.Y + y + (float32(point[1])+.5)*scale
				rx, ry, mapped := mod.remotePoint(px, py)
				if !mapped || rx != point[0] || ry != point[1] {
					t.Fatalf("point %v mapped to %d,%d (%t)", point, rx, ry, mapped)
				}
			}
		})
	}
}

func TestTextLookupFallsBackToSystemLocale(t *testing.T) {
	t.Setenv("ONRDP_LOCALE", "ja_JP.UTF-8")
	if got := i18n.Resolve("", platform.SystemLocale()); got != "ja" {
		t.Fatalf("resolved language = %q", got)
	}
}

// attribute reads a DOM attribute through the engine's own node type, which is
// the only place attribute reads are exposed.
func attribute(context *module.Context, id, name string) (string, bool) {
	root, err := context.Content().HTMLRoot(context.Content().Root())
	if err != nil {
		return "", false
	}
	node, ok := root.FindByID(id)
	if !ok {
		return "", false
	}
	return node.Attribute(name)
}

// visible reports whether the node currently occupies a laid-out box. Hidden
// pages keep their DOM but lose their bounds, so this is the observable signal
// that routing actually swapped the screen.
func visible(node module.HTMLNode) bool {
	bounds, ok := node.Bounds()
	return ok && bounds.Width > 0 && bounds.Height > 0
}

// testImageDecoder keeps the project image feature satisfied without a GPU; the
// tests only require textures to exist, not to be pixel accurate.
type testImageDecoder struct{}

func (testImageDecoder) DecodeImage(_ []byte, _ string, _ graphics.TextureOptions) (*graphics.Texture, error) {
	return graphics.CreateEmptyTexture(1, 1, 0, graphics.ResourceUsageDefault), nil
}

// The session screen must expose a laid-out stage whose image is the remote
// frame, otherwise the session renders into a zero-sized box.
func TestSessionStageIsLaidOut(t *testing.T) {
	context, mod := mountFixture(t)
	// renderSession needs a selected host, so seed one through the editor.
	mod.edit(context, "")
	mod.fields["name"] = "Office"
	mod.fields["address"] = "10.0.0.7"
	mod.fields["user"] = "alice"
	mod.fields["password"] = "secret"
	mod.save(context)
	mod.selected = mod.vault.Snapshot().Hosts[0].ID
	mod.route("rdp")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync html: %v", err)
	}
	stage, ok := mod.node("rdp-stage").Bounds()
	if !ok || stage.Width <= 0 || stage.Height <= 0 {
		t.Fatalf("stage bounds = %+v (%t)", stage, ok)
	}
	frame, ok := mod.node("rdp-frame").Bounds()
	if !ok || frame.Width != stage.Width || frame.Height != stage.Height {
		t.Fatalf("frame bounds = %+v in stage %+v", frame, stage)
	}
	if got, _ := attribute(context, "rdp-frame", "src"); got != framePath {
		t.Fatalf("frame src = %q", got)
	}
	if got, _ := attribute(context, "rdp-frame", "class"); got != "rdp-frame" {
		t.Fatalf("frame class = %q", got)
	}
	// Existing vaults may have disabled fitting; full-frame display must still
	// match the coordinate transform after upgrading.
	if err := mod.vault.SaveSettings(store.Settings{FitToWindow: false}); err != nil {
		t.Fatal(err)
	}
	mod.renderSession()
	if got, _ := attribute(context, "rdp-frame", "class"); got != "rdp-frame" {
		t.Fatalf("frame class with legacy preference = %q", got)
	}
}

func TestRemoteInputReceivesKeyboardFocus(t *testing.T) {
	context, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
	mod.route("rdp")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	if err := ui.SyncHTMLSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	control, ok := context.Content().HTMLControl(context.Content().Root(), "rdp-input")
	if !ok || mod.focusInput || context.Content().FocusedControl() != control {
		t.Fatalf("remote input did not receive keyboard focus: control=%v exists=%t pending=%t focused=%v textfield=%t", control, ok, mod.focusInput, context.Content().FocusedControl(), context.Content().Has[ui.TextField](control))
	}
}

// Module updates run before the UI processes pointer presses. A press on the
// remote image clears the textarea's focus later in that same frame.
func TestRemoteKeyboardSurvivesStageClick(t *testing.T) {
	context, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
	mod.route("rdp")
	mod.hide("rdp-overlay", true)
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	if err := ui.SyncHTMLSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	for _, system := range []ecs.SystemConfig{scene.SyncGlobalTransformSystem(""), scene.SyncUIGlobalTransformSystem("")} {
		if err := system.Run(world); err != nil {
			t.Fatal(err)
		}
	}
	stage, _ := mod.node("rdp-stage").Bounds()
	state, _ := world.GetResource[*input.State]()
	state.PressMouse(input.MouseLeft, float64(stage.X+stage.Width/2), float64(stage.Y+stage.Height/2))
	if err := scene.DispatchInputEvents2DSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	var reader ecs.MessageReader[scene.InputEvent]
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1600, Height: 900}
	for event := range reader.All(world) {
		if err := runtime.HandleInput(*event); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	if err := ui.UpdateSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	state.EndFrame()
	world.AdvanceMessages()
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	control, ok := context.Content().HTMLControl(context.Content().Root(), "rdp-input")
	if !ok || context.Content().FocusedControl() != control {
		t.Fatal("clicking the remote desktop left the keyboard input unfocused")
	}
	// Drive the actual UI keyboard dispatch, not just the Focus API.
	handler := world.GetMut[ui.TextFieldKeyHandler](control)
	if handler == nil || handler.Handle == nil {
		t.Fatal("remote keyboard handler is missing")
	}
	called := false
	handler.Handle = func(s *input.State) bool {
		called = s.KeyPressed(input.KeyA)
		return true
	}
	state.PressKey(input.KeyA)
	if err := ui.UpdateSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("key press did not reach the remote keyboard handler")
	}
	state.EndFrame()
	world.AdvanceMessages()

	// A confirmation keeps local keyboard focus until it is dismissed.
	mod.click(context, "rdp-close")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	if err := ui.SyncHTMLSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	cancel, ok := context.Content().HTMLControl(context.Content().Root(), "confirm-cancel")
	if !ok || !context.Content().Focus(cancel) {
		t.Fatal("could not focus the confirmation button")
	}
	mod.focusInput = true
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	if context.Content().FocusedControl() != cancel {
		t.Fatal("remote keyboard stole focus from the confirmation")
	}
	mod.click(context, "confirm-cancel")
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	if err := ui.SyncHTMLSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Update(1.0 / 60); err != nil {
		t.Fatal(err)
	}
	control, ok = context.Content().HTMLControl(context.Content().Root(), "rdp-input")
	if !ok || context.Content().FocusedControl() != control || world.GetRef[ui.TextFieldKeyHandler](control) == nil {
		t.Fatal("remote keyboard did not recover after dismissing the confirmation")
	}
}

func TestRemoteStageReceivesPointerPress(t *testing.T) {
	context, mod, runtime, world := mountRuntimeWorldSize(t, 1024, 700)
	mod.route("rdp")
	mod.hide("rdp-overlay", true)
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	runtime.SyncScopes()
	for _, system := range []ecs.SystemConfig{scene.SyncGlobalTransformSystem(""), scene.SyncUIGlobalTransformSystem("")} {
		if err := system.Run(world); err != nil {
			t.Fatal(err)
		}
	}
	stage, _ := mod.node("rdp-stage").Bounds()
	state, _ := world.GetResource[*input.State]()
	state.PressMouse(input.MouseLeft, float64(stage.X+stage.Width/2), float64(stage.Y+stage.Height/2))
	if err := scene.DispatchInputEvents2DSystem("").Run(world); err != nil {
		t.Fatal(err)
	}
	var reader ecs.MessageReader[scene.InputEvent]
	mod.status = rdpclient.Snapshot{State: rdpclient.StateConnected, Width: 1600, Height: 900}
	mod.focusInput = false
	for event := range reader.All(world) {
		if event.Kind == scene.InputEventPress && event.Entity != 0 {
			if err := runtime.HandleInput(*event); err != nil {
				t.Fatal(err)
			}
			if !mod.pressed[0] || !mod.focusInput {
				t.Fatal("left press did not reach the remote input handler")
			}
			return
		}
	}
	t.Fatal("remote stage did not receive a mouse press")
}

// Printable ASCII must not be sent twice: the scancode carries it, so the
// Unicode fallback has to drop it and keep only what a scancode cannot express.
func TestUnicodeOnlySkipsScancodeCharacters(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"hello":    "",
		"hello 1!": "",
		"中文":       "中文",
		"café":     "é",
		"a中b文":     "中文",
		"\n":       "\n",
	}
	for input, want := range cases {
		if got := unicodeOnly(input); got != want {
			t.Fatalf("unicodeOnly(%q) = %q, want %q", input, got, want)
		}
	}
}

// The page must react to the same messages the engine delivers at run time:
// clicking a nav button routes, and typing in a field updates the draft. This
// drives the real runtime instead of calling the handlers directly.
func TestRuntimeMessagesDriveThePage(t *testing.T) {
	context, mod, runtime := mountRuntime(t)
	click := func(id string) {
		t.Helper()
		if err := runtime.HandleHTMLEvent(htmlui.Event{Entity: context.Content().Root(), ID: id, Kind: htmlui.EventClick}); err != nil {
			t.Fatalf("click %s: %v", id, err)
		}
	}

	click("nav-settings")
	if mod.page != "settings" {
		t.Fatalf("page after clicking settings = %q", mod.page)
	}
	click("add-host")
	if mod.page != "edit" {
		t.Fatalf("page after clicking add = %q", mod.page)
	}

	if err := runtime.HandleHTMLEvent(htmlui.Event{
		Entity: context.Content().Root(), ID: "field-name", Kind: htmlui.EventInput, Value: "Office",
	}); err != nil {
		t.Fatalf("type name: %v", err)
	}
	if mod.fields["name"] != "Office" {
		t.Fatalf("draft name = %q", mod.fields["name"])
	}

	// The record is incomplete, so saving must report an error and stay put.
	click("edit-save")
	if mod.page != "edit" {
		t.Fatalf("page after an invalid save = %q", mod.page)
	}
	if len(mod.vault.Snapshot().Hosts) != 0 {
		t.Fatal("incomplete host was saved")
	}
	if mod.toastUntil.IsZero() {
		t.Fatal("no error was shown")
	}

	click("edit-cancel")
	if mod.page != "hosts" {
		t.Fatalf("page after cancelling = %q", mod.page)
	}
}

// A toggle in the host form must flip the draft flag it stands for, and the
// toggles must show the values of the record being edited.
func TestHostFormTogglesTrackTheDraft(t *testing.T) {
	context, mod, runtime := mountRuntime(t)
	mod.edit(context, "")
	for _, toggle := range []string{"field-clipboard", "field-console"} {
		if err := runtime.HandleHTMLEvent(htmlui.Event{Entity: context.Content().Root(), ID: toggle, Kind: htmlui.EventClick}); err != nil {
			t.Fatalf("click %s: %v", toggle, err)
		}
	}
	// A fresh host starts with clipboard on, console off, and fixed resolution.
	if mod.draft.Clipboard || mod.draft.DynamicSize || !mod.draft.ConsoleMode {
		t.Fatalf("draft flags = %+v", mod.draft)
	}
	if got, _ := attribute(context, "field-console", "aria-checked"); got != "true" {
		t.Fatalf("console toggle aria-checked = %q", got)
	}
}

// A missing vault key must surface as the fatal panel instead of a blank page,
// and the rest of the module must stay inert rather than panic.
func TestFatalPanelShowsWhenStorageFails(t *testing.T) {
	context, mod := mountFixture(t)
	mod.showFatal(errors.New("磁盘不可用"))
	if err := context.Content().SyncHTML(); err != nil {
		t.Fatalf("sync html: %v", err)
	}
	if !visible(mod.node("fatal")) {
		t.Fatal("fatal panel stayed hidden")
	}
	root, err := context.Content().HTMLRoot(context.Content().Root())
	if err != nil {
		t.Fatalf("html root: %v", err)
	}
	message, ok := root.FindByID("fatal-message")
	if !ok {
		t.Fatal("fatal message node is missing")
	}
	if got, _ := message.Serialize(); !strings.Contains(got, "磁盘不可用") {
		t.Fatalf("fatal message = %q", got)
	}
}
