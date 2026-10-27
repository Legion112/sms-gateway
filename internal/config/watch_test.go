package config_test

import (
	"os"
	"path/filepath"
	"testing"

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
