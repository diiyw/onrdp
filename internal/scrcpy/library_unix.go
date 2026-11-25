//go:build ((linux && !android) || (darwin && !ios)) && (amd64 || arm64)

package scrcpy

import "github.com/ebitengine/purego"

func openLibrary(path string) (func(string) (uintptr, error), func(), error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, nil, err
	}
	return func(name string) (uintptr, error) {
		return purego.Dlsym(handle, name)
	}, func() { _ = purego.Dlclose(handle) }, nil
}
