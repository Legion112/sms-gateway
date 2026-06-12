//go:build linux

package watch

import (
	"context"
	"fmt"

	"github.com/legion/sms-gateway/internal/driver/mm"
	"github.com/legion/sms-gateway/internal/modem"
)

type mmIncoming interface {
	WatchIncoming(ctx context.Context, handler modem.IncomingHandler) error
}

func runMMWatch(ctx context.Context, m modem.Modem, handler modem.IncomingHandler) error {
	w, ok := m.(mmIncoming)
	if !ok {
		if d, ok := m.(*mm.Driver); ok {
			return d.WatchIncoming(ctx, handler)
		}
		return fmt.Errorf("modem driver does not support incoming watch")
	}
	return w.WatchIncoming(ctx, handler)
}
