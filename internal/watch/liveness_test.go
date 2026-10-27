package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
)

func serialModems(names ...string) map[string]config.ModemEntry {
	out := make(map[string]config.ModemEntry, len(names))
	for _, n := range names {
		out[n] = config.ModemEntry{Driver: config.DriverSerial}
	}
	return out
}

// runService starts Run in the background and returns a stop function.
func runService(t *testing.T, svc *Service) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	return ctx, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return within 5s of cancellation")
		}
	}
}

// TestWatchLoopRestartsAfterFailure is the regression test for the bug that lost
// SMS in production: a modem whose watch loop returned an error was never
// reopened, so that SIM went silent until someone restarted the service while
// the daemon still looked healthy.
func TestWatchLoopRestartsAfterFailure(t *testing.T) {
	second := &fakeModem{parts: []modem.Part{inboundPart("1", "Beeline", "after reconnect", "26/10/27,10:00:00+00")}}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensAs(&fakeModem{watchErr: fmt.Errorf("read response: file already closed")}),
		opensAs(second),
	}}
	clock := newTestClock(32)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded after reconnect; opens=%d log=%v", opener.attemptCount(), f.log.all())
	}
	if got := f.ch.count(); got != 1 {
		t.Fatalf("forwards=%d want 1", got)
	}
	if !f.log.contains("modem_down modem=m1") {
		t.Errorf("expected modem_down to be logged, got %v", f.log.all())
	}
	if !f.log.contains("modem_up modem=m1") {
		t.Errorf("expected modem_up to be logged on reconnect, got %v", f.log.all())
	}
}

// TestWatchLoopRetriesOpenFailures covers the device node being absent or busy
// when the daemon starts, as happens when udev has not settled yet at boot.
func TestWatchLoopRetriesOpenFailures(t *testing.T) {
	good := &fakeModem{parts: []modem.Part{inboundPart("1", "Beeline", "late open", "26/10/27,10:00:00+00")}}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensFailing("open /dev/ttyUSB3: no such file or directory"),
		opensFailing("open /dev/ttyUSB3: serial port busy"),
		opensAs(good),
	}}
	clock := newTestClock(32)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; opens=%d log=%v", opener.attemptCount(), f.log.all())
	}
	if opener.attemptCount() < 3 {
		t.Errorf("open attempts=%d want at least 3", opener.attemptCount())
	}
}

// TestFailingModemDoesNotStopTheOther is the second half of the production bug:
// two SIMs share one process, and one dead modem must not take the other with it.
func TestFailingModemDoesNotStopTheOther(t *testing.T) {
	healthy := &fakeModem{parts: []modem.Part{inboundPart("1", "Beeline", "still working", "26/10/27,10:00:00+00")}}

	opener := func(entry config.ModemEntry) (modem.Modem, error) {
		if entry.Serial.Device == "/dev/broken" {
			return nil, fmt.Errorf("open /dev/broken: no such file or directory")
		}
		return healthy, nil
	}
	clock := newTestClock(64)

	modems := map[string]config.ModemEntry{
		"broken":  {Driver: config.DriverSerial, Serial: config.SerialConfig{Device: "/dev/broken"}},
		"healthy": {Driver: config.DriverSerial, Serial: config.SerialConfig{Device: "/dev/ok"}},
	}

	f := newLivenessFixture(t, modems, opener, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("healthy modem never delivered while the other was failing; log=%v", f.log.all())
	}
	if !f.log.contains("modem_down modem=broken") {
		t.Errorf("expected the broken modem to report down, got %v", f.log.all())
	}
}

// TestUnsupportedDriverIsAbandonedNotRetried keeps a config mistake from turning
// into an endless reconnect loop: there is no hardware to come back.
func TestUnsupportedDriverIsAbandonedNotRetried(t *testing.T) {
	opened := 0
	opener := func(config.ModemEntry) (modem.Modem, error) {
		opened++
		return &fakeModem{}, nil
	}
	clock := newTestClock(8)

	modems := map[string]config.ModemEntry{"m1": {Driver: "telepathy"}}
	f := newLivenessFixture(t, modems, opener, clock)
	defer f.close()

	err := f.svc.superviseModem(context.Background(), "m1", modems["m1"])
	if err == nil {
		t.Fatal("expected an error for an unsupported driver")
	}
	var perm permanentError
	if !errors.As(err, &perm) {
		t.Fatalf("error %v should be permanent", err)
	}
	if opened != 0 {
		t.Errorf("opened=%d want 0: an unsupported driver must not touch hardware", opened)
	}
	if len(clock.recorded()) != 0 {
		t.Errorf("slept %v; a permanent failure must not back off", clock.recorded())
	}
	if !f.log.contains("modem_abandoned modem=m1") {
		t.Errorf("expected modem_abandoned, got %v", f.log.all())
	}
}

