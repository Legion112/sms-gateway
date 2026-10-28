package watch

import (
	"context"
	"time"

	"github.com/legion/sms-gateway/internal/alert"
	"github.com/legion/sms-gateway/internal/modem"
)

// registrationReadTimeout bounds the four queries the read makes. It is shorter
// than the posture exchange because nothing here writes: a modem that will not
// answer four reads promptly is better left to the next interval than allowed
// to hold up the poll loop.
const registrationReadTimeout = 20 * time.Second

// checkRegistration reads the modem's per-domain registration, at most once per
// configured interval, and records what it implies for incoming SMS.
//
// It runs from the poll handler because that is the goroutine that owns the AT
// port: the driver serialises nothing, so a second goroutine asking the modem
// questions would interleave its replies with the message listing. The health
// supervisor therefore reads the recorded verdict rather than taking it itself.
//
// Nothing here can fail the poll. The gateway's job is to receive SMS, and a
// check that reports on reception must never be the reason reception stops.
func (s *Service) checkRegistration(ctx context.Context, modemName string, m modem.Modem) {
	reader, ok := m.(modem.RegistrationReader)
	if !ok {
		// The mm driver reports ModemManager's single combined state, which
		// cannot tell the domains apart.
		return
	}
	if !s.beginRegistrationCheck(modemName, s.now(), s.cfg.RegistrationInterval()) {
		return
	}

	readCtx, cancel := context.WithTimeout(ctx, registrationReadTimeout)
	reg, err := reader.ReadRegistration(readCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		// Logged on the edge only. A modem that has stopped answering these
		// queries would otherwise repeat the same line forever.
		if s.setRegistrationUnreadable(modemName, true) {
			s.deps.Log("registration_unreadable modem=%s error=%v", modemName, redactSecrets(err))
		}
		return
	}
	s.setRegistrationUnreadable(modemName, false)

	path := reg.SMSPath()
	previous := s.recordRegistration(modemName, reg, path, s.now())
	if path == previous {
		return
	}
	// A changed verdict is the one thing an operator runs `status` to see, so
	// it is published now instead of at the next heartbeat.
	s.publishState()
	if path.Problem() {
		// Worth its own line: from here on the modem looks healthy everywhere
		// else while an SMS may have no way to reach the SIM.
		s.deps.Log("modem_sms_path_lost modem=%s path=%s %s", modemName, path, reg)
		return
	}
	s.deps.Log("modem_sms_path modem=%s path=%s %s", modemName, path, reg)
}

// beginRegistrationCheck reports whether a read is due now, claiming the slot
// if it is so two passes cannot both decide to read. A non-positive interval
// never comes from config -- the accessor substitutes the default -- and is
// refused here rather than read as "every poll".
func (s *Service) beginRegistrationCheck(modemName string, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return false
	}
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.healthFor(modemName)
	if !h.lastRegCheck.IsZero() && now.Sub(h.lastRegCheck) < interval {
		return false
	}
	h.lastRegCheck = now
	return true
}

// recordRegistration stores a reading and returns the verdict it replaces, so
// the caller can log only transitions.
func (s *Service) recordRegistration(modemName string, reg modem.Registration, path modem.SMSPath, now time.Time) modem.SMSPath {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.healthFor(modemName)
	previous := h.smsPath
	h.registration = reg
	h.smsPath = path
	if path.OK() {
		h.lastSMSPathOK = now
	}
	return previous
}

// setRegistrationUnreadable records whether the last read failed, reporting
// whether that is a change.
func (s *Service) setRegistrationUnreadable(modemName string, failed bool) bool {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	h := s.healthFor(modemName)
	changed := h.regReadFailed != failed
	h.regReadFailed = failed
	return changed
}

// smsPathObservations reports each modem's SMS reachability for the alert
// monitor.
//
// A modem is left out entirely rather than reported healthy when there is
// nothing to say about it, which freezes its state in the monitor: no verdict
// yet, a verdict of unknown, or a modem whose port is down. The last of those
// matters most -- a disconnected modem already has the polling alert, and
// reporting the same outage twice teaches an operator that one of the two
// alerts is noise.
func (s *Service) smsPathObservations() []alert.Observation {
	names := s.modemNames()
	out := make([]alert.Observation, 0, len(names))
	for _, name := range names {
		h := s.healthSnapshot(name)
		if !h.connected || h.smsPath == "" || h.smsPath == modem.SMSPathUnknown {
			continue
		}
		out = append(out, alert.Observation{
			Subject: name,
			LastOK:  h.lastSMSPathOK,
			Detail:  h.registration.String(),
		})
	}
	return out
}
