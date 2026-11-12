package store

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func openTemp(t *testing.T, dir string, key []byte) *Vault {
	t.Helper()
	vault, err := Open(dir, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return vault
}

func TestHostsSurviveReopen(t *testing.T) {
	dir, key := t.TempDir(), newKey(t)
	vault := openTemp(t, dir, key)
	host := Host{ID: NewID(), Name: "Office", Address: "10.0.0.5", User: "alice", Password: "secret", Port: 3389, Width: 1920, Height: 1080, Quality: "smooth"}
	if err := vault.SaveHost(host); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened := openTemp(t, dir, key)
	hosts := reopened.Snapshot().Hosts
	if len(hosts) != 1 || hosts[0] != host {
		t.Fatalf("reopened hosts = %+v, want %+v", hosts, host)
	}
}

// The vault file must not contain the password in the clear.
func TestVaultFileIsEncrypted(t *testing.T) {
	dir, key := t.TempDir(), newKey(t)
	vault := openTemp(t, dir, key)
	if err := vault.SaveHost(Host{ID: NewID(), Name: "n", Address: "a", User: "u", Password: "hunter2", Port: 3389}); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "vault.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("vault file is empty")
	}
	for _, needle := range []string{"hunter2", "alice", "Office"} {
		if contains(raw, needle) {
			t.Fatalf("vault file leaks %q in plaintext", needle)
		}
	}
}

func contains(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// A wrong key must be reported instead of silently replacing the file.
func TestWrongKeyDoesNotOverwrite(t *testing.T) {
	dir, key := t.TempDir(), newKey(t)
	vault := openTemp(t, dir, key)
	if err := vault.SaveHost(Host{ID: NewID(), Name: "n", Address: "a", User: "u", Password: "p", Port: 3389}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(dir, "vault.bin")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, newKey(t)); err == nil {
		t.Fatal("opening with a wrong key succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a failed open modified the vault")
	}
}

func TestDeleteHost(t *testing.T) {
	dir, key := t.TempDir(), newKey(t)
	vault := openTemp(t, dir, key)
	keep := Host{ID: NewID(), Name: "keep", Address: "a", User: "u", Password: "p", Port: 3389}
	drop := Host{ID: NewID(), Name: "drop", Address: "b", User: "u", Password: "p", Port: 3389}
	for _, host := range []Host{keep, drop} {
		if err := vault.SaveHost(host); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	if err := vault.DeleteHost(drop.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hosts := vault.Snapshot().Hosts
	if len(hosts) != 1 || hosts[0].ID != keep.ID {
		t.Fatalf("hosts after delete = %+v", hosts)
	}
	// Deleting an unknown id is a no-op rather than an error.
	if err := vault.DeleteHost("missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSaveHostRejectsIncompleteRecords(t *testing.T) {
	vault := openTemp(t, t.TempDir(), newKey(t))
	for _, host := range []Host{
		{Name: "", Address: "a", User: "u", Port: 3389, Password: "p"},
		{Name: "n", Address: " ", User: "u", Port: 3389, Password: "p"},
		{Name: "n", Address: "a", User: "u", Port: 0, Password: "p"},
		{Name: "n", Address: "a", User: "u", Port: 3389},
	} {
		if err := vault.SaveHost(host); err == nil {
			t.Fatalf("accepted incomplete host %+v", host)
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir, key := t.TempDir(), newKey(t)
	vault := openTemp(t, dir, key)
	settings := vault.Settings()
	if !settings.Reconnect || !settings.Clipboard || !settings.FitToWindow {
		t.Fatalf("default settings = %+v", settings)
	}
	settings.Language = "ja"
	settings.Clipboard = false
	if err := vault.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if got := openTemp(t, dir, key).Settings(); got != settings {
		t.Fatalf("settings = %+v, want %+v", got, settings)
	}
}

// Snapshot must hand out a copy: callers render from it after releasing the lock.
func TestSnapshotIsIndependent(t *testing.T) {
	vault := openTemp(t, t.TempDir(), newKey(t))
	if err := vault.SaveHost(Host{ID: NewID(), Name: "n", Address: "a", User: "u", Password: "p", Port: 3389}); err != nil {
		t.Fatal(err)
	}
	snapshot := vault.Snapshot()
	snapshot.Hosts[0].Name = "mutated"
	if vault.Snapshot().Hosts[0].Name != "n" {
		t.Fatal("Snapshot shared host storage with the vault")
	}
}

func TestResolutionFallsBackToDefault(t *testing.T) {
	if width, height := (Host{}).Resolution(); width != 1600 || height != 900 {
		t.Fatalf("default resolution = %d×%d", width, height)
	}
	if width, height := (Host{Width: 1024, Height: 768}).Resolution(); width != 1024 || height != 768 {
		t.Fatalf("saved resolution = %d×%d", width, height)
	}
}

func TestEndpointIncludesPort(t *testing.T) {
	host := Host{Address: "example.com", Port: 3390}
	if got := host.Endpoint(); got != "example.com:3390" {
		t.Fatalf("Endpoint() = %q", got)
	}
}
