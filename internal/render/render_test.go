package render

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"

	"nospero/internal/protocol"
)

func TestTextLabelDimensionsAndInk(t *testing.T) {
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 24

	img, err := TextLabel("HELLO LW-600P", opts)
	if err != nil {
		t.Fatal(err)
	}
	wantW, _ := protocol.PrintableDotsForTapeWidthMM(24)
	horizontalPadding, _ := protocol.DotsFromMM(opts.HorizontalPaddingMM)
	if img.Bounds().Dx() <= horizontalPadding*2 || img.Bounds().Dy() != wantW {
		t.Fatalf("got %v, want content-derived length and %d-dot tape width", img.Bounds(), wantW)
	}
	if !hasBlackPixel(img) {
		t.Fatal("rendered text label has no black pixels")
	}
}

func TestImageLabelDefaultPaddingAndAutoLength(t *testing.T) {
	opts := DefaultOptions()
	opts.TapeWidthMM = 24
	src := image.NewGray(image.Rect(0, 0, 1, 1))

	img, err := ImageLabel(src, opts)
	if err != nil {
		t.Fatal(err)
	}

	tapeWidth, _ := protocol.PrintableDotsForTapeWidthMM(opts.TapeWidthMM)
	horizontalPadding, _ := protocol.DotsFromMM(opts.HorizontalPaddingMM)
	wantLength := tapeWidth + horizontalPadding*2
	if img.Bounds().Dx() != wantLength || img.Bounds().Dy() != tapeWidth {
		t.Fatalf("got %v, want %dx%d", img.Bounds(), wantLength, tapeWidth)
	}

	ink := blackBounds(img)
	if ink.Min.X != horizontalPadding || ink.Max.X != horizontalPadding+tapeWidth {
		t.Fatalf("got ink x bounds %v, want [%d,%d)", ink, horizontalPadding, horizontalPadding+tapeWidth)
	}
	if ink.Min.Y != 0 || ink.Max.Y != tapeWidth {
		t.Fatalf("got ink y bounds %v, want full tape height [0,%d)", ink, tapeWidth)
	}
}

func TestTextLabelDefaultLengthGrowsWithContent(t *testing.T) {
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 24

	short, err := TextLabel("A", opts)
	if err != nil {
		t.Fatal(err)
	}
	long, err := TextLabel("A MUCH LONGER LABEL", opts)
	if err != nil {
		t.Fatal(err)
	}

	if long.Bounds().Dx() <= short.Bounds().Dx() {
		t.Fatalf("got short=%v long=%v, want content-driven length to grow", short.Bounds(), long.Bounds())
	}
}

func TestTextLabelRunsAlongLabelLength(t *testing.T) {
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 9

	img, err := TextLabel("Hi there buddy boy", opts)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() <= img.Bounds().Dy() {
		t.Fatalf("got %v, want label length on horizontal axis", img.Bounds())
	}
	ink := blackBounds(img)
	if ink.Empty() || ink.Dx() < 100 {
		t.Fatalf("got ink bounds %v, want text to use available label length", ink)
	}
}

func TestTextLabelExplicitNewlinesAffectFontSize(t *testing.T) {
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 24
	tapeWidth, _ := protocol.PrintableDotsForTapeWidthMM(opts.TapeWidthMM)

	oneLine, err := textSizeForHeight("ABC", opts.textStyle(), tapeWidth)
	if err != nil {
		t.Fatal(err)
	}
	twoLines, err := textSizeForHeight("ABC\nABC", opts.textStyle(), tapeWidth)
	if err != nil {
		t.Fatal(err)
	}
	if twoLines.X >= oneLine.X {
		t.Fatalf("got one-line size %v and two-line size %v, want newline count to reduce font size", oneLine, twoLines)
	}
}

func TestSplitTextLinesPreservesExplicitEmptyLines(t *testing.T) {
	lines, err := splitTextLines("top\n\nbottom")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"top", "", "bottom"}
	if len(lines) != len(want) {
		t.Fatalf("got %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("got %q, want %q", lines, want)
		}
	}
}

func TestParseTextAlign(t *testing.T) {
	for _, value := range []string{"left", "center", "right", ""} {
		if _, err := ParseTextAlign(value); err != nil {
			t.Fatalf("ParseTextAlign(%q): %v", value, err)
		}
	}
	if _, err := ParseTextAlign("middle"); err == nil {
		t.Fatal("expected unsupported alignment error")
	}
}

func TestSyntheticFontWeightChangesInk(t *testing.T) {
	regular := testRenderOptions(t)
	regular.TapeWidthMM = 24
	regular.FontWeight = 400
	regular.FontFaceWeight = 400

	bold := regular
	bold.FontWeight = 900

	regularImg, err := TextLabel("Weight", regular)
	if err != nil {
		t.Fatal(err)
	}
	boldImg, err := TextLabel("Weight", bold)
	if err != nil {
		t.Fatal(err)
	}
	if blackPixelCount(boldImg) <= blackPixelCount(regularImg) {
		t.Fatalf("got regular=%d bold=%d black pixels, want bold to add ink", blackPixelCount(regularImg), blackPixelCount(boldImg))
	}
}

