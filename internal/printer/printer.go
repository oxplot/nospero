package printer

import (
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"strings"
	"time"

	"nospero/internal/protocol"
)

const (
	defaultStatusTimeout       = 60 * time.Second
	defaultPrintStatusTimeout  = 300 * time.Second
	defaultAfterPrintTimeout   = 30 * time.Second
	defaultResetDelay          = 500 * time.Millisecond
	defaultRasterSettleDelay   = 500 * time.Millisecond
	printCompletionPollDelay   = 250 * time.Millisecond
	afterPrintResetReadTimeout = 2 * time.Second
	defaultSettleDelay         = 1 * time.Second
	printSequenceStatusIndex   = 0
	printSequenceRasterIndex   = 4
)

type Device struct {
	rw      io.ReadWriteCloser
	pending []byte
}

type drainer interface {
	Drain() error
}

type PrintOptions struct {
	Cut     protocol.CutMode
	Density int

	// MarginDots adds blank feed rows before and after the rendered image.
	// Zero means no protocol-level feed margin; render padding is controlled
	// separately by render.Options.
	MarginDots int

	StatusTimeout     time.Duration
	SkipReadyCheck    bool
	ResetDelay        time.Duration
	RasterSettleDelay time.Duration
	SettleDelay       time.Duration
	StatusReporter    func(phase string, status protocol.Status)
}

type StatusTimeoutError struct {
	Timeout   time.Duration
	FrameSize int
	BytesRead int
	Pending   []byte
}

func (e *StatusTimeoutError) Error() string {
	frameSize := e.FrameSize
	if frameSize == 0 {
		frameSize = protocol.StatusFrameLength
	}
	if e.BytesRead == 0 {
		return fmt.Sprintf("timed out waiting for %d-byte status frame; no bytes received", frameSize)
	}
	return fmt.Sprintf("timed out waiting for %d-byte status frame; received %d bytes but no complete frame", frameSize, e.BytesRead)
}

func (e *StatusTimeoutError) PendingHex() string {
	if len(e.Pending) == 0 {
		return ""
	}
	return strings.ToUpper(hex.EncodeToString(e.Pending))
}

func New(rw io.ReadWriteCloser) *Device {
	return &Device{rw: rw}
}

func (d *Device) Close() error {
	return d.rw.Close()
}

func (d *Device) Status(ctx context.Context, timeout time.Duration) (protocol.Status, error) {
	return d.StatusWithRequest(ctx, protocol.ResetStatusRequestCommand(), timeout)
}

func (d *Device) ResetStatus(ctx context.Context, timeout time.Duration) (protocol.Status, error) {
	return d.Status(ctx, timeout)
}

func (d *Device) StatusWithRequest(ctx context.Context, request []byte, timeout time.Duration) (protocol.Status, error) {
	return d.StatusAfterCommands(ctx, [][]byte{request}, 0, timeout)
}

func (d *Device) StatusAfterCommands(ctx context.Context, commands [][]byte, delayBetweenCommands time.Duration, timeout time.Duration) (protocol.Status, error) {
	if timeout == 0 {
		timeout = defaultStatusTimeout
	}
	if len(commands) == 0 {
		return protocol.Status{}, fmt.Errorf("status command sequence must not be empty")
	}
	for i, command := range commands {
		if len(command) == 0 {
			return protocol.Status{}, fmt.Errorf("status command %d must not be empty", i+1)
		}
		if err := d.writeAll(command); err != nil {
			return protocol.Status{}, fmt.Errorf("send status command %d: %w", i+1, err)
		}
		if i < len(commands)-1 && delayBetweenCommands > 0 {
			if err := sleepContext(ctx, delayBetweenCommands); err != nil {
				return protocol.Status{}, err
			}
		}
	}
	return d.readStatus(ctx, timeout)
}

