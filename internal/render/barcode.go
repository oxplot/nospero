package render

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	extbarcode "github.com/boombuler/barcode"
	"github.com/boombuler/barcode/aztec"
	"github.com/boombuler/barcode/codabar"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/code39"
	"github.com/boombuler/barcode/code93"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/pdf417"
	"github.com/boombuler/barcode/qr"
	"github.com/boombuler/barcode/twooffive"

	"nospero/internal/protocol"
)

type BarcodeKind string

const (
	BarcodeAztec               BarcodeKind = "aztec"
	BarcodeCodabar             BarcodeKind = "codabar"
	BarcodeCode128             BarcodeKind = "code128"
	BarcodeCode39              BarcodeKind = "code39"
	BarcodeCode93              BarcodeKind = "code93"
	BarcodeDataMatrix          BarcodeKind = "datamatrix"
	BarcodeEAN8                BarcodeKind = "ean8"
	BarcodeEAN13               BarcodeKind = "ean13"
	BarcodePDF417              BarcodeKind = "pdf417"
	BarcodeQR                  BarcodeKind = "qr"
	Barcode2of5                BarcodeKind = "2of5"
	Barcode2of5Interleaved     BarcodeKind = "2of5-interleaved"
	default1DBarcodeModuleDots             = 3
	aztecQuietZoneModules                  = 1
	dataMatrixQuietZoneModules             = 1
	pdf417QuietZoneModules                 = 2
	qrQuietZoneModules                     = 4
	maxBarcodeModuleDots                   = 64
)

type BarcodeOptions struct {
	Kind       BarcodeKind
	ModuleDots int
}

func DefaultBarcodeOptions() BarcodeOptions {
	return BarcodeOptions{
		Kind:       BarcodeCode128,
		ModuleDots: 0,
	}
}

func SupportedBarcodeKindNames() []string {
	return []string{
		string(BarcodeAztec),
		string(BarcodeCodabar),
		string(BarcodeCode128),
		string(BarcodeCode39),
		string(BarcodeCode93),
		string(BarcodeDataMatrix),
		string(BarcodeEAN8),
		string(BarcodeEAN13),
		string(BarcodePDF417),
		string(BarcodeQR),
		string(Barcode2of5),
		string(Barcode2of5Interleaved),
	}
}

func ParseBarcodeKind(value string) (BarcodeKind, error) {
	switch normalizeBarcodeKind(value) {
	case "", "code128", "code-128", "c128":
		return BarcodeCode128, nil
	case "aztec":
		return BarcodeAztec, nil
	case "codabar":
		return BarcodeCodabar, nil
	case "code39", "code-39", "c39":
		return BarcodeCode39, nil
	case "code93", "code-93", "c93":
		return BarcodeCode93, nil
	case "datamatrix", "data-matrix", "dm":
		return BarcodeDataMatrix, nil
	case "ean8", "ean-8":
		return BarcodeEAN8, nil
	case "ean13", "ean-13":
		return BarcodeEAN13, nil
	case "pdf417", "pdf-417", "pdf":
		return BarcodePDF417, nil
	case "qr", "qrcode", "qr-code":
		return BarcodeQR, nil
	case "2of5", "2-of-5", "twoof5", "two-of-5", "twooffive", "two-of-five":
		return Barcode2of5, nil
	case "2of5-interleaved", "2-of-5-interleaved", "interleaved-2of5", "interleaved-2-of-5", "itf", "i2of5":
		return Barcode2of5Interleaved, nil
	default:
		return "", fmt.Errorf("unsupported barcode type %q (use %s)", value, strings.Join(SupportedBarcodeKindNames(), ", "))
	}
}

func ValidateBarcodeModuleDots(value int) error {
	if value < 0 || value > maxBarcodeModuleDots {
		return fmt.Errorf("module-dots must be between 0 and %d", maxBarcodeModuleDots)
	}
	return nil
}

func BarcodeLabel(data string, barcodeOpts BarcodeOptions, opts Options) (*image.Gray, error) {
	if err := ValidateBarcodeModuleDots(barcodeOpts.ModuleDots); err != nil {
		return nil, err
	}

	var scaled image.Image
	canvas, content, err := newLabelCanvas(opts, func(contentHeight int) (int, error) {
		code, err := sizedBarcodeForHeight(data, barcodeOpts, contentHeight)
		if err != nil {
			return 0, err
		}
		scaled = code
		return code.Bounds().Dx(), nil
	})
	if err != nil {
		return nil, err
	}
	if scaled == nil {
		return nil, fmt.Errorf("barcode produced an empty render")
	}
	if err := drawBarcode(canvas, content, scaled); err != nil {
		return nil, err
	}
	return canvas, nil
}

func normalizeBarcodeKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func sizedBarcodeForHeight(data string, opts BarcodeOptions, height int) (image.Image, error) {
	if height <= 0 {
		return nil, fmt.Errorf("barcode height must be positive")
	}
	code, err := encodeBarcode(data, opts.Kind)
	if err != nil {
		return nil, err
	}
	return scaleBarcodeForHeight(code, opts.ModuleDots, height)
}

