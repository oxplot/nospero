//go:build darwin && cgo

package transport

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework IOBluetooth
#include <stdlib.h>

void *lwbt_open(const char *address, int channelID, unsigned int baud, char **errOut);
int lwbt_find_lw600p(char **addressOut, char **nameOut, unsigned int *vendorIDOut, unsigned int *productIDOut, char **errOut);
int lwbt_read(void *conn, unsigned char *dst, int length, int timeoutMS, int *codeOut, char **errOut);
int lwbt_write(void *conn, const unsigned char *src, int length, char **errOut);
int lwbt_close(void *conn, char **errOut);
*/
import "C"

import (
	"fmt"
	"io"
	"time"
	"unsafe"
)

type bluetoothPort struct {
	conn        unsafe.Pointer
	readTimeout time.Duration
	closed      bool
}

func FindLW600P() (BluetoothDevice, error) {
	var address *C.char
	var name *C.char
	var vendorID C.uint
	var productID C.uint
	var errMsg *C.char
	ok := C.lwbt_find_lw600p(&address, &name, &vendorID, &productID, &errMsg)
	if ok == 0 {
		return BluetoothDevice{}, takeCError(errMsg)
	}
	defer C.free(unsafe.Pointer(address))
	if name != nil {
		defer C.free(unsafe.Pointer(name))
	}
	deviceName := ""
	if name != nil {
		deviceName = C.GoString(name)
	}
	return BluetoothDevice{
		Address:   C.GoString(address),
		Name:      deviceName,
		VendorID:  int(vendorID),
		ProductID: int(productID),
	}, nil
}

func OpenBluetooth(cfg BluetoothConfig) (io.ReadWriteCloser, error) {
	var err error
	cfg, err = normalizeBluetoothConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Address == "" {
		device, err := FindLW600P()
		if err != nil {
			return nil, err
		}
		cfg.Address = device.Address
	}

	address := C.CString(cfg.Address)
	defer C.free(unsafe.Pointer(address))

	var lastErr error
	for attempt := 1; attempt <= DefaultConnectAttempts; attempt++ {
		var errMsg *C.char
		conn := C.lwbt_open(address, C.int(cfg.ChannelID), C.uint(defaultBaud), &errMsg)
		if conn != nil {
			if cfg.OpenDelay > 0 {
				time.Sleep(cfg.OpenDelay)
			}
			return &bluetoothPort{conn: conn, readTimeout: cfg.ReadTimeout}, nil
		}
		lastErr = takeCError(errMsg)
		if attempt < DefaultConnectAttempts {
			time.Sleep(DefaultConnectRetryDelay)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unknown IOBluetooth error")
	}
	return nil, fmt.Errorf("open RFCOMM channel %d to %s failed after %d attempts: %w", cfg.ChannelID, cfg.Address, DefaultConnectAttempts, lastErr)
}

func (p *bluetoothPort) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if p.closed || p.conn == nil {
		return 0, io.EOF
	}
	timeoutMS := int(p.readTimeout.Milliseconds())
	if timeoutMS <= 0 {
		timeoutMS = int(DefaultReadTimeout.Milliseconds())
	}

	var code C.int
	var errMsg *C.char
	n := C.lwbt_read(p.conn, (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), C.int(timeoutMS), &code, &errMsg)
	if errMsg != nil {
		return int(n), takeCError(errMsg)
	}
	if code == 1 {
		return int(n), io.EOF
	}
	return int(n), nil
}

func (p *bluetoothPort) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if p.closed || p.conn == nil {
		return 0, io.ErrClosedPipe
	}

	var errMsg *C.char
	n := C.lwbt_write(p.conn, (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), &errMsg)
	if errMsg != nil {
		if n < 0 {
			n = 0
		}
		return int(n), takeCError(errMsg)
	}
	if n < 0 {
		return 0, fmt.Errorf("write RFCOMM data failed")
	}
	return int(n), nil
}

func (p *bluetoothPort) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	if p.conn == nil {
		return nil
	}

	var errMsg *C.char
	C.lwbt_close(p.conn, &errMsg)
	p.conn = nil
	if errMsg != nil {
		return takeCError(errMsg)
	}
	return nil
}

func (p *bluetoothPort) Drain() error {
	return nil
}

func takeCError(errMsg *C.char) error {
	if errMsg == nil {
		return fmt.Errorf("unknown IOBluetooth error")
	}
	defer C.free(unsafe.Pointer(errMsg))
	return fmt.Errorf("%s", C.GoString(errMsg))
}
