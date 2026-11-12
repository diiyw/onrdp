// Package platform bridges app lifecycle and the OS-protected vault key.
package platform

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Hooks are the callbacks a native host installs before the app opens storage.
type Hooks struct {
	Language func() string
	Active   func() int
	Resume   func()
	Close    func()
}

var state struct {
	hooks  Hooks
	dir    string
	key    []byte
	locale string
	sync.Mutex
}

// Register installs host hooks; later calls replace earlier ones.
func Register(h Hooks) { state.Lock(); state.hooks = h; state.Unlock() }

// Active reports the number of live sessions that must keep the app awake.
func Active() int {
	state.Lock()
	f := state.hooks.Active
	state.Unlock()
	if f != nil {
		return f()
	}
	return 0
}

// Resume lets the host re-arm work after the app was backgrounded.
func Resume() {
	state.Lock()
	f := state.hooks.Resume
	state.Unlock()
	if f != nil {
		f()
	}
}

// Close asks the host to release sessions before the process exits.
func Close() {
	state.Lock()
	f := state.hooks.Close
	state.Unlock()
	if f != nil {
		f()
	}
}

// Configure stores a host-provided vault directory and key.
func Configure(dir string, key []byte) {
	state.Lock()
	state.dir = dir
	state.key = append([]byte(nil), key...)
	state.Unlock()
}

// SetLocale records the locale a native host reported.
func SetLocale(locale string) { state.Lock(); state.locale = locale; state.Unlock() }

// Storage returns the vault directory and its 32-byte key. Desktop keeps the
// key in a private file; mobile hosts supply one from the platform keystore.
func Storage() (string, []byte, error) {
	state.Lock()
	dir, key := state.dir, state.key
	state.Unlock()
	if dir != "" && len(key) == 32 {
		return dir, append([]byte(nil), key...), nil
	}
	return desktopStorage()
}

func desktopStorage() (string, []byte, error) {
	dir := os.Getenv("ONRDP_DATA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", nil, err
		}
		dir = filepath.Join(base, "onrdp")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return "", nil, err
		}
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return "", nil, createErr
		}
		_, writeErr := file.Write(key)
		closeErr := file.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			return "", nil, err
		}
	case err != nil:
		return "", nil, err
	case len(key) != 32:
		return "", nil, errors.New("无效的本地加密密钥")
	}
	return dir, key, nil
}

// SystemLocale returns the OS locale for message-catalog selection.
func SystemLocale() string {
	state.Lock()
	locale := state.locale
	state.Unlock()
	if locale != "" {
		return locale
	}
	for _, name := range []string{"ONRDP_LOCALE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	if locale := osLocale(); locale != "" {
		return locale
	}
	return "en"
}
