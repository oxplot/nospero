package protocol

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestPackRasterRowUsesMSBFirstThreshold(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 10, 1))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(3, 0, color.Gray{Y: BlackThreshold - 1})
	img.SetGray(8, 0, color.Gray{Y: 0})
	img.SetGray(9, 0, color.Gray{Y: BlackThreshold})

	got := PackRasterRow(img, 0)
	want := []byte{0x90, 0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %08b want %08b", got, want)
	}
}

func TestPackRasterFeedRowUsesRotatedTapeAxis(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 10))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetGray(0, 9, color.Gray{Y: 0})
	img.SetGray(0, 6, color.Gray{Y: BlackThreshold - 1})
	img.SetGray(0, 1, color.Gray{Y: 0})
	img.SetGray(0, 0, color.Gray{Y: BlackThreshold})

	got := PackRasterFeedRow(img, 0)
	want := []byte{0x90, 0x80}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %08b want %08b", got, want)
	}
}

func TestRasterPagePrefixesEveryFeedRow(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetGray(0, 7, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 0})

	got, err := RasterPage(img)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x80,
		0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x01,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestRasterPageWithMarginsAddsBlankFeedRows(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 1, 8))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetGray(0, 7, color.Gray{Y: 0})

	got, err := RasterPageWithMargins(img, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x00,
		0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x80,
		0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}
