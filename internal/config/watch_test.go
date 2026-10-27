package config_test

import (
	"os"
	"path/filepath"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(config.Overrides{ConfigPath: writeWatchConfig(t, tc.lines)})
			if err == nil {
				t.Fatal("expected config load to fail")
			}
		})
	}
}
