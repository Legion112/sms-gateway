package alert

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func obs(subject string, lastOK time.Time) []Observation {
	return []Observation{{Subject: subject, LastOK: lastOK}}
}

// TestNoAlertBeforeThreshold is the point of the grace period: a modem that
// misses a poll or two must not wake anybody.
func TestNoAlertBeforeThreshold(t *testing.T) {
	m := NewMonitor(5 * time.Minute)
	last := t0

	for _, d := range []time.Duration{0, time.Second, time.Minute, 4 * time.Minute, 4*time.Minute + 59*time.Second} {
		if evs := m.Evaluate(t0.Add(d), obs("ec25-main", last)); len(evs) != 0 {
			t.Fatalf("at %s: got %d events, want none", d, len(evs))
		}
	}
}

func TestAlertsOnceAtThreshold(t *testing.T) {
	m := NewMonitor(5 * time.Minute)
	last := t0

	evs := m.Evaluate(t0.Add(5*time.Minute), obs("ec25-main", last))
	if len(evs) != 1 {
		t.Fatalf("got %d events at the threshold, want 1", len(evs))
	}
	if evs[0].Kind != KindDown || evs[0].Subject != "ec25-main" {
		t.Fatalf("event = %+v", evs[0])
	}
	if evs[0].Silent != 5*time.Minute {
		t.Fatalf("silent = %s, want 5m", evs[0].Silent)
	}

	// Still down is not news.
	for _, d := range []time.Duration{6, 10, 60, 600} {
		if evs := m.Evaluate(t0.Add(d*time.Minute), obs("ec25-main", last)); len(evs) != 0 {
			t.Fatalf("at %dm: got %d repeat events, want none", d, len(evs))
		}
	}
	if !m.Down("ec25-main") {
		t.Error("subject should still read as down")
	}
}

func TestRecoveryReportsTheWholeOutage(t *testing.T) {
	m := NewMonitor(5 * time.Minute)

	// Last healthy at t0+3m, so the alert lands at t0+8m.
	lastOK := t0.Add(3 * time.Minute)
	if evs := m.Evaluate(t0.Add(8*time.Minute), obs("ec25-main", lastOK)); len(evs) != 1 {
		t.Fatalf("expected a down alert, got %v", evs)
	}

	// It polls again at t0+10m: the outage ran from 3m to 10m.
	recovered := t0.Add(10 * time.Minute)
	evs := m.Evaluate(recovered, obs("ec25-main", recovered))
	if len(evs) != 1 {
		t.Fatalf("got %d events on recovery, want 1", len(evs))
	}
	if evs[0].Kind != KindRecovered {
		t.Fatalf("kind = %s, want recovered", evs[0].Kind)
	}
	if evs[0].Silent != 7*time.Minute {
		t.Fatalf("outage = %s, want 7m (3m..10m)", evs[0].Silent)
	}
	if m.Down("ec25-main") {
		t.Error("subject should no longer read as down")
	}
}

// TestRecoveryOnlyAfterADownAlert stops a healthy modem from announcing
// recoveries nobody was told to worry about.
func TestRecoveryOnlyAfterADownAlert(t *testing.T) {
	m := NewMonitor(5 * time.Minute)
	for i := 1; i <= 10; i++ {
		at := t0.Add(time.Duration(i) * time.Minute)
		if evs := m.Evaluate(at, obs("ec25-main", at)); len(evs) != 0 {
			t.Fatalf("healthy modem emitted %v", evs)
		}
	}
}

// TestStartupDoesNotAlertImmediately is the zero-value trap: a modem that has
// never polled must not look like it has been down since the epoch.
func TestStartupDoesNotAlertImmediately(t *testing.T) {
	m := NewMonitor(5 * time.Minute)

	if evs := m.Evaluate(t0, obs("ec25-main", time.Time{})); len(evs) != 0 {
		t.Fatalf("alerted on the first evaluation: %v", evs)
	}
	if evs := m.Evaluate(t0.Add(4*time.Minute), obs("ec25-main", time.Time{})); len(evs) != 0 {
		t.Fatalf("alerted before the threshold: %v", evs)
	}
	// Counted from first sight, not from the zero time.
	evs := m.Evaluate(t0.Add(5*time.Minute), obs("ec25-main", time.Time{}))
	if len(evs) != 1 || evs[0].Kind != KindDown {
		t.Fatalf("expected one down alert once the grace period passed, got %v", evs)
	}
	if evs[0].Silent != 5*time.Minute {
		t.Fatalf("silent = %s, want 5m measured from first sight", evs[0].Silent)
	}
}

