package cmdutil

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/factory"
	"github.com/legion/sms-gateway/internal/modem"
)

// Flags holds shared CLI flags for modem commands.
type Flags struct {
	ConfigPath string
	Driver     string
	Device     string
	Timeout    time.Duration
	ModemIndex int
	Verbose    bool
}

// LoadConfig loads configuration from flags.
func LoadConfig(f Flags) (config.Config, error) {
	var indexOverride *int
	if f.ModemIndex >= 0 {
		indexOverride = &f.ModemIndex
	}
	return config.Load(config.Overrides{
		ConfigPath: f.ConfigPath,
		Driver:     f.Driver,
		Device:     f.Device,
		Timeout:    f.Timeout,
		ModemIndex: indexOverride,
		Verbose:    f.Verbose,
	})
}

// OpenModem loads config and opens the modem driver.
func OpenModem(f Flags) (config.Config, modem.Modem, error) {
	cfg, err := LoadConfig(f)
	if err != nil {
		return cfg, nil, err
	}
	m, err := factory.New(cfg)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, m, nil
}

// Context returns a timeout context for modem operations.
func Context(cfg config.Config) (context.Context, context.CancelFunc) {
	timeout := cfg.MM.Timeout
	if cfg.Driver == config.DriverSerial {
		timeout = cfg.Serial.Timeout
	}
	return context.WithTimeout(context.Background(), timeout*2)
}

// SMSContext returns a longer timeout context for multi-step SMS status queries.
func SMSContext(cfg config.Config) (context.Context, context.CancelFunc) {
	timeout := cfg.MM.Timeout * 4
	if cfg.Driver == config.DriverSerial {
		timeout = cfg.Serial.Timeout * 4
	}
	return context.WithTimeout(context.Background(), timeout)
}

// SendContext returns a timeout context for outbound SMS operations.
func SendContext(cfg config.Config) (context.Context, context.CancelFunc) {
	timeout := cfg.MM.Timeout * 6
	if cfg.Driver == config.DriverSerial {
		timeout = cfg.Serial.Timeout * 6
	}
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

// PrintError writes an error to stderr.
func PrintError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
