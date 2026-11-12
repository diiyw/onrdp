package rdpclient

// Quality controls the requested desktop size and RDP visual effects. The zero
// value preserves the original full-resolution behavior of saved hosts.
type Quality string

const (
	QualitySmooth   Quality = "smooth"
	QualityBalanced Quality = "balanced"
	QualityHigh     Quality = "high"
)

func (q Quality) Normalized() Quality {
	switch q {
	case QualitySmooth, QualityBalanced:
		return q
	default:
		return QualityHigh
	}
}

// Resolution fits within a landscape or portrait limit without upscaling or
// changing the aspect ratio. It also applies to native-pixel fullscreen sizes.
func (q Quality) Resolution(width, height int) (int, int) {
	var long, short int
	switch q.Normalized() {
	case QualitySmooth:
		long, short = 1280, 720
	case QualityBalanced:
		long, short = 1920, 1080
	default:
		return width, height
	}
	if width <= 0 || height <= 0 {
		return width, height
	}
	maxWidth, maxHeight := long, short
	if height > width {
		maxWidth, maxHeight = short, long
	}
	scale := min(1.0, float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	return max(200, int(float64(width)*scale)&^1), max(200, int(float64(height)*scale))
}

func (q Quality) colorDepth() int {
	if q.Normalized() == QualitySmooth {
		return 16
	}
	return 32
}
