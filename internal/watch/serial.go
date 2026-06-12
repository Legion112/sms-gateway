package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/legion/sms-gateway/internal/modem"
)

type serialIncoming interface {
	WatchIncoming(ctx context.Context, interval time.Duration, handler modem.IncomingHandler) error
}

func runSerialWatch(ctx context.Context, m modem.Modem, interval time.Duration, handler modem.IncomingHandler) error {
	if w, ok := m.(serialIncoming); ok {
		return w.WatchIncoming(ctx, interval, handler)
	}
	if d, ok := m.(*serial.Driver); ok {
		return d.WatchIncoming(ctx, interval, handler)
	}
	return fmt.Errorf("serial driver does not support incoming watch")
}
