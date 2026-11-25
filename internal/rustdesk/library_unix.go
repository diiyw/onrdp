//go:build (linux && !android && (amd64 || arm64)) || (darwin && !ios)

package rustdesk

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

func libraryPaths() []string {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" {
		return []string{filepath.Join(dir, "../Frameworks/liblibrustdesk.dylib")}
	}
	return []string{filepath.Join(dir, "lib", "librustdesk.so"), "/usr/share/rustdesk/lib/librustdesk.so"}
}

func loadLibrary(path string) (func(string) (uintptr, error), error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) { return purego.Dlsym(handle, name) }, nil
}
