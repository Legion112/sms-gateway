package config_test

import (
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
)

func TestResolveModemTargetsLegacy(t *testing.T) {
	cfg := config.Default()
	targets, err := cfg.ResolveModemTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "" {
		t.Fatalf("got %v, want [\"\"]", targets)
	}
}

func TestResolveModemTargetsAll(t *testing.T) {
	cfg := config.Default()
	cfg.Modems = map[string]config.ModemEntry{
		"b": {Driver: config.DriverMM},
		"a": {Driver: config.DriverMM},
	}
	targets, err := cfg.ResolveModemTargets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] != "a" || targets[1] != "b" {
		t.Fatalf("got %v, want [a b]", targets)
	}
}

func TestResolveModemTargetsNamed(t *testing.T) {
	cfg := config.Default()
	cfg.Modems = map[string]config.ModemEntry{
		"m1": {Driver: config.DriverMM},
		"m2": {Driver: config.DriverMM},
	}
	targets, err := cfg.ResolveModemTargets("m2")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "m2" {
		t.Fatalf("got %v", targets)
	}
}

func TestResolveModemTargetsMissing(t *testing.T) {
	cfg := config.Default()
	cfg.Modems = map[string]config.ModemEntry{
		"m1": {Driver: config.DriverMM},
	}
	_, err := cfg.ResolveModemTargets("missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSendModemTargetLegacy(t *testing.T) {
	cfg := config.Default()
	name, err := cfg.SendModemTarget("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Fatalf("got %q, want empty", name)
	}
}

func TestSendModemTargetSingleModem(t *testing.T) {
	cfg := config.Default()
	cfg.Modems = map[string]config.ModemEntry{
		"only": {Driver: config.DriverMM},
	}
	name, err := cfg.SendModemTarget("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "only" {
		t.Fatalf("got %q", name)
	}
}

func TestSendModemTargetDefaultModem(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultModem = "main"
	cfg.Modems = map[string]config.ModemEntry{
		"main":   {Driver: config.DriverMM},
		"backup": {Driver: config.DriverMM},
	}
	name, err := cfg.SendModemTarget("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "main" {
		t.Fatalf("got %q", name)
	}
}

func TestSendModemTargetRequiresFlag(t *testing.T) {
	cfg := config.Default()
	cfg.Modems = map[string]config.ModemEntry{
		"m1": {Driver: config.DriverMM},
		"m2": {Driver: config.DriverMM},
	}
	_, err := cfg.SendModemTarget("")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--modem is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
