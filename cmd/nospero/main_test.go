package main

import (
	"flag"
	"image"
	"io"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"

	"nospero/internal/fonts"
	"nospero/internal/protocol"
	"nospero/internal/render"
)

func TestParsePrintOptionsDefaultsPaddingAndLeavesTapeWidthUnresolved(t *testing.T) {
	opts, err := parsePrintOptionsForArgs(t)
	if err != nil {
		t.Fatal(err)
	}

	if opts.TapeWidthMM != 0 {
		t.Fatalf("got tape width %gmm, want unresolved tape width", opts.TapeWidthMM)
	}
	if opts.HorizontalPaddingMM != 5 {
		t.Fatalf("got horizontal padding %gmm, want 5mm", opts.HorizontalPaddingMM)
	}
	if opts.VerticalPaddingMM != 0 {
		t.Fatalf("got vertical padding %gmm, want 0mm", opts.VerticalPaddingMM)
	}
}

func TestParsePrintOptionsAcceptsExplicitTapeWidth(t *testing.T) {
	opts, err := parsePrintOptionsForArgs(t, "--tape-width-mm", "18")
	if err != nil {
		t.Fatal(err)
	}

	if opts.TapeWidthMM != 18 {
		t.Fatalf("got tape width %gmm, want 18mm", opts.TapeWidthMM)
	}
}

func TestParsePrintOptionsRejectsUnsupportedExplicitTapeWidth(t *testing.T) {
	_, err := parsePrintOptionsForArgs(t, "--tape-width-mm", "7")
	if err == nil {
		t.Fatal("expected unsupported tape width error")
	}
	if !strings.Contains(err.Error(), "invalid tape-width-mm") {
		t.Fatalf("got %q, want invalid tape-width-mm error", err)
	}
}

func TestParsePrintOptionsLegacyPaddingOverridesBothAxes(t *testing.T) {
	opts, err := parsePrintOptionsForArgs(t, "--padding-mm", "2")
	if err != nil {
		t.Fatal(err)
	}

	if opts.HorizontalPaddingMM != 2 || opts.VerticalPaddingMM != 2 {
		t.Fatalf("got horizontal=%g vertical=%g, want both 2mm", opts.HorizontalPaddingMM, opts.VerticalPaddingMM)
	}
}

func TestParsePrintOptionsRejectsMixedPaddingFlags(t *testing.T) {
	_, err := parsePrintOptionsForArgs(t, "--padding-mm", "2", "--vertical-padding-mm", "0")
	if err == nil {
		t.Fatal("expected mixed padding flag error")
	}
	if !strings.Contains(err.Error(), "padding-mm cannot be combined") {
		t.Fatalf("got %q, want mixed padding flag error", err)
	}
}

func TestDetectedTapeWidthMM(t *testing.T) {
	status := protocol.Status{
		TapeWidthCode: 0x05,
		TapeWidth:     protocol.TapeWidth{Value: 6, Name: "24mm", MM: 24},
	}

	got, err := detectedTapeWidthMM(status)
	if err != nil {
		t.Fatal(err)
	}
	if got != 24 {
		t.Fatalf("got %gmm, want 24mm", got)
	}
}

func TestDetectedTapeWidthMMRequiresKnownTape(t *testing.T) {
	status := protocol.Status{
		TapeWidthCode: 0x00,
		TapeWidth:     protocol.TapeWidth{Value: 0, Name: "none"},
	}

	_, err := detectedTapeWidthMM(status)
	if err == nil {
		t.Fatal("expected missing tape width error")
	}
	if !strings.Contains(err.Error(), "pass --tape-width-mm") {
		t.Fatalf("got %q, want --tape-width-mm guidance", err)
	}
}

func TestRenderAndPrintRequiresTapeWidthForDryRun(t *testing.T) {
	ctx := printContextForArgs(t, "--dry-run")
	opts, popts, err := parsePrintOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}

	called := false
	err = renderAndPrint(ctx, opts, popts, func(opts render.Options) (*image.Gray, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected dry-run tape width error")
	}
	if called {
		t.Fatal("render callback was called before tape width was resolved")
	}
	if !strings.Contains(err.Error(), "--tape-width-mm") {
		t.Fatalf("got %q, want --tape-width-mm guidance", err)
	}
}

