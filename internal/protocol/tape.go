package protocol

import (
	"fmt"
	"math"
)

var printableDotsByTapeWidthValue = map[int]int{
	1:  22,
	2:  36,
	3:  54,
	4:  72,
	5:  108,
	6:  128,
	7:  192,
	10: 297,
	11: 652,
	12: 297,
}

var printableDotsByTapeWidthMM = map[int]int{
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

func PrintableDotsForTapeWidth(tape TapeWidth) (int, bool) {
	if dots, ok := printableDotsByTapeWidthValue[tape.Value]; ok {
		return dots, true
	}
	if tape.MM > 0 {
		return PrintableDotsForTapeWidthMM(float64(tape.MM))
	}
	return 0, false
}

func PrintableDotsForTapeWidthMM(mm float64) (int, bool) {
	if math.IsNaN(mm) || math.IsInf(mm, 0) || mm <= 0 {
		return 0, false
	}
	rounded := int(math.Round(mm))
	if math.Abs(mm-float64(rounded)) > 0.001 {
		return 0, false
	}
	dots, ok := printableDotsByTapeWidthMM[rounded]
	return dots, ok
}

func TapeWidthDotsFromMM(mm float64) (int, error) {
	if dots, ok := PrintableDotsForTapeWidthMM(mm); ok {
		return dots, nil
	}
	if _, err := DotsFromMM(mm); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("unsupported tape width %.3gmm (supported: 4, 6, 9, 12, 18, 24, 36, 50, 100)", mm)
}
