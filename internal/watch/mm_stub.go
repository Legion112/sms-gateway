//go:build !linux

package watch

import (
	"context"
	"fmt"

	"github.com/legion/sms-gateway/internal/modem"
)

func runMMWatch(ctx context.Context, m modem.Modem, handler modem.IncomingHandler) error {
	return fmt.Errorf("mm watch requires linux")
}