func (d *Device) Print(ctx context.Context, img image.Image, opts PrintOptions) (protocol.Status, error) {
	if opts.Cut == "" {
		opts.Cut = protocol.CutAfter
	}
	statusTimeout := timeoutOrDefault(opts.StatusTimeout, defaultStatusTimeout)
	printStatusTimeout := timeoutOrDefault(opts.StatusTimeout, defaultPrintStatusTimeout)

	var status protocol.Status
	if !opts.SkipReadyCheck {
		var err error
		status, err = d.Status(ctx, statusTimeout)
		if err != nil {
			return protocol.Status{}, err
		}
		opts.reportStatus("preflight", status)
		if !status.ReadyForPrint() {
			return status, fmt.Errorf("printer is not ready: status=%s error=%s", status.Status, status.Error)
		}
		if err := validateTapeWidth(status, img); err != nil {
			return status, err
		}
	}
	d.pending = nil
	if err := d.resetForPrint(ctx, opts.ResetDelay); err != nil {
		return status, err
	}
	if !opts.SkipReadyCheck && isTerminalStatus(status.StatusCode) {
		var err error
		status, err = d.Status(ctx, statusTimeout)
		if err != nil {
			return status, fmt.Errorf("read status after printer reset: %w", err)
		}
		opts.reportStatus("after_reset", status)
		if !status.ReadyForPrint() {
			return status, fmt.Errorf("printer is not ready after reset: status=%s error=%s", status.Status, status.Error)
		}
		if err := validateTapeWidth(status, img); err != nil {
			return status, err
		}
	}
	d.pending = nil

	sequence, err := BuildPrintChunks(img, opts)
	if err != nil {
		return protocol.Status{}, err
	}
	for i, chunk := range sequence {
		if err := d.writeAll(chunk); err != nil {
			return protocol.Status{}, err
		}
		switch i {
		case printSequenceStatusIndex:
			if opts.SkipReadyCheck {
				continue
			}
			var err error
			status, err = d.readStatus(ctx, printStatusTimeout)
			if err != nil {
				return status, fmt.Errorf("read print-mode status before job: %w", err)
			}
			opts.reportStatus("print_mode_before_job", status)
			if !status.ReadyForPrint() {
				return status, fmt.Errorf("printer is not ready for print job: status=%s error=%s", status.Status, status.Error)
			}
			if err := validateTapeWidth(status, img); err != nil {
				return status, err
			}
		case printSequenceRasterIndex:
			if err := sleepContext(ctx, normalizeDelay(opts.RasterSettleDelay, defaultRasterSettleDelay)); err != nil {
				return status, err
			}
			if opts.SkipReadyCheck {
				continue
			}
			rasterStatus, err := d.readStatus(ctx, printStatusTimeout)
			if err != nil {
				return status, fmt.Errorf("read print-mode status after raster: %w", err)
			}
			opts.reportStatus("after_raster", rasterStatus)
			if rasterStatus.ErrorCode != 0 {
				return rasterStatus, fmt.Errorf("printer reported an error after raster transfer: status=%s error=%s", rasterStatus.Status, rasterStatus.Error)
			}
		}
	}
	if !opts.SkipReadyCheck {
		afterPrintStatus, err := d.waitAfterPrint(ctx, img, opts)
		if err != nil {
			if afterPrintStatus.RawText != "" {
				return afterPrintStatus, err
			}
			return status, err
		}
		status = afterPrintStatus

		resetStatus, ok, err := d.resetStatusModeAfterPrint(ctx, opts)
		if err != nil {
			return status, fmt.Errorf("reset print status mode after print: %w", err)
		}
		if ok {
			opts.reportStatus("after_status_reset", resetStatus)
			if resetStatus.ErrorCode != 0 {
				return resetStatus, fmt.Errorf("printer reported an error after status reset: status=%s error=%s", resetStatus.Status, resetStatus.Error)
			}
			status = resetStatus
		}
	}
	settleDelay := opts.SettleDelay
	if settleDelay == 0 {
		settleDelay = defaultSettleDelay
	}
	if settleDelay > 0 {
		if err := sleepContext(ctx, settleDelay); err != nil {
			return status, err
		}
	}

	return status, nil
}

