package printer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"

	"nospero/internal/protocol"
)

func TestPrintWritesCompleteSequence(t *testing.T) {
	fake := &fakeSequencedPort{reads: [][]byte{
		testStatusFrame("@ST=00ER=00TW=05"),
		testStatusFrame("@ST=00ER=00TW=05"),
		testStatusFrame("@ST=02ER=00TW=05"),
		testStatusFrame("@ST=05ER=00TW=05"),
		testStatusFrame("@ST=00ER=00TW=05"),
	}}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     50 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.TapeWidth.Name != "24mm" {
		t.Fatalf("unexpected status: %#v", status)
	}

	written := fake.written.Bytes()
	for _, want := range [][]byte{
		protocol.PrintEndCommand(),
		protocol.ResetStatusRequestCommand(),
		protocol.ResetPrinterCommand(),
		protocol.RequestStatusCommand(),
		{0x0c},
	} {
		if !bytes.Contains(written, want) {
			t.Fatalf("written stream missing %x in %x", want, written)
		}
	}
	rasterCommand := []byte{0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00}
	if !bytes.Contains(written, rasterCommand) {
		t.Fatalf("written stream missing raster row command: %x", written)
	}
	if !bytes.HasPrefix(written, protocol.ResetStatusRequestCommand()) {
		t.Fatalf("status request was not sent before print stream: %x", written)
	}
	resetAt := bytes.Index(written, protocol.ResetPrinterCommand())
	if resetAt < len(protocol.ResetStatusRequestCommand()) {
		t.Fatalf("printer reset was not sent after preflight status: %x", written)
	}
	commitAt := bytes.LastIndex(written, protocol.PrintEndCommand())
	statusAt := bytes.LastIndex(written, protocol.ResetStatusRequestCommand())
	if commitAt < 0 || statusAt < 0 || statusAt < commitAt {
		t.Fatalf("final reset status request was not sent after print-end: %x", written)
	}
	if !bytes.HasSuffix(written, protocol.ResetStatusRequestCommand()) {
		t.Fatalf("print stream does not end with reset status request: %x", written)
	}
	firstPrintStatusAt := bytes.Index(written, protocol.RequestStatusCommand())
	secondPrintStatusAt := bytes.LastIndex(written, protocol.RequestStatusCommand())
	jobStartAt := bytes.Index(written, protocol.PrintEndCommand())
	rasterAt := bytes.Index(written, rasterCommand)
	formFeedAt := bytes.Index(written, []byte{0x0c})
	if firstPrintStatusAt == secondPrintStatusAt {
		t.Fatalf("written stream contains one print-mode status request, want two: %x", written)
	}
	if !(resetAt < firstPrintStatusAt && firstPrintStatusAt < jobStartAt && jobStartAt < secondPrintStatusAt && secondPrintStatusAt < rasterAt && rasterAt < formFeedAt && formFeedAt < commitAt && commitAt < statusAt) {
		t.Fatalf("unexpected print sequence order: %x", written)
	}
	if bytes.Contains(written, protocol.OperationSendCommand(true)) || bytes.Contains(written, protocol.OperationSendCommand(false)) {
		t.Fatalf("label print stream contains operation-send command: %x", written)
	}
}

func TestPrintRejectsRenderedWidthWiderThanInstalledTape(t *testing.T) {
	fake := &fakePort{read: testStatusFrame("@ST=00ER=00TW=02")}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 10, 170))

	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     50 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err == nil {
		t.Fatal("expected tape width error")
	}
	if status.TapeWidth.Name != "9mm" {
		t.Fatalf("unexpected tape status: %#v", status.TapeWidth)
	}
	if !strings.Contains(err.Error(), "wider than installed 9mm tape") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrintStopsBeforeFormFeedOnRasterStatusError(t *testing.T) {
	fake := &fakeSequencedPort{reads: [][]byte{
		testStatusFrame("@ST=00ER=00TW=05"),
		testStatusFrame("@ST=00ER=00TW=05"),
		testStatusFrame("@ST=02ER=15TW=05"),
	}}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     50 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err == nil {
		t.Fatal("expected raster status error")
	}
	if status.ErrorCode != 0x15 {
		t.Fatalf("got error code 0x%02x, want 0x15", status.ErrorCode)
	}

	written := fake.written.Bytes()
	if bytes.Contains(written, []byte{0x0c}) {
		t.Fatalf("form feed was sent after raster status error: %x", written)
	}
	if got := bytes.Count(written, protocol.PrintEndCommand()); got != 1 {
		t.Fatalf("got %d print-end commands, want only job-environment reset: %x", got, written)
	}
}

