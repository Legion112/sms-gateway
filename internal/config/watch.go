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
	CatchUpOnStart        bool          `yaml:"catch_up_on_start"`
	DeleteAfterForward    bool          `yaml:"delete_after_forward"`
	SerialPollInterval    time.Duration `yaml:"-"`
	ConcatAssemblyTimeout time.Duration `yaml:"-"`
	ReconnectInitial      time.Duration `yaml:"-"`
	ReconnectMax          time.Duration `yaml:"-"`
	StallTimeout          time.Duration `yaml:"-"`
	HealthInterval        time.Duration `yaml:"-"`
	RegistrationInterval  time.Duration `yaml:"-"`
}

type watchFile struct {
	Storage struct {
		Path string `yaml:"path"`
	} `yaml:"storage"`
	Watch struct {
		CatchUpOnStart        bool   `yaml:"catch_up_on_start"`
		DeleteAfterForward    bool   `yaml:"delete_after_forward"`
		SerialPollInterval    string `yaml:"serial_poll_interval"`
		ConcatAssemblyTimeout string `yaml:"concat_assembly_timeout"`
		ReconnectInitial      string `yaml:"reconnect_initial"`
		ReconnectMax          string `yaml:"reconnect_max"`
		StallTimeout          string `yaml:"stall_timeout"`
		HealthInterval        string `yaml:"health_interval"`
		RegistrationInterval  string `yaml:"registration_interval"`
	} `yaml:"watch"`
}

func applyWatchFile(file *watchFile, cfg *Config) error {
	if file.Storage.Path != "" {
		cfg.Storage.Path = file.Storage.Path
	}
	cfg.Watch.CatchUpOnStart = file.Watch.CatchUpOnStart
	cfg.Watch.DeleteAfterForward = file.Watch.DeleteAfterForward
	for _, d := range []struct {
		key   string
		raw   string
		field *time.Duration
	}{
		{"serial_poll_interval", file.Watch.SerialPollInterval, &cfg.Watch.SerialPollInterval},
		{"concat_assembly_timeout", file.Watch.ConcatAssemblyTimeout, &cfg.Watch.ConcatAssemblyTimeout},
		{"reconnect_initial", file.Watch.ReconnectInitial, &cfg.Watch.ReconnectInitial},
		{"reconnect_max", file.Watch.ReconnectMax, &cfg.Watch.ReconnectMax},
		{"stall_timeout", file.Watch.StallTimeout, &cfg.Watch.StallTimeout},
		{"health_interval", file.Watch.HealthInterval, &cfg.Watch.HealthInterval},
		{"registration_interval", file.Watch.RegistrationInterval, &cfg.Watch.RegistrationInterval},
	} {
		if d.raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(d.raw)
		if err != nil {
			return fmt.Errorf("watch.%s: %w", d.key, err)
		}
		if parsed < 0 {
			return fmt.Errorf("watch.%s: must not be negative", d.key)
		}
		*d.field = parsed
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
	if err := ValidateAlerts(cfg); err != nil {
		return err
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

// ConcatAssemblyTimeout returns how long to wait for all concat parts before forwarding partial.
func (cfg Config) ConcatAssemblyTimeout() time.Duration {
	if cfg.Watch.ConcatAssemblyTimeout > 0 {
		return cfg.Watch.ConcatAssemblyTimeout
	}
	return 60 * time.Second
}

// ReconnectInitial returns the first delay before reopening a modem whose watch
// loop exited, with default 2s.
func (cfg Config) ReconnectInitial() time.Duration {
	if cfg.Watch.ReconnectInitial > 0 {
		return cfg.Watch.ReconnectInitial
	}
	return 2 * time.Second
}

// ReconnectMax caps the exponential reconnect backoff, with default 60s. A modem
// that stays unreachable is still retried at this interval forever: the daemon
// never gives up on a SIM.
func (cfg Config) ReconnectMax() time.Duration {
	initial := cfg.ReconnectInitial()
	if cfg.Watch.ReconnectMax > 0 {
		return max(cfg.Watch.ReconnectMax, initial)
	}
	return max(60*time.Second, initial)
}

// StallTimeout is how long a connected modem may go without a successful poll
// before it is reported stalled, with default 5m when unset.
func (cfg Config) StallTimeout() time.Duration {
	if cfg.Watch.StallTimeout > 0 {
		return cfg.Watch.StallTimeout
	}
	return 5 * time.Minute
}

// HealthInterval is how often a heartbeat line is logged for every modem, so the
// journal carries positive proof that reception is alive. Default 30m when unset.
func (cfg Config) HealthInterval() time.Duration {
	if cfg.Watch.HealthInterval > 0 {
		return cfg.Watch.HealthInterval
	}
	return 30 * time.Minute
}

// RegistrationInterval is how often a connected modem's per-domain registration
// is read, with default 2m when unset. It is a cadence, not a switch: the read
// always happens, and alerts.sms_path is what decides whether its verdict wakes
// anybody.
//
// This is a poll rather than an event because the condition it looks for is a
// non-event: a modem drops its circuit-switched registration and reports
// nothing, keeps answering AT and keeps its data attach. The read costs four AT
// commands and runs between SMS polls on the goroutine that owns the port, so
// the interval trades how fast that is noticed against how much of the port's
// time is spent not looking for messages.
func (cfg Config) RegistrationInterval() time.Duration {
	if cfg.Watch.RegistrationInterval > 0 {
		return cfg.Watch.RegistrationInterval
	}
	return 2 * time.Minute
}
