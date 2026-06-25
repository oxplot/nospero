package protocol

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const StatusFrameLength = 64

const (
	StatusIdle        = 0x00
	StatusFeeding     = 0x01
	StatusPrinting    = 0x02
	StatusDataSending = 0x03
	StatusFeedEnd     = 0x04
	StatusPrintEnd    = 0x05
)

var statusKeys = []string{
	"ST", "ER", "TW", "TR", "EI", "TO", "IR", "RR", "EJ", "WR", "RV", "CT", "OP",
}

var statusErrorPriority = []int{0x01, 0x30, 0x15, 0x21, 0x06, 0x20, 0x22}

type Status struct {
	RawText       string         `json:"raw_text"`
	RawHex        string         `json:"raw_hex"`
	Codes         map[string]int `json:"codes"`
	StatusCode    int            `json:"status_code"`
	Status        string         `json:"status"`
	ErrorCode     int            `json:"error_code"`
	Error         string         `json:"error"`
	TapeWidthCode int            `json:"tape_width_code"`
	TapeWidth     TapeWidth      `json:"tape_width"`
}

type TapeWidth struct {
	Value int    `json:"value"`
	Name  string `json:"name"`
	MM    int    `json:"mm,omitempty"`
}

func FindStatusFrame(buf []byte) ([]byte, bool) {
	_, frame, ok := FindStatusFrameRange(buf)
	return frame, ok
}

func FindStatusFrameRange(buf []byte) (int, []byte, bool) {
	for i := 0; i+StatusFrameLength <= len(buf); i++ {
		if buf[i] == '@' && buf[i+StatusFrameLength-1] == 0xff {
			out := make([]byte, StatusFrameLength)
			copy(out, buf[i:i+StatusFrameLength])
			return i, out, true
		}
	}
	return 0, nil, false
}

func FindLastStatusFrameRange(buf []byte) (int, []byte, bool) {
	if len(buf) < StatusFrameLength {
		return 0, nil, false
	}
	for i := len(buf) - StatusFrameLength; i >= 0; i-- {
		if buf[i] == '@' && buf[i+StatusFrameLength-1] == 0xff {
			out := make([]byte, StatusFrameLength)
			copy(out, buf[i:i+StatusFrameLength])
			return i, out, true
		}
	}
	return 0, nil, false
}

func ParseStatusFrame(frame []byte) (Status, error) {
	if len(frame) != StatusFrameLength {
		return Status{}, fmt.Errorf("status frame must be %d bytes, got %d", StatusFrameLength, len(frame))
	}
	if frame[0] != '@' || frame[StatusFrameLength-1] != 0xff {
		return Status{}, fmt.Errorf("invalid status frame markers")
	}

	raw := make([]byte, len(frame))
	copy(raw, frame)
	raw[StatusFrameLength-1] = 0
	text := string(bytes.TrimRight(raw, "\x00"))

	codes := map[string]int{
		"PP":  0,
		"ER2": 0,
	}
	for _, key := range statusKeys {
		codes[key] = parseStatusCode(text, key)
	}
	codes["ER2"] = codes["ER"]

	tapeWidthCode := codes["TW"]
	statusCode := codes["ST"]
	errorCode := codes["ER"]
	return Status{
		RawText:       text,
		RawHex:        strings.ToUpper(hex.EncodeToString(frame)),
		Codes:         codes,
		StatusCode:    statusCode,
		Status:        StatusName(statusCode),
		ErrorCode:     errorCode,
		Error:         ErrorName(errorCode),
		TapeWidthCode: tapeWidthCode,
		TapeWidth:     TapeWidthFromTWCode(tapeWidthCode),
	}, nil
}

func ParseStatusFrameWithContext(frame, received []byte) (Status, error) {
	status, err := ParseStatusFrame(frame)
	if err != nil {
		return Status{}, err
	}
	return ApplyStatusErrorPriority(status, received), nil
}

func ApplyStatusErrorPriority(status Status, received []byte) Status {
	errorCode, ok := prioritizedStatusErrorCode(string(received))
	if !ok {
		return status
	}
	if status.Codes == nil {
		status.Codes = make(map[string]int)
	}
	status.Codes["ER"] = errorCode
	status.Codes["ER2"] = errorCode
	status.ErrorCode = errorCode
	status.Error = ErrorName(errorCode)
	return status
}

func parseStatusCode(text, key string) int {
	i := strings.LastIndex(text, key)
	if i < 0 || i+5 > len(text) {
		return 0
	}
	v, err := strconv.ParseInt(text[i+3:i+5], 16, 0)
	if err != nil {
		return 0
	}
	return int(v)
}

func prioritizedStatusErrorCode(text string) (int, bool) {
	codes := statusCodesFromText(text, "ER")
	if len(codes) == 0 {
		return 0, false
	}

	selected := 0
	selectedPriority := len(statusErrorPriority)
	for _, code := range codes {
		priority := indexStatusErrorPriority(code)
		if priority < 0 {
			if code != 0 {
				return code, true
			}
			continue
		}
		if priority < selectedPriority {
			selectedPriority = priority
			selected = code
		}
	}
	return selected, true
}

