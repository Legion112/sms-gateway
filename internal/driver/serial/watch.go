package serial

import (
	"context"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/smsconcat"
)

// WatchIncoming polls the modem for new inbound SMS message parts.
func (d *Driver) WatchIncoming(ctx context.Context, interval time.Duration, handler modem.IncomingHandler) error {
	return d.WatchIncomingParts(ctx, interval, func(ctx context.Context, parts []modem.Part) error {
		for _, part := range parts {
			if !modem.IsInbound(part.State) {
				continue
			}
			if err := handler(ctx, smsconcat.PartToMessage(part)); err != nil {
				return err
			}
		}
		return nil
	})
}

// PartsHandler is called for each poll with raw SMS segments.
type PartsHandler func(ctx context.Context, parts []modem.Part) error

// WatchIncomingParts polls the modem and delivers raw segments (including concat metadata).
func (d *Driver) WatchIncomingParts(ctx context.Context, interval time.Duration, handler PartsHandler) error {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	poll := func() error {
		listCtx, cancel := context.WithTimeout(ctx, d.timeout*2)
		parts, err := d.ListMessageParts(listCtx)
		cancel()
		if err != nil {
			return err
		}
		return handler(ctx, parts)
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
