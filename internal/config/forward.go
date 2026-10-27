package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ModemEntry is one named modem (hardware access only).
type ModemEntry struct {
	Driver string       `yaml:"driver"`
	Serial SerialConfig `yaml:"serial"`
	MM     MMConfig     `yaml:"mm"`
	// Radio is the RAT and operator-selection posture to hold this modem in.
	// The zero value means automatic for both; see RadioConfig.
	Radio RadioConfig `yaml:"radio"`
	// Removed fields rejected at validate: phone_number, phone_ussd, expected_iccid
	PhoneNumber   string `yaml:"phone_number,omitempty"`
	PhoneUSSD     string `yaml:"phone_ussd,omitempty"`
	ExpectedICCID string `yaml:"expected_iccid,omitempty"`
}

// ChannelConfig is one named forward destination.
type ChannelConfig struct {
	Driver   string                `yaml:"driver"`
	Telegram TelegramChannelConfig `yaml:"telegram"`
	SMS      SMSChannelConfig      `yaml:"sms"`
	Email    EmailChannelConfig    `yaml:"email"`
}

// TelegramChannelConfig holds telegram_bot channel settings.
type TelegramChannelConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   int64  `yaml:"chat_id"`
	ProxyURL string `yaml:"proxy_url"`
}

// SMSChannelConfig holds sms forward channel settings (future).
type SMSChannelConfig struct {
	Modem  string `yaml:"modem"`
	Number string `yaml:"number"`
}

// EmailChannelConfig holds email forward channel settings (future).
type EmailChannelConfig struct {
	SMTPHost string `yaml:"smtp_host"`
	To       string `yaml:"to"`
}

// ForwardRule routes inbound SMS to one or more channels.
type ForwardRule struct {
	Name   string   `yaml:"name"`
	Modem  string   `yaml:"modem"`
	From   string   `yaml:"from"`
	ToLine string   `yaml:"to_line"` // recipient MSISDN from sim_numbers; "" or "*" = any
	To     []string `yaml:"to"`
}

func applyForwardingFile(file *forwardFile, cfg *Config) error {
	if file.DefaultModem != "" {
		cfg.DefaultModem = file.DefaultModem
	}
	if len(file.Modems) > 0 {
		cfg.Modems = file.Modems
	}
	if len(file.SimNumbers) > 0 {
		cfg.SimNumbers = file.SimNumbers
	}
	if len(file.Channels) > 0 {
		cfg.Channels = file.Channels
	}
	if len(file.ForwardRules) > 0 {
		cfg.ForwardRules = file.ForwardRules
	}
	return nil
}

func applyForwardingEnv(cfg *Config) {
	for name, ch := range cfg.Channels {
		if ch.Driver != "telegram_bot" {
			continue
		}
		if ch.Telegram.BotToken != "" {
			continue
		}
		key := "SMS_GATEWAY_CHANNEL_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_BOT_TOKEN"
		if v := os.Getenv(key); v != "" {
			ch.Telegram.BotToken = v
			cfg.Channels[name] = ch
			continue
		}
		if v := os.Getenv("SMS_GATEWAY_TELEGRAM_BOT_TOKEN"); v != "" {
			ch.Telegram.BotToken = v
			cfg.Channels[name] = ch
		}
	}
}

