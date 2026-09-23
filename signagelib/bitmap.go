package signagelib

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// PackPNGGrayscale4 decodes a PNG, converts to luminance, and packs to
// 4-bit grayscale (2 px/byte, low nibble first). Output dimensions match
// the source image; callers are responsible for matching the panel
// geometry beforehand if needed.
func PackPNGGrayscale4(pngBytes []byte) (PackedImage, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return PackedImage{}, fmt.Errorf("decode png: %w", err)
	}
	bounds := img.Bounds()
	w := int32(bounds.Dx())
	h := int32(bounds.Dy())
	if w <= 0 || h <= 0 {
		return PackedImage{}, errors.New("png has zero area")
	}
	if w%2 != 0 {
		return PackedImage{}, fmt.Errorf("width %d not even (4-bit packing requires even width)", w)
	}
	out := make([]byte, int(w)*int(h)/2)
	idx := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x += 2 {
			n0 := lumaNibble(img.At(x, y))
			n1 := lumaNibble(img.At(x+1, y))
			out[idx] = (n1 << 4) | n0
			idx++
		}
	}
	return NewPackedImage(out, 4, w, h), nil
}

// PackPNGBinary1 decodes a PNG and packs it to 1-bit (8 px/byte, MSB
// first). Threshold is fixed at the perceptual midpoint (luma > 127).
func PackPNGBinary1(pngBytes []byte) (PackedImage, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return PackedImage{}, fmt.Errorf("decode png: %w", err)
	}
	bounds := img.Bounds()
	w := int32(bounds.Dx())
	h := int32(bounds.Dy())
	if w%8 != 0 {
		return PackedImage{}, fmt.Errorf("width %d not a multiple of 8 (1-bit packing constraint)", w)
	}
	out := make([]byte, int(w)*int(h)/8)
	idx := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x += 8 {
			var b byte
			for bit := 0; bit < 8; bit++ {
				if luma8(img, x+bit, y) > 0x7F {
					b |= 1 << (7 - uint(bit))
				}
			}
			out[idx] = b
			idx++
		}
	}
	return NewPackedImage(out, 1, w, h), nil
}

func luma8(img image.Image, x, y int) byte {
	r, g, b, _ := img.At(x, y).RGBA()
	v := (uint32(r)*54 + uint32(g)*183 + uint32(b)*19) >> (16 + 8)
	if v > 0xFF {
		v = 0xFF
	}
	return byte(v)
}

func lumaNibble(c color.Color) byte {
	r, g, b, _ := c.RGBA()
	v := (uint32(r)*54 + uint32(g)*183 + uint32(b)*19) >> (16 + 4)
	if v > 0x0F {
		v = 0x0F
	}
	return byte(v)
}
