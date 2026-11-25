package app

import (
	"context"
	"path/filepath"
	"strings"

	"onrdp/internal/i18n"
)

func (m *Mod) queueAndroidAPK(path string) {
	if m.mirror != nil && strings.EqualFold(filepath.Ext(path), ".apk") {
		m.mirror.apkQueue = append(m.mirror.apkQueue, path)
		m.host.RequestRender()
	}
}

func (m *Mod) syncAndroidAPKs() {
	mirror := m.mirror
	select {
	case message := <-mirror.apkResult:
		mirror.apkResult, mirror.cancelAPK = nil, nil
		m.showToast(message)
	default:
	}
	if mirror.starting || mirror.apkResult != nil || len(mirror.apkQueue) == 0 {
		return
	}
	paths := mirror.apkQueue
	mirror.apkQueue = nil
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan string, 1)
	mirror.apkResult, mirror.cancelAPK = result, cancel
	language, wake := m.language, m.host.RequestRender
	m.showToast(m.tr("正在安装 %s…", filepath.Base(paths[0])))
	mirror.scans.Go(func() {
		defer cancel()
		var messages []string
		for _, path := range paths {
			if ctx.Err() != nil {
				return
			}
			name := filepath.Base(path)
			if err := mirror.tools.InstallAPK(ctx, mirror.host.Address, path); err != nil {
				messages = append(messages, i18n.Text(language, "安装 %s 失败：%s", name, i18n.ErrorText(language, err)))
			} else {
				messages = append(messages, i18n.Text(language, "已安装 %s", name))
			}
		}
		result <- strings.Join(messages, "\n")
		wake()
	})
}
