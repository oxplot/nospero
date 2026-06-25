package render

import (
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"math"
	"strings"

	"golang.org/x/image/draw"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"nospero/internal/protocol"
)

type Layout string

const (
	LayoutLeft  Layout = "left"
	LayoutRight Layout = "right"
	LayoutAbove Layout = "above"
	LayoutBelow Layout = "below"
)

type TextAlign string

const (
	TextAlignLeft   TextAlign = "left"
	TextAlignCenter TextAlign = "center"
	TextAlignRight  TextAlign = "right"
)

type Options struct {
	TapeWidthMM         float64
	HorizontalPaddingMM float64
	VerticalPaddingMM   float64
	GapMM               float64
	Layout              Layout
	TextFont            *opentype.Font
	TextAlign           TextAlign
	LineSpacing         float64
	FontWeight          int
	FontFaceWeight      int
	SyntheticItalic     bool
}

func DefaultOptions() Options {
	return Options{
		TapeWidthMM:         0,
		HorizontalPaddingMM: 5,
		VerticalPaddingMM:   0,
		GapMM:               2,
		Layout:              LayoutLeft,
		TextAlign:           TextAlignLeft,
		LineSpacing:         1,
		FontWeight:          400,
		FontFaceWeight:      400,
	}
}

func ParseLayout(value string) (Layout, error) {
	switch Layout(strings.ToLower(strings.TrimSpace(value))) {
	case LayoutLeft:
		return LayoutLeft, nil
	case LayoutRight:
		return LayoutRight, nil
	case LayoutAbove:
		return LayoutAbove, nil
	case LayoutBelow:
		return LayoutBelow, nil
	case "":
		return LayoutLeft, nil
	default:
		return "", fmt.Errorf("unsupported layout %q (use left, right, above, or below)", value)
	}
}

func ParseTextAlign(value string) (TextAlign, error) {
	switch TextAlign(strings.ToLower(strings.TrimSpace(value))) {
	case TextAlignLeft:
		return TextAlignLeft, nil
	case TextAlignCenter:
		return TextAlignCenter, nil
	case TextAlignRight:
		return TextAlignRight, nil
	case "":
		return TextAlignLeft, nil
	default:
		return "", fmt.Errorf("unsupported text-align %q (use left, center, or right)", value)
	}
}

func ValidateLineSpacing(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return fmt.Errorf("line-spacing must be positive, got %v", value)
	}
	return nil
}

func ValidateFontWeight(value int) error {
	if value < 100 || value > 900 {
		return fmt.Errorf("font-weight must be between 100 and 900")
	}
	return nil
}

func TextLabel(text string, opts Options) (*image.Gray, error) {
	canvas, content, err := newLabelCanvas(opts, func(contentHeight int) (int, error) {
		size, err := textSizeForHeight(text, opts.textStyle(), contentHeight)
		if err != nil {
			return 0, err
		}
		return size.X, nil
	})
	if err != nil {
		return nil, err
	}
	if err := drawText(canvas, content, text, opts.textStyle(), opts.TextAlign); err != nil {
		return nil, err
	}
	return canvas, nil
}

func ImageLabel(src image.Image, opts Options) (*image.Gray, error) {
	canvas, content, err := newLabelCanvas(opts, func(contentHeight int) (int, error) {
		size, err := imageSizeForHeight(src, contentHeight)
		if err != nil {
			return 0, err
		}
		return size.X, nil
	})
	if err != nil {
		return nil, err
	}
	if err := drawImage(canvas, content, src); err != nil {
		return nil, err
	}
	return canvas, nil
}

