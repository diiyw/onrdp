package app

import (
	"image"
	"strconv"
	"strings"

	"github.com/onix-engine/onix/graphics"
	htmlui "github.com/onix-engine/onix/ui/html"
	"onrdp/internal/rdpclient"
)

// framePath is the address the page's <img> uses for the remote desktop. A
// version query distinguishes texture identities, so a resize can re-point the
// image without mutating a sprite the renderer already retained.
// The name has no slash or extension on purpose: the build-time resource
// scanner then leaves it alone, while the runtime provider resolves it.
const framePath = "rdp-frame"

// frameTexture owns the texture the page's image element displays, and doubles
// as the surface's image provider. Any other source falls through to the
// project provider, which keeps the bundled icons working.
type frameTexture struct {
	previous   htmlui.ImageProvider
	texture    *graphics.Texture
	buffer     []byte
	updates    [rdpclient.MaxFrameRegions]graphics.TextureRegionUpdate
	width      int
	height     int
	version    uint64
	generation uint64
}

// ResolveHTMLImage satisfies the engine's HTML image provider contract. Width
// and height are the laid-out CSS size and are ignored: the texture keeps the
// remote framebuffer's resolution and the page scales it with object-fit.
func (f *frameTexture) ResolveHTMLImage(source string, width, height float32) (*graphics.Texture, bool) {
	if path, _, _ := strings.Cut(source, "?"); path == framePath {
		return f.texture, f.texture != nil
	}
	if f.previous != nil {
		return f.previous.ResolveHTMLImage(source, width, height)
	}
	return nil, false
}

// Source is the image address for the current texture. It changes exactly when
// the texture object changes, never for in-place pixel updates.
func (f *frameTexture) Source() string {
	if f.version == 0 {
		return framePath
	}
	return framePath + "?v=" + strconv.FormatUint(f.version, 10)
}

// Apply publishes new framebuffer regions and reports whether the texture
// object was replaced, in which case the caller must re-point the image source.
func (f *frameTexture) Apply(regions []rdpclient.Region, width, height int, generation uint64) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	if f.texture == nil || f.width != width || f.height != height || f.generation != generation {
		return f.rebuild(width, height, generation, regions)
	}
	if len(regions) == 0 {
		return false
	}
	for len(regions) > 0 {
		count := min(len(regions), len(f.updates))
		updates := f.updates[:0]
		for _, region := range regions[:count] {
			if !validRegion(region, width, height) {
				continue
			}
			updates = append(updates, graphics.TextureRegionUpdate{
				X: region.X, Y: region.Y, Width: region.Width, Height: region.Height, Pixels: region.Pixels,
			})
		}
		// Onix consumes CPU and driver uploads before returning. No borrowed
		// pixels survive this call or grow a queue of historical frames.
		f.texture.UpdateRegions(updates)
		clear(updates)
		regions = regions[count:]
	}
	return false
}

func validRegion(region rdpclient.Region, width, height int) bool {
	return region.X >= 0 && region.Y >= 0 && region.Width > 0 && region.Height > 0 &&
		region.X <= width && region.Y <= height &&
		region.Width <= width-region.X && region.Height <= height-region.Y &&
		len(region.Pixels) == region.Width*region.Height*4
}

// rebuild replaces the texture after a resize or reconnect. A reactivated
// session is repainted by the server, but seeding the regions that arrived with
// the new generation avoids a black flash in the meantime.
func (f *frameTexture) rebuild(width, height int, generation uint64, regions []rdpclient.Region) bool {
	// Dispose the old texture before reusing its shared CPU pixels.
	if f.texture != nil {
		f.texture.Dispose()
		f.texture = nil
	}
	size := width * height * 4
	if cap(f.buffer) != size {
		f.buffer = make([]byte, size)
	} else {
		f.buffer = f.buffer[:size]
		clear(f.buffer)
	}
	for _, region := range regions {
		if !validRegion(region, width, height) {
			continue
		}
		rowBytes := region.Width * 4
		for row := 0; row < region.Height; row++ {
			dstOffset := ((region.Y+row)*width + region.X) * 4
			srcOffset := row * rowBytes
			copy(f.buffer[dstOffset:dstOffset+rowBytes], region.Pixels[srcOffset:srcOffset+rowBytes])
		}
	}
	texture := graphics.CreateTextureBySharedImage(&image.RGBA{
		Pix:    f.buffer,
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}, false, graphics.ResourceUsageDefault)
	f.texture = texture
	f.width, f.height, f.generation = width, height, generation
	f.version++
	return true
}

// Dispose releases the GPU texture when the module shuts down.
func (f *frameTexture) Dispose() {
	if f.texture != nil {
		f.texture.Dispose()
		f.texture = nil
	}
	f.buffer = nil
	clear(f.updates[:])
	f.width, f.height, f.generation = 0, 0, 0
}
