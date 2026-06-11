package factory_test

import (
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/factory"
)

func TestNewUnknownDriver(t *testing.T) {
	_, err := factory.New(config.Config{Driver: "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}
