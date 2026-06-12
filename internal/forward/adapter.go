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
func (a *telegramBotAdapter) SendTest(ctx context.Context, text string) error {
	return a.inner.SendText(ctx, text)
}
func (a *telegramBotAdapter) Close() error { return a.inner.Close() }
