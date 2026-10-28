//go:build integration && linux

package integration_test

import (
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/state"
	"github.com/legion/sms-gateway/test/integration/harness"
)

// waitForState polls the published file until cond holds for it.
func waitForState(l *lab, cond func(state.Modem) bool, timeout time.Duration) (state.Modem, bool) {
	var last state.Modem
	ok := waitUntil(func() bool {
		snap, err := state.Read(l.statePath)
		if err != nil {
			return false
		}
		rec, found := snap.Find("lab")
		if !found {
			return false
		}
		last = rec
		return cond(rec)
	}, timeout)
	return last, ok
}

// The fix, end to end over a real tty: the daemon holds the port, so the only
// way anything else can report this modem is the file the daemon publishes.
func TestStateIsPublishedOverRealTTY(t *testing.T) {
	l := startLab(t, false)

	rec, ok := waitForState(l, func(m state.Modem) bool {
		return m.Connected && m.SMSPath == modem.SMSPathDirect
	}, 20*time.Second)
	if !ok {
		t.Fatalf("the modem never reached the state file as connected and direct; got %+v log=%v", rec, l.log.all())
	}
	if rec.Device == "" {
		t.Error("the published record does not name the port")
	}
	if rec.CS != modem.RegRoaming && rec.CS != modem.RegHome {
		t.Errorf("cs = %v, want a real registration", rec.CS)
	}
	if rec.RegCheckedAt.IsZero() {
		t.Error("the registration reading is undated")
	}
	if rec.RegUnreadable {
		t.Error("a modem that answered every query was published as unreadable")
	}

	snap, err := state.Read(l.statePath)
	if err != nil {
		t.Fatalf("reading the published state: %v", err)
	}
	if snap.PID == 0 || snap.WrittenAt.IsZero() {
		t.Errorf("the snapshot header is incomplete: pid=%d at=%v", snap.PID, snap.WrittenAt)
	}
	if snap.Stale(time.Now()) {
		t.Error("a snapshot just written was already stale")
	}
}

// The condition this whole feature exists for has to be readable from the file,
// because while the daemon runs that is the only place it can be read from.
func TestUnprovenPathIsPublishedOverRealTTY(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 0)
	}})

	rec, ok := waitForState(l, func(m state.Modem) bool {
		return m.SMSPath == modem.SMSPathUnproven
	}, 20*time.Second)
	if !ok {
		t.Fatalf("the unproven path never reached the state file; got %+v log=%v", rec, l.log.all())
	}
	// The reader rebuilds the verdict from the domains rather than trusting the
	// stored string, so the two must agree.
	if got := rec.Registration().SMSPath(); got != modem.SMSPathUnproven {
		t.Errorf("the published domains yield %q but the stored verdict is %q", got, rec.SMSPath)
	}
	if !rec.Connected {
		t.Error("the modem is attached for data; it must be published as connected")
	}
}

// A change under the modem has to reach the file, or `status` would keep
// reporting the state the daemon started in.
func TestPublishedStateFollowsTheRadio(t *testing.T) {
	l := startLab(t, false)

	if _, ok := waitForState(l, func(m state.Modem) bool {
		return m.SMSPath == modem.SMSPathDirect
	}, 20*time.Second); !ok {
		t.Fatalf("the healthy path was never published; log=%v", l.log.all())
	}

	l.sim.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 0)
	if rec, ok := waitForState(l, func(m state.Modem) bool {
		return m.SMSPath == modem.SMSPathUnproven
	}, 20*time.Second); !ok {
		t.Fatalf("the loss never reached the state file; got %+v log=%v", rec, l.log.all())
	}

	l.sim.SetRegistration(harness.RegRoaming, harness.RegRoaming, harness.RegNotRegistered, 0)
	if rec, ok := waitForState(l, func(m state.Modem) bool {
		return m.SMSPath == modem.SMSPathDirect
	}, 20*time.Second); !ok {
		t.Fatalf("the recovery never reached the state file; got %+v log=%v", rec, l.log.all())
	}
}

// Publishing must not come at reception's expense: the file is a diagnostic,
// and the daemon's job is to forward SMS.
func TestPublishingDoesNotDisturbReception(t *testing.T) {
	l := startLab(t, false)
	l.sim.AddMessage(1, pduHello)

	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("the message was never forwarded; log=%v", l.log.all())
	}
	rec, ok := waitForState(l, func(m state.Modem) bool { return m.Polls > 0 }, 20*time.Second)
	if !ok {
		t.Fatalf("the poll count never reached the state file; got %+v", rec)
	}
	if l.log.has("state_unpublished") {
		t.Errorf("publishing failed during a clean run: %v", l.log.all())
	}
}
