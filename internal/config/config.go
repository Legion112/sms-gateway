package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DriverMM     = "mm"
	DriverSerial = "serial"

	envConfig       = "SMS_GATEWAY_CONFIG"
	envDriver       = "SMS_GATEWAY_DRIVER"
	envModemDevice  = "MODEM_DEVICE"
	envModemTimeout = "MODEM_TIMEOUT"
	envModemIndex   = "MODEM_INDEX"
)

// Config holds application configuration.
type Config struct {
	Driver       string                   `yaml:"driver"`
	Serial       SerialConfig             `yaml:"serial"`
	MM           MMConfig                 `yaml:"mm"`
	DefaultModem string                   `yaml:"default_modem"`
	Modems       map[string]ModemEntry    `yaml:"modems"`
	Channels     map[string]ChannelConfig `yaml:"channels"`
	ForwardRules []ForwardRule            `yaml:"forward_rules"`
	Verbose      bool                     `yaml:"-"`
}

// SerialConfig holds direct AT serial driver settings.
type SerialConfig struct {
	Device   string        `yaml:"device"`
	BaudRate int           `yaml:"baud_rate"`
	Timeout  time.Duration `yaml:"timeout"`
}

// MMConfig holds ModemManager driver settings.
type MMConfig struct {
	ModemIndex int           `yaml:"modem_index"`
	ModemPath  string        `yaml:"modem_path"`
	Timeout    time.Duration `yaml:"timeout"`
}

// Overrides carries CLI flag overrides. Empty/zero values are ignored.
type Overrides struct {
	ConfigPath string
	Driver     string
	Device     string
	Timeout    time.Duration
	ModemIndex *int
	Verbose    bool
}

// Default returns built-in defaults (driver: mm).
func Default() Config {
	return Config{
		Driver: DriverMM,
		Serial: SerialConfig{
			Device:   "auto",
			BaudRate: 115200,
			Timeout:  2 * time.Second,
		},
		MM: MMConfig{
			ModemIndex: 0,
			Timeout:    5 * time.Second,
		},
	}
}

// Load resolves config: defaults -> YAML file -> env -> CLI overrides.
func Load(overrides Overrides) (Config, error) {
	cfg := Default()

	path, err := resolveConfigPath(overrides.ConfigPath)
	if err != nil {
		return cfg, err
	}
	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return cfg, err
		}
	}

	applyEnv(&cfg)
	applyOverrides(&cfg, overrides)

	if cfg.Driver != DriverMM && cfg.Driver != DriverSerial {
		return cfg, fmt.Errorf("unknown driver %q (want %q or %q)", cfg.Driver, DriverMM, DriverSerial)
	}

	applyForwardingEnv(&cfg)
	if err := validateForwarding(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config file %q: %w", explicit, err)
		}
		return explicit, nil
	}
	if p := os.Getenv(envConfig); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("config file %q: %w", p, err)
		}
		return p, nil
	}
	candidates := []string{"config.yaml", "/etc/sms-gateway/config.yaml"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var file struct {
		Driver string `yaml:"driver"`
		Serial struct {
			Device   string `yaml:"device"`
			BaudRate int    `yaml:"baud_rate"`
			Timeout  string `yaml:"timeout"`
		} `yaml:"serial"`
		MM struct {
			ModemIndex int    `yaml:"modem_index"`
			ModemPath  string `yaml:"modem_path"`
			Timeout    string `yaml:"timeout"`
		} `yaml:"mm"`
		forwardFile `yaml:",inline"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if file.Driver != "" {
		cfg.Driver = file.Driver
	}
	if file.Serial.Device != "" {
		cfg.Serial.Device = file.Serial.Device
	}
	if file.Serial.BaudRate != 0 {
		cfg.Serial.BaudRate = file.Serial.BaudRate
	}
	if file.Serial.Timeout != "" {
		d, err := time.ParseDuration(file.Serial.Timeout)
		if err != nil {
			return fmt.Errorf("serial.timeout: %w", err)
		}
		cfg.Serial.Timeout = d
	}
	if file.MM.ModemIndex != 0 {
		cfg.MM.ModemIndex = file.MM.ModemIndex
	}
	if file.MM.ModemPath != "" {
		cfg.MM.ModemPath = file.MM.ModemPath
	}
	if file.MM.Timeout != "" {
		d, err := time.ParseDuration(file.MM.Timeout)
		if err != nil {
			return fmt.Errorf("mm.timeout: %w", err)
		}
		cfg.MM.Timeout = d
	}

	if err := applyModemTimeouts(cfg.Modems); err != nil {
		return err
	}
	return applyForwardingFile(&file.forwardFile, cfg)
}

func applyModemTimeouts(modems map[string]ModemEntry) error {
	for name, m := range modems {
		if m.Serial.Timeout == 0 {
			m.Serial.Timeout = 2 * time.Second
		}
		if m.MM.Timeout == 0 {
			m.MM.Timeout = 5 * time.Second
		}
		modems[name] = m
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv(envDriver); v != "" {
		cfg.Driver = v
	}
	if v := os.Getenv(envModemDevice); v != "" {
		cfg.Serial.Device = v
	}
	if v := os.Getenv(envModemTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			cfg.Serial.Timeout = d
			cfg.MM.Timeout = d
		}
	}
	if v := os.Getenv(envModemIndex); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MM.ModemIndex = i
		}
	}
}

func applyOverrides(cfg *Config, o Overrides) {
	if o.Driver != "" {
		cfg.Driver = o.Driver
	}
	if o.Device != "" {
		cfg.Serial.Device = o.Device
	}
	if o.Timeout > 0 {
		cfg.Serial.Timeout = o.Timeout
		cfg.MM.Timeout = o.Timeout
	}
	if o.ModemIndex != nil {
		cfg.MM.ModemIndex = *o.ModemIndex
	}
	cfg.Verbose = o.Verbose
}
