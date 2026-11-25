//go:build !(((linux && !android) || (darwin && !ios) || windows) && (amd64 || arm64))

package scrcpy

import "onrdp/internal/i18n"

func (Tools) load() (*library, error) {
	return nil, i18n.NewError("当前平台不支持 Android 投屏")
}