// TestReconnectBackoffGrowsAndIsCapped stops a modem that cannot be opened from
// spinning the CPU, while still guaranteeing it is retried forever.
func TestReconnectBackoffGrowsAndIsCapped(t *testing.T) {
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensFailing("open /dev/ttyUSB3: no such file or directory"),
	}}
	clock := newTestClock(6)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return len(clock.recorded()) == 6 }, 5*time.Second) {
		t.Fatalf("expected 6 backoff delays, got %v", clock.recorded())
	}
	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		80 * time.Millisecond,
		80 * time.Millisecond, // capped by reconnect_max
		80 * time.Millisecond,
	}
	got := clock.recorded()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff[%d]=%s want %s (full %v)", i, got[i], want[i], got)
		}
	}
}

// TestBackoffResetsAfterHealthyRun keeps a modem that ran for a long time from
// inheriting the long delay of an outage that ended hours ago.
func TestBackoffResetsAfterHealthyRun(t *testing.T) {
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensFailing("boom"),
		opensFailing("boom"),
		opensFailing("boom"),
		// The fourth attempt opens and stays up well past the reset window
		// before failing, so the delay after it must drop back to the initial.
		func() (modem.Modem, error) {
			return &fakeModem{watchErr: fmt.Errorf("usb reset")}, nil
		},
		opensFailing("boom"),
	}}
	clock := newTestClock(5)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()

	// The healthy run has to look long to the service clock.
	longRun := f.svc.reconnectResetAfter() + time.Second
	f.svc.openFn = func(entry config.ModemEntry) (modem.Modem, error) {
		m, err := opener.open(entry)
		if err == nil {
			clock.advance(longRun)
		}
		return m, err
	}

	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return len(clock.recorded()) == 5 }, 5*time.Second) {
		t.Fatalf("expected 5 delays, got %v", clock.recorded())
	}
	got := clock.recorded()
	// Delays 1-3 climb while the modem cannot be opened.
	if got[0] != 10*time.Millisecond || got[1] != 20*time.Millisecond || got[2] != 40*time.Millisecond {
		t.Fatalf("growth phase = %v", got[:3])
	}
	// Delay 4 follows the long healthy run, so it starts over.
	if got[3] != 10*time.Millisecond {
		t.Fatalf("delay after a healthy run = %s want %s (full %v)", got[3], 10*time.Millisecond, got)
	}
}

// TestRunReturnsOnContextCancel guarantees shutdown terminates: the supervisor
// loops forever by design, so a missed cancellation would hang the daemon.
func TestRunReturnsOnContextCancel(t *testing.T) {
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensAs(&fakeModem{})}}
	clock := newTestClock(8)

	f := newLivenessFixture(t, serialModems("m1", "m2"), opener.open, clock)
	defer f.close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.svc.Run(ctx) }()

	if !waitFor(func() bool { return opener.attemptCount() >= 2 }, 5*time.Second) {
		t.Fatal("modems never started")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestCatchUpRunsOnEveryReconnect covers the SMS that arrive while the port is
// down. They are only visible in a fresh listing, so a reconnect must re-list
// even when catch_up_on_start is off.
func TestCatchUpRunsOnEveryReconnect(t *testing.T) {
	first := &fakeModem{watchErr: fmt.Errorf("usb unplugged")}
	second := &fakeModem{parts: []modem.Part{inboundPart("4", "Beeline", "arrived while down", "26/10/27,10:00:00+00")}}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensAs(first), opensAs(second),
	}}
	clock := newTestClock(32)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	f.svc.cfg.Watch.CatchUpOnStart = false
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}

	firstList, _, _ := first.counts()
	if firstList != 0 {
		t.Errorf("first connection ran catch-up %d times; catch_up_on_start is off", firstList)
	}
	secondList, _, _ := second.counts()
	if secondList == 0 {
		t.Error("reconnect did not re-list modem storage, so messages received while down would be lost")
	}
}

// TestModemHandleClosedOnEveryReconnect keeps file descriptors from leaking
// across the reconnects that now happen automatically.
func TestModemHandleClosedOnEveryReconnect(t *testing.T) {
	dead := &fakeModem{watchErr: fmt.Errorf("io error")}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensAs(dead)}}
	clock := newTestClock(4)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { _, _, closed := dead.counts(); return closed >= 3 }, 5*time.Second) {
		_, _, closed := dead.counts()
		t.Fatalf("closed=%d want at least 3: every failed connection must be closed", closed)
	}
}

// --- a bad message must never stop reception ---