func MixedLabel(text string, src image.Image, opts Options) (*image.Gray, error) {
	gap, err := nonNegativeDots(opts.GapMM)
	if err != nil {
		return nil, err
	}
	canvas, content, err := newLabelCanvas(opts, func(contentHeight int) (int, error) {
		return mixedContentWidth(text, src, opts, contentHeight, gap)
	})
	if err != nil {
		return nil, err
	}

	var imageRect, textRect image.Rectangle
	if opts.Layout == LayoutLeft || opts.Layout == LayoutRight {
		imageSize, textSize, err := horizontalMixedSizes(text, src, opts, content.Dy())
		if err != nil {
			return nil, err
		}
		imageRect, textRect, err = splitHorizontalContentByWidth(content, opts.Layout, gap, imageSize.X, textSize.X)
	} else {
		imageRect, textRect, err = splitContent(content, opts.Layout, gap)
	}
	if err != nil {
		return nil, err
	}
	if err := drawImage(canvas, imageRect, src); err != nil {
		return nil, err
	}
	if err := drawText(canvas, textRect, text, opts.textStyle(), opts.TextAlign); err != nil {
		return nil, err
	}
	return canvas, nil
}

func (opts Options) textStyle() textStyle {
	weight := opts.FontWeight
	if weight == 0 {
		weight = 400
	}
	faceWeight := opts.FontFaceWeight
	if faceWeight == 0 {
		faceWeight = 400
	}
	return textStyle{
		font:            opts.TextFont,
		lineSpacing:     opts.LineSpacing,
		weight:          weight,
		faceWeight:      faceWeight,
		syntheticItalic: opts.SyntheticItalic,
	}
}

func newLabelCanvas(opts Options, autoContentWidth func(contentHeight int) (int, error)) (*image.Gray, image.Rectangle, error) {
	tapeWidth, err := protocol.TapeWidthDotsFromMM(opts.TapeWidthMM)
	if err != nil {
		return nil, image.Rectangle{}, fmt.Errorf("invalid tape width: %w", err)
	}
	horizontalPadding, err := nonNegativeDots(opts.HorizontalPaddingMM)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	verticalPadding, err := nonNegativeDots(opts.VerticalPaddingMM)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	if tapeWidth <= verticalPadding*2 {
		return nil, image.Rectangle{}, fmt.Errorf("vertical padding leaves no printable area")
	}
	contentHeight := tapeWidth - verticalPadding*2

	length, err := labelLengthDots(contentHeight, horizontalPadding, autoContentWidth)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	if length <= horizontalPadding*2 {
		return nil, image.Rectangle{}, fmt.Errorf("horizontal padding leaves no printable area")
	}

	canvas := image.NewGray(image.Rect(0, 0, length, tapeWidth))
	fillWhite(canvas)
	return canvas, image.Rect(horizontalPadding, verticalPadding, length-horizontalPadding, tapeWidth-verticalPadding), nil
}

func labelLengthDots(contentHeight, horizontalPadding int, autoContentWidth func(contentHeight int) (int, error)) (int, error) {
	if autoContentWidth == nil {
		return 0, fmt.Errorf("label length requires content sizing")
	}
	contentWidth, err := autoContentWidth(contentHeight)
	if err != nil {
		return 0, err
	}
	if contentWidth <= 0 {
		return 0, fmt.Errorf("content produced an empty label length")
	}
	return contentWidth + horizontalPadding*2, nil
}

func nonNegativeDots(mm float64) (int, error) {
	if math.IsNaN(mm) || math.IsInf(mm, 0) || mm < 0 {
		return 0, fmt.Errorf("millimeters must be non-negative, got %v", mm)
	}
	return int(math.Round(mm * protocol.DPI / 25.4)), nil
}

func fillWhite(dst *image.Gray) {
	for i := range dst.Pix {
		dst.Pix[i] = 0xff
	}
}

