package transport

import (
	"fmt"
	"time"
)

const (
	DefaultRFCOMMChannelID   = 1
	DefaultOpenDelay         = 500 * time.Millisecond
	DefaultReadTimeout       = 100 * time.Millisecond
	DefaultConnectAttempts   = 4
	DefaultConnectRetryDelay = 500 * time.Millisecond
	defaultBaud              = 115200

	EpsonVendorID     = 0x0430
	LW600PProductID   = 0x0211
	LW600PProductName = "LW-600P"
)

type BluetoothDevice struct {
	Address   string `json:"address"`
	Name      string `json:"name,omitempty"`
	VendorID  int    `json:"vendor_id,omitempty"`
	ProductID int    `json:"product_id,omitempty"`
}

type BluetoothConfig struct {
	Address     string
	ChannelID   int
	ReadTimeout time.Duration
	OpenDelay   time.Duration
}

func normalizeBluetoothConfig(cfg BluetoothConfig) (BluetoothConfig, error) {
	if cfg.ChannelID == 0 {
		cfg.ChannelID = DefaultRFCOMMChannelID
	}
	if cfg.ChannelID < 1 || cfg.ChannelID > 30 {
		return BluetoothConfig{}, fmt.Errorf("invalid RFCOMM channel %d", cfg.ChannelID)
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = DefaultReadTimeout
	}
	if cfg.ReadTimeout < 0 {
		return BluetoothConfig{}, fmt.Errorf("read timeout must be non-negative")
	}
	if cfg.OpenDelay == 0 {
		cfg.OpenDelay = DefaultOpenDelay
	}
	if cfg.OpenDelay < 0 {
		cfg.OpenDelay = 0
	}
	return cfg, nil
}
