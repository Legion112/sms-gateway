package watch

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/factory"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
)

// Dependencies are shared services for the watch daemon.
type Dependencies struct {
	Store    *storage.Store
	Router   *forward.Router
	Channels map[string]forward.Channel
	Log      func(format string, args ...any)
}

// Service runs the SMS watch and forward pipeline.
type Service struct {
	cfg  config.Config
	deps Dependencies
}

// NewService creates a watch service.
func NewService(cfg config.Config, deps Dependencies) *Service {
	if deps.Log == nil {
		deps.Log = log.Printf
	}
	return &Service{cfg: cfg, deps: deps}
}

// Run starts one goroutine per configured modem and blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	if len(s.cfg.Modems) == 0 {
		return fmt.Errorf("no modems configured")
	}
	errCh := make(chan error, len(s.cfg.Modems))
	for name, entry := range s.cfg.Modems {
		go func(modemName string, modemEntry config.ModemEntry) {
			errCh <- s.runModem(ctx, modemName, modemEntry)
		}(name, entry)
	}
	var firstErr error
	for range s.cfg.Modems {
		if err := <-errCh; err != nil && err != context.Canceled && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) runModem(ctx context.Context, modemName string, entry config.ModemEntry) error {
	m, err := factory.NewFromModemEntry(entry, s.cfg.Verbose)
	if err != nil {
		return fmt.Errorf("modem %s: %w", modemName, err)
	}
	defer m.Close()

	handler := func(ctx context.Context, msg modem.Message) error {
		return s.ingest(ctx, modemName, msg)
	}

	if s.cfg.Watch.CatchUpOnStart {
		if err := s.catchUp(ctx, modemName, m); err != nil && ctx.Err() == nil {
			s.deps.Log("catch-up modem %s: %v", modemName, err)
		}
	}

	switch entry.Driver {
	case config.DriverMM:
		return runMMWatch(ctx, m, handler)
	case config.DriverSerial:
		return runSerialWatch(ctx, m, s.cfg.SerialPollInterval(), handler)
	default:
		return fmt.Errorf("modem %s: unsupported driver %q", modemName, entry.Driver)
	}
}

func (s *Service) catchUp(ctx context.Context, modemName string, m modem.Modem) error {
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	msgs, err := m.ListMessages(listCtx)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if !modem.IsInbound(msg.State) {
			continue
		}
		if err := s.ingest(ctx, modemName, msg); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ingest(ctx context.Context, modemName string, msg modem.Message) error {
	inbound := forward.InboundSMS{
		Modem: modemName,
		ID:    msg.ID,
		From:  msg.From,
		Text:  msg.Text,
		Time:  msg.Timestamp,
	}
	if inbound.Time == "" {
		inbound.Time = time.Now().Format("2006-01-02 15:04:05")
	}

	inserted, err := s.deps.Store.InsertMessage(storage.Message{
		ID:         inbound.ID,
		Modem:      inbound.Modem,
		FromNumber: inbound.From,
		Body:       inbound.Text,
		ReceivedAt: inbound.Time,
	})
	if err != nil {
		return err
	}
	if inserted {
		s.deps.Log("ingested modem=%s id=%s from=%s", modemName, msg.ID, msg.From)
	}

	ruleName, channelNames, ok := s.deps.Router.Resolve(inbound)
	if !ok {
		s.deps.Log("no_rule modem=%s id=%s from=%s", modemName, msg.ID, msg.From)
		return nil
	}

	for _, chName := range channelNames {
		need, err := s.deps.Store.NeedsDelivery(inbound.ID, chName)
		if err != nil {
			return err
		}
		if !need {
			continue
		}
		ch, ok := s.deps.Channels[chName]
		if !ok {
			return fmt.Errorf("channel %q not found", chName)
		}
		fwdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err = ch.Forward(fwdCtx, inbound)
		cancel()
		if err != nil {
			_ = s.deps.Store.MarkDeliveryFailed(inbound.ID, chName, err.Error())
			s.deps.Log("delivery_failed modem=%s id=%s channel=%s rule=%s error=%v", modemName, msg.ID, chName, ruleName, err)
			continue
		}
		if err := s.deps.Store.MarkDelivered(inbound.ID, chName); err != nil {
			return err
		}
		s.deps.Log("forwarded modem=%s id=%s channel=%s rule=%s", modemName, msg.ID, chName, ruleName)
	}
	return nil
}
