//go:build android || ios || onix_drm

package main

import (
	"io/fs"

	"github.com/onix-engine/onix/project"
)

func runProject(plan project.ProjectPlan, source fs.FS) error {
	app, err := newProjectWindow(plan, source, nil)
	if err != nil {
		return err
	}
	return app.Run()
}
