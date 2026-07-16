package config_test

import (
	"testing"

	"github.com/legion/sms-gateway/internal/config"
)

func TestNormalizeICCID(t *testing.T) {
	if got := config.NormalizeICCID(" 8999801234567890123F "); got != "8999801234567890123" {
		t.Fatalf("got %q", got)
	}
}

func TestLookupSimNumber(t *testing.T) {
	cfg := config.Config{
		SimNumbers: map[string]string{
			"8999801234567890123": "+998901234567",
		},
	}
	if got := cfg.LookupSimNumber("8999801234567890123F"); got != "+998901234567" {
		t.Fatalf("got %q", got)
	}
}
