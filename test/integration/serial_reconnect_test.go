//go:build integration && linux

package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/alert"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/storage"
	"github.com/legion/sms-gateway/internal/watch"
	"github.com/legion/sms-gateway/test/integration/harness"
)

// Decodes to from=+79956009754, "Hello this sms is send to mine Uzbekistan SIM card".
const pduHello = "0791999810584059000B919759069057F400006270401212050233C8329BFD06D1D1E93968DE9E83D273D0BCEC2683E86F503BED2E83AA7A71799D9ED3C36ED034D9048DC3723208"

// requirePTY fails rather than skips. A lab that skips itself looks green while
// covering nothing, which is worse than a red build.
func requirePTY(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Fatalf("/dev/ptmx unavailable, cannot run the serial lab: %v", err)
	}
}

type captureChannel struct {
	mu   sync.Mutex
	sent []forward.InboundSMS
}

func (c *captureChannel) Name() string                           { return "lab" }
func (c *captureChannel) Driver() string                         { return "lab" }
func (c *captureChannel) Ping(context.Context) error             { return nil }
func (c *captureChannel) SendText(context.Context, string) error { return nil }
func (c *captureChannel) Close() error                           { return nil }

func (c *captureChannel) Forward(_ context.Context, msg forward.InboundSMS) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *captureChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *captureChannel) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.sent))
	for i, m := range c.sent {
		out[i] = m.Text
	}
	return out
}

type labLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *labLog) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func (l *labLog) has(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func (l *labLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

type lab struct {
	sim   *harness.SerialModem
	ch    *captureChannel
	log   *labLog
	store *storage.Store
}

// labOptions tunes a lab beyond what startLab sets up by default.
type labOptions struct {
	purge bool
	// radio is the posture the daemon is configured to hold the modem in.
	// The zero value is the production default: automatic for both settings.
	radio config.RadioConfig
	// before runs against the fake modem once it exists and before the daemon
	// opens it, which is the only window in which a starting posture can be
	// set without racing the correction under test.
	before func(*harness.SerialModem)
	// registrationInterval paces the per-domain registration read. The lab
	// runs it far faster than production so a change made mid-test is picked
	// up within a poll or two.
	registrationInterval time.Duration
}

// startLab brings up the fake modem and the real watch service against it.
func startLab(t *testing.T, purge bool) *lab {
	t.Helper()
	return startLabWith(t, labOptions{purge: purge})
}

func startLabWith(t *testing.T, opts labOptions) *lab {
	t.Helper()
	requirePTY(t)

	link := filepath.Join(t.TempDir(), "ttyLAB")
	sim, err := harness.NewSerialModem(link)
	if err != nil {
		t.Fatalf("fake modem: %v", err)
	}
	t.Cleanup(sim.Close)
	if opts.before != nil {
		opts.before(sim)
	}

	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.Config{
		Modems: map[string]config.ModemEntry{
			"lab": {
				Driver: config.DriverSerial,
				Serial: config.SerialConfig{Device: link, BaudRate: 115200, Timeout: 2 * time.Second},
				Radio:  opts.radio,
			},
		},
	}
	cfg.Watch.SerialPollInterval = 150 * time.Millisecond
	cfg.Watch.ReconnectInitial = 100 * time.Millisecond
	cfg.Watch.ReconnectMax = 300 * time.Millisecond
	cfg.Watch.DeleteAfterForward = opts.purge
	cfg.Watch.RegistrationInterval = opts.registrationInterval
	if cfg.Watch.RegistrationInterval == 0 {
		cfg.Watch.RegistrationInterval = 50 * time.Millisecond
	}

	ch := &captureChannel{}
	lg := &labLog{}
	svc := watch.NewService(cfg, watch.Dependencies{
		Store:    store,
		Router:   forward.NewRouter([]config.ForwardRule{{Name: "all", From: "*", To: []string{"lab"}}}),
		Channels: map[string]forward.Channel{"lab": ch},
		Log:      lg.logf,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return after cancellation")
		}
	})

	return &lab{sim: sim, ch: ch, log: lg, store: store}
}

func waitUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestSerialModemForwardsOverRealTTY is the baseline: the real serial driver,
// real AT exchange and real PDU decoding, with nothing faked above the device.
func TestSerialModemForwardsOverRealTTY(t *testing.T) {
	l := startLab(t, false)
	l.sim.AddMessage(1, pduHello)

	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("message never forwarded over the tty; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	got := l.ch.texts()[0]
	if !strings.Contains(got, "Uzbekistan SIM card") {
		t.Fatalf("forwarded text = %q", got)
	}
}

// TestSerialModemReconnectsAfterDeviceLoss is the one the unit tests cannot
// reach. The device node is removed and the pty destroyed, which is what a USB
// re-enumeration does; the daemon must reopen the modem on its own and deliver
// a message that arrived while it was gone.
func TestSerialModemReconnectsAfterDeviceLoss(t *testing.T) {
	l := startLab(t, false)
	l.sim.AddMessage(1, pduHello)

	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("first message never forwarded; log=%v", l.log.all())
	}

	// Pull the modem out from under the running daemon.
	l.sim.Unplug()
	if !waitUntil(func() bool { return l.log.has("modem_down modem=lab") }, 20*time.Second) {
		t.Fatalf("device loss was never noticed; log=%v", l.log.all())
	}

	// It comes back as a different pts, holding a message received while away.
	if err := l.sim.Replug(); err != nil {
		t.Fatalf("replug: %v", err)
	}
	l.sim.AddMessage(2, pduHello)

	if !waitUntil(func() bool { return l.log.has("modem_up modem=lab") }, 30*time.Second) {
		t.Fatalf("daemon never reopened the modem; log=%v", l.log.all())
	}
	if !waitUntil(func() bool { return l.ch.count() == 2 }, 30*time.Second) {
		t.Fatalf("message received while unplugged was never delivered: forwards=%d log=%v",
			l.ch.count(), l.log.all())
	}
}

// TestSerialModemSurvivesRepeatedLoss covers a flapping USB port: the daemon
// must still be watching after several disconnects, not have given up.
func TestSerialModemSurvivesRepeatedLoss(t *testing.T) {
	l := startLab(t, false)
	l.sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("first message never forwarded; log=%v", l.log.all())
	}

	for i := 0; i < 3; i++ {
		l.sim.Unplug()
		time.Sleep(150 * time.Millisecond)
		if err := l.sim.Replug(); err != nil {
			t.Fatalf("replug %d: %v", i, err)
		}
	}

	// Still alive: a message queued after the last flap is delivered.
	l.sim.AddMessage(9, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 2 }, 30*time.Second) {
		t.Fatalf("daemon stopped watching after repeated loss: forwards=%d log=%v",
			l.ch.count(), l.log.all())
	}
}

