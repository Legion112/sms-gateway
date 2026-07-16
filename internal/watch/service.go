package watch

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/factory"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/smsconcat"
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
	cfg      config.Config
	deps     Dependencies
	mu       sync.Mutex
	simMu    sync.RWMutex
	simLines map[string]cmdutil.ResolvedLine
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
	if s.cfg.Watch.CatchUpOnStart {
		for name, entry := range s.cfg.Modems {
			if err := s.catchUpModem(ctx, name, entry); err != nil && ctx.Err() == nil {
				s.deps.Log("catch-up modem %s: %v", name, err)
			}
		}
	}
	errCh := make(chan error, len(s.cfg.Modems))
	for name, entry := range s.cfg.Modems {
		go func(modemName string, modemEntry config.ModemEntry) {
			errCh <- s.runModem(ctx, modemName, modemEntry, false)
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

func (s *Service) catchUpModem(ctx context.Context, modemName string, entry config.ModemEntry) error {
	m, err := factory.NewFromModemEntry(entry, s.cfg.Verbose)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer m.Close()
	if err := s.refreshSimLine(ctx, modemName, m); err != nil && ctx.Err() == nil {
		s.deps.Log("sim lookup modem %s: %v", modemName, err)
	}
	return s.catchUp(ctx, modemName, m)
}

func (s *Service) runModem(ctx context.Context, modemName string, entry config.ModemEntry, catchUp bool) error {
	m, err := factory.NewFromModemEntry(entry, s.cfg.Verbose)
	if err != nil {
		return fmt.Errorf("modem %s: %w", modemName, err)
	}
	defer m.Close()

	if err := s.refreshSimLine(ctx, modemName, m); err != nil && ctx.Err() == nil {
		s.deps.Log("sim lookup modem %s: %v", modemName, err)
	}

	partsHandler := func(ctx context.Context, parts []modem.Part) error {
		return s.ingestParts(ctx, modemName, parts)
	}

	if catchUp {
		if err := s.catchUp(ctx, modemName, m); err != nil && ctx.Err() == nil {
			s.deps.Log("catch-up modem %s: %v", modemName, err)
		}
	}

	switch entry.Driver {
	case config.DriverMM:
		return runMMWatch(ctx, m, func(ctx context.Context, msg modem.Message) error {
			return s.ingestParts(ctx, modemName, []modem.Part{
				{
					ID:        msg.ID,
					From:      msg.From,
					State:     msg.State,
					Timestamp: msg.Timestamp,
					Text:      msg.Text,
					Storage:   msg.Storage,
					SMSC:      msg.SMSC,
				},
			})
		})
	case config.DriverSerial:
		return runSerialWatchParts(ctx, m, s.cfg.SerialPollInterval(), partsHandler)
	default:
		return fmt.Errorf("modem %s: unsupported driver %q", modemName, entry.Driver)
	}
}

func (s *Service) catchUp(ctx context.Context, modemName string, m modem.Modem) error {
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	parts, err := listMessageParts(listCtx, m)
	if err != nil {
		return err
	}
	return s.ingestParts(ctx, modemName, parts)
}

func listMessageParts(ctx context.Context, m modem.Modem) ([]modem.Part, error) {
	if pl, ok := m.(modem.PartLister); ok {
		return pl.ListMessageParts(ctx)
	}
	msgs, err := m.ListMessages(ctx)
	if err != nil {
		return nil, err
	}
	parts := make([]modem.Part, len(msgs))
	for i, msg := range msgs {
		parts[i] = modem.Part{
			ID:        msg.ID,
			From:      msg.From,
			State:     msg.State,
			Timestamp: msg.Timestamp,
			Text:      msg.Text,
			Storage:   msg.Storage,
			SMSC:      msg.SMSC,
		}
	}
	return parts, nil
}

func (s *Service) refreshSimLine(ctx context.Context, modemName string, m modem.Modem) error {
	statusCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, err := m.SMSStatus(statusCtx)
	if err != nil {
		return err
	}
	line := cmdutil.ResolveLine(s.cfg, status.ICCID)
	if line.Source == cmdutil.LineSourceUnmapped && line.ICCID != "" {
		s.deps.Log("unknown SIM iccid=%s received on modem=%s", line.ICCID, modemName)
	}
	s.simMu.Lock()
	if s.simLines == nil {
		s.simLines = make(map[string]cmdutil.ResolvedLine)
	}
	s.simLines[modemName] = line
	s.simMu.Unlock()
	return nil
}

func (s *Service) simLineFor(modemName string) cmdutil.ResolvedLine {
	s.simMu.RLock()
	defer s.simMu.RUnlock()
	return s.simLines[modemName]
}

func (s *Service) ingestParts(ctx context.Context, modemName string, parts []modem.Part) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.flushTimedOutConcat(ctx, modemName); err != nil {
		return err
	}

	for _, part := range parts {
		if !modem.IsInbound(part.State) {
			continue
		}
		if err := s.ingestPart(ctx, modemName, part); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ingestPart(ctx context.Context, modemName string, part modem.Part) error {
	segmentID := storage.MessageKey(modemName, part.ID, part.Timestamp)
	superseded, err := s.deps.Store.IsSuperseded(segmentID)
	if err != nil {
		return err
	}
	if superseded {
		return nil
	}

	if !smsconcat.IsConcat(part) {
		return s.ingest(ctx, modemName, smsconcat.PartToMessage(part), "")
	}

	merged, _, err := s.deps.Store.IsConcatMerged(modemName, part.From, part.ConcatRef, part.PartTotal)
	if err != nil {
		return err
	}
	if merged {
		return nil
	}

	complete, storedParts, err := s.deps.Store.UpsertConcatPart(storage.ConcatPart{
		Modem:      modemName,
		FromNumber: part.From,
		ConcatRef:  part.ConcatRef,
		PartTotal:  part.PartTotal,
		PartNum:    part.Part,
		ModemID:    part.ID,
		Body:       part.Text,
		ReceivedAt: part.Timestamp,
		State:      part.State,
	})
	if err != nil {
		return err
	}
	if !complete {
		return nil
	}
	return s.deliverMergedConcat(ctx, modemName, storedParts, false)
}

func (s *Service) flushTimedOutConcat(ctx context.Context, modemName string) error {
	keys, err := s.deps.Store.ListTimedOutConcatGroups(modemName, s.cfg.ConcatAssemblyTimeout())
	if err != nil {
		return err
	}
	for _, key := range keys {
		merged, _, err := s.deps.Store.IsConcatMerged(modemName, key.From, key.ConcatRef, key.PartTotal)
		if err != nil {
			return err
		}
		if merged {
			continue
		}
		parts, err := s.deps.Store.LoadConcatParts(modemName, key)
		if err != nil {
			return err
		}
		if len(parts) == 0 {
			continue
		}
		if err := s.deliverMergedConcat(ctx, modemName, parts, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deliverMergedConcat(ctx context.Context, modemName string, parts []storage.ConcatPart, incomplete bool) error {
	if len(parts) == 0 {
		return nil
	}
	first := parts[0]
	merged, _, err := s.deps.Store.IsConcatMerged(modemName, first.FromNumber, first.ConcatRef, first.PartTotal)
	if err != nil {
		return err
	}
	if merged {
		return nil
	}

	msg := storage.MergedMessageFromParts(parts, incomplete)
	receivedAt := msg.Timestamp
	if receivedAt == "" {
		receivedAt = first.ReceivedAt
	}
	mergedID := storage.ConcatMessageKey(modemName, first.FromNumber, first.ConcatRef, first.PartTotal, receivedAt)

	segmentIDs := make([]string, len(parts))
	for i, p := range parts {
		segmentIDs[i] = storage.MessageKey(modemName, p.ModemID, p.ReceivedAt)
	}

	if err := s.ingest(ctx, modemName, msg, mergedID); err != nil {
		return err
	}
	if err := s.deps.Store.MarkConcatMerged(modemName, first.FromNumber, first.ConcatRef, first.PartTotal, mergedID, segmentIDs); err != nil {
		return err
	}
	if incomplete {
		s.deps.Log("concat incomplete modem=%s from=%s ref=%d total=%d parts=%d", modemName, first.FromNumber, first.ConcatRef, first.PartTotal, len(parts))
	} else {
		s.deps.Log("concat merged modem=%s from=%s ref=%d total=%d", modemName, first.FromNumber, first.ConcatRef, first.PartTotal)
	}
	return nil
}

func (s *Service) ingest(ctx context.Context, modemName string, msg modem.Message, storageID string) error {
	line := s.simLineFor(modemName)
	inbound := forward.InboundSMS{
		Modem:   modemName,
		ID:      msg.ID,
		From:    msg.From,
		ToLine:  line.Number,
		ToICCID: line.ICCID,
		Text:    msg.Text,
		Time:    msg.Timestamp,
	}
	if inbound.Time == "" {
		inbound.Time = time.Now().Format("2006-01-02 15:04:05")
	}
	if storageID == "" {
		storageID = storage.MessageKey(modemName, msg.ID, inbound.Time)
	}

	inserted, err := s.deps.Store.InsertMessage(storage.Message{
		ID:         storageID,
		Modem:      inbound.Modem,
		FromNumber: inbound.From,
		Body:       inbound.Text,
		ReceivedAt: inbound.Time,
	})
	if err != nil {
		return err
	}
	if !inserted {
		if existing, ok, err := s.deps.Store.GetMessage(storageID); err != nil {
			return err
		} else if ok && (existing.Body != inbound.Text || existing.ReceivedAt != inbound.Time) {
			if err := s.deps.Store.ReplaceMessage(storage.Message{
				ID:         storageID,
				Modem:      inbound.Modem,
				FromNumber: inbound.From,
				Body:       inbound.Text,
				ReceivedAt: inbound.Time,
			}); err != nil {
				return err
			}
			s.deps.Log("replaced modem=%s id=%s from=%s (storage slot reused)", modemName, msg.ID, msg.From)
		}
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
		need, err := s.deps.Store.NeedsDelivery(storageID, chName)
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
			_ = s.deps.Store.MarkDeliveryFailed(storageID, chName, err.Error())
			s.deps.Log("delivery_failed modem=%s id=%s channel=%s rule=%s error=%v", modemName, msg.ID, chName, ruleName, redactSecrets(err))
			continue
		}
		if err := s.deps.Store.MarkDelivered(storageID, chName); err != nil {
			return err
		}
		s.deps.Log("forwarded modem=%s id=%s channel=%s rule=%s", modemName, msg.ID, chName, ruleName)
	}
	return nil
}

func redactSecrets(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if i := strings.Index(s, "/bot"); i >= 0 {
		if j := strings.Index(s[i+4:], "/"); j >= 0 {
			s = s[:i+4] + "<TOKEN>" + s[i+4+j:]
		}
	}
	return fmt.Errorf("%s", s)
}
