package protocol

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

const (
	DPI            = 180
	BlackThreshold = 140
)

func DotsFromMM(mm float64) (int, error) {
	if math.IsNaN(mm) || math.IsInf(mm, 0) || mm <= 0 {
		return 0, fmt.Errorf("millimeters must be positive, got %v", mm)
	}
	return int(math.Round(mm * DPI / 25.4)), nil
}

func RasterPage(img image.Image) ([]byte, error) {
	return RasterPageWithMargins(img, 0)
}

func RasterPageWithMargins(img image.Image, marginDots int) ([]byte, error) {
	b := img.Bounds()
	length := b.Dx()
	tapeWidth := b.Dy()
	if length <= 0 || tapeWidth <= 0 {
		return nil, fmt.Errorf("image must have positive dimensions, got %dx%d", length, tapeWidth)
	}
	if tapeWidth > 0xffff {
		return nil, fmt.Errorf("image tape width exceeds LW raster limit: %d dots", tapeWidth)
	}
	if marginDots < 0 {
		return nil, fmt.Errorf("margin must be non-negative, got %d", marginDots)
	}

	line, err := RasterLineCommand(tapeWidth)
	if err != nil {
		return nil, err
	}

	rowBytes := PackedRowBytes(tapeWidth)
	out := make([]byte, 0, (length+marginDots*2)*(len(line)+rowBytes))
	blank := make([]byte, rowBytes)
	for range marginDots {
		out = append(out, line...)
		out = append(out, blank...)
	}
	for x := b.Min.X; x < b.Max.X; x++ {
		out = append(out, line...)
		out = append(out, PackRasterFeedRow(img, x)...)
	}
	for range marginDots {
		out = append(out, line...)
		out = append(out, blank...)
	}
	return out, nil
}

func PackedRowBytes(widthDots int) int {
	if widthDots <= 0 {
		return 0
	}
	return (widthDots + 7) / 8
}

// PackRasterRow packs one horizontal row MSB-first.
// Gray values below 140 are printed as black.
func PackRasterRow(img image.Image, y int) []byte {
	b := img.Bounds()
	width := b.Dx()
	out := make([]byte, PackedRowBytes(width))

	for x := b.Min.X; x < b.Max.X; x++ {
		gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
		if gray >= BlackThreshold {
			continue
		}
		dx := x - b.Min.X
		out[dx/8] |= 0x80 >> uint(dx%8)
	}
	return out
}

// PackRasterFeedRow packs one printer-feed row from a logical label image.
// The logical Y axis is inverted to match the rotated print-head coordinate
// system used when raster rows are sent to the printer.
func PackRasterFeedRow(img image.Image, x int) []byte {
	b := img.Bounds()
	width := b.Dy()
	out := make([]byte, PackedRowBytes(width))

	for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
		gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
		if gray >= BlackThreshold {
			continue
		}
		dy := b.Max.Y - 1 - y
		out[dy/8] |= 0x80 >> uint(dy%8)
	}
	return out
}