// TestMissingChannelDoesNotStopReception covers a forward rule naming a channel
// that is not configured. That used to return an error out of the watch loop,
// which killed the modem for good over a typo.
func TestMissingChannelDoesNotStopReception(t *testing.T) {
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensAs(&fakeModem{parts: []modem.Part{
			inboundPart("1", "Beeline", "first", "26/10/27,10:00:00+00"),
			inboundPart("2", "Beeline", "second", "26/10/27,10:01:00+00"),
		}}),
	}}
	clock := newTestClock(8)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	// The rule names a channel that does not exist alongside one that does.
	f.svc.deps.Router = forward.NewRouter([]config.ForwardRule{
		{Name: "typo", From: "*", To: []string{"does-not-exist", "tg"}},
	})

	_, stop := runService(t, f.svc)
	defer stop()

	// Both messages still reach the channel that is configured.
	if !waitFor(func() bool { return f.ch.count() == 2 }, 5*time.Second) {
		t.Fatalf("forwards=%d want 2; log=%v", f.ch.count(), f.log.all())
	}
	if !f.log.contains("channel_missing") {
		t.Errorf("expected channel_missing to be logged, got %v", f.log.all())
	}
	if f.log.contains("modem_down") {
		t.Errorf("a missing channel must not take the modem down: %v", f.log.all())
	}
}

// TestStoreErrorDoesNotStopTheBatch proves a failure deep in the pipeline is
// contained to its own message. ingestParts must return nil so the poll loop,
// and with it the modem, survives.
func TestStoreErrorDoesNotStopTheBatch(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)

	// A closed store makes every store call fail, which is the harshest version
	// of a transient database problem.
	f.store.Close()

	err := f.svc.ingestParts(context.Background(), "m1", nil, []modem.Part{
		inboundPart("1", "Beeline", "a", "26/10/27,10:00:00+00"),
		inboundPart("2", "Beeline", "b", "26/10/27,10:01:00+00"),
	})
	if err != nil {
		t.Fatalf("ingestParts returned %v; a store error must not escape and kill the watch loop", err)
	}
	if !f.log.contains("ingest_failed modem=m1 id=1") {
		t.Errorf("expected the first failure to be logged, got %v", f.log.all())
	}
	if !f.log.contains("ingest_failed modem=m1 id=2") {
		t.Errorf("the batch stopped at the first bad message, got %v", f.log.all())
	}
}

// TestIngestPartsStopsOnCancelledContext keeps shutdown prompt: a cancelled
// context is the one error that should end the pass immediately.
func TestIngestPartsStopsOnCancelledContext(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.svc.ingestParts(ctx, "m1", nil, []modem.Part{
		inboundPart("1", "Beeline", "a", "26/10/27,10:00:00+00"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ingestParts returned %v want context.Canceled", err)
	}
}

// --- health reporting ---

// TestPollSuccessIsRecorded is what the stall check measures against.
func TestPollSuccessIsRecorded(t *testing.T) {
	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensAs(&fakeModem{parts: []modem.Part{inboundPart("1", "Beeline", "hi", "26/10/27,10:00:00+00")}}),
	}}
	clock := newTestClock(8)

	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.svc.healthSnapshot("m1").polls > 0 }, 5*time.Second) {
		t.Fatalf("no successful poll recorded; log=%v", f.log.all())
	}
	h := f.svc.healthSnapshot("m1")
	if !h.connected {
		t.Error("modem should be marked connected while its loop runs")
	}
	if h.lastPollOK.IsZero() {
		t.Error("lastPollOK should be set after a successful poll")
	}
}

// TestStallIsReportedOnceAndThenRecovers gives the silent-modem case an alarm:
// the port is open and nothing errors, but no poll has completed in a long time.
func TestStallIsReportedOnceAndThenRecovers(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	start := clock.timeNow()
	f.svc.markModemUp("m1")
	f.svc.markPollOK("m1", start)

	// Well inside the window: nothing to report.
	f.svc.checkStalls(start.Add(time.Minute), 5*time.Minute)
	if f.log.contains("modem_stalled") {
		t.Fatalf("stall reported too early: %v", f.log.all())
	}

	// Past the window: reported exactly once, not on every check.
	f.svc.checkStalls(start.Add(6*time.Minute), 5*time.Minute)
	f.svc.checkStalls(start.Add(7*time.Minute), 5*time.Minute)
	stalls := 0
	for _, line := range f.log.all() {
		if strings.Contains(line, "modem_stalled") {
			stalls++
		}
	}
	if stalls != 1 {
		t.Fatalf("modem_stalled logged %d times, want exactly 1: %v", stalls, f.log.all())
	}

	// A completed poll clears it and says so.
	f.svc.markPollOK("m1", start.Add(8*time.Minute))
	if !f.log.contains("modem_recovered modem=m1") {
		t.Errorf("expected modem_recovered after polling resumed, got %v", f.log.all())
	}
	if f.svc.healthSnapshot("m1").stalled {
		t.Error("stalled flag should clear once polling resumes")
	}
}

