package watch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/alert"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

// recordingSender stands in for a Telegram channel.
type recordingSender struct {
	mu   sync.Mutex
	fail error
	sent []string
}

func (r *recordingSender) Name() string { return "ops" }

func (r *recordingSender) SendText(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.sent = append(r.sent, text)
	return nil
}

func (r *recordingSender) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

func (r *recordingSender) setFail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = err
}

func (r *recordingSender) matching(substr string) int {
	n := 0
	for _, m := range r.messages() {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

// alertFixture drives superviseHealth with a clock the test advances, so a
// five-minute threshold can be crossed without waiting five minutes.
type alertFixture struct {
	svc    *Service
	sender *recordingSender
	clock  *testClock
}

func newAlertFixture(t *testing.T, after time.Duration, modems ...string) *alertFixture {
	t.Helper()
	clock := newTestClock(10_000)
	sender := &recordingSender{}

	f := newLivenessFixture(t, serialModems(modems...), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	t.Cleanup(f.close)

	f.svc.cfg.Alerts.After = after
	f.svc.cfg.Alerts.Channels = []string{"ops"}
	f.svc.cfg.Watch.StallTimeout = after
	f.svc.cfg.Watch.HealthInterval = time.Hour
	f.svc.deps.Alerts = alert.NewDispatcher(alert.NewChannelNotifier(sender), f.log.logf)

	return &alertFixture{svc: f.svc, sender: sender, clock: clock}
}

// tick runs one supervisor pass at the fixture's current time.
func (a *alertFixture) tick(ctx context.Context, mons *monitors) {
	a.svc.runAlerts(ctx, mons, a.clock.timeNow())
}

func (a *alertFixture) monitor() *monitors {
	return a.svc.newMonitors(a.svc.cfg.AlertAfter())
}

// TestAlertFiresAfterThreshold is the feature: a modem silent for the
// configured window is reported, and not before.
func TestAlertFiresAfterThreshold(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")
	ctx := context.Background()
	m := a.monitor()

	// It polls once, then stops.
	a.svc.markModemUp("ec25-main")
	a.svc.markPollOK("ec25-main", a.clock.timeNow())

	a.clock.advance(4 * time.Minute)
	a.tick(ctx, m)
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted after 4m: %v", a.sender.messages())
	}

	a.clock.advance(time.Minute)
	a.tick(ctx, m)
	if got := a.sender.matching("PROBLEM"); got != 1 {
		t.Fatalf("problem alerts = %d, want 1: %v", got, a.sender.messages())
	}
	if !strings.Contains(a.sender.messages()[0], "ec25-main") {
		t.Fatalf("alert does not name the modem: %q", a.sender.messages()[0])
	}
}

// TestAlertIsNotRepeatedWhileStillDown keeps a dead modem from paging every tick.
func TestAlertIsNotRepeatedWhileStillDown(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")
	ctx := context.Background()
	m := a.monitor()

	a.svc.markModemUp("ec25-main")
	a.svc.markPollOK("ec25-main", a.clock.timeNow())

	for i := 0; i < 20; i++ {
		a.clock.advance(time.Minute)
		a.tick(ctx, m)
	}
	if got := a.sender.matching("PROBLEM"); got != 1 {
		t.Fatalf("problem alerts = %d over 20 minutes, want exactly 1", got)
	}
}

// TestRecoveryIsReported is the second half of what was asked for.
func TestRecoveryIsReported(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")
	ctx := context.Background()
	m := a.monitor()

	a.svc.markModemUp("ec25-main")
	a.svc.markPollOK("ec25-main", a.clock.timeNow())

	a.clock.advance(6 * time.Minute)
	a.tick(ctx, m)
	if a.sender.matching("PROBLEM") != 1 {
		t.Fatalf("expected a problem alert: %v", a.sender.messages())
	}

	// It polls again.
	a.clock.advance(time.Minute)
	a.svc.markPollOK("ec25-main", a.clock.timeNow())
	a.tick(ctx, m)

	if got := a.sender.matching("RECOVERED"); got != 1 {
		t.Fatalf("recovery alerts = %d, want 1: %v", got, a.sender.messages())
	}
	msgs := a.sender.messages()
	if !strings.Contains(msgs[len(msgs)-1], "7m") {
		t.Fatalf("recovery should report the 7m outage: %q", msgs[len(msgs)-1])
	}
}

// TestNoAlertForAHealthyModem: the common case must stay silent.
func TestNoAlertForAHealthyModem(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")
	ctx := context.Background()
	m := a.monitor()

	a.svc.markModemUp("ec25-main")
	for i := 0; i < 30; i++ {
		a.clock.advance(10 * time.Second)
		a.svc.markPollOK("ec25-main", a.clock.timeNow())
		a.tick(ctx, m)
	}
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("a healthy modem produced %d alerts: %v", got, a.sender.messages())
	}
}

// TestOneModemDownDoesNotImplicateTheOther keeps the alert specific.
func TestOneModemDownDoesNotImplicateTheOther(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main", "ec25-backup")
	ctx := context.Background()
	m := a.monitor()

	a.svc.markModemUp("ec25-main")
	a.svc.markModemUp("ec25-backup")
	a.svc.markPollOK("ec25-main", a.clock.timeNow())
	a.svc.markPollOK("ec25-backup", a.clock.timeNow())

	for i := 0; i < 8; i++ {
		a.clock.advance(time.Minute)
		// Only the backup keeps polling.
		a.svc.markPollOK("ec25-backup", a.clock.timeNow())
		a.tick(ctx, m)
	}

	if got := a.sender.matching("ec25-main"); got != 1 {
		t.Fatalf("alerts naming ec25-main = %d, want 1: %v", got, a.sender.messages())
	}
	if got := a.sender.matching("ec25-backup"); got != 0 {
		t.Fatalf("the healthy modem was reported %d times: %v", got, a.sender.messages())
	}
}

// TestAlertSurvivesAFailedSend is why the dispatcher retries: Telegram being
// unreachable is a plausible reason the gateway is in trouble in the first place.
func TestAlertSurvivesAFailedSend(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")
	ctx := context.Background()
	m := a.monitor()

	a.sender.setFail(fmt.Errorf("telegram unreachable"))
	a.svc.markModemUp("ec25-main")
	a.svc.markPollOK("ec25-main", a.clock.timeNow())

	a.clock.advance(6 * time.Minute)
	a.tick(ctx, m)
	if len(a.sender.messages()) != 0 {
		t.Fatal("message recorded though sending failed")
	}
	if a.svc.deps.Alerts.PendingCount() != 1 {
		t.Fatalf("pending = %d, want the alert kept", a.svc.deps.Alerts.PendingCount())
	}

	// Telegram comes back; the next tick delivers without a new outage.
	a.sender.setFail(nil)
	a.clock.advance(time.Minute)
	a.tick(ctx, m)
	if got := a.sender.matching("PROBLEM"); got != 1 {
		t.Fatalf("problem alerts after recovery of the alert path = %d, want 1: %v",
			got, a.sender.messages())
	}
}

// TestAlertingOffByDefault keeps a gateway with no alert channels working.
func TestAlertingOffByDefault(t *testing.T) {
	clock := newTestClock(32)
	f := newLivenessFixture(t, serialModems("ec25-main"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	if f.svc.deps.Alerts != nil {
		t.Fatal("alerts should be off when nothing is configured")
	}
	// runAlerts must tolerate both nils rather than panic.
	f.svc.runAlerts(context.Background(), nil, clock.timeNow())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.svc.superviseHealth(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("health supervisor hung with alerting disabled")
	}
}

// TestPendingAlertIsFlushedOnShutdown stops a restart from swallowing the alert
// that explains why the restart was needed.
func TestPendingAlertIsFlushedOnShutdown(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main")

	a.svc.deps.Alerts.Enqueue(alert.Event{
		Kind: alert.KindDown, Subject: "ec25-main", At: a.clock.timeNow(), Silent: 5 * time.Minute,
	})
	a.svc.flushAlerts()

	if got := a.sender.matching("PROBLEM"); got != 1 {
		t.Fatalf("queued alert not delivered on shutdown: %v", a.sender.messages())
	}
}
