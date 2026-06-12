package serial

import (
	"context"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
)

// WatchIncoming polls the modem for new inbound SMS messages.
func (d *Driver) WatchIncoming(ctx context.Context, interval time.Duration, handler modem.IncomingHandler) error {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	poll := func() error {
		listCtx, cancel := context.WithTimeout(ctx, d.timeout*2)
		msgs, err := d.ListMessages(listCtx)
		cancel()
		if err != nil {
			return err
		}
		for _, msg := range msgs {
			if !modem.IsInbound(msg.State) {
				continue
			}
			if err := handler(ctx, msg); err != nil {
				return err
			}
		}
		return nil
	}

	if err := poll(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := poll(); err != nil {
				return err
			}
		}
	}
}
