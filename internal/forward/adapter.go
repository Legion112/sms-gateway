package forward

import (
	"context"

	"github.com/legion/sms-gateway/internal/forward/telegrambot"
)

type telegramBotAdapter struct {
	inner *telegrambot.Channel
}

func (a *telegramBotAdapter) Name() string   { return a.inner.Name() }
func (a *telegramBotAdapter) Driver() string { return a.inner.Driver() }
func (a *telegramBotAdapter) Ping(ctx context.Context) error {
	return a.inner.Ping(ctx)
}
func (a *telegramBotAdapter) Forward(ctx context.Context, msg InboundSMS) error {
	return a.inner.Forward(ctx, telegrambot.SMSMessage{
		From: msg.From,
		Text: msg.Text,
		Time: msg.Time,
	})
}
func (a *telegramBotAdapter) Close() error { return a.inner.Close() }

// TelegramBot returns the underlying telegram channel for test helpers.
func TelegramBot(ch Channel) *telegrambot.Channel {
	if a, ok := ch.(*telegramBotAdapter); ok {
		return a.inner
	}
	return nil
}
