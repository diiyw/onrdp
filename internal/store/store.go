// Package store persists the address book and credentials in an authenticated
// vault so a stolen file cannot be read or silently rewritten.
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Host is one saved desktop. Credentials live in the same encrypted blob as the
// rest of the record, never in a separate plaintext field.
type Host struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	User        string `json:"user"`
	Domain      string `json:"domain,omitempty"`
	Password    string `json:"password,omitempty"`
	Group       string `json:"group,omitempty"`
	Port        int    `json:"port"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Quality     string `json:"quality,omitempty"`
	Favorite    bool   `json:"favorite"`
	Clipboard   bool   `json:"clipboard"`
	ConsoleMode bool   `json:"consoleMode"`
	// DynamicSize is retained for compatibility; windows only scale locally.
	DynamicSize bool `json:"dynamicSize"`
}

// NewID returns a random identifier for a freshly created host.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Endpoint is the host:port pair a socket connects to.
func (h Host) Endpoint() string { return net.JoinHostPort(h.Address, strconv.Itoa(h.Port)) }

// Resolution reports the session size, falling back to the app default.
func (h Host) Resolution() (int, int) {
	width, height := h.Width, h.Height
	if width <= 0 || height <= 0 {
		width, height = 1600, 900
	}
	return width, height
}

// Validate reports the first field that cannot produce a connection.
func (h Host) Validate() error {
	if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Address) == "" || strings.TrimSpace(h.User) == "" {
		return ErrFieldMissing
	}
	if h.Port < 1 || h.Port > 65535 {
		return ErrPortRange
	}
	if strings.ContainsAny(h.Address, " /\t\r\n") || strings.ContainsAny(h.User, "\r\n") {
		return ErrFieldFormat
	}
	if h.Password == "" {
		return ErrPasswordMissing
	}
	return nil
}

var (
	ErrFieldMissing    = errors.New("请填写名称、地址和用户名")
	ErrPortRange       = errors.New("端口必须介于 1 和 65535 之间")
	ErrFieldFormat     = errors.New("地址或用户名格式不正确")
	ErrPasswordMissing = errors.New("请输入远程桌面密码")
	ErrVaultUnreadable = errors.New("无法识别凭据库")
	ErrVaultCorrupt    = errors.New("凭据库校验失败，未覆盖原文件")
)

// Settings are app-wide preferences, kept in the same vault as the hosts.
type Settings struct {
	Language  string `json:"language,omitempty"`
	Reconnect bool   `json:"reconnect"`
	Clipboard bool   `json:"clipboard"`
	// FitToWindow is retained for compatibility; the full frame always fits.
	FitToWindow bool `json:"fitToWindow"`
}

// Data is the decrypted vault payload.
type Data struct {
	Hosts    []Host   `json:"hosts"`
	Settings Settings `json:"settings"`
}

// Vault owns the decrypted data and serializes writes to the backing file.
type Vault struct {
	aead cipher.AEAD
	path string
	mu   sync.Mutex
	data Data
}

var header = []byte("ONRDP\x01")

// Open decrypts the vault in dir with key, creating it when absent. A vault that
// fails authentication is reported rather than overwritten.
func Open(dir string, key []byte) (*Vault, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	v := &Vault{path: filepath.Join(dir, "vault.bin"), aead: aead, data: defaultData()}
	raw, err := os.ReadFile(v.path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(raw) < len(header)+nonceSize || string(raw[:len(header)]) != string(header) {
		return nil, ErrVaultUnreadable
	}
	nonce := raw[len(header) : len(header)+nonceSize]
	plain, err := aead.Open(nil, nonce, raw[len(header)+nonceSize:], header)
	if err != nil {
		return nil, ErrVaultCorrupt
	}
	if err = json.Unmarshal(plain, &v.data); err != nil {
		return nil, err
	}
	return v, nil
}

func defaultData() Data {
	return Data{Settings: Settings{Reconnect: true, Clipboard: true, FitToWindow: true}}
}

// Snapshot returns a copy so callers can render without holding the lock.
func (v *Vault) Snapshot() Data {
	v.mu.Lock()
	defer v.mu.Unlock()
	return Data{Hosts: slices.Clone(v.data.Hosts), Settings: v.data.Settings}
}

// Settings returns the current preferences.
func (v *Vault) Settings() Settings {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.data.Settings
}

// SaveSettings persists preferences and returns once the file is durable.
func (v *Vault) SaveSettings(settings Settings) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.data.Settings = settings
	return v.writeLocked()
}

// SaveHost inserts or replaces one record, keyed by ID.
func (v *Vault) SaveHost(host Host) error {
	if err := host.Validate(); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	index := slices.IndexFunc(v.data.Hosts, func(h Host) bool { return h.ID == host.ID })
	if index >= 0 {
		v.data.Hosts[index] = host
	} else {
		v.data.Hosts = append(v.data.Hosts, host)
	}
	return v.writeLocked()
}

// DeleteHost removes one record and reports whether it existed.
func (v *Vault) DeleteHost(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	index := slices.IndexFunc(v.data.Hosts, func(h Host) bool { return h.ID == id })
	if index < 0 {
		return nil
	}
	v.data.Hosts = slices.Delete(v.data.Hosts, index, index+1)
	return v.writeLocked()
}

// writeLocked encrypts and atomically replaces the vault file. A temporary file
// keeps a crash mid-write from destroying the previous contents.
func (v *Vault) writeLocked() error {
	plain, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := v.aead.Seal(nil, nonce, plain, header)
	payload := make([]byte, 0, len(header)+len(nonce)+len(sealed))
	payload = append(payload, header...)
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)

	temp := v.path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(payload); err != nil {
		file.Close()
		os.Remove(temp)
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		os.Remove(temp)
		return err
	}
	if err = file.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	return os.Rename(temp, v.path)
}
