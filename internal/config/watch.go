package config

import (
	"fmt"
	"time"
)

// StorageConfig holds local persistence settings.
type StorageConfig struct {
	Path string `yaml:"path"`
}

// WatchConfig holds daemon runtime settings.
type WatchConfig struct {
	CatchUpOnStart     bool          `yaml:"catch_up_on_start"`
	SerialPollInterval time.Duration `yaml:"-"`
}

type watchFile struct {
	Storage struct {
		Path string `yaml:"path"`
	} `yaml:"storage"`
	Watch struct {
		CatchUpOnStart     bool   `yaml:"catch_up_on_start"`
		SerialPollInterval string `yaml:"serial_poll_interval"`
	} `yaml:"watch"`
}

func applyWatchFile(file *watchFile, cfg *Config) error {
	if file.Storage.Path != "" {
		cfg.Storage.Path = file.Storage.Path
	}
	cfg.Watch.CatchUpOnStart = file.Watch.CatchUpOnStart
	if file.Watch.SerialPollInterval != "" {
		d, err := time.ParseDuration(file.Watch.SerialPollInterval)
		if err != nil {
			return fmt.Errorf("watch.serial_poll_interval: %w", err)
		}
		cfg.Watch.SerialPollInterval = d
	}
	return nil
}

// ValidateWatch checks config required for sms-gateway-watch.
func ValidateWatch(cfg Config) error {
	if len(cfg.Modems) == 0 {
		return fmt.Errorf("watch: modems must be configured")
	}
	if len(cfg.Channels) == 0 {
		return fmt.Errorf("watch: channels must be configured")
	}
	if len(cfg.ForwardRules) == 0 {
		return fmt.Errorf("watch: forward_rules must be configured")
	}
	if cfg.Storage.Path == "" {
		return fmt.Errorf("watch: storage.path is required")
	}

	seen := make(map[string]struct{})
	for _, rule := range cfg.ForwardRules {
		for _, chName := range rule.To {
			seen[chName] = struct{}{}
		}
	}
	for chName := range seen {
		ch, ok := cfg.Channels[chName]
		if !ok {
			return fmt.Errorf("watch: channel %q not found", chName)
		}
		if ch.Driver != "telegram_bot" {
			return fmt.Errorf("watch: channel %q uses unimplemented driver %q", chName, ch.Driver)
		}
	}
	return nil
}

// SerialPollInterval returns serial poll interval with default 10s.
func (cfg Config) SerialPollInterval() time.Duration {
	if cfg.Watch.SerialPollInterval > 0 {
		return cfg.Watch.SerialPollInterval
	}
	return 10 * time.Second
}