func TestTextArgumentReadsSinglePositionalValue(t *testing.T) {
	got, err := textArgument(printContextForArgs(t, "Hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello world" {
		t.Fatalf("got %q, want positional text value", got)
	}
}

func TestTextArgumentPreservesExplicitNewlines(t *testing.T) {
	got, err := textArgument(printContextForArgs(t, "Hello\nworld"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello\nworld" {
		t.Fatalf("got %q, want explicit newline preserved", got)
	}
}

func TestTextArgumentRequiresValue(t *testing.T) {
	_, err := textArgument(printContextForArgs(t))
	if err == nil {
		t.Fatal("expected missing text error")
	}
	if !strings.Contains(err.Error(), "text argument is required") {
		t.Fatalf("got %q, want missing text error", err)
	}
}

func TestTextArgumentRejectsMultipleValues(t *testing.T) {
	_, err := textArgument(printContextForArgs(t, "Hello", "world"))
	if err == nil {
		t.Fatal("expected multiple text argument error")
	}
	if !strings.Contains(err.Error(), "single argument") {
		t.Fatalf("got %q, want single argument error", err)
	}
}

func TestBarcodeArgumentReadsSinglePositionalValue(t *testing.T) {
	got, err := barcodeArgument(printContextForArgs(t, "ASSET-42"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "ASSET-42" {
		t.Fatalf("got %q, want barcode data value", got)
	}
}

func TestParseBarcodeOptionsAcceptsAliases(t *testing.T) {
	ctx := barcodePrintContextForArgs(t, "--type", "qr-code", "--module-dots", "4")

	opts, err := parseBarcodeOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Kind != render.BarcodeQR {
		t.Fatalf("got kind %q, want qr", opts.Kind)
	}
	if opts.ModuleDots != 4 {
		t.Fatalf("got module dots %d, want 4", opts.ModuleDots)
	}
}

func TestParseBarcodeOptionsRejectsUnsupportedType(t *testing.T) {
	ctx := barcodePrintContextForArgs(t, "--type", "upc")

	_, err := parseBarcodeOptions(ctx)
	if err == nil {
		t.Fatal("expected unsupported barcode type error")
	}
	if !strings.Contains(err.Error(), "unsupported barcode type") {
		t.Fatalf("got %q, want unsupported barcode type error", err)
	}
}

func TestParseBarcodeOptionsRejectsHugeModuleDots(t *testing.T) {
	ctx := barcodePrintContextForArgs(t, "--module-dots", "65")

	_, err := parseBarcodeOptions(ctx)
	if err == nil {
		t.Fatal("expected module-dots range error")
	}
	if !strings.Contains(err.Error(), "module-dots") {
		t.Fatalf("got %q, want module-dots error", err)
	}
}

func TestApplyTextRenderOptionsRequiresDownloadedFont(t *testing.T) {
	oldCache := defaultFontCache
	t.Cleanup(func() {
		defaultFontCache = oldCache
	})
	defaultFontCache = func() (fonts.Cache, error) {
		return fonts.Cache{Dir: t.TempDir()}, nil
	}

	ctx := textPrintContextForArgs(t)
	opts := render.DefaultOptions()
	err := applyTextRenderOptions(ctx, &opts)
	if err == nil {
		t.Fatal("expected missing local fonts error")
	}
	if !strings.Contains(err.Error(), "nospero fonts add") {
		t.Fatalf("got %q, want fonts add guidance", err)
	}
}

func TestApplyTextRenderOptionsRejectsInvalidAlignmentBeforeLoadingFont(t *testing.T) {
	ctx := textPrintContextForArgs(t, "--text-align", "middle")
	opts := render.DefaultOptions()
	err := applyTextRenderOptions(ctx, &opts)
	if err == nil {
		t.Fatal("expected invalid alignment error")
	}
	if !strings.Contains(err.Error(), "unsupported text-align") {
		t.Fatalf("got %q, want text-align error", err)
	}
}

func TestApplyTextRenderOptionsRejectsInvalidFontWeightBeforeLoadingFont(t *testing.T) {
	ctx := textPrintContextForArgs(t, "--font-weight", "950")
	opts := render.DefaultOptions()
	err := applyTextRenderOptions(ctx, &opts)
	if err == nil {
		t.Fatal("expected invalid font weight error")
	}
	if !strings.Contains(err.Error(), "font-weight") {
		t.Fatalf("got %q, want font-weight error", err)
	}
}

func parsePrintOptionsForArgs(t *testing.T, args ...string) (render.Options, error) {
	t.Helper()

	ctx := printContextForArgs(t, args...)
	opts, _, err := parsePrintOptions(ctx)
	return opts, err
}

func textPrintContextForArgs(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	return contextForFlags(t, textPrintFlags(), args...)
}

func barcodePrintContextForArgs(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	return contextForFlags(t, barcodePrintFlags(), args...)
}

func printContextForArgs(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	return contextForFlags(t, commonPrintFlags(), args...)
}

func contextForFlags(t *testing.T, cliFlags []cli.Flag, args ...string) *cli.Context {
	t.Helper()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, f := range cliFlags {
		if err := f.Apply(flags); err != nil {
			t.Fatal(err)
		}
	}
	if err := flags.Parse(args); err != nil {
		t.Fatal(err)
	}

	return cli.NewContext(cli.NewApp(), flags, nil)
}