func encodeBarcode(data string, kind BarcodeKind) (extbarcode.Barcode, error) {
	if strings.TrimSpace(data) == "" {
		return nil, fmt.Errorf("barcode data must not be empty")
	}
	if kind == "" {
		kind = DefaultBarcodeOptions().Kind
	}

	var (
		code extbarcode.Barcode
		err  error
	)
	switch kind {
	case BarcodeAztec:
		code, err = aztec.Encode([]byte(data), aztec.DEFAULT_EC_PERCENT, aztec.DEFAULT_LAYERS)
	case BarcodeCodabar:
		code, err = codabar.Encode(data)
	case BarcodeCode128:
		code, err = code128.Encode(data)
	case BarcodeCode39:
		code, err = code39.Encode(data, false, true)
	case BarcodeCode93:
		code, err = code93.Encode(data, true, true)
	case BarcodeDataMatrix:
		code, err = datamatrix.Encode(data)
	case BarcodeEAN8, BarcodeEAN13:
		code, err = encodeEANBarcode(data, kind)
	case BarcodePDF417:
		code, err = pdf417.Encode(data, 2)
	case BarcodeQR:
		code, err = qr.Encode(data, qr.M, qr.Auto)
	case Barcode2of5:
		code, err = twooffive.Encode(data, false)
	case Barcode2of5Interleaved:
		code, err = twooffive.Encode(data, true)
	default:
		return nil, fmt.Errorf("unsupported barcode type %q (use %s)", kind, strings.Join(SupportedBarcodeKindNames(), ", "))
	}
	if err != nil {
		return nil, fmt.Errorf("encode %s barcode: %w", kind, err)
	}
	return code, nil
}

func encodeEANBarcode(data string, kind BarcodeKind) (extbarcode.Barcode, error) {
	if !asciiDigits(data) {
		return nil, fmt.Errorf("%s barcode data must contain only digits", kind)
	}
	switch kind {
	case BarcodeEAN8:
		if len(data) != 7 && len(data) != 8 {
			return nil, fmt.Errorf("ean8 barcode data must be 7 or 8 digits")
		}
	case BarcodeEAN13:
		if len(data) != 12 && len(data) != 13 {
			return nil, fmt.Errorf("ean13 barcode data must be 12 or 13 digits")
		}
	}
	code, err := ean.Encode(data)
	if err != nil {
		return nil, err
	}
	return code, nil
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func scaleBarcodeForHeight(code extbarcode.Barcode, moduleDots, contentHeight int) (image.Image, error) {
	bounds := code.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("barcode produced empty bounds")
	}

	switch code.Metadata().Dimensions {
	case 1:
		if moduleDots == 0 {
			moduleDots = default1DBarcodeModuleDots
		}
		return extbarcode.ScaleWithFill(code, width*moduleDots, contentHeight, color.White)
	case 2:
		quietZoneModules := barcodeQuietZoneModules(code)
		totalHeightModules := height + quietZoneModules*2
		if moduleDots == 0 {
			moduleDots = contentHeight / totalHeightModules
			if moduleDots == 0 {
				return nil, fmt.Errorf("barcode is %d modules tall with quiet zone and does not fit in %d-dot content height", totalHeightModules, contentHeight)
			}
		}
		totalHeight := totalHeightModules * moduleDots
		if totalHeight > contentHeight {
			return nil, fmt.Errorf("barcode is %d dots tall with quiet zone and module-dots=%d, and does not fit in %d-dot content height", totalHeight, moduleDots, contentHeight)
		}
		scaled, err := extbarcode.ScaleWithFill(code, width*moduleDots, height*moduleDots, color.White)
		if err != nil {
			return nil, err
		}
		return addWhiteBorder(scaled, quietZoneModules*moduleDots), nil
	default:
		return nil, fmt.Errorf("unsupported barcode dimension %d", code.Metadata().Dimensions)
	}
}

// barcodeQuietZoneModules returns the minimum white border, in modules,
// required by the encoded 2D symbology before label-level padding is considered.
func barcodeQuietZoneModules(code extbarcode.Barcode) int {
	switch code.Metadata().CodeKind {
	case extbarcode.TypeAztec:
		return aztecQuietZoneModules
	case extbarcode.TypeDataMatrix:
		return dataMatrixQuietZoneModules
	case extbarcode.TypePDF:
		return pdf417QuietZoneModules
	case extbarcode.TypeQR:
		return qrQuietZoneModules
	default:
		return 0
	}
}

func addWhiteBorder(src image.Image, border int) image.Image {
	if border <= 0 {
		return src
	}
	sb := src.Bounds()
	dst := image.NewGray(image.Rect(0, 0, sb.Dx()+border*2, sb.Dy()+border*2))
	fillWhite(dst)
	copyBarcodeInk(dst, image.Pt(border, border), src)
	return dst
}

func drawBarcode(dst *image.Gray, rect image.Rectangle, src image.Image) error {
	sb, err := imageBounds(src)
	if err != nil {
		return err
	}
	if sb.Dx() > rect.Dx() || sb.Dy() > rect.Dy() {
		return fmt.Errorf("barcode does not fit in label content area")
	}
	target := centerRect(rect, sb.Dx(), sb.Dy())
	copyBarcodeInk(dst, target.Min, src)
	return nil
}

func copyBarcodeInk(dst *image.Gray, at image.Point, src image.Image) {
	sb := src.Bounds()
	for y := 0; y < sb.Dy(); y++ {
		for x := 0; x < sb.Dx(); x++ {
			gray := color.GrayModel.Convert(src.At(sb.Min.X+x, sb.Min.Y+y)).(color.Gray)
			if gray.Y >= protocol.BlackThreshold {
				continue
			}
			dst.SetGray(at.X+x, at.Y+y, color.Gray{Y: 0})
		}
	}
}