func (opts PrintOptions) reportStatus(phase string, status protocol.Status) {
	if opts.StatusReporter != nil {
		opts.StatusReporter(phase, status)
	}
}

func BuildPrintChunks(img image.Image, opts PrintOptions) ([][]byte, error) {
	if opts.Cut == "" {
		opts.Cut = protocol.CutAfter
	}
	job, err := protocol.JobEnvironmentCommand(opts.Cut, opts.Density)
	if err != nil {
		return nil, err
	}
	if opts.MarginDots < 0 {
		return nil, fmt.Errorf("margin must be non-negative, got %d", opts.MarginDots)
	}
	marginDots := opts.MarginDots
	page, err := protocol.PageEnvironmentCommand(img.Bounds().Dx()+marginDots*2, marginDots)
	if err != nil {
		return nil, err
	}
	raster, err := protocol.RasterPageWithMargins(img, marginDots)
	if err != nil {
		return nil, err
	}

	return [][]byte{
		protocol.RequestStatusCommand(),
		job,
		protocol.RequestStatusCommand(),
		page,
		raster,
		{0x0c},
		protocol.PrintEndCommand(),
	}, nil
}

func BuildDryRunStream(img image.Image, opts PrintOptions) ([]byte, error) {
	chunks, err := BuildPrintChunks(img, opts)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	out := make([]byte, 0, total)
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out, nil
}

func validateTapeWidth(status protocol.Status, img image.Image) error {
	if status.TapeWidth.MM <= 0 {
		return nil
	}
	maxWidth, ok := protocol.PrintableDotsForTapeWidth(status.TapeWidth)
	if !ok {
		var err error
		maxWidth, err = protocol.DotsFromMM(float64(status.TapeWidth.MM))
		if err != nil {
			return fmt.Errorf("invalid reported tape width %s: %w", status.TapeWidth.Name, err)
		}
	}
	renderedTapeWidth := img.Bounds().Dy()
	if renderedTapeWidth > maxWidth {
		return fmt.Errorf("rendered label tape width is %d dots, wider than installed %s tape (%d dots); pass --tape-width-mm %d or install wider tape", renderedTapeWidth, status.TapeWidth.Name, maxWidth, status.TapeWidth.MM)
	}
	return nil
}

func (d *Device) resetForPrint(ctx context.Context, delay time.Duration) error {
	if err := d.writeAll(protocol.ResetPrinterCommand()); err != nil {
		return fmt.Errorf("reset printer before print: %w", err)
	}
	if delay == 0 {
		delay = defaultResetDelay
	}
	if delay > 0 {
		if err := sleepContext(ctx, delay); err != nil {
			return err
		}
	}
	d.pending = nil
	return nil
}

func isTerminalStatus(statusCode int) bool {
	return statusCode == protocol.StatusFeedEnd || statusCode == protocol.StatusPrintEnd
}

func isActivePrintStatus(statusCode int) bool {
	switch statusCode {
	case protocol.StatusFeeding, protocol.StatusPrinting, protocol.StatusDataSending, 0x11, 0x12:
		return true
	default:
		return false
	}
}

func timeoutOrDefault(timeout, fallback time.Duration) time.Duration {
	if timeout == 0 {
		return fallback
	}
	return timeout
}

