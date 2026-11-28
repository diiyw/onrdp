//go:build linux && !android && (amd64 || arm64)

package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/onix-engine/onix/module"
	"onrdp/internal/platform"
	"onrdp/internal/rdpclient"
	"onrdp/internal/rustdesk"
	"onrdp/internal/store"
)

type clipboardConfigClient struct {
	remoteClient
	config rdpclient.Config
}

func (c *clipboardConfigClient) Connect(config rdpclient.Config) { c.config = config }

func TestRustDeskUsesGlobalClipboardSetting(t *testing.T) {
	_, m, _, _ := mountRuntimeWithVault(t, 1024, 700, &module.Host{}, t.TempDir())
	client := &clipboardConfigClient{remoteClient: m.client}
	m.client = client
	// Existing RustDesk hosts were saved without a per-host clipboard setting.
	host := store.Host{Protocol: store.RustDesk, Address: "Office_PC-01"}
	m.connect(host)
	if !client.config.Clipboard {
		t.Fatal("existing RustDesk host did not inherit the enabled clipboard setting")
	}

	settings := m.vault.Settings()
	settings.Clipboard = false
	if err := m.vault.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	m.connect(host)
	if client.config.Clipboard {
		t.Fatal("RustDesk clipboard remained enabled after turning off the global setting")
	}
}

