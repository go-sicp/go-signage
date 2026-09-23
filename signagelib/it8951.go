package signagelib

// IT8951 SPI/I80 protocol constants. Values come from the publicly
// available IT8951 USB/SPI command-set documentation as shipped by
// Waveshare and GoodDisplay.

// SPI preamble words (big-endian on the wire).
const (
	preambleWriteCmd  uint16 = 0x6000
	preambleWriteData uint16 = 0x0000
	preambleReadData  uint16 = 0x1000
)

// User-level commands, sent as a 16-bit op code following preambleWriteCmd.
const (
	cmdSysRun      uint16 = 0x0001
	cmdStandby     uint16 = 0x0002
	cmdSleep       uint16 = 0x0003
	cmdRegRead     uint16 = 0x0010
	cmdRegWrite    uint16 = 0x0011
	cmdMemBurstRdT uint16 = 0x0012
	cmdMemBurstRdS uint16 = 0x0018
	cmdMemBurstWr  uint16 = 0x0019
	cmdMemBurstEnd uint16 = 0x001E
	cmdLdImg       uint16 = 0x0020
	cmdLdImgArea   uint16 = 0x0021
	cmdLdImgEnd    uint16 = 0x0022
	cmdDpyArea     uint16 = 0x0034
	cmdGetDevInfo  uint16 = 0x0302
	cmdDpyBufArea  uint16 = 0x0037
	cmdVCom        uint16 = 0x0039
)

// IT8951 internal register addresses used by SetTargetMemoryAddr and the
// LUT-busy poll. The names follow the firmware reference.
const (
	regI80CPCR  uint16 = 0x0004
	regLISAR0   uint16 = 0x0208 // image buffer base, low 16 bits
	regLISAR1   uint16 = 0x020A // image buffer base, high 16 bits
	regDispBase uint16 = 0x1000
	regLUTAFSR  uint16 = 0x1224 // LUT busy bitmap; non-zero = busy
)

// LdImgInfo is the 16-bit argument word to LD_IMG / LD_IMG_AREA. The four
// fields are bit-packed: endianness:1 | bpp:2 | rotation:2.
type LdImgInfo struct {
	BigEndian    bool
	BitsPerPixel uint8 // encoded value: 0=2bpp, 1=3bpp(reserved), 2=4bpp, 3=8bpp
	Rotation     uint8 // 0=0deg, 1=90, 2=180, 3=270
}

// Encode packs the LdImgInfo into the wire word.
func (l LdImgInfo) Encode() uint16 {
	var v uint16
	if l.BigEndian {
		v |= 1 << 8
	}
	v |= uint16(l.BitsPerPixel&0x3) << 4
	v |= uint16(l.Rotation & 0x3)
	return v
}

// EncodeBPP returns the 2-bit BPP code for the given pixel depth. Only 2,
// 4 and 8 are valid for IT8951 LD_IMG; 1-bit images must be promoted to
// 2-bit by replicating each bit, or sent via the dedicated 1bpp pipeline.
func EncodeBPP(bpp int8) (uint8, bool) {
	switch bpp {
	case 2:
		return 0, true
	case 4:
		return 2, true
	case 8:
		return 3, true
	}
	return 0, false
}

// DevInfo is the binary layout of GET_DEV_INFO's response (40 bytes,
// expressed as 20 big-endian 16-bit words). Field order is fixed by the
// IT8951 firmware and must not be reordered.
type DevInfo struct {
	PanelW          uint16
	PanelH          uint16
	ImgBufAddrLow   uint16
	ImgBufAddrHigh  uint16
	FirmwareVersion [16]byte
	LUTVersion      [16]byte
}

// ImageBufferAddress reassembles the 32-bit memory address from the two
// 16-bit halves returned by GET_DEV_INFO. Big-endian on the wire.
func (d DevInfo) ImageBufferAddress() uint32 {
	return uint32(d.ImgBufAddrHigh)<<16 | uint32(d.ImgBufAddrLow)
}
