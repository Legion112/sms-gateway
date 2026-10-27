// Package alert decides when a modem's silence is worth telling someone about,
// and reports it when it recovers.
//
// The decision and the delivery are deliberately separate. Deciding is pure:
// Monitor holds no clock, performs no I/O and knows nothing about Telegram, so
// the policy can be tested exhaustively and does not change when the transport
// does. Delivery is behind Notifier. Today everything is one binary and alerts
// leave through the same channels that carry forwarded SMS; when a separate
// notification service arrives, it implements Notifier and nothing here moves.
package alert

import (
	"fmt"
	"sort"
	"time"
)

// Kind is what happened to a subject.
type Kind string

const (
	// KindDown means the subject has been silent past the threshold.
	KindDown Kind = "down"
	// KindRecovered means a subject previously reported down is healthy again.
	KindRecovered Kind = "recovered"
)

// Event is one thing an operator needs to know.
type Event struct {
	Kind    Kind
	Subject string
	At      time.Time
	// Silent is how long the subject had gone without a sign of life: at the
	// moment of the alert for KindDown, and in total for KindRecovered.
	Silent time.Duration
}

// Text is the operator-facing message, in the same plain style as a forwarded SMS.
func (e Event) Text() string {
	switch e.Kind {
	case KindDown:
		return fmt.Sprintf(
			"sms-gateway PROBLEM\nmodem %s has not polled for %s\n\nSMS to that SIM are not being received.",
			e.Subject, round(e.Silent))
	case KindRecovered:
		return fmt.Sprintf(
			"sms-gateway RECOVERED\nmodem %s is polling again after %s",
			e.Subject, round(e.Silent))
	default:
		return fmt.Sprintf("sms-gateway: modem %s: %s", e.Subject, e.Kind)
	}
}

func round(d time.Duration) time.Duration {
	if d >= time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(time.Millisecond)
}

// Observation is the health of one subject at evaluation time.
type Observation struct {
	Subject string
	// LastOK is when the subject was last known good. The zero value means it
	// has never been seen healthy, which is the normal state at startup and
	// must not count as an outage stretching back to the epoch.
	LastOK time.Time
}

type subjectState struct {
	firstSeen time.Time
	alerted   bool
	// alertedSilent is the silence at the moment of the down alert, kept so a
	// recovery can report the full outage rather than only its tail.
	alertedAt time.Time
}

// Monitor turns a stream of observations into the few events worth sending.
//
// It is edge-triggered: a subject that stays down produces one event, not one
// per evaluation, because an operator needs to know that something broke and
// that it came back, not to be told every tick that it is still broken.
type Monitor struct {
	after time.Duration
	state map[string]*subjectState
}

// NewMonitor reports a subject down once it has been silent for after.
func NewMonitor(after time.Duration) *Monitor {
	return &Monitor{after: after, state: make(map[string]*subjectState)}
}

// Evaluate records the observations and returns the events they imply, ordered
// by subject so output is stable. Calling it with unchanged input produces
// nothing.
func (m *Monitor) Evaluate(now time.Time, obs []Observation) []Event {
	if m.after <= 0 {
		return nil
	}

	var events []Event
	for _, o := range obs {
		st, ok := m.state[o.Subject]
		if !ok {
			// A subject seen for the first time starts its clock now. Without
			// this a modem that has not polled yet would look like it had been
			// down since the zero time and alert on the first evaluation.
			st = &subjectState{firstSeen: now}
			m.state[o.Subject] = st
		}

		// A real last-known-good time always wins. firstSeen stands in only
		// when there has never been one, so a modem that has not polled yet is
		// measured from when we started watching it rather than from the zero
		// time -- but a modem that polled and then stopped is measured from
		// the poll, which is the truth.
		healthySince := o.LastOK
		if healthySince.IsZero() {
			healthySince = st.firstSeen
		}
		silent := now.Sub(healthySince)

		switch {
		case !st.alerted && silent >= m.after:
			st.alerted = true
			st.alertedAt = healthySince
			events = append(events, Event{
				Kind: KindDown, Subject: o.Subject, At: now, Silent: silent,
			})
		case st.alerted && silent < m.after:
			st.alerted = false
			events = append(events, Event{
				Kind: KindRecovered, Subject: o.Subject, At: now,
				Silent: now.Sub(st.alertedAt) - silent,
			})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Subject != events[j].Subject {
			return events[i].Subject < events[j].Subject
		}
		return events[i].Kind < events[j].Kind
	})
	return events
}

// Down reports whether a subject is currently in the alerted state. Exposed for
// callers that want the state without re-deriving it, such as a status command.
func (m *Monitor) Down(subject string) bool {
	st, ok := m.state[subject]
	return ok && st.alerted
}