func TestRustDeskHostAndSession(t *testing.T) {
	target := os.Getenv("ONRDP_RUSTDESK_TEST_PEER")
	connectedTest := target != "" && os.Getenv("ONRDP_RUSTDESK_LIB") != ""
	if !connectedTest {
		target = "Office_PC-01"
		t.Setenv("ONRDP_RUSTDESK_LIB", t.TempDir()+"/missing.so")
	}
	password := os.Getenv("ONRDP_RUSTDESK_TEST_PASSWORD")
	network := rustdesk.Network{
		IDServer:    os.Getenv("ONRDP_RUSTDESK_TEST_ID_SERVER"),
		RelayServer: os.Getenv("ONRDP_RUSTDESK_TEST_RELAY_SERVER"),
		Key:         os.Getenv("ONRDP_RUSTDESK_TEST_KEY"),
		ForceRelay:  os.Getenv("ONRDP_RUSTDESK_TEST_FORCE_RELAY") == "1",
	}
	if !connectedTest {
		password = "test-password"
		network = rustdesk.Network{IDServer: "id.example:21116", RelayServer: "relay.example:21117", Key: "a+/b=", ForceRelay: true}
	}

	var session *platform.DesktopWindow
	launcher := &platform.DesktopWindow{OpenSession: func(w *platform.DesktopWindow) { session = w }}
	var clipboard string
	host := &module.Host{RequestRender: func() {}, SetClipboardText: func(text string) bool {
		clipboard = text
		return true
	}}
	unbind := launcher.Bind(host)
	defer unbind()
	vault := t.TempDir()
	c, m, _, _ := mountRuntimeWithVault(t, 1024, 700, host, vault)
	m.click(c, "add-host")
	m.click(c, "quality-smooth")
	m.click(c, "protocol-rustdesk")
	if _, hidden := attribute(c, "rdp-credentials", "hidden"); !hidden {
		t.Fatal("RDP fields are visible for a RustDesk host")
	}
	if _, hidden := attribute(c, "rustdesk-fields", "hidden"); hidden {
		t.Fatal("RustDesk fields are hidden")
	}
	if pressed, _ := attribute(c, "quality-balanced", "aria-pressed"); pressed != "true" {
		t.Fatal("RustDesk did not start with the balanced preset")
	}
	if _, hidden := attribute(c, "rustdesk-custom-quality", "hidden"); !hidden {
		t.Fatal("custom settings are visible for a RustDesk preset")
	}
	m.click(c, "quality-custom")
	if _, hidden := attribute(c, "rustdesk-custom-quality", "hidden"); hidden {
		t.Fatal("RustDesk custom quality fields are hidden")
	}
	m.click(c, "protocol-rdp")
	if pressed, _ := attribute(c, "quality-smooth", "aria-pressed"); pressed != "true" {
		t.Fatal("switching protocol did not restore the RDP preset")
	}
	if _, hidden := attribute(c, "quality-custom", "hidden"); !hidden {
		t.Fatal("RustDesk custom quality is visible for RDP")
	}
	m.click(c, "protocol-rustdesk")
	if err := c.Content().SyncHTML(); err != nil {
		t.Fatal(err)
	}
	toggle, toggleOK := m.node("rustdesk-force-relay").Bounds()
	label, labelOK := m.node("rustdesk-force-relay-label").Bounds()
	if !toggleOK || !labelOK || label.Width <= 0 || label.X+label.Width > toggle.X+toggle.Width {
		t.Fatalf("relay checkbox clips its label: checkbox=%+v label=%+v", toggle, label)
	}
	m.input("field-name", "Workshop")
	m.input("field-password", "rdp password")
	m.input("field-rustdesk-id", target)
	m.input("field-rustdesk-password", password)
	m.input("field-rustdesk-id-server", network.IDServer)
	m.input("field-rustdesk-relay-server", network.RelayServer)
	m.input("field-rustdesk-key", network.Key)
	if network.ForceRelay {
		m.click(c, "rustdesk-force-relay")
	}
	if network.IDServer != "" {
		m.click(c, "rustdesk-copy-config")
		var exported map[string]string
		if err := json.Unmarshal([]byte(clipboard), &exported); err != nil || len(exported) != 3 ||
			exported["host"] != network.IDServer || exported["relay"] != network.RelayServer || exported["key"] != network.Key {
			t.Fatal("unexpected server configuration for the remote peer")
		}
	}
	m.input("field-rustdesk-image-quality", "70")
	m.input("field-rustdesk-fps", "121")
	m.click(c, "edit-save")
	if m.page != "edit" || len(m.vault.Snapshot().Hosts) != 0 {
		t.Fatal("out-of-range RustDesk frame rate was saved")
	}
	m.input("field-rustdesk-fps", "25")
	m.click(c, "edit-save")
	records := m.vault.Snapshot().Hosts
	if len(records) != 1 {
		t.Fatal("RustDesk host was not saved")
	}
	record := records[0]
	if record.Protocol != store.RustDesk || record.Address != target || record.Password != password || record.Port != 0 || record.RustDeskNetwork != network {
		t.Fatal("saved RustDesk host has unexpected connection settings")
	}
	quality := rustdesk.VideoQuality{Mode: rustdesk.QualityCustom, ImageQuality: 70, FPS: 25}
	if record.RustDeskQuality != quality {
		t.Fatalf("saved RustDesk quality = %+v, want %+v", record.RustDeskQuality, quality)
	}
	m.click(c, "edit:"+record.ID)
	if m.rustDeskNetwork() != network {
		t.Fatal("reopened editor did not restore the server settings")
	}
	if pressed, _ := attribute(c, "quality-custom", "aria-pressed"); pressed != "true" ||
		m.fields["rustdesk-image-quality"] != "70" || m.fields["rustdesk-fps"] != "25" {
		t.Fatal("reopened editor did not restore the custom RustDesk quality")
	}
	m.click(c, "edit-cancel")
	m.click(c, "connect:"+record.ID)
	if session == nil || session.Host.Protocol != store.RustDesk || session.Host.Password != "" || session.Host.RustDeskQuality != quality {
		t.Fatal("RustDesk did not request an embedded session window")
	}
	wake := make(chan struct{}, 1)
	childHost := &module.Host{SetWindowTitle: func(string) {}, RequestClose: func() {}, RequestRender: func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}}
	unbindChild := session.Bind(childHost)
	defer unbindChild()
	childContext, child, _, _ := mountRuntimeWithVault(t, 1024, 700, childHost, vault)
	if child.page != "rdp" {
		t.Fatal("session did not mount the remote desktop surface")
	}
	timeout := time.After(10 * time.Second)
	reconnected := false
	for {
		if err := child.Update(childContext, module.Tick{}); err != nil {
			t.Fatal(err)
		}
		if connectedTest && child.status.State == rdpclient.StateConnected {
			if child.frame == nil || child.frame.width == 0 || child.frame.height == 0 {
				t.Fatal("the session received no pixels")
			}
			if !reconnected {
				reconnected = true
				child.click(childContext, "rdp-reconnect")
				continue
			}
			child.disconnect()
			return
		}
		if child.status.State == rdpclient.StateClosed {
			if connectedTest {
				t.Fatalf("RustDesk session failed: %v", child.status.Error)
			}
			if child.status.Error == nil {
				t.Fatal("missing RustDesk core did not surface as an error")
			}
			return
		}
		select {
		case <-wake:
		case <-timeout:
			t.Fatalf("RustDesk session timed out: %+v", child.status)
		}
	}
}