func TestSyntheticItalicAddsWidth(t *testing.T) {
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 24
	tapeWidth, _ := protocol.PrintableDotsForTapeWidthMM(opts.TapeWidthMM)

	normal, err := textSizeForHeight("Italic", opts.textStyle(), tapeWidth)
	if err != nil {
		t.Fatal(err)
	}
	opts.SyntheticItalic = true
	italic, err := textSizeForHeight("Italic", opts.textStyle(), tapeWidth)
	if err != nil {
		t.Fatal(err)
	}
	if italic.X <= normal.X {
		t.Fatalf("got normal=%v italic=%v, want synthetic italic to reserve slant width", normal, italic)
	}
}

func TestValidateFontWeight(t *testing.T) {
	for _, value := range []int{100, 400, 900} {
		if err := ValidateFontWeight(value); err != nil {
			t.Fatalf("ValidateFontWeight(%d): %v", value, err)
		}
	}
	for _, value := range []int{99, 901} {
		if err := ValidateFontWeight(value); err == nil {
			t.Fatalf("ValidateFontWeight(%d) unexpectedly succeeded", value)
		}
	}
}

func TestMixedLayoutsRender(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			src.Set(x, y, color.Black)
		}
	}
	opts := testRenderOptions(t)
	opts.TapeWidthMM = 24
	for _, layout := range []Layout{LayoutLeft, LayoutRight, LayoutAbove, LayoutBelow} {
		t.Run(string(layout), func(t *testing.T) {
			opts.Layout = layout
			img, err := MixedLabel("ABC", src, opts)
			if err != nil {
				t.Fatal(err)
			}
			if !hasBlackPixel(img) {
				t.Fatal("mixed label has no black pixels")
			}
		})
	}
}

func TestBarcodeLabelRendersSupportedKinds(t *testing.T) {
	tests := []struct {
		kind BarcodeKind
		data string
	}{
		{BarcodeAztec, "Aztec label 42"},
		{BarcodeCodabar, "A12345B"},
		{BarcodeCode128, "ASSET-42"},
		{BarcodeCode39, "Asset 42"},
		{BarcodeCode93, "Asset 42"},
		{BarcodeDataMatrix, "DM-42"},
		{BarcodeEAN8, "1234567"},
		{BarcodeEAN13, "590123412345"},
		{BarcodePDF417, "PDF417 label 42"},
		{BarcodeQR, "https://example.com/asset/42"},
		{Barcode2of5, "12345"},
		{Barcode2of5Interleaved, "123456"},
	}

	opts := DefaultOptions()
	opts.TapeWidthMM = 24
	wantTapeWidth, _ := protocol.PrintableDotsForTapeWidthMM(opts.TapeWidthMM)
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			img, err := BarcodeLabel(tt.data, BarcodeOptions{Kind: tt.kind}, opts)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dy() != wantTapeWidth {
				t.Fatalf("got %v, want %d-dot tape width", img.Bounds(), wantTapeWidth)
			}
			if !hasBlackPixel(img) {
				t.Fatal("rendered barcode label has no black pixels")
			}
		})
	}
}

func TestBarcodeLabelHonorsExplicitModuleDots(t *testing.T) {
	opts := DefaultOptions()
	opts.TapeWidthMM = 24
	opts.HorizontalPaddingMM = 0
	opts.VerticalPaddingMM = 0

	code, err := encodeBarcode("https://example.com/asset/42", BarcodeQR)
	if err != nil {
		t.Fatal(err)
	}
	moduleDots := 2
	img, err := BarcodeLabel("https://example.com/asset/42", BarcodeOptions{
		Kind:       BarcodeQR,
		ModuleDots: moduleDots,
	}, opts)
	if err != nil {
		t.Fatal(err)
	}

	wantWidth := (code.Bounds().Dx() + qrQuietZoneModules*2) * moduleDots
	if img.Bounds().Dx() != wantWidth {
		t.Fatalf("got label width %d, want %d", img.Bounds().Dx(), wantWidth)
	}
	assertQuietZone(t, img, qrQuietZoneModules*moduleDots)
}

