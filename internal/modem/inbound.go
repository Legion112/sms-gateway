package modem

import (
	"context"
	"strings"
)

// IncomingHandler is called when a new inbound SMS is available.
type IncomingHandler func(ctx context.Context, msg Message) error

// IsInbound reports whether a message state represents a received SMS.
func IsInbound(state string) bool {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "received" {
		return true
	}
	if strings.HasPrefix(s, "rec ") {
		return true
	}
	return strings.Contains(s, "received")
}

// IncomingWatcher can subscribe to new inbound SMS (MM D-Bus or serial poll wrapper).
type IncomingWatcher interface {
	Modem
	WatchIncoming(ctx context.Context, handler IncomingHandler) error
}