func statusCodesFromText(text, key string) []int {
	var codes []int
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], key)
		if i < 0 {
			break
		}
		i += offset
		valueStart := i + len(key)
		if valueStart+3 <= len(text) && (text[valueStart] == ':' || text[valueStart] == '=') {
			v, err := strconv.ParseInt(text[valueStart+1:valueStart+3], 16, 0)
			if err == nil {
				codes = append(codes, int(v))
			}
			offset = valueStart + 3
			continue
		}
		offset = i + len(key)
	}
	return codes
}

func indexStatusErrorPriority(code int) int {
	for i, priorityCode := range statusErrorPriority {
		if priorityCode == code {
			return i
		}
	}
	return -1
}

func (s Status) ReadyForPrint() bool {
	if s.ErrorCode != 0 {
		return false
	}
	switch s.StatusCode {
	case StatusIdle, StatusFeedEnd, StatusPrintEnd:
		return true
	default:
		return false
	}
}

func StatusName(code int) string {
	switch code {
	case StatusIdle:
		return "idle"
	case StatusFeeding:
		return "feeding"
	case StatusPrinting:
		return "printing"
	case StatusDataSending:
		return "data_sending"
	case StatusFeedEnd:
		return "feed_end"
	case StatusPrintEnd:
		return "print_end"
	case 0x06:
		return "pick_and_print_printing"
	case 0x10:
		return "demo_printing"
	case 0x11:
		return "device_feeding"
	case 0x12:
		return "device_printing"
	case 0x13:
		return "firmware_updating"
	case 0x20:
		return "small_roll_waiting"
	case 0x22:
		return "waiting_for_tape_removal"
	case 0x48:
		return "engraving"
	case 0x49:
		return "engraving_end"
	case 0x4a:
		return "engraving_feed"
	case 0x4b:
		return "engraving_feed_end"
	case 0xff:
		return "unexpected_error"
	default:
		return "unknown"
	}
}

func ErrorName(code int) string {
	switch code {
	case 0x00:
		return "no_error"
	case 0x01:
		return "cutter_error"
	case 0x06:
		return "no_tape_cartridge"
	case 0x15:
		return "head_overheated"
	case 0x20:
		return "printer_cancel"
	case 0x21:
		return "cover_open"
	case 0x22:
		return "low_voltage"
	case 0x23:
		return "power_off_cancel"
	case 0x24:
		return "tape_eject_error"
	case 0x30:
		return "tape_feed_error"
	case 0x40:
		return "ink_ribbon_slack"
	case 0x41:
		return "ink_ribbon_short"
	case 0x42:
		return "tape_end"
	case 0x43:
		return "cut_label_error"
	case 0x44:
		return "temperature_error"
	case 0x45:
		return "insufficient_parameters"
	case 0x50:
		return "half_cutter_blade_not_set"
	case 0x51:
		return "full_cutter_blade_not_set"
	case 0x52:
		return "half_cutter_blade_off"
	case 0x53:
		return "full_cutter_blade_off"
	case 0x54:
		return "winder_cover_open"
	case 0x55:
		return "vinyl_tape_temperature_error"
	case 0x56:
		return "winder_error"
	case 0x57:
		return "half_cut_all_cut"
	case 0x58:
		return "bigroll_recognition_abnormality"
	case 0x59:
		return "bigroll_non_compliant"
	case 0x5c:
		return "stop_printing_by_auto_power_off"
	case 0x5d:
		return "stop_printing_by_power_supply_change"
	case 0x5e:
		return "winder_set"
	case 0x5f:
		return "winder_not_set"
	case 0x60:
		return "winder_half_cut_all_cut"
	default:
		return "unknown"
	}
}

func TapeWidthFromTWCode(code int) TapeWidth {
	switch code {
	case 0x00:
		return TapeWidth{Value: 0, Name: "none"}
	case 0x0b, 0x5b:
		return TapeWidth{Value: 1, Name: "4mm", MM: 4}
	case 0x01, 0x51:
		return TapeWidth{Value: 2, Name: "6mm", MM: 6}
	case 0x02, 0x52:
		return TapeWidth{Value: 3, Name: "9mm", MM: 9}
	case 0x03, 0x53:
		return TapeWidth{Value: 4, Name: "12mm", MM: 12}
	case 0x04, 0x54:
		return TapeWidth{Value: 5, Name: "18mm", MM: 18}
	case 0x05, 0x55:
		return TapeWidth{Value: 6, Name: "24mm", MM: 24}
	case 0x06, 0x56:
		return TapeWidth{Value: 7, Name: "36mm", MM: 36}
	case 0x11:
		return TapeWidth{Value: 8, Name: "24mm_cable", MM: 24}
	case 0x12:
		return TapeWidth{Value: 9, Name: "36mm_cable", MM: 36}
	case 0x21:
		return TapeWidth{Value: 10, Name: "50mm", MM: 50}
	case 0x23:
		return TapeWidth{Value: 11, Name: "100mm", MM: 100}
	case 0x07, 0x57:
		return TapeWidth{Value: 12, Name: "new_50mm", MM: 50}
	default:
		return TapeWidth{Value: -1, Name: "unknown"}
	}
}
