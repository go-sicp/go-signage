//go:build linux

package signagelib

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

const (
	defaultSPIHz     = 12_000_000
	defaultResetPin  = "GPIO17"
	defaultBusyPin   = "GPIO24"
	hrdyPollInterval = 100 * time.Microsecond
	hrdyTimeout      = 5 * time.Second
	resetAssertTime  = 100 * time.Millisecond
	resetSettleTime  = 100 * time.Millisecond
)

// Open initialises periph.io, opens the SPI port and the GPIO pins, runs
// the IT8951 reset/wake sequence, and queries GET_DEV_INFO.
func Open(cfg Config) (Display, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("host init: %w", err)
	}

	port, err := spireg.Open(cfg.SPIPort)
	if err != nil {
		return nil, fmt.Errorf("spi open %q: %w", cfg.SPIPort, err)
	}
	hz := cfg.SPIHz
	if hz <= 0 {
		hz = defaultSPIHz
	}
	conn, err := port.Connect(physic.Frequency(hz)*physic.Hertz, spi.Mode0, 8)
	if err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("spi connect: %w", err)
	}

	rstName := cfg.ResetPin
	if rstName == "" {
		rstName = defaultResetPin
	}
	rst := gpioreg.ByName(rstName)
	if rst == nil {
		_ = port.Close()
		return nil, fmt.Errorf("gpio %q not found", rstName)
	}
	if err := rst.Out(gpio.High); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("rst Out: %w", err)
	}

	busyName := cfg.BusyPin
	if busyName == "" {
		busyName = defaultBusyPin
	}
	busy := gpioreg.ByName(busyName)
	if busy == nil {
		_ = port.Close()
		return nil, fmt.Errorf("gpio %q not found", busyName)
	}
	if err := busy.In(gpio.PullUp, gpio.NoEdge); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("busy In: %w", err)
	}

	d := &driver{
		port: port,
		conn: conn,
		rst:  rst,
		busy: busy,
	}

	if err := d.reset(); err != nil {
		_ = d.Close()
		return nil, err
	}
	if err := d.writeCmd(cmdSysRun); err != nil {
		_ = d.Close()
		return nil, err
	}
	dev, err := d.readDevInfo()
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	d.info = DisplayInfo{
		width:           int32(dev.PanelW),
		height:          int32(dev.PanelH),
		imageBufferAddr: dev.ImageBufferAddress(),
		firmwareVersion: trimAscii(dev.FirmwareVersion[:]),
		lutVersion:      trimAscii(dev.LUTVersion[:]),
	}

	// Enable I80 packed-write mode in the controller (bit 0 of I80CPCR).
	// This is a one-shot init step that conditions every subsequent
	// LD_IMG / LD_IMG_AREA call to use the same packing as our buffer.
	if err := d.writeReg(regI80CPCR, 0x0001); err != nil {
		_ = d.Close()
		return nil, err
	}
	if cfg.VCOMmV > 0 {
		if err := d.setVCOM(int16(cfg.VCOMmV)); err != nil {
			_ = d.Close()
			return nil, err
		}
	}
	return d, nil
}

type driver struct {
	port spi.PortCloser
	conn spi.Conn
	rst  gpio.PinOut
	busy gpio.PinIn

	mu   sync.Mutex
	info DisplayInfo
}

func (d *driver) Info() (DisplayInfo, error) {
	return d.info, nil
}

func (d *driver) reset() error {
	if err := d.rst.Out(gpio.Low); err != nil {
		return err
	}
	time.Sleep(resetAssertTime)
	if err := d.rst.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(resetSettleTime)
	return d.waitReady()
}

func (d *driver) waitReady() error {
	deadline := time.Now().Add(hrdyTimeout)
	for {
		if d.busy.Read() == gpio.High {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("signagelib: HRDY did not go high (display unresponsive)")
		}
		time.Sleep(hrdyPollInterval)
	}
}

// writeCmd sends a 16-bit command word with the WRITE_CMD preamble.
func (d *driver) writeCmd(cmd uint16) error {
	if err := d.waitReady(); err != nil {
		return err
	}
	// Both words go out big-endian, the way writeData and readData below
	// already do it. The hand-rolled version here did not compile:
	//
	//	byte(preambleWriteCmd)   // constant 24576 overflows byte
	//
	// preambleWriteCmd is the CONSTANT 0x6000, so `byte(...)` is evaluated at
	// compile time and must be representable — unlike byte(cmd), where cmd is
	// a variable and the conversion simply truncates at run time. The two look
	// identical on the same line and are not the same operation.
	buf := make([]byte, 4)
	binary.BigEndian.PutUint16(buf[0:2], preambleWriteCmd)
	binary.BigEndian.PutUint16(buf[2:4], cmd)
	return d.conn.Tx(buf, nil)
}

// writeData writes one or more 16-bit data words after the WRITE_DATA
// preamble. Each word is transmitted big-endian.
func (d *driver) writeData(words ...uint16) error {
	if err := d.waitReady(); err != nil {
		return err
	}
	buf := make([]byte, 2+2*len(words))
	binary.BigEndian.PutUint16(buf[0:2], preambleWriteData)
	for i, w := range words {
		binary.BigEndian.PutUint16(buf[2+2*i:4+2*i], w)
	}
	return d.conn.Tx(buf, nil)
}

