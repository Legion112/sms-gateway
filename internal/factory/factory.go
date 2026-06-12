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
	return NewFromModemEntry(config.ModemEntry{
		Driver: cfg.Driver,
		Serial: cfg.Serial,
		MM:     cfg.MM,
	}, cfg.Verbose)
}

// NewFromModemEntry creates a modem driver from a named modem config entry.
func NewFromModemEntry(entry config.ModemEntry, verbose bool) (modem.Modem, error) {
	switch entry.Driver {
	case config.DriverMM:
		return mm.New(entry.MM, verbose)
	case config.DriverSerial:
		return serial.New(entry.Serial, verbose)
	default:
		return nil, fmt.Errorf("unknown driver %q", entry.Driver)
	}
}
