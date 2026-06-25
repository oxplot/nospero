package transport

import (
	"testing"
	"time"
)

func TestNormalizeBluetoothConfigDefaults(t *testing.T) {
	cfg, err := normalizeBluetoothConfig(BluetoothConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChannelID != DefaultRFCOMMChannelID {
		t.Fatalf("got channel %d, want %d", cfg.ChannelID, DefaultRFCOMMChannelID)
	}
	if cfg.ReadTimeout != DefaultReadTimeout {
		t.Fatalf("got read timeout %s, want %s", cfg.ReadTimeout, DefaultReadTimeout)
	}
	if cfg.OpenDelay != DefaultOpenDelay {
		t.Fatalf("got open delay %s, want %s", cfg.OpenDelay, DefaultOpenDelay)
	}
}

func TestNormalizeBluetoothConfigValidation(t *testing.T) {
	if _, err := normalizeBluetoothConfig(BluetoothConfig{ChannelID: 31}); err == nil {
		t.Fatal("expected invalid channel error")
	}
	if _, err := normalizeBluetoothConfig(BluetoothConfig{ReadTimeout: -time.Millisecond}); err == nil {
		t.Fatal("expected invalid read timeout error")
	}
	cfg, err := normalizeBluetoothConfig(BluetoothConfig{OpenDelay: -time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenDelay != 0 {
		t.Fatalf("got open delay %s, want disabled delay", cfg.OpenDelay)
	}
}
