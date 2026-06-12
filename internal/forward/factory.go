package forward

import (
	"fmt"
	"os"
	"strings"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward/telegrambot"
)

// NewChannel creates a forward channel by driver name from config.
func NewChannel(name string, cfg config.ChannelConfig, verbose bool) (Channel, error) {
	switch cfg.Driver {
	case DriverTelegramBot:
		token := resolveBotToken(name, cfg.Telegram)
		inner, err := telegrambot.New(name, cfg.Telegram, token, verbose)
		if err != nil {
			return nil, err
		}
		return &telegramBotAdapter{inner: inner}, nil
	case DriverTelegramSecret:
		return newStub(name, DriverTelegramSecret), nil
	case DriverEmail:
		return newStub(name, DriverEmail), nil
	case DriverSMS:
		return newStub(name, DriverSMS), nil
	default:
		return nil, fmt.Errorf("unknown channel driver %q for channel %q", cfg.Driver, name)
	}
}

// NewChannels builds all configured channels keyed by name.
func NewChannels(channels map[string]config.ChannelConfig, verbose bool) (map[string]Channel, error) {
	out := make(map[string]Channel, len(channels))
	for name, cfg := range channels {
		ch, err := NewChannel(name, cfg, verbose)
		if err != nil {
			return nil, err
		}
		out[name] = ch
	}
	return out, nil
}

func resolveBotToken(channelName string, cfg config.TelegramChannelConfig) string {
	if cfg.BotToken != "" {
		return cfg.BotToken
	}
	key := "SMS_GATEWAY_CHANNEL_" + strings.ToUpper(strings.ReplaceAll(channelName, "-", "_")) + "_BOT_TOKEN"
	if v := os.Getenv(key); v != "" {
		return v
	}
	return os.Getenv("SMS_GATEWAY_TELEGRAM_BOT_TOKEN")
}