// TestSerialModemSlotFreedOverRealTTY checks delete_after_forward against a
// modem that really processes AT+CMGD, rather than a fake that records the call.
func TestSerialModemSlotFreedOverRealTTY(t *testing.T) {
	l := startLab(t, true)
	l.sim.AddMessage(7, pduHello)

	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("message never forwarded; log=%v", l.log.all())
	}
	if !waitUntil(func() bool { return len(l.sim.Deleted()) == 1 }, 20*time.Second) {
		t.Fatalf("slot never freed on the modem; deleted=%v log=%v", l.sim.Deleted(), l.log.all())
	}
	if got := l.sim.Deleted()[0]; got != 7 {
		t.Fatalf("freed slot %d, want 7", got)
	}
	if l.sim.Stored() != 0 {
		t.Fatalf("modem still holds %d messages", l.sim.Stored())
	}
}

// alertSender captures operational alerts in the lab.
type alertSender struct {
	mu   sync.Mutex
	sent []string
}

func (a *alertSender) Name() string { return "ops" }

func (a *alertSender) SendText(_ context.Context, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, text)
	return nil
}

func (a *alertSender) matching(substr string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, m := range a.sent {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

func (a *alertSender) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.sent...)
}

// TestAlertOnRealDeviceLossAndRecovery is the end-to-end proof of the feature:
// a tty that really disappears produces a problem alert, and bringing it back
// produces a recovery alert. Nothing here is faked above the device.
func TestAlertOnRealDeviceLossAndRecovery(t *testing.T) {
	requirePTY(t)

	link := filepath.Join(t.TempDir(), "ttyLAB")
	sim, err := harness.NewSerialModem(link)
	if err != nil {
		t.Fatalf("fake modem: %v", err)
	}
	t.Cleanup(sim.Close)

	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// A short threshold so the lab does not wait five real minutes; the policy
	// itself is covered exhaustively by the unit tests in internal/alert.
	const alertAfter = 1500 * time.Millisecond

	cfg := config.Config{
		Modems: map[string]config.ModemEntry{
			"lab": {
				Driver: config.DriverSerial,
				Serial: config.SerialConfig{Device: link, BaudRate: 115200, Timeout: 500 * time.Millisecond},
			},
		},
	}
	cfg.Watch.SerialPollInterval = 150 * time.Millisecond
	cfg.Watch.ReconnectInitial = 100 * time.Millisecond
	cfg.Watch.ReconnectMax = 200 * time.Millisecond
	cfg.Watch.StallTimeout = alertAfter
	cfg.Watch.HealthInterval = time.Hour
	cfg.Alerts.After = alertAfter
	cfg.Alerts.Channels = []string{"ops"}

	ops := &alertSender{}
	lg := &labLog{}
	svc := watch.NewService(cfg, watch.Dependencies{
		Store:    store,
		Router:   forward.NewRouter([]config.ForwardRule{{Name: "all", From: "*", To: []string{"lab"}}}),
		Channels: map[string]forward.Channel{"lab": &captureChannel{}},
		Log:      lg.logf,
		Alerts:   alert.NewDispatcher(alert.NewChannelNotifier(ops), lg.logf),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return")
		}
	})

	sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return lg.has("modem_up modem=lab") || lg.has("forwarded") }, 20*time.Second) {
		t.Fatalf("modem never came up; log=%v", lg.all())
	}

	// Nothing wrong yet, so nobody should have been told anything.
	if got := len(ops.all()); got != 0 {
		t.Fatalf("alerted while healthy: %v", ops.all())
	}

	// Pull the device out and leave it out past the threshold.
	sim.Unplug()
	if !waitUntil(func() bool { return ops.matching("PROBLEM") == 1 }, 30*time.Second) {
		t.Fatalf("no problem alert after real device loss: alerts=%v log=%v", ops.all(), lg.all())
	}
	if !strings.Contains(ops.all()[0], "lab") {
		t.Fatalf("alert does not name the modem: %q", ops.all()[0])
	}

	// Put it back; the daemon reconnects on its own and says so.
	if err := sim.Replug(); err != nil {
		t.Fatalf("replug: %v", err)
	}
	if !waitUntil(func() bool { return ops.matching("RECOVERED") == 1 }, 30*time.Second) {
		t.Fatalf("no recovery alert after the device returned: alerts=%v log=%v", ops.all(), lg.all())
	}

	// One of each, not a stream.
	if got := ops.matching("PROBLEM"); got != 1 {
		t.Fatalf("problem alerts = %d, want exactly 1: %v", got, ops.all())
	}
}
