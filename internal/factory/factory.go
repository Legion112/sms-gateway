package factory

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/driver/mm"
	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/legion/sms-gateway/internal/modem"
)

// New creates a modem driver from configuration.
func New(cfg config.Config) (modem.Modem, error) {
	switch cfg.Driver {
	case config.DriverMM:
		return mm.New(cfg.MM, cfg.Verbose)
	case config.DriverSerial:
		return serial.New(cfg.Serial, cfg.Verbose)
	default:
		return nil, fmt.Errorf("unknown driver %q", cfg.Driver)
	}
}
