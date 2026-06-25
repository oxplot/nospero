package protocol

import "testing"

func TestParseStatusFrame(t *testing.T) {
	frame := testStatusFrame("@ST=00ER=00TW=05TR=02EI=00TO=00IR=00RR=00EJ=00")
	status, err := ParseStatusFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "idle" || status.StatusCode != 0 {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.Error != "no_error" || status.ErrorCode != 0 {
		t.Fatalf("unexpected error: %#v", status)
	}
	if status.TapeWidth.Name != "24mm" || status.TapeWidth.MM != 24 {
		t.Fatalf("unexpected tape width: %#v", status.TapeWidth)
	}
	if status.Codes["ER2"] != status.Codes["ER"] {
		t.Fatalf("ER2 did not mirror ER: %#v", status.Codes)
	}
}

func TestFindStatusFrameSkipsLeadingNoise(t *testing.T) {
	frame := testStatusFrame("@ST=21ER=21TW=55")
	buf := append([]byte{0x00, 0x01, 0x02}, frame...)
	got, ok := FindStatusFrame(buf)
	if !ok {
		t.Fatal("status frame not found")
	}
	status, err := ParseStatusFrame(got)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "unknown" || status.Error != "cover_open" {
		t.Fatalf("unexpected parsed status: %#v", status)
	}
	if status.TapeWidth.Name != "24mm" {
		t.Fatalf("unexpected tape width: %#v", status.TapeWidth)
	}
}

func TestFindLastStatusFrameRangePrefersLatestFrame(t *testing.T) {
	first := testStatusFrame("@ST=02ER=21TW=05")
	last := testStatusFrame("@ST=05ER=00TW=05")
	buf := append(append([]byte("noise"), first...), last...)

	start, got, ok := FindLastStatusFrameRange(buf)
	if !ok {
		t.Fatal("status frame not found")
	}
	if start != len(buf)-StatusFrameLength {
		t.Fatalf("got start %d, want latest frame start %d", start, len(buf)-StatusFrameLength)
	}
	status, err := ParseStatusFrame(got)
	if err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != StatusPrintEnd || status.ErrorCode != 0 {
		t.Fatalf("unexpected latest status: %#v", status)
	}
}

func TestParseStatusFrameWithContextAppliesEpsonErrorPriority(t *testing.T) {
	first := testStatusFrame("@ST=02ER=21TW=05")
	last := testStatusFrame("@ST=05ER=00TW=05")
	received := append(first, last...)

	status, err := ParseStatusFrameWithContext(last, received)
	if err != nil {
		t.Fatal(err)
	}
	if status.StatusCode != StatusPrintEnd {
		t.Fatalf("got status 0x%02x, want latest frame status", status.StatusCode)
	}
	if status.ErrorCode != 0x21 || status.Codes["ER2"] != 0x21 {
		t.Fatalf("got error codes %#v, want prioritized cover-open error", status.Codes)
	}
}

func testStatusFrame(text string) []byte {
	frame := make([]byte, StatusFrameLength)
	copy(frame, []byte(text))
	frame[0] = '@'
	frame[StatusFrameLength-1] = 0xff
	return frame
}
