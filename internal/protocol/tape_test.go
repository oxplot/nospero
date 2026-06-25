package protocol

import "testing"

func TestPrintableDotsForTapeWidthMMUsesPrintableWidth(t *testing.T) {
	tests := map[float64]int{
		4:   22,
		6:   36,
		9:   54,
		12:  72,
		18:  108,
		24:  128,
		36:  192,
		50:  297,
		100: 652,
	}
	for mm, want := range tests {
		got, ok := PrintableDotsForTapeWidthMM(mm)
		if !ok {
			t.Fatalf("expected printable width for %gmm", mm)
		}
		if got != want {
			t.Fatalf("got %d dots for %gmm, want %d", got, mm, want)
		}
	}
}

func TestTapeWidthDotsFromMMRejectsUnknownWidths(t *testing.T) {
	if _, err := TapeWidthDotsFromMM(10); err == nil {
		t.Fatal("expected unsupported tape width error")
	}
}

func TestPrintableDotsForTapeWidthUsesStatusValue(t *testing.T) {
	got, ok := PrintableDotsForTapeWidth(TapeWidth{Value: 3, Name: "9mm", MM: 9})
	if !ok {
		t.Fatal("expected printable width for 9mm status")
	}
	if got != 54 {
		t.Fatalf("got %d dots, want 54", got)
	}
}
