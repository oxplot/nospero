package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/urfave/cli/v2"

	"nospero/internal/fonts"
	"nospero/internal/printer"
	"nospero/internal/protocol"
	"nospero/internal/render"
	"nospero/internal/transport"
)

var macAddressPattern = regexp.MustCompile(`(?i)^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

const (
	defaultStatusTimeout = 60 * time.Second
	defaultProbeTimeout  = 5 * time.Second
	defaultProbeDelay    = 500 * time.Millisecond
)

type runtimeConfig struct {
	Address         string        `json:"address"`
	DeviceName      string        `json:"device_name,omitempty"`
	VendorID        int           `json:"vendor_id,omitempty"`
	ProductID       int           `json:"product_id,omitempty"`
	RFCOMMChannelID int           `json:"rfcomm_channel,omitempty"`
	OpenDelay       time.Duration `json:"open_delay"`
}

func init() {
	// IOBluetooth is not safe to initialize/open from an arbitrary Go worker
	// thread. Keep command execution on the process main thread so cgo calls
	// into the macOS Bluetooth framework use the expected run-loop context.
	runtime.LockOSThread()
}

func main() {
	app := &cli.App{
		Name:   "nospero",
		Usage:  "print and inspect Epson LabelWorks LW-600P labels over Bluetooth SPP",
		Before: validateColorMode,
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:    "address",
				Usage:   "Bluetooth MAC address override; by default the paired LW-600P is discovered by vendor/product ID",
				EnvVars: []string{"LW600P_ADDRESS", "EPSON_LW600P_ADDRESS"},
			},
			&cli.IntFlag{
				Name:    "rfcomm-channel",
				Usage:   "RFCOMM channel for the paired Bluetooth printer",
				Value:   transport.DefaultRFCOMMChannelID,
				EnvVars: []string{"LW600P_RFCOMM_CHANNEL", "EPSON_LW600P_RFCOMM_CHANNEL"},
			},
			&cli.DurationFlag{
				Name:    "timeout",
				Usage:   "status read timeout",
				Value:   defaultStatusTimeout,
				EnvVars: []string{"LW600P_TIMEOUT"},
			},
			&cli.DurationFlag{
				Name:    "open-delay",
				Usage:   "delay after opening the Bluetooth channel before sending the first command",
				Value:   transport.DefaultOpenDelay,
				EnvVars: []string{"LW600P_OPEN_DELAY"},
			},
			&cli.BoolFlag{
				Name:    "debug-io",
				Usage:   "write Bluetooth I/O diagnostics to stderr",
				EnvVars: []string{"LW600P_DEBUG_IO"},
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "write machine-readable JSON output",
			},
		}, colorFlags()...),
		Commands: []*cli.Command{
			envCommand(),
			fontsCommand(),
			statusCommand(),
			diagnoseCommand(),
			printCommand(),
		},
	}
	configureHelp(app)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer stop()

	if err := app.RunContext(ctx, os.Args); err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, errorPrefix(os.Stderr)+":", err)
		os.Exit(1)
	}
}

var defaultFontCache = fonts.DefaultCache

type envValue struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

func envCommand() *cli.Command {
	return &cli.Command{
		Name:  "env",
		Usage: "print dotenv-compatible effective configuration",
		Flags: diagnosticConfigFlags(),
		Action: func(c *cli.Context) error {
			if err := validateEnvCommandConfig(c); err != nil {
				return err
			}
			values := effectiveEnvValues(c)
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(values)
			}
			if c.String("address") == "" {
				fmt.Fprintln(os.Stdout, "# LW600P_ADDRESS is optional; unset means auto-discover the single paired LW-600P.")
			}
			for _, value := range values {
				fmt.Fprintf(os.Stdout, "%s=%s\n", value.Key, dotenvValue(value.Value))
			}
			return nil
		},
	}
}

func validateEnvCommandConfig(c *cli.Context) error {
	if address := c.String("address"); address != "" && !macAddressPattern.MatchString(address) {
		return fmt.Errorf("invalid Bluetooth address %q", address)
	}
	channelID := c.Int("rfcomm-channel")
	if channelID < 1 || channelID > 30 {
		return fmt.Errorf("rfcomm-channel must be between 1 and 30")
	}
	return nil
}

func effectiveEnvValues(c *cli.Context) []envValue {
	values := make([]envValue, 0, 7)
	if address := c.String("address"); address != "" {
		values = append(values, envValue{
			Key:    "LW600P_ADDRESS",
			Value:  strings.ToUpper(address),
			Source: configSource(c, "address"),
		})
	}
	values = append(values,
		envValue{
			Key:    "LW600P_RFCOMM_CHANNEL",
			Value:  strconv.Itoa(c.Int("rfcomm-channel")),
			Source: configSource(c, "rfcomm-channel"),
		},
		envValue{
			Key:    "LW600P_TIMEOUT",
			Value:  c.Duration("timeout").String(),
			Source: configSource(c, "timeout"),
		},
		envValue{
			Key:    "LW600P_OPEN_DELAY",
			Value:  c.Duration("open-delay").String(),
			Source: configSource(c, "open-delay"),
		},
		envValue{
			Key:    "LW600P_DEBUG_IO",
			Value:  strconv.FormatBool(c.Bool("debug-io")),
			Source: configSource(c, "debug-io"),
		},
		envValue{
			Key:    "LW600P_DIAG_TIMEOUT",
			Value:  c.Duration("probe-timeout").String(),
			Source: configSource(c, "probe-timeout"),
		},
		envValue{
			Key:    "LW600P_DIAG_DELAY",
			Value:  c.Duration("probe-delay").String(),
			Source: configSource(c, "probe-delay"),
		},
	)
	return values
}

func configSource(c *cli.Context, name string) string {
	if c.IsSet(name) {
		return "configured"
	}
	return "default"
}

func dotenvValue(value string) string {
	if value == "" {
		return ""
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.' || r == ':' || r == '/' {
			continue
		}
		return strconv.Quote(value)
	}
	return value
}

func fontsCommand() *cli.Command {
	return &cli.Command{
		Name:  "fonts",
		Usage: "manage downloaded Google Fonts",
		Subcommands: []*cli.Command{
			fontsAddCommand(),
			fontsListCommand(),
		},
	}
}

func fontsAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "download a Google Font into the local cache",
		ArgsUsage: "[FONT_URL_OR_NAME]",
		Action: func(c *cli.Context) error {
			input, err := fontInput(c)
			if err != nil {
				return err
			}
			cache, err := defaultFontCache()
			if err != nil {
				return err
			}
			font, err := cache.Add(c.Context, input)
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(font)
			}
			fmt.Fprintf(os.Stdout, "added font: %s\nstyles: %s\nurl: %s\n", font.Name, fonts.FaceSummary(font), font.URL)
			return nil
		},
	}
}

func fontsListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list downloaded fonts",
		Action: func(c *cli.Context) error {
			cache, err := defaultFontCache()
			if err != nil {
				return err
			}
			records, err := cache.List()
			if err != nil {
				return err
			}
			if c.Bool("json") {
				return json.NewEncoder(os.Stdout).Encode(records)
			}
			if len(records) == 0 {
				fmt.Fprintln(os.Stdout, "no fonts downloaded; run nospero fonts add")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTYLES\tURL")
			for _, font := range records {
				fmt.Fprintf(w, "%s\t%s\t%s\n", font.Name, fonts.FaceSummary(font), font.URL)
			}
			return w.Flush()
		},
	}
}

func fontInput(c *cli.Context) (string, error) {
	if c.NArg() > 1 {
		return "", fmt.Errorf("font add accepts one font URL or family name")
	}
	if c.NArg() == 1 {
		return c.Args().First(), nil
	}

	fmt.Fprintln(os.Stdout, fonts.GoogleFontsURL)
	fmt.Fprint(os.Stderr, "Font URL or family name: ")
	var reader io.Reader = os.Stdin
	if c.App != nil && c.App.Reader != nil {
		reader = c.App.Reader
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("font URL or family name is required")
	}
	return scanner.Text(), nil
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "read printer status",
		Action: func(c *cli.Context) error {
			cfg, dev, err := openPrinter(c)
			if err != nil {
				return err
			}
			defer dev.Close()

			status, err := dev.Status(c.Context, c.Duration("timeout"))
			if err != nil {
				writePrinterErrorDetails(c, cfg, err)
				return err
			}
			return writeStatus(c, cfg, status)
		},
	}
}

func diagnoseCommand() *cli.Command {
	return &cli.Command{
		Name:  "diagnose",
		Usage: "run safe status-only Bluetooth transport probes",
		Flags: diagnosticConfigFlags(),
		Action: func(c *cli.Context) error {
			cfgs, err := resolveDiagnosticConfigs(c)
			if err != nil {
				return err
			}
			results := runDiagnostics(c, cfgs, c.Duration("probe-timeout"), c.Duration("probe-delay"))
			writeDiagnostics(c, cfgs[0].Address, results)
			if !diagnosticsHaveStatus(results) {
				return fmt.Errorf("no diagnostic probe received a status frame")
			}
			return nil
		},
	}
}

func diagnosticConfigFlags() []cli.Flag {
	return []cli.Flag{
		&cli.DurationFlag{
			Name:    "probe-timeout",
			Usage:   "timeout per status probe",
			Value:   defaultProbeTimeout,
			EnvVars: []string{"LW600P_DIAG_TIMEOUT"},
		},
		&cli.DurationFlag{
			Name:    "probe-delay",
			Usage:   "delay between status probe commands",
			Value:   defaultProbeDelay,
			EnvVars: []string{"LW600P_DIAG_DELAY"},
		},
	}
}

func printCommand() *cli.Command {
	return &cli.Command{
		Name:  "print",
		Usage: "print a label",
		Subcommands: []*cli.Command{
			printTextCommand(),
			printBarcodeCommand(),
			printImageCommand(),
			printMixedCommand(),
		},
	}
}

type diagnosticProbe struct {
	Name     string
	Commands [][]byte
}

type diagnosticResult struct {
	Address         string           `json:"address"`
	DeviceName      string           `json:"device_name,omitempty"`
	VendorID        int              `json:"vendor_id,omitempty"`
	ProductID       int              `json:"product_id,omitempty"`
	RFCOMMChannelID int              `json:"rfcomm_channel,omitempty"`
	OpenDelay       string           `json:"open_delay"`
	Probe           string           `json:"probe"`
	RequestHex      string           `json:"request_hex"`
	Duration        string           `json:"duration"`
	Status          *protocol.Status `json:"status,omitempty"`
	Error           string           `json:"error,omitempty"`
	BytesRead       int              `json:"bytes_read,omitempty"`
	PendingBytes    int              `json:"pending_bytes,omitempty"`
	PendingHex      string           `json:"pending_hex,omitempty"`
}

func diagnosticProbes() []diagnosticProbe {
	return []diagnosticProbe{
		{Name: "request-status", Commands: [][]byte{protocol.RequestStatusCommand()}},
		{Name: "reset-status", Commands: [][]byte{protocol.ResetStatusRequestCommand()}},
		{Name: "reset-printer-then-reset-status", Commands: [][]byte{protocol.ResetPrinterCommand(), protocol.ResetStatusRequestCommand()}},
	}
}

func resolveDiagnosticConfigs(c *cli.Context) ([]runtimeConfig, error) {
	cfg, err := resolveConfig(c)
	if err != nil {
		return nil, err
	}
	return []runtimeConfig{cfg}, nil
}

func runDiagnostics(c *cli.Context, cfgs []runtimeConfig, probeTimeout, probeDelay time.Duration) []diagnosticResult {
	probes := diagnosticProbes()
	results := make([]diagnosticResult, 0, len(cfgs)*len(probes))
	for _, cfg := range cfgs {
		for _, probe := range probes {
			results = append(results, runDiagnosticProbe(c, cfg, probe, probeTimeout, probeDelay))
		}
	}
	return results
}

func runDiagnosticProbe(c *cli.Context, cfg runtimeConfig, probe diagnosticProbe, probeTimeout, probeDelay time.Duration) diagnosticResult {
	start := time.Now()
	result := diagnosticResult{
		Address:         cfg.Address,
		DeviceName:      cfg.DeviceName,
		VendorID:        cfg.VendorID,
		ProductID:       cfg.ProductID,
		RFCOMMChannelID: cfg.RFCOMMChannelID,
		OpenDelay:       cfg.OpenDelay.String(),
		Probe:           probe.Name,
		RequestHex:      hexCommandSequence(probe.Commands),
	}

	port, err := transport.OpenBluetooth(transport.BluetoothConfig{
		Address:     cfg.Address,
		ChannelID:   cfg.RFCOMMChannelID,
		ReadTimeout: transport.DefaultReadTimeout,
		OpenDelay:   cfg.OpenDelay,
	})
	if err != nil {
		result.Duration = time.Since(start).String()
		result.Error = err.Error()
		return result
	}
	defer port.Close()

	dev := printer.New(port)
	status, err := dev.StatusAfterCommands(c.Context, probe.Commands, probeDelay, probeTimeout)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		var timeoutErr *printer.StatusTimeoutError
		if errors.As(err, &timeoutErr) {
			result.BytesRead = timeoutErr.BytesRead
			result.PendingBytes = len(timeoutErr.Pending)
			result.PendingHex = timeoutErr.PendingHex()
		}
		return result
	}
	result.Status = &status
	return result
}

func diagnosticsHaveStatus(results []diagnosticResult) bool {
	for _, result := range results {
		if result.Status != nil {
			return true
		}
	}
	return false
}

func writeDiagnostics(c *cli.Context, address string, results []diagnosticResult) {
	if c.Bool("json") {
		_ = json.NewEncoder(os.Stdout).Encode(results)
		return
	}

	fmt.Fprintf(os.Stdout, "address: %s\n", address)
	for _, result := range results {
		if result.DeviceName != "" {
			fmt.Fprintf(os.Stdout, "device: %s", result.DeviceName)
			if result.VendorID != 0 || result.ProductID != 0 {
				fmt.Fprintf(os.Stdout, " (vendor=0x%04x product=0x%04x)", result.VendorID, result.ProductID)
			}
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintf(os.Stdout, "rfcomm channel: %d\n", result.RFCOMMChannelID)
		fmt.Fprintf(os.Stdout, "probe: %s request=%s duration=%s\n", result.Probe, result.RequestHex, result.Duration)
		if result.Status != nil {
			fmt.Fprintf(os.Stdout, "result: status=%s error=%s tape=%s raw=%s\n",
				result.Status.Status,
				result.Status.Error,
				result.Status.TapeWidth.Name,
				result.Status.RawText,
			)
			continue
		}
		fmt.Fprintf(os.Stdout, "result: %s\n", result.Error)
		if result.BytesRead > 0 || result.PendingBytes > 0 {
			fmt.Fprintf(os.Stdout, "bytes: read=%d pending=%d pending_hex=%s\n", result.BytesRead, result.PendingBytes, result.PendingHex)
		}
	}
}

func printTextCommand() *cli.Command {
	return &cli.Command{
		Name:      "text",
		Usage:     "print a text label",
		ArgsUsage: "TEXT",
		Flags:     textPrintFlags(),
		Action: func(c *cli.Context) error {
			text, err := textArgument(c)
			if err != nil {
				return err
			}
			opts, popts, err := parsePrintOptions(c)
			if err != nil {
				return err
			}
			if err := applyTextRenderOptions(c, &opts); err != nil {
				return err
			}
			return renderAndPrint(c, opts, popts, func(opts render.Options) (*image.Gray, error) {
				return render.TextLabel(text, opts)
			})
		},
	}
}

func textArgument(c *cli.Context) (string, error) {
	return singleArgument(c, "text")
}

func printBarcodeCommand() *cli.Command {
	return &cli.Command{
		Name:      "barcode",
		Usage:     "print a barcode label",
		ArgsUsage: "DATA",
		Flags:     barcodePrintFlags(),
		Action: func(c *cli.Context) error {
			data, err := barcodeArgument(c)
			if err != nil {
				return err
			}
			opts, popts, err := parsePrintOptions(c)
			if err != nil {
				return err
			}
			barcodeOpts, err := parseBarcodeOptions(c)
			if err != nil {
				return err
			}
			return renderAndPrint(c, opts, popts, func(opts render.Options) (*image.Gray, error) {
				return render.BarcodeLabel(data, barcodeOpts, opts)
			})
		},
	}
}

func barcodeArgument(c *cli.Context) (string, error) {
	return singleArgument(c, "barcode data")
}

func singleArgument(c *cli.Context, name string) (string, error) {
	args := c.Args().Slice()
	if len(args) == 0 {
		return "", fmt.Errorf("%s argument is required", name)
	}
	if len(args) > 1 {
		return "", fmt.Errorf("%s must be provided as a single argument; quote values containing spaces", name)
	}
	if strings.TrimSpace(args[0]) == "" {
		return "", fmt.Errorf("%s argument must not be empty", name)
	}
	return args[0], nil
}

func printImageCommand() *cli.Command {
	return &cli.Command{
		Name:  "image",
		Usage: "print an image label",
		Flags: append(commonPrintFlags(), &cli.StringFlag{
			Name:     "file",
			Aliases:  []string{"f"},
			Usage:    "image file to print",
			Required: true,
		}),
		Action: func(c *cli.Context) error {
			opts, popts, err := parsePrintOptions(c)
			if err != nil {
				return err
			}
			src, err := loadImage(c.String("file"))
			if err != nil {
				return err
			}
			return renderAndPrint(c, opts, popts, func(opts render.Options) (*image.Gray, error) {
				return render.ImageLabel(src, opts)
			})
		},
	}
}

func printMixedCommand() *cli.Command {
	return &cli.Command{
		Name:  "mixed",
		Usage: "print a label containing image and text",
		Flags: append(textPrintFlags(),
			&cli.StringFlag{
				Name:     "text",
				Usage:    "text to print",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "file",
				Aliases:  []string{"f"},
				Usage:    "image file to print",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "layout",
				Usage: "mixed layout: left, right, above, or below",
				Value: string(render.LayoutLeft),
			},
			&cli.Float64Flag{
				Name:  "gap-mm",
				Usage: "gap between image and text in millimeters",
				Value: render.DefaultOptions().GapMM,
			},
		),
		Action: func(c *cli.Context) error {
			opts, popts, err := parsePrintOptions(c)
			if err != nil {
				return err
			}
			if err := applyTextRenderOptions(c, &opts); err != nil {
				return err
			}
			layout, err := render.ParseLayout(c.String("layout"))
			if err != nil {
				return err
			}
			opts.Layout = layout
			opts.GapMM = c.Float64("gap-mm")

			src, err := loadImage(c.String("file"))
			if err != nil {
				return err
			}
			return renderAndPrint(c, opts, popts, func(opts render.Options) (*image.Gray, error) {
				return render.MixedLabel(c.String("text"), src, opts)
			})
		},
	}
}

func textPrintFlags() []cli.Flag {
	return append(commonPrintFlags(), textRenderingFlags()...)
}

func barcodePrintFlags() []cli.Flag {
	defaults := render.DefaultBarcodeOptions()
	return append(commonPrintFlags(),
		&cli.StringFlag{
			Name:    "type",
			Aliases: []string{"format", "symbology"},
			Usage:   "barcode type: " + strings.Join(render.SupportedBarcodeKindNames(), ", "),
			Value:   string(defaults.Kind),
		},
		&cli.IntFlag{
			Name:        "module-dots",
			Usage:       "barcode module width in printer dots; 0 chooses a safe default",
			Value:       defaults.ModuleDots,
			DefaultText: "auto",
		},
	)
}

func textRenderingFlags() []cli.Flag {
	defaults := render.DefaultOptions()
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "font",
			Usage: "downloaded Google Font family name",
			Value: fonts.DefaultName,
		},
		&cli.IntFlag{
			Name:  "font-weight",
			Usage: "font weight from 100 to 900",
			Value: defaults.FontWeight,
		},
		&cli.BoolFlag{
			Name:  "italic",
			Usage: "render text with an italic face or Go-synthesized slant",
		},
		&cli.StringFlag{
			Name:  "text-align",
			Usage: "align explicit text lines: left, center, or right",
			Value: string(defaults.TextAlign),
		},
		&cli.Float64Flag{
			Name:  "line-spacing",
			Usage: "line spacing multiplier between text baselines",
			Value: defaults.LineSpacing,
		},
	}
}

func commonPrintFlags() []cli.Flag {
	defaults := render.DefaultOptions()
	return []cli.Flag{
		&cli.Float64Flag{
			Name:        "tape-width-mm",
			Usage:       "tape width in millimeters; detected from the printer when omitted",
			Value:       defaults.TapeWidthMM,
			DefaultText: "auto",
		},
		&cli.Float64Flag{
			Name:  "horizontal-padding-mm",
			Usage: "left and right content padding in millimeters",
			Value: defaults.HorizontalPaddingMM,
		},
		&cli.Float64Flag{
			Name:  "vertical-padding-mm",
			Usage: "top and bottom content padding in millimeters",
			Value: defaults.VerticalPaddingMM,
		},
		&cli.Float64Flag{
			Name:        "padding-mm",
			Usage:       "legacy uniform content padding in millimeters; cannot be combined with horizontal-padding-mm or vertical-padding-mm",
			DefaultText: "not set",
		},
		&cli.StringFlag{
			Name:  "cut",
			Usage: "cut mode: after, each, or none",
			Value: string(protocol.CutAfter),
		},
		&cli.IntFlag{
			Name:  "density",
			Usage: "print density from -5 to 5",
			Value: 0,
		},
		&cli.BoolFlag{
			Name:  "skip-ready-check",
			Usage: "send print bytes without first requiring an idle no-error status",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "render and validate but do not open the printer",
		},
		&cli.StringFlag{
			Name:  "preview-png",
			Usage: "write the rendered label image to a PNG file",
		},
	}
}

func parsePrintOptions(c *cli.Context) (render.Options, printer.PrintOptions, error) {
	cut, err := protocol.ParseCutMode(c.String("cut"))
	if err != nil {
		return render.Options{}, printer.PrintOptions{}, err
	}
	density := c.Int("density")
	if density < -5 || density > 5 {
		return render.Options{}, printer.PrintOptions{}, fmt.Errorf("density must be between -5 and 5")
	}

	opts := render.DefaultOptions()
	if c.IsSet("tape-width-mm") {
		opts.TapeWidthMM = c.Float64("tape-width-mm")
		if _, err := protocol.TapeWidthDotsFromMM(opts.TapeWidthMM); err != nil {
			return render.Options{}, printer.PrintOptions{}, fmt.Errorf("invalid tape-width-mm: %w", err)
		}
	}
	if c.IsSet("padding-mm") {
		if c.IsSet("horizontal-padding-mm") || c.IsSet("vertical-padding-mm") {
			return render.Options{}, printer.PrintOptions{}, fmt.Errorf("padding-mm cannot be combined with horizontal-padding-mm or vertical-padding-mm")
		}
		opts.HorizontalPaddingMM = c.Float64("padding-mm")
		opts.VerticalPaddingMM = c.Float64("padding-mm")
	} else {
		opts.HorizontalPaddingMM = c.Float64("horizontal-padding-mm")
		opts.VerticalPaddingMM = c.Float64("vertical-padding-mm")
	}
	if opts.HorizontalPaddingMM < 0 {
		return render.Options{}, printer.PrintOptions{}, fmt.Errorf("horizontal-padding-mm must be non-negative")
	}
	if opts.VerticalPaddingMM < 0 {
		return render.Options{}, printer.PrintOptions{}, fmt.Errorf("vertical-padding-mm must be non-negative")
	}

	popts := printer.PrintOptions{
		Cut:            cut,
		Density:        density,
		SkipReadyCheck: c.Bool("skip-ready-check"),
	}
	if c.IsSet("timeout") {
		popts.StatusTimeout = c.Duration("timeout")
	}
	if c.Bool("debug-io") {
		popts.StatusReporter = func(phase string, status protocol.Status) {
			fmt.Fprintf(os.Stderr, "debug: %s status=%s(0x%02x) error=%s(0x%02x) tape=%s raw=%q\n",
				phase,
				status.Status,
				status.StatusCode,
				status.Error,
				status.ErrorCode,
				status.TapeWidth.Name,
				status.RawText,
			)
		}
	}
	return opts, popts, nil
}

func parseBarcodeOptions(c *cli.Context) (render.BarcodeOptions, error) {
	kind, err := render.ParseBarcodeKind(c.String("type"))
	if err != nil {
		return render.BarcodeOptions{}, err
	}
	moduleDots := c.Int("module-dots")
	if err := render.ValidateBarcodeModuleDots(moduleDots); err != nil {
		return render.BarcodeOptions{}, err
	}
	return render.BarcodeOptions{
		Kind:       kind,
		ModuleDots: moduleDots,
	}, nil
}

func applyTextRenderOptions(c *cli.Context, opts *render.Options) error {
	align, err := render.ParseTextAlign(c.String("text-align"))
	if err != nil {
		return err
	}
	lineSpacing := c.Float64("line-spacing")
	if err := render.ValidateLineSpacing(lineSpacing); err != nil {
		return err
	}
	weight := c.Int("font-weight")
	if err := render.ValidateFontWeight(weight); err != nil {
		return err
	}
	fontName := strings.TrimSpace(c.String("font"))
	if fontName == "" {
		fontName = fonts.DefaultName
	}

	cache, err := defaultFontCache()
	if err != nil {
		return err
	}
	_, face, font, err := cache.LoadFace(fontName, fonts.FaceRequest{
		Weight: weight,
		Italic: c.Bool("italic"),
	})
	if errors.Is(err, fonts.ErrNoFonts) {
		return fmt.Errorf("no local fonts downloaded; run nospero fonts add to add %s", fonts.DefaultName)
	}
	var notFound *fonts.NotFoundError
	if errors.As(err, &notFound) {
		return fmt.Errorf("font %q is not downloaded; run nospero fonts add or choose one from nospero fonts list", fontName)
	}
	if err != nil {
		return err
	}
	opts.TextFont = font
	opts.FontWeight = weight
	opts.FontFaceWeight = face.Weight
	opts.SyntheticItalic = c.Bool("italic") && face.Style != "italic"
	opts.TextAlign = align
	opts.LineSpacing = lineSpacing
	return nil
}

type labelRenderer func(render.Options) (*image.Gray, error)

func renderAndPrint(c *cli.Context, opts render.Options, popts printer.PrintOptions, renderLabel labelRenderer) error {
	if opts.TapeWidthMM > 0 {
		img, err := renderLabel(opts)
		if err != nil {
			return err
		}
		return printRendered(c, img, popts, opts.TapeWidthMM)
	}
	if c.Bool("dry-run") {
		return fmt.Errorf("tape width must be specified with --tape-width-mm when --dry-run is used")
	}

	cfg, dev, err := openPrinter(c)
	if err != nil {
		return fmt.Errorf("auto-detect tape width from printer: %w; pass --tape-width-mm", err)
	}
	defer dev.Close()

	status, err := dev.Status(c.Context, c.Duration("timeout"))
	if err != nil {
		writePrinterErrorDetails(c, cfg, err)
		return fmt.Errorf("auto-detect tape width from printer: %w; pass --tape-width-mm", err)
	}
	if popts.StatusReporter != nil {
		popts.StatusReporter("auto_detect_tape_width", status)
	}

	tapeWidthMM, err := detectedTapeWidthMM(status)
	if err != nil {
		if status.RawText != "" {
			_ = writeStatus(c, cfg, status)
		}
		return err
	}
	opts.TapeWidthMM = tapeWidthMM

	img, err := renderLabel(opts)
	if err != nil {
		return err
	}
	handled, err := prepareRenderedOutput(c, img, popts, opts.TapeWidthMM)
	if err != nil || handled {
		return err
	}
	return printRenderedWithDevice(c, cfg, dev, img, popts)
}

func detectedTapeWidthMM(status protocol.Status) (float64, error) {
	if status.TapeWidth.MM <= 0 {
		return 0, fmt.Errorf("could not auto-detect tape width from printer (reported tape=%s TW=0x%02x); pass --tape-width-mm", status.TapeWidth.Name, status.TapeWidthCode)
	}
	if _, ok := protocol.PrintableDotsForTapeWidth(status.TapeWidth); !ok {
		return 0, fmt.Errorf("auto-detected unsupported tape width %s; pass --tape-width-mm", status.TapeWidth.Name)
	}
	return float64(status.TapeWidth.MM), nil
}

func printRendered(c *cli.Context, img image.Image, popts printer.PrintOptions, tapeWidthMM float64) error {
	handled, err := prepareRenderedOutput(c, img, popts, tapeWidthMM)
	if err != nil || handled {
		return err
	}

	cfg, dev, err := openPrinter(c)
	if err != nil {
		return err
	}
	defer dev.Close()

	return printRenderedWithDevice(c, cfg, dev, img, popts)
}

func prepareRenderedOutput(c *cli.Context, img image.Image, popts printer.PrintOptions, tapeWidthMM float64) (bool, error) {
	if c.Bool("debug-io") {
		b := img.Bounds()
		if tapeWidth, ok := protocol.PrintableDotsForTapeWidthMM(tapeWidthMM); ok {
			fmt.Fprintf(os.Stderr, "debug: rendered_label=%dx%d dots printable_tape_width=%d dots\n", b.Dx(), b.Dy(), tapeWidth)
		} else {
			fmt.Fprintf(os.Stderr, "debug: rendered_label=%dx%d dots\n", b.Dx(), b.Dy())
		}
	}
	if path := c.String("preview-png"); path != "" {
		if err := writePNG(path, img); err != nil {
			return false, err
		}
	}
	if c.Bool("dry-run") {
		return true, writeDryRun(c, img, popts)
	}
	return false, nil
}

func printRenderedWithDevice(c *cli.Context, cfg runtimeConfig, dev *printer.Device, img image.Image, popts printer.PrintOptions) error {
	status, err := dev.Print(c.Context, img, popts)
	if err != nil {
		if status.RawText != "" {
			_ = writeStatus(c, cfg, status)
		}
		writePrinterErrorDetails(c, cfg, err)
		return err
	}
	if status.RawText == "" {
		return nil
	}
	return writeStatus(c, cfg, status)
}

func openPrinter(c *cli.Context) (runtimeConfig, *printer.Device, error) {
	cfg, err := resolveConfig(c)
	if err != nil {
		return runtimeConfig{}, nil, err
	}
	port, err := transport.OpenBluetooth(transport.BluetoothConfig{
		Address:     cfg.Address,
		ChannelID:   cfg.RFCOMMChannelID,
		ReadTimeout: transport.DefaultReadTimeout,
		OpenDelay:   cfg.OpenDelay,
	})
	if err != nil {
		return runtimeConfig{}, nil, err
	}
	if err := c.Context.Err(); err != nil {
		_ = port.Close()
		return runtimeConfig{}, nil, err
	}
	if c.Bool("debug-io") {
		fmt.Fprintf(os.Stderr, "debug: opened rfcomm_channel=%d open_delay=%s timeout=%s\n", cfg.RFCOMMChannelID, cfg.OpenDelay, c.Duration("timeout"))
	}
	return cfg, printer.New(port), nil
}

func resolveConfig(c *cli.Context) (runtimeConfig, error) {
	device := transport.BluetoothDevice{}
	if address := c.String("address"); address != "" {
		if !macAddressPattern.MatchString(address) {
			return runtimeConfig{}, fmt.Errorf("invalid Bluetooth address %q", address)
		}
		device.Address = strings.ToUpper(address)
	} else {
		discovered, err := transport.FindLW600P()
		if err != nil {
			return runtimeConfig{}, err
		}
		device = discovered
	}

	channelID := c.Int("rfcomm-channel")
	if channelID < 1 || channelID > 30 {
		return runtimeConfig{}, fmt.Errorf("rfcomm-channel must be between 1 and 30")
	}

	return runtimeConfig{
		Address:         device.Address,
		DeviceName:      device.Name,
		VendorID:        device.VendorID,
		ProductID:       device.ProductID,
		RFCOMMChannelID: channelID,
		OpenDelay:       c.Duration("open-delay"),
	}, nil
}

func writeStatus(c *cli.Context, cfg runtimeConfig, status protocol.Status) error {
	if c.Bool("json") {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Address         string          `json:"address"`
			DeviceName      string          `json:"device_name,omitempty"`
			VendorID        int             `json:"vendor_id,omitempty"`
			ProductID       int             `json:"product_id,omitempty"`
			RFCOMMChannelID int             `json:"rfcomm_channel,omitempty"`
			OpenDelay       string          `json:"open_delay"`
			Status          protocol.Status `json:"status"`
		}{
			Address:         cfg.Address,
			DeviceName:      cfg.DeviceName,
			VendorID:        cfg.VendorID,
			ProductID:       cfg.ProductID,
			RFCOMMChannelID: cfg.RFCOMMChannelID,
			OpenDelay:       cfg.OpenDelay.String(),
			Status:          status,
		})
	}

	fmt.Fprintf(os.Stdout, "address: %s\n", cfg.Address)
	if cfg.DeviceName != "" {
		fmt.Fprintf(os.Stdout, "device: %s", cfg.DeviceName)
		if cfg.VendorID != 0 || cfg.ProductID != 0 {
			fmt.Fprintf(os.Stdout, " (vendor=0x%04x product=0x%04x)", cfg.VendorID, cfg.ProductID)
		}
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintf(os.Stdout, "rfcomm channel: %d\n", cfg.RFCOMMChannelID)
	fmt.Fprintf(os.Stdout, "status: %s (0x%02x)\n", status.Status, status.StatusCode)
	fmt.Fprintf(os.Stdout, "error: %s (0x%02x)\n", status.Error, status.ErrorCode)
	fmt.Fprintf(os.Stdout, "tape width: %s (TW=0x%02x, value=%d)\n", status.TapeWidth.Name, status.TapeWidthCode, status.TapeWidth.Value)
	fmt.Fprintf(os.Stdout, "raw: %s\n", status.RawText)
	return nil
}

func writeDryRun(c *cli.Context, img image.Image, popts printer.PrintOptions) error {
	b := img.Bounds()
	stream, err := printer.BuildDryRunStream(img, popts)
	if err != nil {
		return err
	}
	if c.Bool("json") {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"dry_run":     true,
			"width":       b.Dx(),
			"height":      b.Dy(),
			"print_bytes": len(stream),
		})
	}
	fmt.Fprintf(os.Stdout, "dry run: rendered %dx%d dots, built %d printer bytes; no printer connection opened\n", b.Dx(), b.Dy(), len(stream))
	return nil
}

func writePrinterErrorDetails(c *cli.Context, cfg runtimeConfig, err error) {
	if !c.Bool("debug-io") {
		return
	}
	fmt.Fprintf(os.Stderr, "debug: address=%s rfcomm_channel=%d open_delay=%s\n", cfg.Address, cfg.RFCOMMChannelID, cfg.OpenDelay)
	fmt.Fprintf(os.Stderr, "debug: reset_status_request=%s\n", hexBytes(protocol.ResetStatusRequestCommand()))
	fmt.Fprintf(os.Stderr, "debug: print_status_request=%s\n", hexBytes(protocol.RequestStatusCommand()))

	var timeoutErr *printer.StatusTimeoutError
	if errors.As(err, &timeoutErr) {
		fmt.Fprintf(os.Stderr, "debug: status_timeout=%s bytes_read=%d pending_bytes=%d\n", timeoutErr.Timeout, timeoutErr.BytesRead, len(timeoutErr.Pending))
		if raw := timeoutErr.PendingHex(); raw != "" {
			fmt.Fprintf(os.Stderr, "debug: pending_hex=%s\n", raw)
		}
	}
}

func hexBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	encoded := strings.ToUpper(hex.EncodeToString(data))
	parts := make([]string, 0, len(encoded)/2)
	for i := 0; i < len(encoded); i += 2 {
		parts = append(parts, encoded[i:i+2])
	}
	return strings.Join(parts, " ")
}

func hexCommandSequence(commands [][]byte) string {
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		parts = append(parts, hexBytes(command))
	}
	return strings.Join(parts, " | ")
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image %s: %w", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode image %s: %w", path, err)
	}
	return img, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create preview PNG %s: %w", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("write preview PNG %s: %w", path, err)
	}
	return nil
}
