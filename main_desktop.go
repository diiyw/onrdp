//go:build !android && !ios && !onix_drm

package main

import (
	"image"
	"io/fs"
	"math"

	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/window"
	"onrdp/internal/platform"
	"onrdp/internal/store"
)

func runProject(plan project.ProjectPlan, source fs.FS) error {
	var application onix.Application
	var pending []*platform.DesktopWindow
	launcher := &platform.DesktopWindow{OpenSession: func(w *platform.DesktopWindow) {
		pending = append(pending, w)
	}}
	add := func(w *platform.DesktopWindow) error {
		windowPlan := plan
		configureWindow(&windowPlan.Spec, w)
		app, err := newProjectWindow(windowPlan, source, w)
		if err != nil {
			return err
		}
		if w.Host.Protocol == store.Android {
			w.FitFrame = androidWindowFitter(app.Context)
		} else if w.Host.ID != "" {
			w.Center = func() { placeWindow(app.Context, 0, 0, true) }
		}
		if err := application.Add(app, func() { w.DidClose(nil) }); err != nil {
			app.Destroy()
			return err
		}
		if w.Host.ID != "" {
			w.Activate = func() { activateWindow(app.Context) }
		}
		return nil
	}
	if err := add(launcher); err != nil {
		return err
	}
	// Opening a window switches the current render device. Finish the input
	// and rendering of all existing windows before mounting the next one.
	application.BetweenFrames = func() error {
		for _, w := range pending {
			if w.Closing {
				w.DidClose(nil)
			} else if err := add(w); err != nil {
				w.DidClose(err)
			}
		}
		clear(pending)
		pending = pending[:0]
		return nil
	}
	return application.Run()
}

func configureWindow(spec *project.ProjectSpec, w *platform.DesktopWindow) {
	if w.Host.ID == "" || w.Host.Protocol == store.Android {
		// Keep toolbar controls at their logical size in narrow portrait windows.
		spec.Viewport.Mode = window.ViewportModeAdaptive
		spec.Viewport.Width, spec.Viewport.Height = 1, 1
		return
	}
	spec.Viewport.Mode = window.ViewportModeExpand
}

func androidWindowFitter(ctx *window.Context) func(int, int, bool) bool {
	var bounds, last image.Point
	return func(width, height int, center bool) bool {
		w, h := ctx.GetViewport()
		current := image.Pt(w, h)
		limit := bounds
		if current != last || limit.X == 0 {
			// Adopt manual resizes, but retain the same bounding box across
			// automatic landscape/portrait switches to avoid progressive shrinkage.
			limit = current
		}
		scale := min(float64(limit.X)/float64(width), float64(limit.Y)/float64(height))
		size := image.Pt(max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale))))
		if !placeWindow(ctx, size.X, size.Y, center) {
			return false
		}
		bounds, last = limit, size
		return true
	}
}
