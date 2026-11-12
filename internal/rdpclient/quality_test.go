package rdpclient

import "testing"

func TestQualityResolution(t *testing.T) {
	for _, tt := range []struct {
		name                                 string
		quality                              Quality
		width, height, wantWidth, wantHeight int
	}{
		{"legacy keeps native size", "", 3456, 2234, 3456, 2234},
		{"unknown keeps native size", "future", 3840, 2160, 3840, 2160},
		{"smooth 4K", QualitySmooth, 3840, 2160, 1280, 720},
		{"balanced 4K", QualityBalanced, 3840, 2160, 1920, 1080},
		{"Retina aspect ratio", QualitySmooth, 3456, 2234, 1112, 720},
		{"portrait", QualitySmooth, 2160, 3840, 720, 1280},
		{"ultrawide", QualityBalanced, 3440, 1440, 1920, 803},
		{"no upscale", QualitySmooth, 640, 480, 640, 480},
		{"unavailable surface", QualitySmooth, 0, 0, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w, h := tt.quality.Resolution(tt.width, tt.height)
			if w != tt.wantWidth || h != tt.wantHeight {
				t.Fatalf("resolution = %dx%d, want %dx%d", w, h, tt.wantWidth, tt.wantHeight)
			}
			if againW, againH := tt.quality.Resolution(w, h); againW != w || againH != h {
				t.Fatalf("reconnect shrinks resolution to %dx%d", againW, againH)
			}
		})
	}
}

func TestQualityCapsPendingNativeResize(t *testing.T) {
	s := newNativeSession(New(nil))
	s.quality = QualitySmooth
	s.ResizeDesktop(3840, 2160)
	if s.desktopWidth != 1280 || s.desktopHeight != 720 {
		t.Fatalf("fullscreen bypassed quality cap: %dx%d", s.desktopWidth, s.desktopHeight)
	}
}