func drawText(dst *image.Gray, rect image.Rectangle, text string, style textStyle, align TextAlign) error {
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return fmt.Errorf("text rectangle is empty")
	}

	layout, err := textLayoutForHeight(text, style, rect.Dy())
	if err != nil {
		return err
	}
	defer layout.face.Close()
	if layout.width > rect.Dx() || layout.height > rect.Dy() {
		return fmt.Errorf("text does not fit in label content area")
	}

	layer := image.NewGray(image.Rect(0, 0, rect.Dx(), layout.height))
	fillWhite(layer)
	baseRect := image.Rect(layout.weightPad, 0, rect.Dx()-layout.italicExtra-layout.weightPad, layout.height)
	if baseRect.Dx() < layout.baseWidth {
		return fmt.Errorf("text does not fit in label content area")
	}
	drawer := &xfont.Drawer{
		Dst:  layer,
		Src:  image.Black,
		Face: layout.face,
	}
	for i, line := range layout.lines {
		x := alignedTextX(baseRect, fixedCeil(layout.lineWidths[i]), align)
		drawer.Dot = fixed.Point26_6{
			X: fixed.I(x),
			Y: layout.metrics.Ascent + fixed.Int26_6(i)*layout.baselineStep,
		}
		drawer.DrawString(line)
	}
	if layout.weightRadius != 0 {
		layer = applySyntheticWeight(layer, layout.weightRadius)
	}
	if layout.italicExtra > 0 {
		layer = applySyntheticItalic(layer, layout.italicExtra)
	}
	top := rect.Min.Y + (rect.Dy()-layout.height)/2
	copyTextLayer(dst, image.Pt(rect.Min.X, top), layer)
	return nil
}

func drawImage(dst *image.Gray, rect image.Rectangle, src image.Image) error {
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return fmt.Errorf("image rectangle is empty")
	}
	sb, err := imageBounds(src)
	if err != nil {
		return err
	}

	scale := math.Min(float64(rect.Dx())/float64(sb.Dx()), float64(rect.Dy())/float64(sb.Dy()))
	w := max(1, int(math.Round(float64(sb.Dx())*scale)))
	h := max(1, int(math.Round(float64(sb.Dy())*scale)))
	target := centerRect(rect, w, h)
	draw.CatmullRom.Scale(dst, target, src, sb, imagedraw.Over, nil)
	return nil
}

func textSizeForHeight(text string, style textStyle, height int) (image.Point, error) {
	if height <= 0 {
		return image.Point{}, fmt.Errorf("text height must be positive")
	}
	layout, err := textLayoutForHeight(text, style, height)
	if err != nil {
		return image.Point{}, err
	}
	defer layout.face.Close()
	return image.Pt(layout.width, layout.height), nil
}

func imageSizeForHeight(src image.Image, height int) (image.Point, error) {
	if height <= 0 {
		return image.Point{}, fmt.Errorf("image height must be positive")
	}
	sb, err := imageBounds(src)
	if err != nil {
		return image.Point{}, err
	}
	scale := float64(height) / float64(sb.Dy())
	return image.Pt(max(1, int(math.Round(float64(sb.Dx())*scale))), height), nil
}

func imageBounds(src image.Image) (image.Rectangle, error) {
	if src == nil {
		return image.Rectangle{}, fmt.Errorf("image must not be nil")
	}
	sb := src.Bounds()
	if sb.Dx() <= 0 || sb.Dy() <= 0 {
		return image.Rectangle{}, fmt.Errorf("image has empty bounds")
	}
	return sb, nil
}

func mixedContentWidth(text string, src image.Image, opts Options, contentHeight, gap int) (int, error) {
	switch opts.Layout {
	case LayoutLeft, LayoutRight:
		imageSize, textSize, err := horizontalMixedSizes(text, src, opts, contentHeight)
		if err != nil {
			return 0, err
		}
		return imageSize.X + gap + textSize.X, nil
	case LayoutAbove, LayoutBelow:
		firstHeight, secondHeight, err := splitVerticalHeights(contentHeight, gap)
		if err != nil {
			return 0, err
		}
		imageHeight, textHeight := firstHeight, secondHeight
		if opts.Layout == LayoutBelow {
			imageHeight, textHeight = secondHeight, firstHeight
		}
		imageSize, err := imageSizeForHeight(src, imageHeight)
		if err != nil {
			return 0, err
		}
		textSize, err := textSizeForHeight(text, opts.textStyle(), textHeight)
		if err != nil {
			return 0, err
		}
		return max(imageSize.X, textSize.X), nil
	default:
		return 0, fmt.Errorf("unsupported layout %q", opts.Layout)
	}
}

