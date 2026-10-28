package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
)

func TestValidateWatch(t *testing.T) {
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{
			"m1": {Driver: config.DriverMM},
		},
		Channels: map[string]config.ChannelConfig{
			"tg": {Driver: "telegram_bot", Telegram: config.TelegramChannelConfig{ChatID: 1, BotToken: "x"}},
		},
		ForwardRules: []config.ForwardRule{
			{Name: "all", Modem: "m1", To: []string{"tg"}},
		},
		Storage: config.StorageConfig{Path: "./data/sms.db"},
	}
	if err := config.ValidateWatch(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWatchRejectsStubChannel(t *testing.T) {
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
		Channels: map[string]config.ChannelConfig{
			"email": {Driver: "email"},
		},
		ForwardRules: []config.ForwardRule{{To: []string{"email"}}},
		Storage:      config.StorageConfig{Path: "./data/sms.db"},
	}
	if err := config.ValidateWatch(cfg); err == nil {
		t.Fatal("expected error for stub channel driver")
	}
}

func TestLoadDeleteAfterForward(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"absent defaults off", "", false},
		{"explicit off", "  delete_after_forward: false\n", false},
		{"explicit on", "  delete_after_forward: true\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := `driver: serial
modems:
  ec25-main:
    driver: serial
    serial:
      device: /dev/ttyUSB3
channels:
  tg:
    driver: telegram_bot
    telegram:
      chat_id: 123
      bot_token: x
forward_rules:
  - name: all
    from: "*"
    to: [tg]
storage:
  path: ./data/sms.db
watch:
  catch_up_on_start: true
` + tc.line
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(config.Overrides{ConfigPath: path})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Watch.DeleteAfterForward != tc.want {
				t.Fatalf("delete_after_forward=%v want %v", cfg.Watch.DeleteAfterForward, tc.want)
			}
		})
	}
}

// writeWatchConfig writes a minimal valid config with extra lines appended under
// the watch: block.
func writeWatchConfig(t *testing.T, watchLines string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `driver: serial
modems:
  ec25-main:
    driver: serial
    serial:
      device: /dev/ttyUSB3
channels:
  tg:
    driver: telegram_bot
    telegram:
      chat_id: 123
      bot_token: x
forward_rules:
  - name: all
    from: "*"
    to: [tg]
storage:
  path: ./data/sms.db
watch:
  catch_up_on_start: true
` + watchLines
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLivenessDefaults(t *testing.T) {
	cfg, err := config.Load(config.Overrides{ConfigPath: writeWatchConfig(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"reconnect_initial", cfg.ReconnectInitial(), 2 * time.Second},
		{"reconnect_max", cfg.ReconnectMax(), 60 * time.Second},
		{"stall_timeout", cfg.StallTimeout(), 5 * time.Minute},
		{"health_interval", cfg.HealthInterval(), 30 * time.Minute},
		{"registration_interval", cfg.RegistrationInterval(), 2 * time.Minute},
	} {
		if tc.got != tc.want {
			t.Errorf("%s default = %s want %s", tc.name, tc.got, tc.want)
		}
	}
}

func TestLivenessSettingsLoad(t *testing.T) {
	path := writeWatchConfig(t, `  reconnect_initial: 500ms
  reconnect_max: 2m
  stall_timeout: 90s
  health_interval: 10m
  registration_interval: 30s
`)
	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"reconnect_initial", cfg.ReconnectInitial(), 500 * time.Millisecond},
		{"reconnect_max", cfg.ReconnectMax(), 2 * time.Minute},
		{"stall_timeout", cfg.StallTimeout(), 90 * time.Second},
		{"health_interval", cfg.HealthInterval(), 10 * time.Minute},
		{"registration_interval", cfg.RegistrationInterval(), 30 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %s want %s", tc.name, tc.got, tc.want)
		}
	}
}

// TestReconnectMaxNeverBelowInitial keeps a misconfigured pair from producing a
// backoff that shrinks instead of growing.
func TestReconnectMaxNeverBelowInitial(t *testing.T) {
	path := writeWatchConfig(t, `  reconnect_initial: 30s
  reconnect_max: 5s
`)
	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ReconnectMax(); got != 30*time.Second {
		t.Fatalf("reconnect_max=%s want it raised to reconnect_initial (30s)", got)
	}
}

func TestLivenessSettingsRejectBadValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
	}{
		{"unparseable", "  reconnect_initial: soon\n"},
		{"negative", "  stall_timeout: -5m\n"},
		{"unparseable registration interval", "  registration_interval: often\n"},
		{"negative registration interval", "  registration_interval: -2m\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(config.Overrides{ConfigPath: writeWatchConfig(t, tc.lines)})
			if err == nil {
				t.Fatal("expected config load to fail")
			}
		})
	}
}

func TestAlertsDefaults(t *testing.T) {
	cfg, err := config.Load(config.Overrides{ConfigPath: writeWatchConfig(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AlertAfter(); got != 5*time.Minute {
		t.Errorf("alerts.after default = %s want 5m", got)
	}
	if cfg.AlertsEnabled() {
		t.Error("alerting should be off when no channels are listed")
	}
	// On by default: the condition it reports is invisible everywhere else, so
	// an operator who configured alerts at all should hear about it.
	if !cfg.SMSPathAlerts() {
		t.Error("alerts.sms_path should default to on")
	}
}

// The sms_path switch silences one alert without touching the other, and
// leaving it out is not the same as setting it to false.
func TestSMSPathAlertSwitch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		want  bool
	}{
		{"absent", "alerts:\n  channels: [tg]\n", true},
		{"explicitly on", "alerts:\n  channels: [tg]\n  sms_path: true\n", true},
		{"off", "alerts:\n  channels: [tg]\n  sms_path: false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeWatchConfig(t, "")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content = append(content, []byte("\n"+tc.lines)...)
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(config.Overrides{ConfigPath: path})
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.SMSPathAlerts(); got != tc.want {
				t.Errorf("SMSPathAlerts() = %v, want %v", got, tc.want)
			}
			if !cfg.AlertsEnabled() {
				t.Error("the sms_path switch must not affect whether alerting is on")
			}
		})
	}
}

func TestAlertsLoad(t *testing.T) {
	path := writeWatchConfig(t, "")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("\nalerts:\n  after: 90s\n  channels: [tg]\n")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AlertAfter(); got != 90*time.Second {
		t.Errorf("alerts.after = %s want 90s", got)
	}
	if !cfg.AlertsEnabled() {
		t.Error("alerting should be on")
	}
	if len(cfg.Alerts.Channels) != 1 || cfg.Alerts.Channels[0] != "tg" {
		t.Errorf("channels = %v", cfg.Alerts.Channels)
	}
}

// TestAlertsRejectUnknownChannel: a typo here is silent in the worst way,
// so it must fail at load rather than on the day a modem dies.
func TestAlertsRejectUnknownChannel(t *testing.T) {
	cfg := config.Config{
		Modems:   map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
		Channels: map[string]config.ChannelConfig{"tg": {Driver: "telegram_bot", Telegram: config.TelegramChannelConfig{ChatID: 1, BotToken: "x"}}},
		ForwardRules: []config.ForwardRule{
			{Name: "all", Modem: "m1", To: []string{"tg"}},
		},
		Storage: config.StorageConfig{Path: "./data/sms.db"},
		Alerts:  config.AlertsConfig{Channels: []string{"typo-telegram"}},
	}
	err := config.ValidateWatch(cfg)
	if err == nil {
		t.Fatal("expected an error for an alert channel that does not exist")
	}
	if !strings.Contains(err.Error(), "typo-telegram") {
		t.Errorf("error %v should name the bad channel", err)
	}
}

func TestAlertsRejectNegativeAfter(t *testing.T) {
	path := writeWatchConfig(t, "")
	content, _ := os.ReadFile(path)
	content = append(content, []byte("\nalerts:\n  after: -5m\n")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(config.Overrides{ConfigPath: path}); err == nil {
		t.Fatal("expected a negative alerts.after to fail")
	}
}

// TestExampleConfigLoads keeps the documented example honest. It is the first
// thing an operator copies, so a key that no longer parses, or a default the
// comments describe wrongly, is a working config turned into a broken one.
func TestExampleConfigLoads(t *testing.T) {
	// The example deliberately keeps every bot token out of the file.
	for _, env := range []string{
		"SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN",
		"SMS_GATEWAY_CHANNEL_VIKA_TELEGRAM_BOT_TOKEN",
	} {
		t.Setenv(env, "123456:test-token")
	}

	cfg, err := config.Load(config.Overrides{ConfigPath: filepath.Join("..", "..", "config.example.yaml")})
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
	if err := config.ValidateWatch(cfg); err != nil {
		t.Fatalf("config.example.yaml is not a valid watch config: %v", err)
	}
	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"registration_interval", cfg.RegistrationInterval(), 2 * time.Minute},
		{"alerts.after", cfg.AlertAfter(), 5 * time.Minute},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %s, want the documented %s", tc.name, tc.got, tc.want)
		}
	}
	if !cfg.SMSPathAlerts() {
		t.Error("the example sets alerts.sms_path: true but it did not take effect")
	}
	if !cfg.AlertsEnabled() {
		t.Error("the example configures alert channels but alerting reads as off")
	}
}

// The daemon publishes its view beside the message database by default, which
// is the one directory it is already known to be able to write.
func TestStatePathDefaultsBesideTheDatabase(t *testing.T) {
	cfg, err := config.Load(config.Overrides{ConfigPath: writeWatchConfig(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("data", "watch-state.json")
	if got := cfg.StatePath(); got != want {
		t.Errorf("StatePath() = %q, want %q", got, want)
	}
}

func TestStatePathLoads(t *testing.T) {
	path := writeWatchConfig(t, "  state_path: /run/sms-gateway/state.json\n")
	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.StatePath(); got != "/run/sms-gateway/state.json" {
		t.Errorf("StatePath() = %q, want the configured path", got)
	}
}

// No database configured means nowhere to derive a default from, which switches
// publishing off rather than guessing at a writable directory.
func TestStatePathEmptyWithoutStorage(t *testing.T) {
	if got := (config.Config{}).StatePath(); got != "" {
		t.Errorf("StatePath() = %q, want empty", got)
	}
}
