package main

import (
	"sync"

	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/module"
	"github.com/onix-engine/onix/project"
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

// start loads the same project on mobile and desktop. Native host capabilities
// and desktop-only window/input setup are installed by startDesktop.
func start() error {
	plan, source, err := project.OpenModuleProject()
	if err != nil {
		return err
	}
	app := onix.NewApp()
	app.SetRenderMode(onix.RenderOnDemand)
	registry, err := OnixRegistry()
	if err != nil {
		return err
	}
	if err = OnixInstallSystems(app); err != nil {
		return err
	}
	if err = startDesktop(app); err != nil {
		return err
	}
	if err = OnixOpenWindow(app, plan.Spec, source); err != nil {
		return err
	}
	options := OnixModuleOptions(source)
	options.Host = desktopHost(app, plan.Spec)
	if _, err = app.LoadModules[OnixContentProfile](plan, registry, options); err != nil {
		return err
	}
	return app.Run()
}

// A mobile host calls OnixEntryStart once and then owns the render loop.
var mobileStart sync.Once
var mobileError error

// OnixEntryStart starts the app for a native mobile host.
func OnixEntryStart(_, _ int) error {
	mobileStart.Do(func() { mobileError = start() })
	return mobileError
}
