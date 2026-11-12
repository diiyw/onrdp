package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/onix-engine/onix/graphics"
	"github.com/onix-engine/onix/render"
	"onrdp/internal/i18n"
	"onrdp/internal/rdpclient"
)

// pageIDs returns every id declared in the HTML page, so tests can prove that
// the module only ever addresses nodes the document actually contains.
func pageIDs(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("app.html")
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	matcher := regexp.MustCompile(`id="([^"]+)"`)
	ids := make(map[string]bool)
	for _, match := range matcher.FindAllStringSubmatch(string(source), -1) {
		ids[match[1]] = true
	}
	if len(ids) == 0 {
		t.Fatal("page declares no ids")
	}
	return ids
}

// A stale binding is invisible at runtime: the node lookup just returns the zero
// handle and the text silently never updates.
func TestTextBindingsExistInPage(t *testing.T) {
	ids := pageIDs(t)
	for _, binding := range textBindings {
		if !ids[binding.id] {
			t.Fatalf("text binding %q has no node in app.html", binding.id)
		}
	}
}

// Dynamic ids are produced by prefixing a template id with an action name.
func TestDynamicIDsUseTemplatePrefixes(t *testing.T) {
	ids := pageIDs(t)
	for _, template := range []string{"host-connect", "host-edit", "host-favorite", "session-open", "session-close"} {
		if !ids[template] {
			t.Fatalf("template %q is missing from app.html", template)
		}
	}
	// The framebuffer image must be addressable by the frame provider.
	if !ids["rdp-frame"] {
		t.Fatal("app.html has no rdp-frame image")
	}
}

// Every page the router toggles must exist, or routing hides nothing and leaves
// the previous screen visible.
func TestRoutedPagesExist(t *testing.T) {
	ids := pageIDs(t)
	for _, page := range []string{"hosts", "sessions", "settings", "edit", "rdp"} {
		if !ids[page+"-page"] {
			t.Fatalf("page %q has no section", page)
		}
	}
	if !ids["bottom-nav"] {
		t.Fatal("bottom navigation is missing")
	}
}

// The frame source must stay a bare name: the build-time resource scanner treats
// anything with a slash or extension as a file reference and fails the build.
func TestFrameSourceIsNotAResourceReference(t *testing.T) {
	if strings.ContainsAny(framePath, "/\\.") {
		t.Fatalf("frame path %q would be scanned as a file reference", framePath)
	}
}

func TestFrameSourceCarriesVersion(t *testing.T) {
	frame := &frameTexture{}
	if got := frame.Source(); got != framePath {
		t.Fatalf("empty frame source = %q, want %q", got, framePath)
	}
	frame.version = 3
	if got := frame.Source(); got != framePath+"?v=3" {
		t.Fatalf("versioned frame source = %q", got)
	}
}

// Unknown sources must fall through to the project provider so bundled icons
// keep resolving after the module installs itself.
func TestFrameProviderDelegates(t *testing.T) {
	previous := &stubProvider{}
	frame := &frameTexture{previous: previous}
	if texture, ok := frame.ResolveHTMLImage("icons/plus.svg", 0, 0); !ok || texture != previous.texture {
		t.Fatal("frame provider did not delegate to the project provider")
	}
	if _, ok := frame.ResolveHTMLImage(framePath, 0, 0); ok {
		t.Fatal("frame provider resolved a frame before one existed")
	}
}

// stubProvider is a stand-in for the project image provider.
type stubProvider struct {
	texture *graphics.Texture
}

func (p *stubProvider) ResolveHTMLImage(source string, _, _ float32) (*graphics.Texture, bool) {
	if p.texture == nil {
		p.texture = graphics.CreateEmptyTexture(1, 1, render.TextureFormatRGBA8, graphics.ResourceUsageDefault)
	}
	return p.texture, true
}

// A resize or reconnect must produce a new texture and a new image source, while
// an ordinary pixel update keeps both so the retained sprite is reused.
func TestFrameApplyRebuildsOnlyOnGeometryChange(t *testing.T) {
	frame := &frameTexture{}
	region := rdpclient.Region{X: 1, Y: 1, Width: 2, Height: 2, Pixels: []byte{
		1, 2, 3, 255, 4, 5, 6, 255,
		7, 8, 9, 255, 10, 11, 12, 255,
	}}
	if !frame.Apply([]rdpclient.Region{region}, 4, 4, 1) {
		t.Fatal("first frame did not build a texture")
	}
	source := frame.Source()
	if !frame.Apply([]rdpclient.Region{region}, 4, 4, 1) {
		// Same geometry and generation: the texture is reused in place.
	} else {
		t.Fatal("pixel update rebuilt the texture")
	}
	if frame.Source() != source {
		t.Fatal("pixel update changed the image source")
	}
	if !frame.Apply(nil, 8, 8, 2) {
		t.Fatal("resize did not rebuild the texture")
	}
	if frame.Source() == source {
		t.Fatal("resize did not change the image source")
	}
	if frame.width != 8 || frame.height != 8 || frame.generation != 2 {
		t.Fatalf("frame geometry = %d×%d gen %d", frame.width, frame.height, frame.generation)
	}
}

// The seeded pixels must land at the requested offset, not at the origin.
func TestFrameSeedsRegionsAtTheirOffset(t *testing.T) {
	frame := &frameTexture{}
	region := rdpclient.Region{X: 1, Y: 1, Width: 1, Height: 1, Pixels: []byte{9, 8, 7, 255}}
	frame.Apply([]rdpclient.Region{region}, 3, 3, 1)
	offset := (1*3 + 1) * 4
	if got := frame.buffer[offset : offset+4]; got[0] != 9 || got[1] != 8 || got[2] != 7 || got[3] != 255 {
		t.Fatalf("seeded pixel = %v", got)
	}
	if frame.buffer[0] != 0 {
		t.Fatal("origin pixel was written by an offset region")
	}
}

// Every static binding must resolve in every shipped catalog. A key that exists
// only in the fallback language shows up as untranslated text at runtime, which
// no other test would catch.
func TestTextBindingsAreTranslated(t *testing.T) {
	for _, language := range i18n.Languages() {
		for _, binding := range textBindings {
			if binding.key == "" {
				continue
			}
			if _, ok := i18n.Catalog(language.Code)[binding.key]; !ok {
				t.Fatalf("%s is missing %q (bound to %q)", language.Code, binding.key, binding.id)
			}
		}
	}
}