func TestPrintRefreshesTerminalStatusAfterReset(t *testing.T) {
	fake := &fakeSequencedPort{reads: [][]byte{
		testStatusFrame("@ST=05ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=02ER=00TW=02"),
		testStatusFrame("@ST=05ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
	}}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     50 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != protocol.StatusIdle {
		t.Fatalf("got status %s (0x%02x), want idle", status.Status, status.StatusCode)
	}

	written := fake.written.Bytes()
	firstStatusAt := bytes.Index(written, protocol.ResetStatusRequestCommand())
	resetAt := bytes.Index(written, protocol.ResetPrinterCommand())
	refreshStatusAt := indexAfter(written, protocol.ResetStatusRequestCommand(), resetAt)
	printStatusAt := bytes.Index(written, protocol.RequestStatusCommand())
	if !(firstStatusAt == 0 && resetAt > firstStatusAt && refreshStatusAt > resetAt && printStatusAt > refreshStatusAt) {
		t.Fatalf("unexpected terminal-status reset sequence: %x", written)
	}
}

func TestPrintWaitsForAfterPrintCompletionBeforeResetStatus(t *testing.T) {
	fake := &fakeSequencedPort{reads: [][]byte{
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=02ER=00TW=02"),
		testStatusFrame("@ST=05ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
	}}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     500 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != protocol.StatusIdle {
		t.Fatalf("got status %s (0x%02x), want idle after reset", status.Status, status.StatusCode)
	}

	finalResetReadCount := -1
	for i, chunk := range fake.writeChunks {
		if bytes.Equal(chunk, protocol.ResetStatusRequestCommand()) {
			finalResetReadCount = fake.writeReadCounts[i]
		}
	}
	if finalResetReadCount < 5 {
		t.Fatalf("reset status was written before after-print completion; read count at reset=%d, write chunks=%x", finalResetReadCount, fake.writeChunks)
	}
}

func TestWaitAfterPrintTimesOutWhenOnlyActiveStatusIsSeen(t *testing.T) {
	fake := &fakePort{read: testStatusFrame("@ST=02ER=00TW=02")}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	start := time.Now()
	status, err := dev.waitAfterPrint(context.Background(), img, PrintOptions{
		StatusTimeout: 25 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected after-print timeout")
	}
	if status.StatusCode != protocol.StatusPrinting {
		t.Fatalf("got status %s (0x%02x), want last active printing status", status.Status, status.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waitAfterPrint took %s, want bounded timeout", elapsed)
	}
	if !strings.Contains(err.Error(), "last status=printing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrintFailsWhenAfterPrintNeverCompletes(t *testing.T) {
	fake := &fakeSequencedPort{reads: [][]byte{
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=00ER=00TW=02"),
		testStatusFrame("@ST=02ER=00TW=02"),
	}}
	dev := New(fake)
	img := image.NewGray(image.Rect(0, 0, 1, 8))

	start := time.Now()
	status, err := dev.Print(context.Background(), img, PrintOptions{
		Cut:               protocol.CutAfter,
		StatusTimeout:     25 * time.Millisecond,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	})
	if err == nil {
		t.Fatal("expected after-print completion error")
	}
	if status.StatusCode != protocol.StatusPrinting {
		t.Fatalf("got status %s (0x%02x), want last active printing status", status.Status, status.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("print took %s, want bounded timeout", elapsed)
	}
	if strings.Contains(err.Error(), "after status reset") {
		t.Fatalf("unexpected reset-status error, want after-print completion failure: %v", err)
	}
}

func TestPrintWritesRasterAsSingleCommandBuffer(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 425, 54))
	opts := PrintOptions{
		Cut:               protocol.CutAfter,
		SkipReadyCheck:    true,
		ResetDelay:        -1,
		RasterSettleDelay: -1,
		SettleDelay:       -1,
	}
	chunks, err := BuildPrintChunks(img, opts)
	if err != nil {
		t.Fatal(err)
	}
	rasterLen := len(chunks[printSequenceRasterIndex])
	if rasterLen <= 4096 {
		t.Fatalf("test raster length is %d, want more than old chunk boundary", rasterLen)
	}

	fake := &fakeSequencedPort{}
	dev := New(fake)
	if _, err := dev.Print(context.Background(), img, opts); err != nil {
		t.Fatal(err)
	}

	if !containsInt(fake.writeSizes, rasterLen) {
		t.Fatalf("raster buffer length %d was not written as one buffer; write sizes: %v", rasterLen, fake.writeSizes)
	}
	if containsInt(fake.writeSizes, 4096) {
		t.Fatalf("print stream was split at old 4096-byte boundary; write sizes: %v", fake.writeSizes)
	}
}

func TestStatusUsesResetStatusRequest(t *testing.T) {
	fake := &fakePort{read: testStatusFrame("@ST=00ER=00TW=05")}
	dev := New(fake)

	if _, err := dev.Status(context.Background(), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.written.Bytes(), protocol.ResetStatusRequestCommand(); !bytes.Equal(got, want) {
		t.Fatalf("got status command %x want %x", got, want)
	}
}

func TestStatusWithRequestUsesCustomRequestAndDrains(t *testing.T) {
	fake := &fakeDrainPort{fakePort: fakePort{read: testStatusFrame("@ST=00ER=00TW=05")}}
	dev := New(fake)

	if _, err := dev.StatusWithRequest(context.Background(), protocol.RequestStatusCommand(), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.written.Bytes(), protocol.RequestStatusCommand(); !bytes.Equal(got, want) {
		t.Fatalf("got status command %x want %x", got, want)
	}
	if fake.drains != 1 {
		t.Fatalf("got %d drains, want 1", fake.drains)
	}
}

func TestStatusAfterCommandsWritesSequence(t *testing.T) {
	fake := &fakeDrainPort{fakePort: fakePort{read: testStatusFrame("@ST=00ER=00TW=05")}}
	dev := New(fake)

	if _, err := dev.StatusAfterCommands(context.Background(), [][]byte{
		protocol.ResetPrinterCommand(),
		protocol.ResetStatusRequestCommand(),
	}, 0, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	want := append(protocol.ResetPrinterCommand(), protocol.ResetStatusRequestCommand()...)
	if got := fake.written.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("got sequence %x want %x", got, want)
	}
	if fake.drains != 2 {
		t.Fatalf("got %d drains, want 2", fake.drains)
	}
}

func TestReadStatusUsesLatestFrameAndPrioritizesBufferedErrors(t *testing.T) {
	first := testStatusFrame("@ST=02ER=21TW=05")
	last := testStatusFrame("@ST=05ER=00TW=05")
	fake := &fakePort{read: append(first, last...)}
	dev := New(fake)

	status, err := dev.readStatus(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != protocol.StatusPrintEnd {
		t.Fatalf("got status %s (0x%02x), want latest print-end frame", status.Status, status.StatusCode)
	}
	if status.ErrorCode != 0x21 || status.Codes["ER2"] != 0x21 {
		t.Fatalf("got error codes %#v, want prioritized cover-open error", status.Codes)
	}
}

func TestBuildDryRunStreamValidatesPrinterBytes(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 1, 8))
	stream, err := BuildDryRunStream(img, PrintOptions{Cut: protocol.CutAfter})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		protocol.PrintEndCommand(),
		protocol.RequestStatusCommand(),
		mustPageEnvironment(t, 15, protocol.DefaultMarginDots),
		{0x1b, '.', 0x00, 0x00, 0x00, 0x01, 0x08, 0x00},
	} {
		if !bytes.Contains(stream, want) {
			t.Fatalf("dry-run stream missing %x in %x", want, stream)
		}
	}
	if !bytes.HasSuffix(stream, protocol.PrintEndCommand()) {
		t.Fatalf("dry-run stream does not end with print-end command: %x", stream)
	}
	commitAt := bytes.LastIndex(stream, protocol.PrintEndCommand())
	if commitAt < 0 {
		t.Fatalf("dry-run stream missing final print-end: %x", stream)
	}
	if bytes.Contains(stream, protocol.OperationSendCommand(true)) || bytes.Contains(stream, protocol.OperationSendCommand(false)) {
		t.Fatalf("dry-run stream contains operation-send command: %x", stream)
	}
	if bytes.Contains(stream, protocol.ResetStatusRequestCommand()) {
		t.Fatalf("dry-run stream contains a reset status request: %x", stream)
	}
	if got := bytes.Count(stream, protocol.RequestStatusCommand()); got != 2 {
		t.Fatalf("dry-run stream contains %d print-mode status requests, want 2: %x", got, stream)
	}
}

func TestStatusTimeoutIncludesReadEvidence(t *testing.T) {
	fake := &fakePort{read: []byte("@ST=00")}
	dev := New(fake)

	_, err := dev.Status(context.Background(), 5*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
	var timeoutErr *StatusTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected StatusTimeoutError, got %T: %v", err, err)
	}
	if timeoutErr.BytesRead != 6 {
		t.Fatalf("got %d bytes read, want 6", timeoutErr.BytesRead)
	}
	if got, want := timeoutErr.PendingHex(), "4053543D3030"; got != want {
		t.Fatalf("got pending hex %q want %q", got, want)
	}
}

type fakePort struct {
	written         bytes.Buffer
	read            []byte
	writeSizes      []int
	writeChunks     [][]byte
	writeReadCounts []int
	readCount       int
}

type fakeDrainPort struct {
	fakePort
	drains int
}

type fakeSequencedPort struct {
	written         bytes.Buffer
	reads           [][]byte
	writeSizes      []int
	writeChunks     [][]byte
	writeReadCounts []int
	readCount       int
}

func (f *fakeDrainPort) Drain() error {
	f.drains++
	return nil
}

func (f *fakePort) Read(p []byte) (int, error) {
	if len(f.read) == 0 {
		time.Sleep(time.Millisecond)
		return 0, nil
	}
	n := copy(p, f.read)
	f.read = f.read[n:]
	if len(f.read) == 0 {
		f.readCount++
	}
	return n, nil
}

func (f *fakePort) Write(p []byte) (int, error) {
	f.writeSizes = append(f.writeSizes, len(p))
	f.writeChunks = append(f.writeChunks, cloneBytes(p))
	f.writeReadCounts = append(f.writeReadCounts, f.readCount)
	return f.written.Write(p)
}

func (f *fakePort) Close() error {
	return nil
}

func (f *fakeSequencedPort) Read(p []byte) (int, error) {
	if len(f.reads) == 0 {
		time.Sleep(time.Millisecond)
		return 0, nil
	}
	n := copy(p, f.reads[0])
	f.reads[0] = f.reads[0][n:]
	if len(f.reads[0]) == 0 {
		f.reads = f.reads[1:]
		f.readCount++
	}
	return n, nil
}

func (f *fakeSequencedPort) Write(p []byte) (int, error) {
	f.writeSizes = append(f.writeSizes, len(p))
	f.writeChunks = append(f.writeChunks, cloneBytes(p))
	f.writeReadCounts = append(f.writeReadCounts, f.readCount)
	return f.written.Write(p)
}

func (f *fakeSequencedPort) Close() error {
	return nil
}

func testStatusFrame(text string) []byte {
	frame := make([]byte, protocol.StatusFrameLength)
	copy(frame, []byte(text))
	frame[0] = '@'
	frame[protocol.StatusFrameLength-1] = 0xff
	return frame
}

func mustPageEnvironment(t *testing.T, heightDots, marginDots int) []byte {
	t.Helper()
	out, err := protocol.PageEnvironmentCommand(heightDots, marginDots)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func indexAfter(buf, sep []byte, after int) int {
	if after < 0 || after >= len(buf) {
		return -1
	}
	i := bytes.Index(buf[after+1:], sep)
	if i < 0 {
		return -1
	}
	return after + 1 + i
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeChunksContain(chunks [][]byte, want []byte) bool {
	for _, chunk := range chunks {
		if bytes.Equal(chunk, want) {
			return true
		}
	}
	return false
}

func cloneBytes(value []byte) []byte {
	out := make([]byte, len(value))
	copy(out, value)
	return out
}