// TestFlappingProducesAPairPerOutage keeps an intermittent modem honest: each
// outage gets exactly one down and one recovery.
func TestFlappingProducesAPairPerOutage(t *testing.T) {
	m := NewMonitor(5 * time.Minute)
	var kinds []Kind
	now := t0
	lastOK := t0

	for cycle := 0; cycle < 3; cycle++ {
		for i := 0; i < 7; i++ { // silent for 7 minutes
			now = now.Add(time.Minute)
			for _, ev := range m.Evaluate(now, obs("ec25-main", lastOK)) {
				kinds = append(kinds, ev.Kind)
			}
		}
		now = now.Add(time.Minute) // it polls again
		lastOK = now
		for _, ev := range m.Evaluate(now, obs("ec25-main", lastOK)) {
			kinds = append(kinds, ev.Kind)
		}
	}

	want := []Kind{
		KindDown, KindRecovered,
		KindDown, KindRecovered,
		KindDown, KindRecovered,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
}

// TestSubjectsAreIndependent keeps one dead modem from masking or implicating
// the other.
func TestSubjectsAreIndependent(t *testing.T) {
	m := NewMonitor(5 * time.Minute)
	healthy := t0.Add(9 * time.Minute)

	evs := m.Evaluate(t0.Add(9*time.Minute), []Observation{
		{Subject: "ec25-main", LastOK: t0},        // silent 9m
		{Subject: "ec25-backup", LastOK: healthy}, // just polled
	})
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(evs), evs)
	}
	if evs[0].Subject != "ec25-main" {
		t.Fatalf("alerted on %s, want ec25-main", evs[0].Subject)
	}
	if m.Down("ec25-backup") {
		t.Error("the healthy modem must not be marked down")
	}
}

// TestEventOrderIsStable keeps log and message order from reshuffling between
// evaluations, which would make an operator think something changed.
func TestEventOrderIsStable(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		m := NewMonitor(5 * time.Minute)
		evs := m.Evaluate(t0.Add(6*time.Minute), []Observation{
			{Subject: "zeta", LastOK: t0},
			{Subject: "alpha", LastOK: t0},
			{Subject: "mid", LastOK: t0},
		})
		if len(evs) != 3 {
			t.Fatalf("got %d events, want 3", len(evs))
		}
		if evs[0].Subject != "alpha" || evs[1].Subject != "mid" || evs[2].Subject != "zeta" {
			t.Fatalf("order = %s,%s,%s", evs[0].Subject, evs[1].Subject, evs[2].Subject)
		}
	}
}

// TestZeroThresholdDisables lets the feature be switched off without the caller
// having to special-case it.
func TestZeroThresholdDisables(t *testing.T) {
	m := NewMonitor(0)
	if evs := m.Evaluate(t0.Add(100*time.Hour), obs("ec25-main", t0)); len(evs) != 0 {
		t.Fatalf("a zero threshold must not alert, got %v", evs)
	}
}

func TestEventText(t *testing.T) {
	down := Event{Kind: KindDown, Subject: "ec25-main", Silent: 5*time.Minute + 2*time.Second}.Text()
	for _, want := range []string{"PROBLEM", "ec25-main", "5m2s", "not being received"} {
		if !strings.Contains(down, want) {
			t.Errorf("down text %q missing %q", down, want)
		}
	}

	up := Event{Kind: KindRecovered, Subject: "ec25-main", Silent: 7 * time.Minute}.Text()
	for _, want := range []string{"RECOVERED", "ec25-main", "7m0s"} {
		if !strings.Contains(up, want) {
			t.Errorf("recovered text %q missing %q", up, want)
		}
	}
	if strings.Contains(up, "PROBLEM") {
		t.Error("a recovery must not read as a problem")
	}
}
