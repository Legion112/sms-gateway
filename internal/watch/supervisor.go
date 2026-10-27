package watch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/legion/sms-gateway/internal/alert"
	"github.com/legion/sms-gateway/internal/config"
)

// permanentError marks a failure that retrying cannot fix, such as a modem
// configured with a driver this build does not support. Everything else —
// a missing device node, a busy port, a wedged modem — is treated as transient,
// because the hardware can come back at any time and a SIM must never be
// abandoned while the daemon keeps running.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func permanent(format string, args ...any) error {
	return permanentError{err: fmt.Errorf(format, args...)}
}

// superviseModem keeps one modem watched for as long as ctx lives. Opening the
// modem and the watch loop itself both fail from time to time: a USB re-enumeration,
// a read timeout, a modem that stops answering AT. Each of those used to end the
// goroutine and silently strand that SIM until someone restarted the service, so
// every exit here is retried with exponential backoff instead.
func (s *Service) superviseModem(ctx context.Context, modemName string, entry config.ModemEntry) error {
	delay := s.cfg.ReconnectInitial()
	maxDelay := s.cfg.ReconnectMax()
	catchUp := s.cfg.Watch.CatchUpOnStart

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		startedAt := s.now()
		err := s.runModem(ctx, modemName, entry, catchUp)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		// Every reconnect re-lists the modem's storage regardless of
		// catch_up_on_start: messages that arrived while the port was down are
		// only visible in that first listing.
		catchUp = true

		uptime := s.now().Sub(startedAt)
		s.markModemDown(modemName, err)

		var perm permanentError
		if errors.As(err, &perm) {
			s.deps.Log("modem_abandoned modem=%s error=%v (not retryable)", modemName, redactSecrets(err))
			return err
		}

		// A loop that stayed up is a healthy modem having one bad moment, not a
		// broken one: start its backoff over so a long-lived modem always gets a
		// fast retry on its first stumble.
		if uptime >= s.reconnectResetAfter() {
			delay = s.cfg.ReconnectInitial()
		}

		s.deps.Log("modem_down modem=%s uptime=%s retry_in=%s error=%v",
			modemName, uptime.Truncate(time.Second), delay, redactSecrets(err))

		if !s.sleep(ctx, delay) {
			return ctx.Err()
		}
		if delay = delay * 2; delay > maxDelay {
			delay = maxDelay
		}
	}
}

// reconnectResetAfter is how long a watch loop must survive before its next
// failure is treated as a fresh one rather than a continuing outage.
func (s *Service) reconnectResetAfter() time.Duration {
	return s.cfg.ReconnectMax() * 2
}

// superviseHealth reports modems that are connected but have stopped producing
// successful polls, and emits a periodic heartbeat so the journal shows positive
// evidence that reception is alive rather than only showing failures.
func (s *Service) superviseHealth(ctx context.Context) {
	stall := s.cfg.StallTimeout()
	heartbeat := s.cfg.HealthInterval()
	alertAfter := s.cfg.AlertAfter()

	// Tick fast enough to serve whichever signal needs it soonest: an alert
	// threshold of five minutes is only honoured to within one tick.
	tick := halve(stall)
	if d := halve(alertAfter); s.deps.Alerts != nil && d > 0 && (tick <= 0 || d < tick) {
		tick = d
	}
	if heartbeat > 0 && (tick <= 0 || heartbeat < tick) {
		tick = heartbeat
	}
	if tick <= 0 {
		return
	}

	var monitor *alert.Monitor
	if s.deps.Alerts != nil {
		monitor = alert.NewMonitor(alertAfter)
	}

	lastBeat := s.now()
	for {
		if !s.sleep(ctx, tick) {
			// Shutting down: one last attempt to get a queued alert out, with
			// a fresh deadline since ctx is already done.
			s.flushAlerts()
			return
		}
		now := s.now()
		s.checkStalls(now, stall)
		s.runAlerts(ctx, monitor, now)
		if heartbeat > 0 && now.Sub(lastBeat) >= heartbeat {
			lastBeat = now
			s.logHealth(now)
		}
	}
}

func halve(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return d / 2
}

// runAlerts turns the current health into events and delivers them. Flush runs
// on every tick, not only when there is something new, because that is also the
// retry for an alert an earlier tick could not get out.
func (s *Service) runAlerts(ctx context.Context, monitor *alert.Monitor, now time.Time) {
	if monitor == nil || s.deps.Alerts == nil {
		return
	}
	s.deps.Alerts.Enqueue(monitor.Evaluate(now, s.observations())...)
	s.deps.Alerts.Flush(ctx)
}

// observations reports when each modem was last known to be working.
func (s *Service) observations() []alert.Observation {
	names := s.modemNames()
	out := make([]alert.Observation, 0, len(names))
	for _, name := range names {
		out = append(out, alert.Observation{Subject: name, LastOK: s.healthSnapshot(name).lastPollOK})
	}
	return out
}

// flushAlerts delivers anything still queued, on a deadline of its own so a
// cancelled run context does not drop the last alert on the way out.
func (s *Service) flushAlerts() {
	if s.deps.Alerts == nil || s.deps.Alerts.PendingCount() == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.deps.Alerts.Flush(ctx)
}

func (s *Service) checkStalls(now time.Time, stall time.Duration) {
	if stall <= 0 {
		return
	}
	for _, name := range s.modemNames() {
		h := s.healthSnapshot(name)
		if !h.connected || h.lastPollOK.IsZero() {
			continue
		}
		idle := now.Sub(h.lastPollOK)
		if idle >= stall && !h.stalled {
			s.setStalled(name, true)
			s.deps.Log("modem_stalled modem=%s last_poll=%s ago (no successful poll)", name, idle.Truncate(time.Second))
		}
	}
}

func (s *Service) logHealth(now time.Time) {
	for _, name := range s.modemNames() {
		h := s.healthSnapshot(name)
		state := "down"
		if h.connected {
			state = "up"
		}
		last := "never"
		if !h.lastPollOK.IsZero() {
			last = now.Sub(h.lastPollOK).Truncate(time.Second).String()
		}
		s.deps.Log("modem_health modem=%s state=%s last_poll=%s polls=%d reconnects=%d",
			name, state, last, h.polls, h.reconnects)
	}
}
