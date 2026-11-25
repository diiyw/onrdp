//go:build linux || darwin

package app

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/onix-engine/onix/input"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/ui"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/scrcpy"
	"onrdp/internal/store"
)

func TestDroppedAPKsInstallOnWindowDevice(t *testing.T) {
	_, m, rt, _ := mountRuntimeWorldSizeWithHost(t, 900, 700, &module.Host{RequestRender: func() {}})
	drop := func(path string) {
		t.Helper()
		if err := rt.HandleInput(scene.InputEvent{Kind: scene.InputEventFilesDropped, File: path}); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	adb := filepath.Join(dir, "adb")
	const mock = `#!/bin/sh
[ "$#" = 5 ] && [ "$1" = "-s" ] && [ "$2" = "test-device" ] && [ "$3" = install ] && [ "$4" = "-r" ] || exit 2
printf '%s\n' "$5" >> "$APK_LOG"
case "$5" in *.APK) echo 'INSTALL_FAILED_TEST' >&2; exit 1 ;; esac
`
	if err := os.WriteFile(adb, []byte(mock), 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "installed")
	t.Setenv("APK_LOG", log)
	bad, good := filepath.Join(dir, "first.APK"), filepath.Join(dir, "测试 app.apk")
	for _, path := range []string{bad, good} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	drop(good) // Outside a mirror window.
	m.mirror = &androidMirror{tools: scrcpy.Tools{ADB: adb}, host: store.Host{Address: "test-device"}}
	drop(bad)
	drop(filepath.Join(dir, "notes.txt"))
	drop(good)
	m.syncAndroidAPKs()
	select {
	case result := <-m.mirror.apkResult:
		if !strings.Contains(result, "INSTALL_FAILED_TEST") || !strings.Contains(result, m.tr("已安装 %s", filepath.Base(good))) {
			t.Fatalf("install results: %q", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no install result after file drop")
	}
	got, err := os.ReadFile(log)
	if err != nil || string(got) != bad+"\n"+good+"\n" {
		t.Fatalf("installed paths %q, error %v", got, err)
	}
}

func TestAndroidDevicePickerAndSession(t *testing.T) {
	dir := t.TempDir()
	writeTool := func(name, script string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	keys := make(chan []byte, 12)
	streams := make(chan net.Conn, 8)
	opened := filepath.Join(dir, "opened")
	t.Setenv("MIRROR_TEST_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	go func() {
		for {
			video, err := listener.Accept()
			if err != nil {
				return
			}
			video.Write([]byte{0})
			control, err := listener.Accept()
			if err != nil {
				video.Close()
				return
			}
			// One scrcpy H.264 access unit for the fixture decoder.
			video.Write(append([]byte("h264"), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 42))
			streams <- video
			for {
				packet := make([]byte, 14)
				if _, err := io.ReadFull(control, packet[:1]); err != nil {
					break
				}
				if packet[0] == 2 {
					var touch [31]byte
					if _, err := io.ReadFull(control, touch[:]); err != nil {
						break
					}
					if touch[0] == 1 {
						if err := os.WriteFile(opened, nil, 0600); err != nil {
							t.Error(err)
						}
					}
					continue
				}
				if packet[0] == 1 {
					packet = packet[:5]
				}
				if _, err := io.ReadFull(control, packet[1:]); err != nil {
					break
				}
				if packet[0] == 1 || packet[0] == 9 {
					n := binary.BigEndian.Uint32(packet[len(packet)-4:])
					payload := make([]byte, n)
					if _, err := io.ReadFull(control, payload); err != nil {
						break
					}
					packet = append(packet, payload...)
				}
				keys <- packet
			}
			video.Close()
			control.Close()
		}
	}()
	t.Setenv("MIRROR_TEST_OPEN", opened)
	exited := filepath.Join(dir, "exited")
	t.Setenv("MIRROR_TEST_EXIT", exited)
	writeTool("adb", `case "$*" in
"devices -l")
    echo 'List of devices attached'
    echo '10.0.0.1:5555 device model:My_Phone transport_id:1'
    echo 'other unauthorized' ;;
"-s 10.0.0.1:5555 shell cmd display get-displays")
    echo 'Displays:'
    echo 'Display id 0: DisplayInfo{"Built-in screen", displayId 0}'
    echo 'Display id 7: DisplayInfo{"other app", displayId 7}'
    if [ -e "$MIRROR_TEST_OPEN" ]; then echo 'Display id 15: DisplayInfo{"mumuscreen012", displayId 15, type INTERNAL}'; fi
    echo 'Display id 14: DisplayInfo{"scrcpy", displayId 14, FLAG_PRIVATE, type VIRTUAL}' ;;
"-s 10.0.0.1:5555 shell cmd activity stack list")
    echo "RootTask id=1 displayId=0 userId=0"
    echo " configuration={mActivityType=home info}"
    echo " taskId=2: launcher visible=true topActivity=ComponentInfo{launcher/Home}"
    echo "RootTask id=77 displayId=7 userId=0"
    echo " configuration={mActivityType=standard info}"
    echo " taskId=77: other.app visible=true topActivity=ComponentInfo{other.app/Main}"
    if [ -e "$MIRROR_TEST_OPEN" ] && [ ! -e "$MIRROR_TEST_EXIT" ]; then
        display=14
        if [ -e "$MIRROR_TEST_OPEN.moved" ]; then display=15; fi
        echo "RootTask id=81 displayId=$display userId=0"
        echo " configuration={mActivityType=standard info}"
        echo " taskId=81: example.app visible=true topActivity=ComponentInfo{example.app/Main}"
    fi ;;
"-s 10.0.0.1:5555 shell cmd activity display move-stack 81 15")
    touch "$MIRROR_TEST_OPEN.moved" ;;
"-s 10.0.0.1:5555 shell cmd activity stack remove 81")
    if [ -e "$MIRROR_TEST_EXIT.reject" ]; then echo 'remove denied'; exit 1; fi
    touch "$MIRROR_TEST_EXIT" ;;
*'dumpsys input'*)
    if [ -e "$MIRROR_TEST_OPEN.moved" ] && [ ! -e "$MIRROR_TEST_EXIT" ]; then
        echo '  FocusedDisplayId: 15'
    elif [ -e "$MIRROR_TEST_EXIT" ]; then
        echo '  FocusedDisplayId: 7'
    else
        echo '  FocusedDisplayId: 0'
    fi ;;
"connect 10.0.0.1:5555"|*' push '*|*' forward --remove '*|*' shell rm '*) exit 0 ;;
*' forward tcp:0 '*)
    if [ -e "$MIRROR_TEST_OPEN" ] && [ ! -e "$MIRROR_TEST_OPEN.retry" ]; then
        touch "$MIRROR_TEST_OPEN.retry"
        exit 1
    fi
    echo "$MIRROR_TEST_PORT" ;;
*'com.genymobile.scrcpy.Server 5.0.1 '*display_id=0*|*'com.genymobile.scrcpy.Server 5.0.1 '*display_id=7*|*'com.genymobile.scrcpy.Server 5.0.1 '*display_id=15*) exec sleep 60 ;;
*) exit 1 ;;
esac
`)
	buildTestMirrorLibrary(t, dir)
	t.Setenv("ADB", "")
	wake := make(chan struct{}, 8)
	host := &module.Host{RequestRender: func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}}
	var session *platform.DesktopWindow
	launcherWindow := &platform.DesktopWindow{OpenSession: func(w *platform.DesktopWindow) { session = w }}
	unbind := launcherWindow.Bind(host)
	vault := t.TempDir()
	c, m, _, _ := mountRuntimeWithVault(t, 1024, 700, host, vault)
	unbind()
	m.click(c, "add-host")
	m.click(c, "protocol-android")
	deadline := time.After(8 * time.Second)
	wait := func(check func() bool, tick func()) {
		t.Helper()
		for !check() {
			select {
			case <-wake:
				tick()
			case <-deadline:
				t.Fatal("timed out waiting for Android session")
			}
		}
	}
	wait(func() bool { return m.android.pending == nil }, m.syncAndroidDevices)
	m.click(c, "android-device:other")
	if m.fields["adb"] != "" {
		t.Fatal("unauthorized device selected")
	}
	m.click(c, "android-device:10.0.0.1:5555")
	m.input("field-group", "Lab")
	m.click(c, "edit-save")
	records := m.vault.Snapshot().Hosts
	if m.page != "hosts" || len(records) != 1 {
		t.Fatal("Android device was not saved")
	}
	h := records[0]
	if h.Name != "My Phone" || h.Protocol != store.Android || h.Address != "10.0.0.1:5555" || !h.Wireless || h.Password != "" || h.Group != "Lab" {
		t.Fatalf("saved host %+v", h)
	}
	m.click(c, "favorite:"+h.ID)
	m.click(c, "edit:"+h.ID)
	if !m.draft.Favorite || m.fields["adb"] != h.Address {
		t.Fatal("favorite or address lost on edit")
	}
	m.click(c, "edit-cancel")

	settings := m.vault.Settings()
	settings.Clipboard = false
	if err := m.vault.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	m.click(c, "connect:"+h.ID)
	if session == nil || len(m.sessionWindows) != 1 {
		t.Fatal("launcher did not request a session window")
	}
	launcher := m
	closed := false
	sessionHost := &module.Host{RequestRender: host.RequestRender, RequestClose: func() { closed = true }, SetWindowTitle: func(string) {}}
	var nativeClose func() bool
	sessionHost.OnCloseRequested = func(f func() bool) { nativeClose = f }
	sessionHost.WakeAt = func(deadline time.Time) {
		timer := time.AfterFunc(time.Until(deadline), host.RequestRender)
		t.Cleanup(func() { timer.Stop() })
	}
	unbind = session.Bind(sessionHost)
	var fitted [][2]int
	var centered []int
	session.FitFrame = func(w, h int, center bool) bool {
		fitted = append(fitted, [2]int{w, h})
		if center {
			centered = append(centered, m.mirror.run.display)
		}
		return true
	}
	c, m, childRuntime, world := mountRuntimeWithVault(t, 900, 700, sessionHost, vault)
	unbind()
	wait(func() bool {
		return m.window.State == rdpclient.StateConnected && m.mirror.indexed && len(m.mirror.displays) == 2
	}, m.syncMirror)
	if m.page != "rdp" || m.frame.width != 2 || m.frame.height != 2 || launcher.page != "hosts" || launcher.androidDecoder(session) != launcher.tr("%s 硬件解码", "VA-API") {
		t.Fatal("session window or launcher did not receive the session state")
	}
	if len(fitted) != 1 || fitted[0] != [2]int{2, 2} || !slices.Equal(centered, []int{0}) {
		t.Fatalf("first frame fit requests: %v, centered displays: %v", fitted, centered)
	}
	// Drive the focused text field through candidate cancellation and commit.
	state, _ := world.GetResource[*input.State]()
	frame := func() {
		t.Helper()
		if err := childRuntime.Update(1.0 / 60); err != nil {
			t.Fatal(err)
		}
		if err := ui.UpdateSystem("").Run(world); err != nil {
			t.Fatal(err)
		}
		state.EndFrame()
	}
	frame()
	sessionHost.ToggleFullscreen = func() { t.Fatal("IME key toggled fullscreen") }
	m.fullscreen = true
	state.ApplyTextComposition(input.TextComposition{Active: true, Text: "ni"})
	state.PressKey(input.KeyEscape)
	state.ApplyTextComposition(input.TextComposition{})
	frame()
	state.ReleaseKey(input.KeyEscape)
	frame()
	state.ApplyTextComposition(input.TextComposition{Active: true, Text: "nihao"})
	frame()
	state.PressKey(input.KeyEnter)
	state.CommitText("你好")
	state.CommitText(" OnRDP🙂")
	frame()
	state.ReleaseKey(input.KeyEnter)
	frame()
	state.CommitText("abc")
	frame()
	for _, want := range []struct {
		kind   byte
		header int
		text   string
	}{{9, 14, "你好 OnRDP🙂"}, {1, 5, "abc"}} {
		select {
		case packet := <-keys:
			if packet[0] != want.kind || string(packet[want.header:]) != want.text || (want.kind == 9 && packet[9] != 1) {
				t.Fatalf("unexpected input packet %x, want type %d and %q", packet, want.kind, want.text)
			}
		case <-deadline:
			t.Fatalf("missing input packet for %q", want.text)
		}
	}
	m.fullscreen = false
	// A rotation changes frame dimensions on the same connection.
	video := <-streams
	for _, size := range [][2]int{{4, 2}, {2, 4}} {
		if _, err := video.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, byte(size[0])}); err != nil {
			t.Fatal(err)
		}
		wait(func() bool { return fitted[len(fitted)-1] == size }, m.syncMirror)
	}
	// Discovery must also wake an idle session without another video frame.
	m.mirror.run.MouseButton(0, true, 1, 1)
	m.mirror.run.MouseButton(0, false, 1, 1)
	wait(func() bool { return m.window.State == rdpclient.StateConnected && m.mirror.run.display == 15 }, m.syncMirror)
	m.click(c, "display:0")
	wait(func() bool { return m.window.State == rdpclient.StateConnected && m.mirror.run.display == 0 }, m.syncMirror)
	m.click(c, "display:7")
	wait(func() bool { return m.window.State == rdpclient.StateConnected && m.mirror.run.display == 7 }, m.syncMirror)
	m.click(c, "screens-toggle")
	if !m.mirror.picker {
		t.Fatal("display picker did not open inside toolbar")
	}
	old := m.mirror.run
	m.click(c, "display:follow")
	if m.mirror.picker {
		t.Fatal("display picker remained open after selection")
	}
	wait(func() bool { return m.mirror.run != old && m.window.State == rdpclient.StateConnected }, m.syncMirror)
	if done, _ := old.native.State(); !done {
		t.Fatal("previous display still running")
	}
	if got := m.mirror.run.display; got != 15 {
		t.Fatalf("selected display %d, want 15", got)
	}
	if !slices.Equal(centered, []int{0, 15, 0, 7, 15}) {
		t.Fatalf("centered displays after rotations and switches: %v", centered)
	}
	if m.mirror.pinned {
		t.Fatal("returning to follow mode left the manual pin active")
	}
	m.click(c, "rdp-keys-toggle")
	if !m.toolbar.keys || m.mirror.picker {
		t.Fatal("Android button panel did not open")
	}
	for _, button := range []struct {
		id   string
		code uint32
	}{{"android-back", 4}, {"android-key:home", 3}, {"android-key:recents", 187},
		{"android-key:volume-down", 25}, {"android-key:volume-up", 24}, {"android-key:power", 26}} {
		m.click(c, button.id)
		for _, action := range []byte{0, 1} {
			select {
			case packet := <-keys:
				if packet[0] != 0 || packet[1] != action || binary.BigEndian.Uint32(packet[2:6]) != button.code {
					t.Fatalf("%s action %d: packet %x", button.id, action, packet)
				}
			case <-deadline:
				t.Fatalf("%s did not send action %d", button.id, action)
			}
		}
	}
	m.click(c, "display:15") // Pin the app, then close it from its title bar.
	if err := os.WriteFile(exited+".reject", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if nativeClose() {
		t.Fatal("closing an app screen dismissed the session window")
	}
	wait(func() bool { return m.mirror.exitResult == nil }, m.syncMirror)
	if err := c.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	if closed || m.mirror.display != 15 || !visible(m.node("toast")) {
		t.Fatal("failed task removal did not keep the app open and report the error")
	}
	if err := os.Remove(exited + ".reject"); err != nil {
		t.Fatal(err)
	}
	if nativeClose() {
		t.Fatal("retry closed the session window")
	}
	pending := m.mirror.exitResult
	if pending == nil || nativeClose() || m.mirror.exitResult != pending {
		t.Fatal("repeated close did not wait for the pending task removal")
	}
	// The device may end the app stream as soon as its task disappears.
	m.mirror.run.native.Close()
	m.mirror.run.native.Wait()
	wait(func() bool {
		return m.mirror.run != nil && m.mirror.run.display == 0 && m.window.State == rdpclient.StateConnected && !m.mirror.returningHome
	}, m.syncMirror)
	if _, err := os.Stat(exited); err != nil {
		t.Fatal("closing the app screen did not remove its task")
	}
	for _, action := range []byte{0, 1} {
		select {
		case packet := <-keys:
			if packet[0] != 0 || packet[1] != action || binary.BigEndian.Uint32(packet[2:6]) != 3 {
				t.Fatalf("unexpected Home packet on the built-in screen: %x", packet)
			}
		case <-deadline:
			t.Fatal("missing Home key on the built-in screen")
		}
	}
	if closed || m.mirror.pinned || !nativeClose() {
		t.Fatal("the session should stay open until the built-in screen is closed")
	}
	// A normally closed session must also close its session window.
	m.mirror.run.native.Close()
	wait(func() bool { return closed }, m.syncMirror)
	if err := childRuntime.Close(); err != nil {
		t.Fatal(err)
	}
	session.DidClose(nil)
	if len(launcher.sessionWindows) != 0 || !launcher.sessionLauncher() {
		t.Fatal("closing the session window did not return the launcher to zero sessions")
	}
}

func buildTestMirrorLibrary(t *testing.T, dir string) {
	t.Helper()
	source := filepath.Join(dir, "mirror.c")
	const fixture = `#include <stdint.h>
#include <stdlib.h>
#include <string.h>

uint32_t onrdp_scrcpy_abi(void) { return 2; }
void *onrdp_scrcpy_open(void) {return malloc(1);}
int32_t onrdp_scrcpy_decode(void *session, unsigned char *data, int32_t len,
                            unsigned char **pixels,int32_t *w,int32_t *h,int32_t *stride) {
    static unsigned char rgba[32];
    if(len != 1 || (data[0] != 42 && data[0] != 4 && data[0] != 2))return -1;
    *pixels=rgba;*w=data[0]==4?4:2;*h=data[0]==2?4:2;*stride=*w*4;
    return 2;
}
void onrdp_scrcpy_close(void *session) {free(session);}
`
	if err := os.WriteFile(source, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	suffix := ".so"
	if runtime.GOOS == "darwin" {
		suffix = ".dylib"
	}
	path := filepath.Join(dir, "libonrdp-scrcpy"+suffix)
	if output, err := exec.Command("cc", "-shared", "-fPIC", "-o", path, source).CombinedOutput(); err != nil {
		t.Fatalf("build scrcpy test library: %v\n%s", err, output)
	}
	t.Setenv("ONRDP_SCRCPY_LIBRARY", path)
}

func TestAndroidDisplayNavigation(t *testing.T) {
	home := scrcpy.Display{ID: 0, TasksKnown: true}
	first := scrcpy.Display{ID: 15, TasksKnown: true, App: "example.first", TaskID: 18}
	next := scrcpy.Display{ID: 21, TasksKnown: true, App: "example.next", TaskID: 23}
	empty := scrcpy.Display{ID: 21, TasksKnown: true}
	mirror := androidMirror{}
	if id := mirror.updateDisplays([]scrcpy.Display{home, first}); id != 0 {
		t.Fatal("pre-existing display stole focus")
	}
	mirror.display, mirror.history = 15, []int{0}
	if id := mirror.updateDisplays([]scrcpy.Display{home, first, empty}); id != 15 {
		t.Fatal("empty display stole focus")
	}
	if id := mirror.updateDisplays([]scrcpy.Display{home, first, next}); id != 21 {
		t.Fatal("did not follow the app opening on a new display")
	}
	mirror.display, mirror.history = 21, []int{0, 15}
	if id := mirror.updateDisplays([]scrcpy.Display{home, first, empty}); id != 15 {
		t.Fatal("did not return to the previous app after exit")
	}
	if id := mirror.updateDisplays([]scrcpy.Display{home}); id != 0 {
		t.Fatal("did not skip a removed previous display")
	}
}

func TestAndroidFollowsNewAppWithLauncherFocus(t *testing.T) {
	home := scrcpy.Display{ID: 0, Focused: true, TasksKnown: true}
	app := scrcpy.Display{ID: 15, TasksKnown: true}
	mirror := androidMirror{}
	scan := func(want int, displays ...scrcpy.Display) {
		t.Helper()
		if got := mirror.updateDisplays(displays); got != want {
			t.Fatalf("selected %d, want %d for %+v", got, want, displays)
		}
		mirror.display = want
	}
	scan(0, home)
	scan(0, home, app) // MuMu preallocates an empty display.
	app.App = "example.app"
	scan(15, home, app)
	home.Focused = false // One scan has no global focus information.
	scan(15, home, app)
	home.Focused = true
	scan(15, home, app)
	scan(0, home)
	app.ID = 18
	scan(18, home, app) // Display and task appear together.
}

func TestAndroidFollowsFocusAcrossExistingDisplays(t *testing.T) {
	home, app := scrcpy.Display{ID: 0}, scrcpy.Display{ID: 15, Focused: true}
	mirror := androidMirror{}
	scan := func(want int) {
		t.Helper()
		got := mirror.updateDisplays([]scrcpy.Display{home, app})
		if got != want {
			t.Fatalf("proposed display %d, want %d", got, want)
		}
		mirror.display = got
	}
	scan(15) // Already active at connection.
	home.Focused, app.Focused = true, false
	scan(0)
	mirror.pinned = true
	home.Focused, app.Focused = false, true
	scan(0)
	if id := mirror.updateDisplays([]scrcpy.Display{app}); id != 15 || mirror.pinned {
		t.Fatal("disappearing pinned screen did not recover to the active display")
	}
}