func horizontalMixedSizes(text string, src image.Image, opts Options, contentHeight int) (image.Point, image.Point, error) {
	imageSize, err := imageSizeForHeight(src, contentHeight)
	if err != nil {
		return image.Point{}, image.Point{}, err
	}
	textSize, err := textSizeForHeight(text, opts.textStyle(), contentHeight)
	if err != nil {
		return image.Point{}, image.Point{}, err
	}
	return imageSize, textSize, nil
}

func splitContent(rect image.Rectangle, layout Layout, gap int) (image.Rectangle, image.Rectangle, error) {
	switch layout {
	case LayoutLeft, LayoutRight:
		if rect.Dx() <= gap+2 {
			return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("gap leaves no horizontal content area")
		}
		firstWidth := (rect.Dx() - gap) / 2
		left := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+firstWidth, rect.Max.Y)
		right := image.Rect(left.Max.X+gap, rect.Min.Y, rect.Max.X, rect.Max.Y)
		if layout == LayoutRight {
			return right, left, nil
		}
		return left, right, nil
	case LayoutAbove, LayoutBelow:
		if rect.Dy() <= gap+2 {
			return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("gap leaves no vertical content area")
		}
		firstHeight := (rect.Dy() - gap) / 2
		top := image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+firstHeight)
		bottom := image.Rect(rect.Min.X, top.Max.Y+gap, rect.Max.X, rect.Max.Y)
		if layout == LayoutBelow {
			return bottom, top, nil
		}
		return top, bottom, nil
	default:
		return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("unsupported layout %q", layout)
	}
}

func splitHorizontalContentByWidth(rect image.Rectangle, layout Layout, gap, imageWidth, textWidth int) (image.Rectangle, image.Rectangle, error) {
	if imageWidth <= 0 || textWidth <= 0 {
		return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("mixed content has empty bounds")
	}
	if rect.Dx() < imageWidth+gap+textWidth {
		return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("mixed content does not fit in label content area")
	}

	switch layout {
	case LayoutRight:
		textRect := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+textWidth, rect.Max.Y)
		imageRect := image.Rect(textRect.Max.X+gap, rect.Min.Y, textRect.Max.X+gap+imageWidth, rect.Max.Y)
		return imageRect, textRect, nil
	case LayoutLeft:
		imageRect := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+imageWidth, rect.Max.Y)
		textRect := image.Rect(imageRect.Max.X+gap, rect.Min.Y, imageRect.Max.X+gap+textWidth, rect.Max.Y)
		return imageRect, textRect, nil
	default:
		return image.Rectangle{}, image.Rectangle{}, fmt.Errorf("unsupported layout %q", layout)
	}
}

func splitVerticalHeights(contentHeight, gap int) (int, int, error) {
	if contentHeight <= gap+2 {
		return 0, 0, fmt.Errorf("gap leaves no vertical content area")
	}
	firstHeight := (contentHeight - gap) / 2
	return firstHeight, contentHeight - firstHeight - gap, nil
}

func centerRect(container image.Rectangle, width, height int) image.Rectangle {
	x := container.Min.X + (container.Dx()-width)/2
	y := container.Min.Y + (container.Dy()-height)/2
	return image.Rect(x, y, x+width, y+height)
}

type textLayout struct {
	lines        []string
	face         xfont.Face
	metrics      xfont.Metrics
	lineWidths   []fixed.Int26_6
	baselineStep fixed.Int26_6
	baseWidth    int
	width        int
	height       int
	weightPad    int
	weightRadius int
	italicExtra  int
}

