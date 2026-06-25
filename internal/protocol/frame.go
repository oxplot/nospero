package protocol

import "fmt"

const (
	esc       = 0x1b
	frameEnd  = 0x7d
	frameOpen = 0x7b
)

// Frame builds an Epson LW command frame.
// The length byte counts itself, the subcommand, payload bytes, and checksum.
// The checksum is the low byte of subcommand+payload and deliberately excludes
// the length byte and ESC/{/} framing bytes.
func Frame(subcommand byte, payload []byte) ([]byte, error) {
	if len(payload) > 252 {
		return nil, fmt.Errorf("payload too large for LW frame: %d bytes", len(payload))
	}

	length := byte(len(payload) + 3)
	checksum := subcommand
	for _, b := range payload {
		checksum += b
	}

	out := make([]byte, 0, len(payload)+6)
	out = append(out, esc, frameOpen, length, subcommand)
	out = append(out, payload...)
	out = append(out, checksum, frameEnd)
	return out, nil
}

func appendFrame(out []byte, subcommand byte, payload []byte) ([]byte, error) {
	frame, err := Frame(subcommand, payload)
	if err != nil {
		return nil, err
	}
	return append(out, frame...), nil
}

func RequestStatusCommand() []byte {
	return []byte{esc, frameOpen, 0x05, 'Q', 0x05, 0x00, 0x56, frameEnd}
}

func ResetStatusRequestCommand() []byte {
	return []byte{esc, frameOpen, 0x05, 'Q', 0x00, 0x00, 0x51, frameEnd}
}

func ResetPrinterCommand() []byte {
	return []byte{esc, frameOpen, 0x03, '!', '!', frameEnd}
}

func PrintEndCommand() []byte {
	return []byte{esc, frameOpen, 0x03, '@', '@', frameEnd}
}

func OperationSendCommand(cut bool) []byte {
	if cut {
		return []byte{esc, frameOpen, 0x04, '+', 0x01, ',', frameEnd}
	}
	return []byte{esc, frameOpen, 0x04, '+', 0x00, '+', frameEnd}
}
