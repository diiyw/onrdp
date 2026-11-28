package rustdesk

const (
	QualityCustom = "custom"
	MinQuality    = 10
	MaxQuality    = 2000
	MinFPS        = 5
	MaxFPS        = 120
)

// VideoQuality describes the peer's encoding preset or custom quality and FPS.
// Its zero value uses RustDesk's balanced preset.
type VideoQuality struct {
	Mode         string `json:"mode,omitempty"`
	ImageQuality int    `json:"imageQuality,omitempty"`
	FPS          int    `json:"fps,omitempty"`
}

func (q VideoQuality) Normalized() VideoQuality {
	switch q.Mode {
	case "smooth", "high", QualityCustom:
	default:
		q.Mode = "balanced"
	}
	if q.ImageQuality < MinQuality || q.ImageQuality > MaxQuality {
		q.ImageQuality = 50
	}
	if q.FPS < MinFPS || q.FPS > MaxFPS {
		q.FPS = 30
	}
	return q
}
