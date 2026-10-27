package modem

import "context"

// Part is one SMS segment, optionally part of a concatenated message.
type Part struct {
	ID        string
	From      string
	State     string
	Timestamp string
	Text      string
	Storage   string
	SMSC      string
	ConcatRef int
	Part      int // 1-based segment number; 0 if not concatenated
	PartTotal int
}

// PartLister can return raw SMS segments including concat metadata.
type PartLister interface {
	Modem
	ListMessageParts(ctx context.Context) ([]Part, error)
}

// SlotDeleter can free a message slot in the modem's own storage.
// IDs are driver-local: a CMGL index for serial, a D-Bus object path for mm.
type SlotDeleter interface {
	Modem
	DeleteMessage(ctx context.Context, id string) error
}