func validateForwarding(cfg Config) error {
	modemNames := make(map[string]struct{}, len(cfg.Modems))
	for name, m := range cfg.Modems {
		if m.Driver == "" {
			return fmt.Errorf("modems.%s: driver is required", name)
		}
		if m.Driver != DriverMM && m.Driver != DriverSerial {
			return fmt.Errorf("modems.%s: unknown driver %q", name, m.Driver)
		}
		if err := validateModemEntryLegacyFields(name, m); err != nil {
			return err
		}
		if err := validateRadio(fmt.Sprintf("modems.%s", name), m.Radio); err != nil {
			return err
		}
		if m.ExpectedICCID != "" {
			return fmt.Errorf("modems.%s: expected_iccid is removed; use top-level sim_numbers keyed by ICCID", name)
		}
		modemNames[name] = struct{}{}
	}

	if err := validateSimNumbers(cfg.SimNumbers); err != nil {
		return err
	}

	channelNames := make(map[string]struct{}, len(cfg.Channels))
	for name, ch := range cfg.Channels {
		if ch.Driver == "" {
			return fmt.Errorf("channels.%s: driver is required", name)
		}
		switch ch.Driver {
		case "telegram_bot":
			if ch.Telegram.ChatID == 0 {
				return fmt.Errorf("channels.%s: telegram.chat_id is required", name)
			}
			if ch.Telegram.BotToken == "" {
				key := "SMS_GATEWAY_CHANNEL_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_BOT_TOKEN"
				if os.Getenv(key) == "" && os.Getenv("SMS_GATEWAY_TELEGRAM_BOT_TOKEN") == "" {
					return fmt.Errorf("channels.%s: telegram.bot_token is required (config or %s)", name, key)
				}
			}
		case "telegram_secret", "email", "sms":
			// stubs — config shape only
		default:
			return fmt.Errorf("channels.%s: unknown driver %q", name, ch.Driver)
		}
		channelNames[name] = struct{}{}
	}

	for i, rule := range cfg.ForwardRules {
		label := ruleLabel(rule, i)
		if rule.Modem != "" {
			if _, ok := modemNames[rule.Modem]; !ok {
				return fmt.Errorf("%s: modem %q not found in modems", label, rule.Modem)
			}
		}
		if err := validateToLine(label, rule.ToLine); err != nil {
			return err
		}
		if len(rule.To) == 0 {
			return fmt.Errorf("%s: to must list at least one channel", label)
		}
		for _, chName := range rule.To {
			if _, ok := channelNames[chName]; !ok {
				return fmt.Errorf("%s: channel %q not found in channels", label, chName)
			}
		}
	}

	if cfg.DefaultModem != "" {
		if _, ok := modemNames[cfg.DefaultModem]; !ok {
			return fmt.Errorf("default_modem %q not found in modems", cfg.DefaultModem)
		}
	}

	return nil
}

func ruleLabel(rule ForwardRule, index int) string {
	if rule.Name != "" {
		return fmt.Sprintf("forward_rules.%s", rule.Name)
	}
	return fmt.Sprintf("forward_rules[%d]", index)
}

func validateToLine(label, toLine string) error {
	pattern := strings.TrimSpace(toLine)
	if pattern == "" || pattern == "*" {
		return nil
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if prefix == "" {
			return fmt.Errorf("%s: to_line %q is invalid", label, toLine)
		}
		return nil
	}
	if !e164Re.MatchString(pattern) {
		return fmt.Errorf("%s: to_line must be E.164, \"*\", or a prefix wildcard (e.g. +1555*)", label)
	}
	return nil
}

// ModemConfig returns a Config for factory.New from a named modem entry.
func (cfg Config) ModemConfig(name string) (Config, error) {
	entry, ok := cfg.Modems[name]
	if !ok {
		return Config{}, fmt.Errorf("modem %q not found", name)
	}
	out := Default()
	out.Driver = entry.Driver
	out.Serial = entry.Serial
	out.MM = entry.MM
	out.Verbose = cfg.Verbose
	if out.Serial.Timeout == 0 {
		out.Serial.Timeout = 2 * time.Second
	}
	if out.MM.Timeout == 0 {
		out.MM.Timeout = 5 * time.Second
	}
	return out, nil
}

type forwardFile struct {
	DefaultModem string                   `yaml:"default_modem"`
	SimNumbers   map[string]string        `yaml:"sim_numbers"`
	Modems       map[string]ModemEntry    `yaml:"modems"`
	Channels     map[string]ChannelConfig `yaml:"channels"`
	ForwardRules []ForwardRule            `yaml:"forward_rules"`
}
