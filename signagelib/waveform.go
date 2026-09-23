package signagelib

// WaveformMode mirrors the IT8951 firmware display mode IDs. Values are
// chosen so the cast to uint16 in the SPI command is a no-op.
type WaveformMode int32

const (
	WaveformInit  WaveformMode = 0 // INIT — full clear to white
	WaveformDU    WaveformMode = 1 // direct update, 1-bit, fast
	WaveformGC16  WaveformMode = 2 // grayscale 16, full quality
	WaveformGL16  WaveformMode = 3 // grayscale 16, less ghost
	WaveformGLR16 WaveformMode = 4 // glare reduce
	WaveformGLD16 WaveformMode = 5 // ghost level reduce
	WaveformA2    WaveformMode = 6 // 1-bit very fast, animation
)

func (m WaveformMode) String() string {
	switch m {
	case WaveformInit:
		return "Init"
	case WaveformDU:
		return "DU"
	case WaveformGC16:
		return "GC16"
	case WaveformGL16:
		return "GL16"
	case WaveformGLR16:
		return "GLR16"
	case WaveformGLD16:
		return "GLD16"
	case WaveformA2:
		return "A2"
	}
	return "Unknown"
}

func ParseWaveformMode(s string) (WaveformMode, bool) {
	for m := WaveformInit; m <= WaveformA2; m++ {
		if eqFold(m.String(), s) {
			return m, true
		}
	}
	return 0, false
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
