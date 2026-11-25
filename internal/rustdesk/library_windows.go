//go:build windows && (amd64 || arm64)

package rustdesk

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func libraryPaths() []string {
	exe, _ := os.Executable()
	return []string{filepath.Join(filepath.Dir(exe), "librustdesk.dll")}
}

func loadLibrary(path string) (func(string) (uintptr, error), error) {
	handle, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	if err != nil {
		return nil, err
	}
	return func(name string) (uintptr, error) { return windows.GetProcAddress(handle, name) }, nil
}
