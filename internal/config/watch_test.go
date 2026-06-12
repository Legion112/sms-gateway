package config_test

import (
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
