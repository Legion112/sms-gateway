package config

import (
	"fmt"
	"time"
)

// AlertsConfig controls reporting about the gateway's own health, as opposed to
// forwarding received SMS. It is a section of its own rather than a few more
// keys under watch: today alerts leave through the same channels that carry
// forwarded SMS, but the intent is a separate notification service, and this is
// where that would be configured.
type AlertsConfig struct {
	// After is how long a modem may go without a successful poll before it is
	// reported. Zero uses the default; see Config.AlertAfter.
	After time.Duration `yaml:"-"`
	// Channels names the forward channels alerts are sent to. Empty switches
	// alerting off.
	Channels []string `yaml:"channels"`
}

type alertsFile struct {
	Alerts struct {
		After    string   `yaml:"after"`
		Channels []string `yaml:"channels"`
	} `yaml:"alerts"`
}

func applyAlertsFile(file *alertsFile, cfg *Config) error {
	cfg.Alerts.Channels = file.Alerts.Channels
	if file.Alerts.After == "" {
		return nil
	}
	d, err := time.ParseDuration(file.Alerts.After)
	if err != nil {
		return fmt.Errorf("alerts.after: %w", err)
	}
	if d < 0 {
		return fmt.Errorf("alerts.after: must not be negative")
	}
	cfg.Alerts.After = d
	return nil
}

// AlertAfter is how long a modem may be silent before it is reported, with
// default 5m when unset.
func (cfg Config) AlertAfter() time.Duration {
	if cfg.Alerts.After > 0 {
		return cfg.Alerts.After
	}
	return 5 * time.Minute
}

// AlertsEnabled reports whether anywhere is configured to receive alerts.
func (cfg Config) AlertsEnabled() bool {
	return len(cfg.Alerts.Channels) > 0
}

// ValidateAlerts checks that every alert destination is a channel that exists.
// A typo here is silent in the worst possible way: nothing looks wrong until
// the day a modem dies and nobody is told.
func ValidateAlerts(cfg Config) error {
	for _, name := range cfg.Alerts.Channels {
		if _, ok := cfg.Channels[name]; !ok {
			return fmt.Errorf("alerts: channel %q not found", name)
		}
	}
	return nil
}
