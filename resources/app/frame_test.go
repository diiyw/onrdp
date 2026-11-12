package app

import (
	"bytes"
	"testing"

	"github.com/onix-engine/onix/render"
	"onrdp/internal/rdpclient"
)

// frameDriver consumes uploads synchronously just like the render contract.
// Its pixel copy also catches accidental reuse of borrowed Flush memory.
type frameDriver struct {
	render.Driver
	pixels    []byte
	width     int
	created   uint32
	destroyed int
	batches   int
}

func (d *frameDriver) CreateTexture(desc render.TextureDescriptor, pixels []byte) (render.TextureHandle, error) {
	d.width = desc.Width
	d.pixels = append(d.pixels[:0], pixels...)
	d.created++
	return render.NewHandle[render.TextureResource](d.created, 1), nil
}

func (d *frameDriver) UpdateTextures(updates []render.TextureUpdate) error {
	d.batches++
	for _, update := range updates {
		r := update.Region
		for row := range r.Height {
			offset := ((r.Y+row)*d.width + r.X) * 4
			copy(d.pixels[offset:offset+r.Width*4], update.Pixels[row*r.Width*4:(row+1)*r.Width*4])
		}
	}
	return nil
}

func (d *frameDriver) DestroyTexture(render.TextureHandle) error {
	d.destroyed++
	return nil
}

func (d *frameDriver) Destroy() {}

func TestFrameUploadReusesMemoryAndConsumesBorrowedPixels(t *testing.T) {
	driver := &frameDriver{}
	if err := render.Install(driver); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(render.Uninstall)
	f := &frameTexture{}
	defer f.Dispose()
	f.Apply(nil, 64, 64, 1)
	if !f.texture.RenderHandle().Valid() {
		t.Fatal("texture was not uploaded")
	}
	var regions [rdpclient.MaxFrameRegions]rdpclient.Region
	var pixels [rdpclient.MaxFrameRegions][4]byte
	for i := range regions {
		pixels[i] = [4]byte{byte(i), 23, 42, 255}
		regions[i] = rdpclient.Region{X: i, Y: i, Width: 1, Height: 1, Pixels: pixels[i][:]}
	}
	texture := f.texture
	allocs := testing.AllocsPerRun(1000, func() {
		if f.Apply(regions[:], 64, 64, 1) {
			t.Fatal("update replaced the texture")
		}
	})
	if allocs != 0 {
		t.Fatalf("warm frame upload allocated %g objects", allocs)
	}
	if f.texture != texture || driver.created != 1 {
		t.Fatal("steady uploads created new textures")
	}
	for i, region := range regions {
		offset := (region.Y*64 + region.X) * 4
		if !bytes.Equal(f.buffer[offset:offset+4], pixels[i][:]) ||
			!bytes.Equal(driver.pixels[offset:offset+4], pixels[i][:]) {
			t.Fatal("update did not reach both shared CPU shadow and GPU")
		}
		clear(pixels[i][:]) // the next Flush may immediately reuse these bytes
		if f.buffer[offset+1] != 23 || driver.pixels[offset+1] != 23 {
			t.Fatal("texture retained borrowed pixels")
		}
	}
	for _, update := range f.updates {
		if update.Pixels != nil {
			t.Fatal("upload list retained expired framebuffer pixels")
		}
	}
	if driver.batches != 1001 { // AllocsPerRun includes one warm-up call
		t.Fatalf("uploads used %d batches, want 1001", driver.batches)
	}
}

func TestFrameResizeReleasesOldTextureAndOversizedPixels(t *testing.T) {
	driver := &frameDriver{}
	if err := render.Install(driver); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(render.Uninstall)
	f := &frameTexture{}
	defer f.Dispose()
	f.Apply(nil, 64, 64, 1)
	f.texture.RenderHandle()
	old := f.texture
	f.Apply(nil, 8, 8, 2)
	if old.Valid() || driver.destroyed != 1 || cap(f.buffer) != 8*8*4 {
		t.Fatal("resize retained old texture or oversized framebuffer")
	}
}

func TestFrameIgnoresInvalidRegions(t *testing.T) {
	f := &frameTexture{}
	defer f.Dispose()
	invalid := []rdpclient.Region{
		{X: -1, Width: 1, Height: 1, Pixels: []byte{1, 2, 3, 4}},
		{X: 3, Width: 2, Height: 1, Pixels: make([]byte, 8)},
		{Width: 1, Height: 1, Pixels: []byte{1}},
	}
	f.Apply(invalid, 4, 4, 1)
	f.Apply(invalid, 4, 4, 1)
	if !bytes.Equal(f.buffer, make([]byte, 4*4*4)) {
		t.Fatal("invalid region changed framebuffer pixels")
	}
}