func TestBarcodeLabelAdds2DQuietZoneWithDefaultScaling(t *testing.T) {
	tests := []struct {
		name             string
		kind             BarcodeKind
		data             string
		quietZoneModules int
	}{
		{
			name:             "aztec",
			kind:             BarcodeAztec,
			data:             "Aztec label 42",
			quietZoneModules: aztecQuietZoneModules,
		},
		{
			name:             "qr",
			kind:             BarcodeQR,
			data:             "https://example.com/asset/42",
			quietZoneModules: qrQuietZoneModules,
		},
		{
			name:             "datamatrix",
			kind:             BarcodeDataMatrix,
			data:             "DM-42",
			quietZoneModules: dataMatrixQuietZoneModules,
		},
		{
			name:             "pdf417",
			kind:             BarcodePDF417,
			data:             "PDF417 label 42",
			quietZoneModules: pdf417QuietZoneModules,
		},
	}

	opts := DefaultOptions()
	opts.TapeWidthMM = 24
	opts.HorizontalPaddingMM = 0
	opts.VerticalPaddingMM = 0
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := encodeBarcode(tt.data, tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			tapeWidth, ok := protocol.PrintableDotsForTapeWidthMM(opts.TapeWidthMM)
			if !ok {
				t.Fatalf("unsupported test tape width %g", opts.TapeWidthMM)
			}
			quietDots := tt.quietZoneModules * (tapeWidth / (code.Bounds().Dy() + tt.quietZoneModules*2))

			img, err := BarcodeLabel(tt.data, BarcodeOptions{Kind: tt.kind}, opts)
			if err != nil {
				t.Fatal(err)
			}
			assertQuietZone(t, img, quietDots)
		})
	}
}

func TestBarcodeLabelRejectsEAN13Nondigits(t *testing.T) {
	opts := DefaultOptions()
	opts.TapeWidthMM = 24

	_, err := BarcodeLabel("not-digits", BarcodeOptions{Kind: BarcodeEAN13}, opts)
	if err == nil {
		t.Fatal("expected ean13 digit validation error")
	}
	if !strings.Contains(err.Error(), "digits") {
		t.Fatalf("got %q, want digit validation error", err)
	}
}

func TestValidateBarcodeModuleDots(t *testing.T) {
	for _, value := range []int{0, 1, 64} {
		if err := ValidateBarcodeModuleDots(value); err != nil {
			t.Fatalf("ValidateBarcodeModuleDots(%d): %v", value, err)
		}
	}
	for _, value := range []int{-1, 65} {
		if err := ValidateBarcodeModuleDots(value); err == nil {
			t.Fatalf("ValidateBarcodeModuleDots(%d) unexpectedly succeeded", value)
		}
	}
}

func TestSplitHorizontalContentByWidthPreservesLayoutRightWidths(t *testing.T) {
	content := image.Rect(0, 0, 220, 100)

	imageRect, textRect, err := splitHorizontalContentByWidth(content, LayoutRight, 10, 120, 90)
	if err != nil {
		t.Fatal(err)
	}

	if imageRect.Dx() != 120 {
		t.Fatalf("got image rect %v, want 120-dot width", imageRect)
	}
	if textRect.Dx() != 90 {
		t.Fatalf("got text rect %v, want 90-dot width", textRect)
	}
	if textRect.Min.X != content.Min.X || imageRect.Max.X != content.Max.X {
		t.Fatalf("got image=%v text=%v, want text left and image right", imageRect, textRect)
	}
	if imageRect.Min.X-textRect.Max.X != 10 {
		t.Fatalf("got image=%v text=%v, want 10-dot gap", imageRect, textRect)
	}
}

func testRenderOptions(t *testing.T) Options {
	t.Helper()
	opts := DefaultOptions()
	font, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	opts.TextFont = font
	return opts
}

func hasBlackPixel(img *image.Gray) bool {
	return blackPixelCount(img) > 0
}

func blackPixelCount(img *image.Gray) int {
	count := 0
	for _, p := range img.Pix {
		if p < protocol.BlackThreshold {
			count++
		}
	}
	return count
}

func blackBounds(img *image.Gray) image.Rectangle {
	b := img.Bounds()
	out := image.Rectangle{
		Min: image.Point{X: b.Max.X, Y: b.Max.Y},
		Max: image.Point{X: b.Min.X, Y: b.Min.Y},
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.GrayAt(x, y).Y >= protocol.BlackThreshold {
				continue
			}
			if x < out.Min.X {
				out.Min.X = x
			}
			if y < out.Min.Y {
				out.Min.Y = y
			}
			if x+1 > out.Max.X {
				out.Max.X = x + 1
			}
			if y+1 > out.Max.Y {
				out.Max.Y = y + 1
			}
		}
	}
	if out.Min.X > out.Max.X || out.Min.Y > out.Max.Y {
		return image.Rectangle{}
	}
	return out
}

func assertQuietZone(t *testing.T, img *image.Gray, quietDots int) {
	t.Helper()
	ink := blackBounds(img)
	if ink.Empty() {
		t.Fatal("rendered barcode has no black pixels")
	}
	b := img.Bounds()
	if ink.Min.X < quietDots ||
		ink.Min.Y < quietDots ||
		b.Max.X-ink.Max.X < quietDots ||
		b.Max.Y-ink.Max.Y < quietDots {
		t.Fatalf("got ink bounds %v in image %v, want at least %d-dot quiet zone", ink, b, quietDots)
	}
}
