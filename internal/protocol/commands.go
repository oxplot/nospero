package protocol

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type CutMode string

const (
	CutEach  CutMode = "each"
	CutAfter CutMode = "after"
	CutNone  CutMode = "none"
)

func ParseCutMode(value string) (CutMode, error) {
	switch CutMode(strings.ToLower(strings.TrimSpace(value))) {
	case CutEach:
		return CutEach, nil
	case CutAfter, "":
		return CutAfter, nil
	case CutNone:
		return CutNone, nil
	default:
		return "", fmt.Errorf("unsupported cut mode %q (use each, after, or none)", value)
	}
}

// JobEnvironmentCommand builds the LW-600P job setup: old-model job reset, ST
// job marker, cut policy, density, and job-start command. Density uses the
// valid range -5..5 and is sent as density+5.
func JobEnvironmentCommand(cut CutMode, density int) ([]byte, error) {
	if density < -5 || density > 5 {
		return nil, fmt.Errorf("density must be between -5 and 5, got %d", density)
	}

	out := make([]byte, 0, 46)
	out = append(out, PrintEndCommand()...)
	var err error
	for _, frame := range []struct {
		subcommand byte
		payload    []byte
	}{
		{'{', []byte{0x00, 0x00, 'S', 'T'}},
		{'C', littleEndianUint32(cutWord(cut))},
		{'D', []byte{byte(density + 5)}},
		{'G', nil},
	} {
		out, err = appendFrame(out, frame.subcommand, frame.payload)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PageEnvironmentCommand is the command-level-1 page setup used by LW-600P.
// Newer models append width/object commands; LW-600P emits only L and T.
func PageEnvironmentCommand(heightDots, marginDots int) ([]byte, error) {
	if heightDots <= 0 {
		return nil, fmt.Errorf("height must be positive, got %d", heightDots)
	}
	if marginDots < 0 || marginDots > 0xffff {
		return nil, fmt.Errorf("margin must be between 0 and 65535 dots, got %d", marginDots)
	}

	out := make([]byte, 0, 18)
	var err error
	out, err = appendFrame(out, 'L', littleEndianUint32(uint32(heightDots)))
	if err != nil {
		return nil, err
	}
	out, err = appendFrame(out, 'T', littleEndianUint16(uint16(marginDots)))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func RasterLineCommand(widthDots int) ([]byte, error) {
	if widthDots <= 0 || widthDots > 0xffff {
		return nil, fmt.Errorf("raster width must be between 1 and 65535 dots, got %d", widthDots)
	}
	return []byte{
		esc, '.', 0x00, 0x00, 0x00, 0x01,
		byte(widthDots),
		byte(widthDots >> 8),
	}, nil
}

func cutWord(cut CutMode) uint32 {
	switch cut {
	case CutEach:
		return 0x01010101
	case CutNone:
		return 0
	case CutAfter, "":
		return 0x01010001
	default:
		return 0x01010001
	}
}

func littleEndianUint16(v uint16) []byte {
	out := make([]byte, 2)
	binary.LittleEndian.PutUint16(out, v)
	return out
}

func littleEndianUint32(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}
