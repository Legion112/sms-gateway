package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
)

func TestDefaultDriverIsMM(t *testing.T) {
	cfg, err := config.Load(config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != config.DriverMM {
		t.Fatalf("driver = %q, want %q", cfg.Driver, config.DriverMM)
	}
	if cfg.Serial.Device != "auto" {
		t.Fatalf("serial device = %q, want auto", cfg.Serial.Device)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "driver: serial\nserial:\n  device: /dev/ttyUSB3\n  timeout: 3s\nmm:\n  modem_index: 1\n  timeout: 10s\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != config.DriverSerial {
		t.Fatalf("driver = %q", cfg.Driver)
	}
	if cfg.Serial.Device != "/dev/ttyUSB3" {
		t.Fatalf("device = %q", cfg.Serial.Device)
	}
	if cfg.Serial.Timeout != 3*time.Second {
		t.Fatalf("serial timeout = %v", cfg.Serial.Timeout)
	}
	if cfg.MM.ModemIndex != 1 {
		t.Fatalf("modem index = %d", cfg.MM.ModemIndex)
	}
	if cfg.MM.Timeout != 10*time.Second {
		t.Fatalf("mm timeout = %v", cfg.MM.Timeout)
	}
}

func TestOverridePrecedenceCLIOverEnvOverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("driver: serial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMS_GATEWAY_DRIVER", "mm")

	cfg, err := config.Load(config.Overrides{
		ConfigPath: path,
		Driver:     "serial",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != config.DriverSerial {
		t.Fatalf("driver = %q, want serial (CLI wins)", cfg.Driver)
	}
}

func TestUnknownDriverRejected(t *testing.T) {
	_, err := config.Load(config.Overrides{Driver: "usb"})
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}
