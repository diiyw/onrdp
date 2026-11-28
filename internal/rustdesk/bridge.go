//go:build (linux && !android && (amd64 || arm64)) || (darwin && !ios) || (windows && (amd64 || arm64))

package rustdesk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const coreVersion = "1.5.3"

type wireBytes struct {
	data *byte
	len  int32
}

// dartObject holds the Dart_CObject layout used by flutter_rust_bridge 1.80.
// The Rust library owns every object until its callback returns.
type dartObject struct {
	kind     int32
	_        int32
	data     unsafe.Pointer
	elements **dartObject
	_        [16]byte
}

func (d *dartObject) array() []*dartObject {
	if d == nil || d.kind != 6 {
		return nil
	}
	n := *(*int)(unsafe.Pointer(&d.data))
	if n < 0 || n > 16 || (n > 0 && d.elements == nil) {
		return nil
	}
	return unsafe.Slice(d.elements, n)
}

func (d *dartObject) integer() int {
	switch d.kind {
	case 2:
		return int(*(*int32)(unsafe.Pointer(&d.data)))
	case 3:
		return *(*int)(unsafe.Pointer(&d.data))
	}
	return -1
}

func (d *dartObject) text() string {
	if d.kind != 5 || d.data == nil {
		return ""
	}
	for n := 0; n < 1<<20; n++ {
		if *(*byte)(unsafe.Add(d.data, n)) == 0 {
			return strings.Clone(unsafe.String((*byte)(d.data), n))
		}
	}
	return ""
}

func (d *dartObject) boolean() bool {
	return d.kind == 1 && *(*bool)(unsafe.Pointer(&d.data))
}

type core struct {
	alloc            func(int32) *wireBytes
	free             func(*dartObject)
	post             func(uintptr)
	version          func(int64)
	init             func(int64, *wireBytes, *wireBytes)
	option           func(int64, *wireBytes, *wireBytes)
	add              func(*wireBytes, *wireBytes, bool, bool, bool, bool, bool, *wireBytes, bool, *wireBytes, bool, *wireBytes, *wireBytes) *dartObject
	start            func(int64, *wireBytes, *wireBytes)
	close            func(int64, *wireBytes)
	toggle           func(int64, *wireBytes, *wireBytes)
	getToggle        func(*wireBytes, *wireBytes) *dartObject
	setQuality       func(int64, *wireBytes, *wireBytes)
	setCustomQuality func(int64, *wireBytes, int32)
	setCustomFPS     func(int64, *wireBytes, int32)
	getSize          func(*wireBytes, uintptr) *dartObject
	rgba             func(string, uintptr) *byte
	next             func(*wireBytes, uintptr) *dartObject
	mouse            func(int64, *wireBytes, *wireBytes)
	key              func(int64, *wireBytes, *wireBytes, bool, bool, bool, bool, bool, bool)
	mappedKey        func(int64, *wireBytes, *wireBytes, int32, int32, bool)
	supportsKeyMode  func(*wireBytes, *wireBytes) *dartObject
	setKeyMode       func(int64, *wireBytes, *wireBytes)
	text             func(int64, *wireBytes, *wireBytes)
	ctrlAltDel       func(int64, *wireBytes)
}

var coreState struct {
	sync.Mutex
	value *core
}

var ports sync.Map
var nextPort atomic.Int64
var postObject = purego.NewCallback(func(port int64, object *dartObject) uintptr {
	if handler, ok := ports.Load(port); ok {
		handler.(func(*dartObject))(object)
		return 1
	}
	return 0
})

func registerPort(handler func(*dartObject)) (int64, func()) {
	port := nextPort.Add(1)
	ports.Store(port, handler)
	return port, func() { ports.Delete(port) }
}

// call waits for a reply to an asynchronous bridge operation. The callback
// copies just the fields it needs so that no borrowed pointer escapes Rust.
func call(ctx context.Context, fn func(int64)) (string, error) {
	type reply struct {
		action int
		text   string
	}
	result := make(chan reply, 1)
	port, unregister := registerPort(func(object *dartObject) {
		a := object.array()
		if len(a) == 0 {
			return
		}
		r := reply{action: a[0].integer()}
		if len(a) > 1 {
			r.text = a[len(a)-1].text()
		}
		select {
		case result <- r:
		default:
		}
	})
	defer unregister()
	fn(port)
	select {
	case r := <-result:
		if r.action != 0 {
			return "", fmt.Errorf("RustDesk: %s", r.text)
		}
		return r.text, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *core) bytes(value []byte) *wireBytes {
	b := c.alloc(int32(len(value)))
	copy(unsafe.Slice(b.data, len(value)), value)
	return b
}

func (c *core) str(value string) *wireBytes { return c.bytes([]byte(value)) }

func (c *core) syncValue(result *dartObject, consume func(*dartObject)) {
	if result == nil {
		return
	}
	defer c.free(result)
	if a := result.array(); len(a) == 2 && a[1].boolean() {
		consume(a[0])
	}
}

func loadCore() (*core, error) {
	coreState.Lock()
	defer coreState.Unlock()
	if coreState.value != nil {
		return coreState.value, nil
	}
	path, err := corePath()
	if err != nil {
		return nil, err
	}
	symbol, err := loadLibrary(path)
	if err != nil {
		return nil, err
	}
	c := &core{}
	for name, target := range map[string]any{
		"new_uint_8_list_0": &c.alloc, "free_WireSyncReturn": &c.free,
		"store_dart_post_cobject": &c.post, "wire_main_get_version": &c.version,
		"wire_main_init": &c.init, "wire_main_set_local_option": &c.option,
		"wire_session_add_sync": &c.add, "wire_session_start": &c.start,
		"wire_session_close": &c.close, "wire_session_get_rgba_size": &c.getSize,
		"session_get_rgba": &c.rgba, "wire_session_next_rgba": &c.next,
		"wire_session_send_mouse": &c.mouse, "wire_session_input_key": &c.key,
		"wire_session_handle_flutter_key_event":   &c.mappedKey,
		"wire_session_is_keyboard_mode_supported": &c.supportsKeyMode,
		"wire_session_set_keyboard_mode":          &c.setKeyMode,
		"wire_session_input_string":               &c.text, "wire_session_ctrl_alt_del": &c.ctrlAltDel,
		"wire_session_toggle_option": &c.toggle, "wire_session_get_toggle_option_sync": &c.getToggle,
		"wire_session_set_image_quality":        &c.setQuality,
		"wire_session_set_custom_image_quality": &c.setCustomQuality, "wire_session_set_custom_fps": &c.setCustomFPS,
	} {
		addr, err := symbol(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		purego.RegisterFunc(target, addr)
	}
	// The core starts process-wide Rust workers. Keep its image loaded for the
	// lifetime of the process, including if version negotiation fails.
	c.post(postObject)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := call(ctx, c.version)
	if err != nil {
		return nil, err
	}
	if version != coreVersion {
		return nil, fmt.Errorf("RustDesk %s, expected %s", version, coreVersion)
	}
	if _, err := call(ctx, func(port int64) { c.init(port, c.str(""), c.str("")) }); err != nil {
		return nil, err
	}
	if _, err := call(ctx, func(port int64) {
		c.option(port, c.str("use-texture-render"), c.str("N"))
	}); err != nil {
		return nil, err
	}
	coreState.value = c
	return c, nil
}

func corePath() (string, error) {
	if path := os.Getenv("ONRDP_RUSTDESK_LIB"); path != "" {
		return filepath.Abs(path)
	}
	for _, path := range libraryPaths() {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}
