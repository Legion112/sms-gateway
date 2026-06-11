package modem

import "context"

// Driver identifies the modem backend implementation.
type Driver string

const (
	DriverMM     Driver = "mm"
	DriverSerial Driver = "serial"
)

// PingResult is returned by a successful modem connectivity check.
type PingResult struct {
	Driver     Driver
	Detail     string
	Device     string
	ModemIndex int
}

// Modem is the backend interface for communicating with the EC25.
type Modem interface {
	Ping(ctx context.Context) (PingResult, error)
	Close() error
}
