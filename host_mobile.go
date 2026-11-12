//go:build android || ios

package main

import (
	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
)

// Mobile supplies rendering, lifecycle and clipboard through the shared host.
func desktopHost(app *onix.App, spec project.ProjectSpec) *module.Host {
	return &module.Host{
		Context:       app.Context,
		RequestRender: app.RequestRender,
		WakeAt:        app.WakeAt,
		GetViewport: func() (int, int) {
			if app.Context == nil {
				return spec.Viewport.Width, spec.Viewport.Height
			}
			return app.GetViewport()
		},
	}
}
