package main

import (
	"io/fs"
	"sync"

	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
	"onrdp/internal/platform"
)

// Main forwards platform input into the UI module. Everything else is owned by
// the module itself, so the scene behavior stays a pure router.
type Main struct{}

func (*Main) OnMessage(c *module.Context, m module.Message) error {
	if m.Topic == module.InputTopic {
		return c.Forward("app", m)
	}
	return nil
}

func start() error {
	plan, source, err := project.OpenModuleProject()
	if err != nil {
		return err
	}
	return runProject(*plan, source)
}

// Each window gets its own world, input handlers, module state and GPU resources.
func newProjectWindow(plan project.ProjectPlan, source fs.FS, window *platform.DesktopWindow) (*onix.App, error) {
	app := onix.NewApp()
	app.SetRenderMode(onix.RenderOnDemand)
	registry, err := OnixRegistry()
	if err != nil {
		return nil, err
	}
	if err = OnixInstallSystems(app); err != nil {
		return nil, err
	}
	if err = openNativeWindow(app, plan.Spec, source); err != nil {
		app.Destroy()
		return nil, err
	}
	options := OnixModuleOptions(source)
	options.Host = desktopHost(app, plan.Spec)
	if window != nil {
		unbind := window.Bind(options.Host)
		defer unbind()
	}
	if _, err = app.LoadModules[OnixContentProfile](&plan, registry, options); err != nil {
		app.Destroy()
		return nil, err
	}
	return app, nil
}

// Onix native backends report window/device creation failures by panicking.
func openNativeWindow(app *onix.App, spec project.ProjectSpec, source fs.FS) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			var ok bool
			if err, ok = failure.(error); !ok {
				panic(failure)
			}
		}
	}()
	return OnixOpenWindow(app, spec, source)
}

// A mobile host calls OnixEntryStart once and then owns the render loop.
var mobileStart sync.Once
var mobileError error

// OnixEntryStart starts the app for a native mobile host.
func OnixEntryStart(_, _ int) error {
	mobileStart.Do(func() { mobileError = start() })
	return mobileError
}
