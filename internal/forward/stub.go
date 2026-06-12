package forward

import (
	"context"
	"fmt"
)

// ErrNotImplemented is returned by channel drivers not yet built.
var ErrNotImplemented = fmt.Errorf("channel driver not implemented")

type stubChannel struct {
	name   string
	driver string
}

func newStub(name, driver string) *stubChannel {
	return &stubChannel{name: name, driver: driver}
}

func (s *stubChannel) Name() string   { return s.name }
func (s *stubChannel) Driver() string { return s.driver }

func (s *stubChannel) Ping(context.Context) error {
	return fmt.Errorf("%w: %s", ErrNotImplemented, s.driver)
}

func (s *stubChannel) Forward(context.Context, InboundSMS) error {
	return fmt.Errorf("%w: %s", ErrNotImplemented, s.driver)
}

func (s *stubChannel) Close() error { return nil }
