package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestFrameCommandsMatchKnownBytes(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{
			name: "request status",
			got:  RequestStatusCommand(),
			want: "1b7b05510500567d",
		},
		{
			name: "reset status request",
			got:  ResetStatusRequestCommand(),
			want: "1b7b05510000517d",
		},
		{
			name: "print end",
			got:  PrintEndCommand(),
			want: "1b7b0340407d",
		},
		{
			name: "reset printer",
			got:  ResetPrinterCommand(),
			want: "1b7b0321217d",
		},
		{
			name: "send and cut",
			got:  OperationSendCommand(true),
			want: "1b7b042b012c7d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := hex.DecodeString(tt.want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(tt.got, want) {
				t.Fatalf("got %x want %x", tt.got, want)
			}
		})
	}
}

func TestJobEnvironmentDefaultBytes(t *testing.T) {
	got, err := JobEnvironmentCommand(CutAfter, 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString(
		"1b7b0340407d" +
			"1b7b077b00005354227d" +
			"1b7b074301000101467d" +
			"1b7b044405497d" +
			"1b7b0347477d",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestPageEnvironmentLevelOneBytes(t *testing.T) {
	got, err := PageEnvironmentCommand(0x12345678, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("1b7b074c78563412607d1b7b055434129a7d")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}