type textStyle struct {
	font            *opentype.Font
	lineSpacing     float64
	weight          int
	faceWeight      int
	syntheticItalic bool
}

func textLayoutForHeight(text string, style textStyle, height int) (*textLayout, error) {
	if style.font == nil {
		return nil, fmt.Errorf("font is required")
	}
	if err := ValidateLineSpacing(style.lineSpacing); err != nil {
		return nil, err
	}
	lines, err := splitTextLines(text)
	if err != nil {
		return nil, err
	}
	size, err := fitFontSize(style.font, len(lines), style.lineSpacing, height)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(style.font, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: xfont.HintingFull,
	})
	if err != nil {
		return nil, err
	}

	metrics := face.Metrics()
	baselineStep, blockHeight, err := textBlockHeight(metrics, len(lines), style.lineSpacing)
	if err != nil {
		_ = face.Close()
		return nil, err
	}
	lineWidths := make([]fixed.Int26_6, len(lines))
	maxWidth := fixed.Int26_6(0)
	drawer := &xfont.Drawer{Face: face}
	for i, line := range lines {
		width := drawer.MeasureString(line)
		lineWidths[i] = width
		if width > maxWidth {
			maxWidth = width
		}
	}
	if maxWidth <= 0 {
		_ = face.Close()
		return nil, fmt.Errorf("text produced an empty render")
	}
	blockHeightDots := fixedCeil(blockHeight)
	baseWidth := fixedCeil(maxWidth)
	weightRadius := syntheticWeightRadius(style.weight, style.faceWeight)
	weightPad := absInt(weightRadius)
	italicExtra := 0
	if style.syntheticItalic {
		italicExtra = syntheticItalicExtra(blockHeightDots)
	}
	return &textLayout{
		lines:        lines,
		face:         face,
		metrics:      metrics,
		lineWidths:   lineWidths,
		baselineStep: baselineStep,
		baseWidth:    baseWidth,
		width:        baseWidth + weightPad*2 + italicExtra,
		height:       blockHeightDots,
		weightPad:    weightPad,
		weightRadius: weightRadius,
		italicExtra:  italicExtra,
	}, nil
}

func fitFontSize(textFont *opentype.Font, lineCount int, lineSpacing float64, height int) (float64, error) {
	low := 0.1
	lowHeight, err := textBlockHeightAtSize(textFont, low, lineCount, lineSpacing)
	if err != nil {
		return 0, err
	}
	if fixedCeil(lowHeight) > height {
		return 0, fmt.Errorf("text does not fit in label content area")
	}

	high := math.Max(1, float64(height)*2)
	for i := 0; i < 32; i++ {
		mid := (low + high) / 2
		blockHeight, err := textBlockHeightAtSize(textFont, mid, lineCount, lineSpacing)
		if err != nil {
			return 0, err
		}
		if fixedCeil(blockHeight) <= height {
			low = mid
		} else {
			high = mid
		}
	}
	return low, nil
}

func textBlockHeightAtSize(textFont *opentype.Font, size float64, lineCount int, lineSpacing float64) (fixed.Int26_6, error) {
	face, err := opentype.NewFace(textFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: xfont.HintingFull,
	})
	if err != nil {
		return 0, err
	}
	defer face.Close()
	_, blockHeight, err := textBlockHeight(face.Metrics(), lineCount, lineSpacing)
	return blockHeight, err
}

func textBlockHeight(metrics xfont.Metrics, lineCount int, lineSpacing float64) (fixed.Int26_6, fixed.Int26_6, error) {
	if lineCount <= 0 {
		return 0, 0, fmt.Errorf("text produced no lines")
	}
	lineBody := metrics.Ascent + metrics.Descent
	lineBox := metrics.Height
	if lineBox <= 0 {
		lineBox = lineBody
	}
	if lineBody <= 0 || lineBox <= 0 {
		return 0, 0, fmt.Errorf("font produced invalid metrics")
	}
	baselineStep := fixed.Int26_6(math.Round(float64(lineBox) * lineSpacing))
	if baselineStep <= 0 {
		return 0, 0, fmt.Errorf("line-spacing produced invalid baseline distance")
	}
	blockHeight := lineBody + fixed.Int26_6(lineCount-1)*baselineStep
	return baselineStep, blockHeight, nil
}

