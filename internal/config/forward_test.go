package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
)

func TestValidateForwardingRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
driver: mm
modems:
  ec25-main:
    driver: mm
    mm:
      modem_index: 0
channels:
  my-telegram:
    driver: telegram_bot
    telegram:
      chat_id: 123
forward_rules:
  - name: default
    modem: ec25-main
    from: "*"
    to: [my-telegram]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN", "test-token")

	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 1 {
		t.Fatalf("channels=%d", len(cfg.Channels))
	}
	if cfg.Channels["my-telegram"].Telegram.BotToken != "test-token" {
		t.Fatalf("token not applied from env")
	}
}

func TestRejectLegacyPhoneNumber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
driver: mm
modems:
  m1:
    driver: mm
    phone_number: "+998901234567"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(config.Overrides{ConfigPath: path})
	if err == nil || !strings.Contains(err.Error(), "phone_number is removed") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateForwardingDanglingChannel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
driver: mm
modems:
  m1:
    driver: mm
forward_rules:
  - name: bad
    modem: m1
    to: [missing]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(config.Overrides{ConfigPath: path})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateToLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
driver: mm
modems:
  m1:
    driver: mm
channels:
  my-telegram:
    driver: telegram_bot
    telegram:
      chat_id: 123
forward_rules:
  - name: line-a
    to_line: "+15550001001"
    from: "*"
    to: [my-telegram]
  - name: default
    to_line: "*"
    from: "*"
    to: [my-telegram]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN", "test-token")

	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ForwardRules[0].ToLine != "+15550001001" {
		t.Fatalf("to_line=%q", cfg.ForwardRules[0].ToLine)
	}
}

func TestRejectInvalidToLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
driver: mm
modems:
  m1:
    driver: mm
channels:
  my-telegram:
    driver: telegram_bot
    telegram:
      chat_id: 123
forward_rules:
  - name: bad
    to_line: "not-a-number"
    to: [my-telegram]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN", "test-token")

	_, err := config.Load(config.Overrides{ConfigPath: path})
	if err == nil || !strings.Contains(err.Error(), "to_line") {
		t.Fatalf("err=%v", err)
	}
}
