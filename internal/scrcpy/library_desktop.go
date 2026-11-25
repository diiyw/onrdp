//go:build ((linux && !android) || (darwin && !ios) || windows) && (amd64 || arm64)

package scrcpy

import (
	"fmt"

	"github.com/ebitengine/purego"

	"onrdp/internal/i18n"
)

func (t Tools) load() (*library, error) {
	symbol, unload, err := openLibrary(t.library)
	if err != nil {
		return nil, i18n.NewError("无法加载 scrcpy 动态库：%s", err.Error())
	}

	core := &library{unload: unload}
	for _, binding := range []struct {
		name string
		fn   any
	}{
		{"abi", &core.abi},
		{"open", &core.open},
		{"decode", &core.decode},
		{"close", &core.close},
	} {
		name := "onrdp_scrcpy_" + binding.name
		address, err := symbol(name)
		if err != nil {
			unload()
			return nil, i18n.NewError("无法加载 scrcpy 动态库：%s", fmt.Sprintf("%s: %v", name, err))
		}
		purego.RegisterFunc(binding.fn, address)
	}
	const abi = 2
	if got := core.abi(); got != abi {
		unload()
		return nil, i18n.NewError("无法加载 scrcpy 动态库：%s", fmt.Sprintf("ABI %d (expected %d)", got, abi))
	}

	return core, nil
}