// TestStallIsNotReportedForADisconnectedModem avoids a duplicate alarm: a modem
// that is down already reports modem_down from the supervisor.
func TestStallIsNotReportedForADisconnectedModem(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	start := clock.timeNow()
	f.svc.markModemUp("m1")
	f.svc.markPollOK("m1", start)
	f.svc.markModemDown("m1", errors.New("usb gone"))

	f.svc.checkStalls(start.Add(time.Hour), 5*time.Minute)
	if f.log.contains("modem_stalled") {
		t.Errorf("a disconnected modem must not also be reported stalled: %v", f.log.all())
	}
}

// TestHeartbeatReportsEveryModem gives the journal positive proof that reception
// is alive, rather than only ever showing failures.
func TestHeartbeatReportsEveryModem(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("alpha", "beta"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	now := clock.timeNow()
	f.svc.markModemUp("alpha")
	f.svc.markPollOK("alpha", now)

	f.svc.logHealth(now.Add(30 * time.Second))

	if !f.log.contains("modem_health modem=alpha state=up last_poll=30s polls=1") {
		t.Errorf("alpha heartbeat missing or wrong: %v", f.log.all())
	}
	if !f.log.contains("modem_health modem=beta state=down last_poll=never") {
		t.Errorf("beta heartbeat missing or wrong: %v", f.log.all())
	}
}

// TestHealthSupervisorLoopEmitsHeartbeats exercises the health goroutine itself,
// not just the functions it calls, so a broken tick calculation cannot silence
// every liveness signal at once.
func TestHealthSupervisorLoopEmitsHeartbeats(t *testing.T) {
	clock := newTestClock(64)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()
	f.svc.cfg.Watch.StallTimeout = 20 * time.Millisecond
	f.svc.cfg.Watch.HealthInterval = 20 * time.Millisecond

	f.svc.markModemUp("m1")
	f.svc.markPollOK("m1", clock.timeNow())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.svc.superviseHealth(ctx); close(done) }()

	// The clock only advances when the supervisor sleeps, so reaching both the
	// heartbeat and the stall threshold proves the loop is ticking.
	if !waitFor(func() bool { return f.log.contains("modem_health modem=m1") }, 5*time.Second) {
		t.Fatalf("no heartbeat emitted; log=%v", f.log.all())
	}
	if !waitFor(func() bool { return f.log.contains("modem_stalled modem=m1") }, 5*time.Second) {
		t.Fatalf("stall never reported though no poll completed; log=%v", f.log.all())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("health supervisor did not stop on cancellation")
	}
}

// TestFirstConnectionIsNotLoggedAsReconnect keeps startup quiet, so a
// modem_up line in the journal always means something actually recovered.
func TestFirstConnectionIsNotLoggedAsReconnect(t *testing.T) {
	clock := newTestClock(4)
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	f.svc.markModemUp("m1")
	if f.log.contains("modem_up") {
		t.Fatalf("first connection should not log a reconnect: %v", f.log.all())
	}

	f.svc.markModemDown("m1", errors.New("usb gone"))
	f.svc.markModemUp("m1")
	if !f.log.contains("modem_up modem=m1 (reconnected)") {
		t.Fatalf("expected a reconnect to be logged: %v", f.log.all())
	}
}

// TestRunFailsWhenEveryModemIsAbandoned makes a config that can never work exit
// non-zero rather than look like a clean shutdown while nothing is received.
func TestRunFailsWhenEveryModemIsAbandoned(t *testing.T) {
	clock := newTestClock(8)
	modems := map[string]config.ModemEntry{
		"m1": {Driver: "telepathy"},
		"m2": {Driver: "carrier-pigeon"},
	}
	f := newLivenessFixture(t, modems, func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, clock)
	defer f.close()

	done := make(chan error, 1)
	go func() { done <- f.svc.Run(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil though no modem could ever be watched")
		}
		if !strings.Contains(err.Error(), "unsupported driver") {
			t.Fatalf("Run error = %v, want it to name the unsupported driver", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when every modem was abandoned")
	}
}

// TestRunSurvivesWhenOnlyOneModemIsAbandoned keeps a single bad entry from
// taking down the modems that do work.
func TestRunSurvivesWhenOnlyOneModemIsAbandoned(t *testing.T) {
	clock := newTestClock(16)
	modems := map[string]config.ModemEntry{
		"bad":  {Driver: "telepathy"},
		"good": {Driver: config.DriverSerial},
	}
	f := newLivenessFixture(t, modems, func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{parts: []modem.Part{
			inboundPart("1", "Beeline", "still here", "26/10/27,10:00:00+00"),
		}}, nil
	}, clock)
	defer f.close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.svc.Run(ctx) }()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("working modem never delivered; log=%v", f.log.all())
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned %v while a healthy modem was still watching", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
