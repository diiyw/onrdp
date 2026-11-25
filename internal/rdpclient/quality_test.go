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

func TestExplicitNativeResizeOverridesQualityOnReconnect(t *testing.T) {
	for _, quality := range []Quality{QualitySmooth, QualityBalanced, QualityHigh} {
		t.Run(string(quality), func(t *testing.T) {
			c := New(nil)
			s := newNativeSession(c)
			c.rdp, c.state = s, StateConnecting
			defer c.Close()
			cfg := Config{Width: 2560, Height: 1440, Quality: quality}
			windowWidth, windowHeight := quality.Resolution(cfg.Width, cfg.Height)
			if w, h := s.desktopResolution(cfg); w != windowWidth || h != windowHeight {
				t.Fatalf("initial windowed resolution=%dx%d, want %dx%d", w, h, windowWidth, windowHeight)
			}
			c.ResizeDesktop(3840, 2160)
			if s.desktopWidth != 3840 || s.desktopHeight != 2160 {
				t.Fatalf("pending fullscreen resolution=%dx%d", s.desktopWidth, s.desktopHeight)
			}
			if w, h := s.desktopResolution(cfg); w != 3840 || h != 2160 {
				t.Fatalf("reconnected fullscreen resolution=%dx%d", w, h)
			}
			c.ResizeDesktop(windowWidth, windowHeight)
			if w, h := s.desktopResolution(cfg); w != windowWidth || h != windowHeight {
				t.Fatalf("restored windowed resolution=%dx%d, want %dx%d", w, h, windowWidth, windowHeight)
			}
		})
	}
}