func splitTextLines(text string) ([]string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text must not be empty")
	}
	return strings.Split(text, "\n"), nil
}

func alignedTextX(rect image.Rectangle, width int, align TextAlign) int {
	switch align {
	case TextAlignCenter:
		return rect.Min.X + (rect.Dx()-width)/2
	case TextAlignRight:
		return rect.Max.X - width
	default:
		return rect.Min.X
	}
}

func fixedCeil(value fixed.Int26_6) int {
	if value <= 0 {
		return 0
	}
	return (int(value) + 63) >> 6
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func syntheticWeightRadius(requested, face int) int {
	delta := requested - face
	switch {
	case delta >= 450:
		return 2
	case delta >= 150:
		return 1
	case delta <= -450:
		return -2
	case delta <= -150:
		return -1
	default:
		return 0
	}
}

func syntheticItalicExtra(height int) int {
	return int(math.Ceil(float64(height) * 0.22))
}

func applySyntheticWeight(src *image.Gray, radius int) *image.Gray {
	if radius == 0 {
		return src
	}
	if radius > 0 {
		return dilateText(src, radius)
	}
	return erodeText(src, -radius)
}

func dilateText(src *image.Gray, radius int) *image.Gray {
	dst := image.NewGray(src.Bounds())
	fillWhite(dst)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !hasBlackNeighbor(src, x, y, radius) {
				continue
			}
			dst.SetGray(x, y, color.Gray{Y: 0})
		}
	}
	return dst
}

func erodeText(src *image.Gray, radius int) *image.Gray {
	dst := image.NewGray(src.Bounds())
	fillWhite(dst)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !srcBlack(src, x, y) || hasWhiteNeighbor(src, x, y, radius) {
				continue
			}
			dst.SetGray(x, y, color.Gray{Y: 0})
		}
	}
	return dst
}

func hasBlackNeighbor(img *image.Gray, x, y, radius int) bool {
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if srcBlack(img, x+dx, y+dy) {
				return true
			}
		}
	}
	return false
}

func hasWhiteNeighbor(img *image.Gray, x, y, radius int) bool {
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if !srcBlack(img, x+dx, y+dy) {
				return true
			}
		}
	}
	return false
}

func srcBlack(img *image.Gray, x, y int) bool {
	if !image.Pt(x, y).In(img.Bounds()) {
		return false
	}
	return img.GrayAt(x, y).Y < protocol.BlackThreshold
}

func applySyntheticItalic(src *image.Gray, extra int) *image.Gray {
	dst := image.NewGray(src.Bounds())
	fillWhite(dst)
	b := src.Bounds()
	height := max(1, b.Dy()-1)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		offset := int(math.Round(float64(extra) * float64(b.Max.Y-y-1) / float64(height)))
		for x := b.Min.X; x < b.Max.X; x++ {
			if !srcBlack(src, x, y) {
				continue
			}
			targetX := x + offset
			if targetX >= b.Max.X {
				continue
			}
			dst.SetGray(targetX, y, color.Gray{Y: 0})
		}
	}
	return dst
}

func copyTextLayer(dst *image.Gray, at image.Point, src *image.Gray) {
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			gray := src.GrayAt(x, y)
			if gray.Y == 0xff {
				continue
			}
			p := image.Pt(at.X+x-b.Min.X, at.Y+y-b.Min.Y)
			if p.In(dst.Bounds()) {
				dst.SetGray(p.X, p.Y, gray)
			}
		}
	}
}
