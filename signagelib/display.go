package signagelib

// Display is the high-level handle on an IT8951-driven e-paper panel.
//
// Implementations must serialise calls; the contract is single-writer.
// Callers wanting concurrent rendering should pre-pack their bitmap and
// feed it to LoadImage in one shot.
type Display interface {
	Info() (DisplayInfo, error)
	LoadImage(img PackedImage, region Region) error
	Refresh(mode WaveformMode, region Region) error
	Standby() error
	Sleep() error
	Wake() error
	Close() error
}

// DisplayInfo describes the panel geometry and IT8951 firmware identifiers.
type DisplayInfo struct {
	width, height   int32
	imageBufferAddr uint32
	firmwareVersion string
	lutVersion      string
}

func (d DisplayInfo) Width() int32               { return d.width }
func (d DisplayInfo) Height() int32              { return d.height }
func (d DisplayInfo) ImageBufferAddress() uint32 { return d.imageBufferAddr }
func (d DisplayInfo) FirmwareVersion() string    { return d.firmwareVersion }
func (d DisplayInfo) LUTVersion() string         { return d.lutVersion }

// Region is an inclusive top-left, exclusive bottom-right rectangle. A
// zero-valued Region is taken to mean the full panel.
type Region struct {
	x, y, w, h int32
}

func NewRegion(x, y, w, h int32) Region { return Region{x: x, y: y, w: w, h: h} }

func (r Region) X() int32 { return r.x }
func (r Region) Y() int32 { return r.y }
func (r Region) W() int32 { return r.w }
func (r Region) H() int32 { return r.h }

// IsZero reports whether the region was default-initialised.
func (r Region) IsZero() bool { return r.x == 0 && r.y == 0 && r.w == 0 && r.h == 0 }

// PackedImage is a tightly-packed pixel buffer ready to be uploaded to the
// controller. The IT8951 protocol negotiates BPP at LoadImage time so the
// caller decides the trade-off between bandwidth and grayscale fidelity.
type PackedImage struct {
	pixels       []byte
	bitsPerPixel int8
	width        int32
	height       int32
}

func NewPackedImage(pixels []byte, bpp int8, w, h int32) PackedImage {
	return PackedImage{pixels: pixels, bitsPerPixel: bpp, width: w, height: h}
}

func (p PackedImage) Pixels() []byte     { return p.pixels }
func (p PackedImage) BitsPerPixel() int8 { return p.bitsPerPixel }
func (p PackedImage) Width() int32       { return p.width }
func (p PackedImage) Height() int32      { return p.height }

// ExpectedBytes is the buffer size for the declared geometry and BPP. Used
// by callers to assert their pack output before calling LoadImage.
func (p PackedImage) ExpectedBytes() int {
	bits := int(p.width) * int(p.height) * int(p.bitsPerPixel)
	return (bits + 7) / 8
}

// Config is the construction-time settings of an Open call. Zero values
// fall back to defaults appropriate for the Waveshare IT8951 HAT on a Pi.
type Config struct {
	SPIPort  string // e.g. "SPI0.0"; empty = first available
	ResetPin string // GPIO name; empty = "GPIO17"
	BusyPin  string // GPIO name; empty = "GPIO24"
	SPIHz    int64  // 0 = 12 MHz
	VCOMmV   int    // panel-specific VCOM in millivolts; 0 = leave EEPROM default
}
