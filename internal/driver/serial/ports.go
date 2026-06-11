package serial

import (
	"fmt"
	"strings"

	"go.bug.st/serial"
)

// ATPorts lists candidate AT ports for Quectel EC25 on Linux.
var ATPorts = []string{"/dev/ttyUSB2", "/dev/ttyUSB3"}

// IsPortBusy reports whether a serial open error is caused by another process holding the port.
func IsPortBusy(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "busy")
}

func openAuto(cfg openConfig) (serial.Port, string, error) {
	if cfg.Device != "" && cfg.Device != "auto" {
		port, err := openPort(cfg)
		return port, cfg.Device, err
	}

	var lastErr error
	for _, device := range ATPorts {
		cfg.Device = device
		port, err := openPort(cfg)
		if err == nil {
			return port, device, nil
		}
		lastErr = err
		if !IsPortBusy(err) {
			return nil, device, err
		}
	}
	if lastErr != nil {
		return nil, "", fmt.Errorf("no available AT port (tried %v): %w", ATPorts, lastErr)
	}
	return nil, "", fmt.Errorf("no AT ports to probe")
}
