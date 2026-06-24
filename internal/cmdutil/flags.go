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
	Modem      string
	Verbose    bool
}

// ModemHandle is an opened modem target.
type ModemHandle struct {
	Name   string
	Config config.Config
	Modem  modem.Modem
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

// OpenModem loads config and opens the modem driver (legacy single target).
func OpenModem(f Flags) (config.Config, modem.Modem, error) {
	base, targets, err := OpenModemTargets(f)
	if err != nil {
		return base, nil, err
	}
	if len(targets) != 1 {
		return base, nil, fmt.Errorf("expected one modem target, got %d", len(targets))
	}
	return targets[0].Config, targets[0].Modem, nil
}

// OpenModemTargets resolves modem names and opens each driver.
func OpenModemTargets(f Flags) (config.Config, []ModemHandle, error) {
	base, err := LoadConfig(f)
	if err != nil {
		return base, nil, err
	}
	names, err := base.ResolveModemTargets(f.Modem)
	if err != nil {
		return base, nil, err
	}
	handles := make([]ModemHandle, 0, len(names))
	for _, name := range names {
		h, err := OpenModemTarget(base, name, f)
		if err != nil {
			for _, opened := range handles {
				_ = opened.Modem.Close()
			}
			return base, nil, err
		}
		handles = append(handles, h)
	}
	return base, handles, nil
}

// OpenModemTarget opens one modem by config name (empty = legacy top-level).
func OpenModemTarget(base config.Config, name string, f Flags) (ModemHandle, error) {
	var cfg config.Config
	var m modem.Modem
	var err error

	if name == "" {
		cfg = base
		m, err = factory.New(cfg)
	} else {
		cfg, err = base.ModemConfig(name)
		if err != nil {
			return ModemHandle{}, err
		}
		applyTargetOverrides(&cfg, f)
		m, err = factory.New(cfg)
	}
	if err != nil {
		return ModemHandle{}, err
	}
	return ModemHandle{Name: name, Config: cfg, Modem: m}, nil
}

// OpenSendModem opens the modem used by the send command.
func OpenSendModem(f Flags) (ModemHandle, error) {
	base, err := LoadConfig(f)
	if err != nil {
		return ModemHandle{}, err
	}
	name, err := base.SendModemTarget(f.Modem)
	if err != nil {
		return ModemHandle{}, err
	}
	return OpenModemTarget(base, name, f)
}

func applyTargetOverrides(cfg *config.Config, f Flags) {
	if f.Driver != "" {
		cfg.Driver = f.Driver
	}
	if f.Device != "" {
		cfg.Serial.Device = f.Device
	}
	if f.Timeout > 0 {
		cfg.Serial.Timeout = f.Timeout
		cfg.MM.Timeout = f.Timeout
	}
	if f.ModemIndex >= 0 {
		cfg.MM.ModemIndex = f.ModemIndex
	}
}

// PrintSection prints a modem section header for multi-modem output.
func PrintSection(name string, multi bool) {
	if !multi || name == "" {
		return
	}
	fmt.Printf("=== %s ===\n", name)
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