func (d *Device) waitAfterPrint(ctx context.Context, img image.Image, opts PrintOptions) (protocol.Status, error) {
	_ = img
	timeout := timeoutOrDefault(opts.StatusTimeout, defaultAfterPrintTimeout)
	if err := sleepContext(ctx, defaultRasterSettleDelay); err != nil {
		return protocol.Status{}, err
	}

	deadline := time.Now().Add(timeout)
	var last protocol.Status
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if last.RawText != "" {
				return last, fmt.Errorf("timed out waiting for printer to finish; last status=%s error=%s", last.Status, last.Error)
			}
			return protocol.Status{}, fmt.Errorf("timed out waiting for printer to finish")
		}

		status, err := d.readStatus(ctx, remaining)
		if err != nil {
			if last.RawText != "" {
				return last, fmt.Errorf("read after-print status: %w; last status=%s error=%s", err, last.Status, last.Error)
			}
			return protocol.Status{}, fmt.Errorf("read after-print status: %w", err)
		}
		if last.RawText == "" || status.RawText != last.RawText {
			opts.reportStatus("after_print", status)
		}
		last = status
		if status.ErrorCode != 0 {
			return status, fmt.Errorf("printer reported an error after print-end: status=%s error=%s", status.Status, status.Error)
		}
		if status.ReadyForPrint() {
			return status, nil
		}
		if !isActivePrintStatus(status.StatusCode) {
			return status, fmt.Errorf("printer reported unexpected after-print status: status=%s error=%s", status.Status, status.Error)
		}
		if err := sleepContext(ctx, printCompletionPollDelay); err != nil {
			return status, err
		}
	}
}

func (d *Device) resetStatusModeAfterPrint(ctx context.Context, opts PrintOptions) (protocol.Status, bool, error) {
	d.pending = nil
	if err := d.writeAll(protocol.ResetStatusRequestCommand()); err != nil {
		return protocol.Status{}, false, err
	}
	timeout := opts.StatusTimeout
	if timeout == 0 || timeout > afterPrintResetReadTimeout {
		timeout = afterPrintResetReadTimeout
	}
	status, err := d.readStatus(ctx, timeout)
	if err != nil {
		if _, ok := err.(*StatusTimeoutError); ok {
			d.pending = nil
			return protocol.Status{}, false, nil
		}
		return protocol.Status{}, false, err
	}
	return status, true, nil
}

func (d *Device) readStatus(ctx context.Context, timeout time.Duration) (protocol.Status, error) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 512)
	bytesRead := 0

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return protocol.Status{}, err
		}
		if frame, received, ok := d.takeStatusFrame(); ok {
			return protocol.ParseStatusFrameWithContext(frame, received)
		}
		n, err := d.rw.Read(buf)
		if n > 0 {
			bytesRead += n
			d.pending = append(d.pending, buf[:n]...)
			if frame, received, ok := d.takeStatusFrame(); ok {
				return protocol.ParseStatusFrameWithContext(frame, received)
			}
			if len(d.pending) > 4096 {
				d.pending = d.pending[len(d.pending)-4096:]
			}
		}
		if err != nil {
			if err == io.EOF {
				continue
			}
			return protocol.Status{}, fmt.Errorf("read status: %w", err)
		}
	}
	pending := make([]byte, len(d.pending))
	copy(pending, d.pending)
	return protocol.Status{}, &StatusTimeoutError{
		Timeout:   timeout,
		FrameSize: protocol.StatusFrameLength,
		BytesRead: bytesRead,
		Pending:   pending,
	}
}

func (d *Device) takeStatusFrame() ([]byte, []byte, bool) {
	start, frame, ok := protocol.FindLastStatusFrameRange(d.pending)
	if !ok {
		return nil, nil, false
	}
	received := cloneStatusBytes(d.pending)
	end := start + protocol.StatusFrameLength
	d.pending = append(d.pending[:0], d.pending[end:]...)
	return frame, received, true
}

func cloneStatusBytes(value []byte) []byte {
	out := make([]byte, len(value))
	copy(out, value)
	return out
}

func (d *Device) writeAll(data []byte) error {
	wrote := len(data) > 0
	for len(data) > 0 {
		n, err := d.rw.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	if wrote {
		if drainable, ok := d.rw.(drainer); ok {
			if err := drainable.Drain(); err != nil {
				return fmt.Errorf("drain write: %w", err)
			}
		}
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeDelay(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}