// writeBytes writes a raw byte stream after the WRITE_DATA preamble. Used
// during LD_IMG_AREA pixel transfer. The buffer length must be even (the
// IT8951 always reads 16-bit words).
func (d *driver) writeBytes(payload []byte) error {
	if len(payload)%2 != 0 {
		return errors.New("signagelib: writeBytes requires even length (16-bit alignment)")
	}
	if err := d.waitReady(); err != nil {
		return err
	}
	buf := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(buf[0:2], preambleWriteData)
	copy(buf[2:], payload)
	return d.conn.Tx(buf, nil)
}

// readWords reads n 16-bit words after the READ_DATA preamble. The IT8951
// inserts a 16-bit dummy word right after the preamble, so we send 4
// dummy preamble bytes then ignore the first 2 bytes of the response.
func (d *driver) readWords(n int) ([]uint16, error) {
	if err := d.waitReady(); err != nil {
		return nil, err
	}
	total := 2 + 2 + 2*n // preamble + dummy + payload
	tx := make([]byte, total)
	rx := make([]byte, total)
	binary.BigEndian.PutUint16(tx[0:2], preambleReadData)
	if err := d.conn.Tx(tx, rx); err != nil {
		return nil, err
	}
	out := make([]uint16, n)
	for i := 0; i < n; i++ {
		out[i] = binary.BigEndian.Uint16(rx[4+2*i : 6+2*i])
	}
	return out, nil
}

func (d *driver) writeReg(addr, value uint16) error {
	if err := d.writeCmd(cmdRegWrite); err != nil {
		return err
	}
	return d.writeData(addr, value)
}

func (d *driver) readReg(addr uint16) (uint16, error) {
	if err := d.writeCmd(cmdRegRead); err != nil {
		return 0, err
	}
	if err := d.writeData(addr); err != nil {
		return 0, err
	}
	w, err := d.readWords(1)
	if err != nil {
		return 0, err
	}
	return w[0], nil
}

func (d *driver) readDevInfo() (DevInfo, error) {
	if err := d.writeCmd(cmdGetDevInfo); err != nil {
		return DevInfo{}, err
	}
	w, err := d.readWords(20)
	if err != nil {
		return DevInfo{}, err
	}
	out := DevInfo{
		PanelW:         w[0],
		PanelH:         w[1],
		ImgBufAddrLow:  w[2],
		ImgBufAddrHigh: w[3],
	}
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint16(out.FirmwareVersion[2*i:2*i+2], w[4+i])
		binary.BigEndian.PutUint16(out.LUTVersion[2*i:2*i+2], w[12+i])
	}
	return out, nil
}

func (d *driver) setTargetMemoryAddr(addr uint32) error {
	if err := d.writeReg(regLISAR1, uint16(addr>>16)); err != nil {
		return err
	}
	return d.writeReg(regLISAR0, uint16(addr&0xFFFF))
}

// LoadImage uploads a packed pixel buffer to the controller's image memory
// at the location returned by GET_DEV_INFO. The IT8951 keeps the image
// resident until the next LoadImage; Refresh consumes it.
func (d *driver) LoadImage(img PackedImage, region Region) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	bppCode, ok := EncodeBPP(img.BitsPerPixel())
	if !ok {
		return fmt.Errorf("signagelib: unsupported bpp %d (want 2, 4 or 8)", img.BitsPerPixel())
	}
	if err := d.setTargetMemoryAddr(d.info.imageBufferAddr); err != nil {
		return err
	}
	r := region
	if r.IsZero() {
		r = NewRegion(0, 0, img.Width(), img.Height())
	}
	info := LdImgInfo{BigEndian: false, BitsPerPixel: bppCode, Rotation: 0}.Encode()
	if err := d.writeCmd(cmdLdImgArea); err != nil {
		return err
	}
	if err := d.writeData(info, uint16(r.x), uint16(r.y), uint16(r.w), uint16(r.h)); err != nil {
		return err
	}
	if err := d.writeBytes(img.pixels); err != nil {
		return err
	}
	return d.writeCmd(cmdLdImgEnd)
}

// Refresh triggers a panel update of the given region using the requested
// waveform. A zero region means full-panel.
func (d *driver) Refresh(mode WaveformMode, region Region) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := region
	if r.IsZero() {
		r = NewRegion(0, 0, d.info.width, d.info.height)
	}
	if err := d.writeCmd(cmdDpyArea); err != nil {
		return err
	}
	if err := d.writeData(uint16(r.x), uint16(r.y), uint16(r.w), uint16(r.h), uint16(mode)); err != nil {
		return err
	}
	return d.waitLUTIdle()
}

// waitLUTIdle polls regLUTAFSR until every LUT is free. The controller
// triggers the panel asynchronously, so callers that want to chain a
// second Refresh must wait here first.
func (d *driver) waitLUTIdle() error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		v, err := d.readReg(regLUTAFSR)
		if err != nil {
			return err
		}
		if v == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("signagelib: LUT busy timeout (refresh did not complete)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (d *driver) Standby() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeCmd(cmdStandby)
}

func (d *driver) Sleep() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeCmd(cmdSleep)
}

func (d *driver) Wake() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeCmd(cmdSysRun)
}

// setVCOM writes the panel-specific VCOM value (in millivolts, as a
// negative quantity expressed unsigned). Each panel has its own value
// printed on the FPC ribbon — wrong VCOM degrades the image.
func (d *driver) setVCOM(mv int16) error {
	if err := d.writeCmd(cmdVCom); err != nil {
		return err
	}
	return d.writeData(0x0001, uint16(mv))
}

func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.port == nil {
		return nil
	}
	err := d.port.Close()
	d.port = nil
	return err
}

func trimAscii(b []byte) string {
	return strings.TrimRight(string(b), "\x00 ")
}
