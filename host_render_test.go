//go:build !android && !ios

package main

import (
	"image"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/onix-engine/onix"
	"github.com/onix-engine/onix/graphics"
	"github.com/onix-engine/onix/project"
	"github.com/onix-engine/onix/render"
	"github.com/onix-engine/onix/scene"
	"github.com/onix-engine/onix/window"
)

// Record the actual render path without needing a native window or GPU.
type desktopRenderDriver struct {
	render.Driver
	viewport render.Rect
	nextID   uint32
	targets  []render.RenderTargetDescriptor
}

func (*desktopRenderDriver) RendererInfo() render.RendererInfo { return render.RendererInfo{} }
func (*desktopRenderDriver) BeginFrame(render.Color) error     { return nil }
func (d *desktopRenderDriver) SetViewport(r render.Rect)       { d.viewport = r }
func (d *desktopRenderDriver) Viewport() render.Rect           { return d.viewport }
func (*desktopRenderDriver) SetBlend(render.BlendState) error  { return nil }
func (*desktopRenderDriver) SetScissor(*render.Rect)           {}
func (d *desktopRenderDriver) CreateTexture(desc render.TextureDescriptor, pixels []byte) (render.TextureHandle, error) {
	if err := desc.Validate(pixels); err != nil {
		return render.TextureHandle{}, err
	}
	d.nextID++
	return render.NewHandle[render.TextureResource](d.nextID, 1), nil
}
func (*desktopRenderDriver) UpdateTextures([]render.TextureUpdate) error { return nil }
func (*desktopRenderDriver) DestroyTexture(render.TextureHandle) error   { return nil }
func (*desktopRenderDriver) DrawSprites(render.SpriteBatch) error        { return nil }
func (*desktopRenderDriver) DrawShapes(render.ShapeBatch) error          { return nil }
func (*desktopRenderDriver) Destroy()                                    {}
func (d *desktopRenderDriver) CreateRenderTarget(desc render.RenderTargetDescriptor) (render.RenderTargetHandle, render.TextureHandle, error) {
	d.targets = append(d.targets, desc)
	d.nextID += 2
	return render.NewHandle[render.RenderTargetResource](d.nextID-1, 1), render.NewHandle[render.TextureResource](d.nextID, 1), nil
}
func (d *desktopRenderDriver) ResizeRenderTarget(_ render.RenderTargetHandle, _ render.RenderTargetDescriptor) (render.TextureHandle, error) {
	d.nextID++
	return render.NewHandle[render.TextureResource](d.nextID, 1), nil
}
func (*desktopRenderDriver) DestroyRenderTarget(render.RenderTargetHandle) error { return nil }
func (*desktopRenderDriver) BeginRenderTarget(render.RenderTargetHandle, render.RenderTargetClear) error {
	return nil
}
func (*desktopRenderDriver) EndRenderTarget() error { return nil }

func TestDesktopHiDPIFramesKeepDirectRendering(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "engine-default-reproduces-quality-switch"
		if configured {
			name = "desktop-host-keeps-native-quality"
		}
		t.Run(name, func(t *testing.T) {
			driver := &desktopRenderDriver{viewport: render.Rect{Width: 2048, Height: 1400}}
			previous := render.ActiveDevice()
			device, err := render.NewDevice(driver)
			if err != nil {
				t.Fatal(err)
			}
			device.Activate()
			canvas := graphics.NewCanvas2D(device, 1024, 700)
			canvas.SetRasterScale(2)
			restore := graphics.SetCurrentContextForTesting(canvas)
			defer func() { device.Destroy(); previous.Activate(); restore() }()

			app := onix.NewApp()
			app.Context = &window.Context{Device: device, Width: 2048, Height: 1400}
			if configured {
				desktopHost(app, project.ProjectSpec{})
			}
			world := app.World
			graphics.BindFrame(world, canvas)
			scene.RegisterSpriteRenderer2D(world)
			scene.RegisterCameraRenderer2D(world)
			state := scene.RenderWorldFor(world)
			defer state.Dispose()
			state.Cameras = []scene.ExtractedCamera{{Entity: 1, Camera: scene.Camera{Clear: true, ClearColor: graphics.Black}, Pipeline: scene.CameraPipeline2D}}
			texture := graphics.CreateTextureBySharedImage(image.NewRGBA(image.Rect(0, 0, 2048, 1400)), false, graphics.ResourceUsageDefault)
			defer texture.Dispose()
			spawn := world.NewSpawner5[scene.Sprite2D, scene.UIGlobalTransform, scene.Visibility, scene.Anchor, scene.UIStackIndex]()
			sprite := scene.NewSprite2D()
			sprite.Texture, sprite.Width, sprite.Height = texture, 1024, 700
			spawn.Spawn(sprite, scene.UIGlobalTransform{Matrix: mgl32.Ident4()}, scene.Visibility{Visible: true}, scene.AnchorTopLeft, 0)

			// An initial frame, a small remote update (which invalidates the
			// full-screen texture), then an unchanged frame reproduce the switch.
			var direct [3]bool
			for i := range direct {
				if i == 1 {
					texture.UpdateRegions([]graphics.TextureRegionUpdate{{Width: 1, Height: 1, Pixels: []byte{255, 255, 255, 255}}})
				}
				if err := scene.ExtractRender2DSystem("").Run(world); err != nil {
					t.Fatal(err)
				}
				if err := scene.RenderCamerasSystem("").Run(world); err != nil {
					t.Fatal(err)
				}
				if err := canvas.FinishFrame(); err != nil {
					t.Fatal(err)
				}
				direct[i] = state.Composite2D.Direct
			}
			if configured {
				if direct != [3]bool{true, true, true} || len(driver.targets) != 0 {
					t.Fatalf("desktop frames did not stay direct: direct=%v targets=%+v", direct, driver.targets)
				}
			} else {
				if direct != [3]bool{false, true, false} || len(driver.targets) != 1 || driver.targets[0].Width != 1024 || driver.targets[0].Height != 700 {
					t.Fatalf("expected cached/direct/cached rendering at logical resolution; direct=%v targets=%+v", direct, driver.targets)
				}
			}
		})
	}
}
