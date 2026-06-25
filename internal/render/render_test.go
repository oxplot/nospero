package render

import (
	"image"
	"image/color"
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
