package watch

import (
	"context"
	"sort"
	"time"
)

// modemHealth is the liveness record for one modem. It answers the only question
// that matters for this daemon: is this SIM still able to receive SMS right now?
type modemHealth struct {
	connected  bool
	stalled    bool
	lastPollOK time.Time
	lastErr    error
	polls      int
	reconnects int
}

func (s *Service) healthFor(modemName string) *modemHealth {
	if s.health == nil {
		s.health = make(map[string]*modemHealth)
	}
	h, ok := s.health[modemName]
	if !ok {
		h = &modemHealth{}
		s.health[modemName] = h
	}
	return h
}

// markModemUp records that the modem opened and its watch loop is starting.
func (s *Service) markModemUp(modemName string) {
	s.healthMu.Lock()
	h := s.healthFor(modemName)
	reconnected := h.reconnects > 0 && !h.connected
	h.connected = true
	h.lastErr = nil
	s.healthMu.Unlock()

	if reconnected {
		s.deps.Log("modem_up modem=%s (reconnected)", modemName)
	}
}

// markModemDown records that the watch loop exited and the modem is unwatched
// until the supervisor reopens it.
func (s *Service) markModemDown(modemName string, err error) {
	s.healthMu.Lock()
	h := s.healthFor(modemName)
	h.connected = false
	h.stalled = false
	h.lastErr = err
	h.reconnects++
	s.healthMu.Unlock()
}

// markPollOK records a completed poll: the modem answered and everything it
// returned was ingested. This is the heartbeat the stall check measures against.
func (s *Service) markPollOK(modemName string, now time.Time) {
	s.healthMu.Lock()
	h := s.healthFor(modemName)
	h.lastPollOK = now
	h.polls++
	wasStalled := h.stalled
	h.stalled = false
	s.healthMu.Unlock()

	if wasStalled {
		s.deps.Log("modem_recovered modem=%s (polling again)", modemName)
	}
}

func (s *Service) setStalled(modemName string, stalled bool) {
	s.healthMu.Lock()
	s.healthFor(modemName).stalled = stalled
	s.healthMu.Unlock()
}

func (s *Service) healthSnapshot(modemName string) modemHealth {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	return *s.healthFor(modemName)
}

// modemNames returns configured modem names in a stable order so log output does
// not reshuffle between heartbeats.
func (s *Service) modemNames() []string {
	names := make([]string, 0, len(s.cfg.Modems))
	for name := range s.cfg.Modems {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// now reads the clock through a seam so tests can drive timing deterministically.
func (s *Service) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// sleep waits for d or until ctx ends, reporting whether the full delay elapsed.
func (s *Service) sleep(ctx context.Context, d time.Duration) bool {
	if s.sleepFn != nil {
		return s.sleepFn(ctx, d)
	}
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
