package forward

import "context"

// Driver names for forward channels.
const (
	DriverTelegramBot    = "telegram_bot"
	DriverTelegramSecret = "telegram_secret"
	DriverEmail          = "email"
	DriverSMS            = "sms"
)

// Channel delivers an inbound SMS notification to an external destination.
type Channel interface {
	Name() string
	Driver() string
	Ping(ctx context.Context) error
	Forward(ctx context.Context, msg InboundSMS) error
	// SendText delivers plain text that did not come from an SMS: the channel
	// test command and operational alerts.
	SendText(ctx context.Context, text string) error
	Close() error
}

// InboundSMS is one received SMS passed to forward channels.
type InboundSMS struct {
	Modem   string // config modem name, e.g. "ec25-main" (hardware context)
	ID      string // modem message id
	From    string // sender phone number (E.164 preferred)
	ToLine  string // resolved recipient line from sim_numbers (empty if unmapped)
	ToICCID string // ICCID of receiving SIM (for debug when unmapped)
	Text    string
	Time    string // display timestamp
}
