//go:build !linux

package mm

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

// New is unavailable on non-Linux platforms.
func New(_ config.MMConfig, _ bool) (modem.Modem, error) {
	return nil, fmt.Errorf("ModemManager driver requires Linux")
}
