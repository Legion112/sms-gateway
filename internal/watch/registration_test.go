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
	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/legion/sms-gateway/internal/modem"
)

// registrationModem is a fakeModem that also answers per-domain registration
// queries, as the serial driver does, and keeps polling rather than delivering
// one batch and going quiet -- the check is paced between polls, so a fake that
// polls once could never show the pacing.
type registrationModem struct {
	*fakeModem

	mu        sync.Mutex
	reg       modem.Registration
	readErr   error
	reads     int
	delivered bool
}

func newRegistrationModem(reg modem.Registration, parts ...modem.Part) *registrationModem {
	return &registrationModem{fakeModem: &fakeModem{parts: parts}, reg: reg}
}

func (r *registrationModem) ReadRegistration(context.Context) (modem.Registration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	if r.readErr != nil {
		return modem.NewRegistration(), r.readErr
	}
	return r.reg, nil
}

func (r *registrationModem) set(reg modem.Registration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reg = reg
}

func (r *registrationModem) setReadErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readErr = err
}

func (r *registrationModem) readCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

// WatchIncomingParts polls until the context ends. The messages are handed over
// once; every later pass is an empty poll, which is what a real idle modem does.
func (r *registrationModem) WatchIncomingParts(ctx context.Context, _ time.Duration, handler serial.PartsHandler) error {
	for {
		r.mu.Lock()
		var parts []modem.Part
		if !r.delivered {
			parts = r.fakeModem.parts
			r.delivered = true
		}
		r.mu.Unlock()

		if err := handler(ctx, parts); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func opensRegistrationModem(m *registrationModem) func() (modem.Modem, error) {
	return func() (modem.Modem, error) { return m, nil }
}

// unprovenReg is the registration the live backup modem was found in: attached
// for data over LTE, nothing in the circuit-switched domain, no IMS.
func unprovenReg() modem.Registration {
	return modem.Registration{CS: modem.RegNotRegistered, PS: modem.RegRoaming, IMS: modem.RegNotRegistered}
}

func directReg() modem.Registration {
	return modem.Registration{CS: modem.RegRoaming, PS: modem.RegRoaming, IMS: modem.RegNotRegistered}
}

func searchingReg() modem.Registration {
	return modem.Registration{CS: modem.RegSearching, PS: modem.RegNotRegistered, IMS: modem.RegAbsent}
}

func hello() modem.Part {
	return inboundPart("1", "Beeline", "hello", "26/10/27,10:00:00+00")
}

// TestSMSPathLostIsReported is the feature: a modem that registers for data
// only must be called out, because every other signal the daemon produces says
// it is healthy.
func TestSMSPathLostIsReported(t *testing.T) {
	m := newRegistrationModem(unprovenReg(), hello())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.log.contains("modem_sms_path_lost modem=m1") }, 5*time.Second) {
		t.Fatalf("the unproven SMS path was never reported; log=%v", f.log.all())
	}
	if !f.log.contains("path=unproven") {
		t.Errorf("expected the verdict in the log, got %v", f.log.all())
	}
	if !f.log.contains("cs=not registered ps=registered (roaming) ims=not registered") {
		t.Errorf("expected the registration in the log, got %v", f.log.all())
	}
	if f.ch.count() != 1 {
		t.Errorf("forwarded %d messages, want 1: the check must not disturb reception", f.ch.count())
	}
}

// A modem with a circuit-switched registration is reported once, on the plain
// line, and is never called out as a problem.
func TestSMSPathDirectIsReportedOnce(t *testing.T) {
	m := newRegistrationModem(directReg(), hello())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.log.contains("modem_sms_path modem=m1 path=direct") }, 5*time.Second) {
		t.Fatalf("the SMS path was never reported; log=%v", f.log.all())
	}
	if f.log.contains("modem_sms_path_lost") {
		t.Errorf("a healthy modem was reported as a problem: %v", f.log.all())
	}
	if n := countLines(f.log.all(), "modem_sms_path modem=m1"); n != 1 {
		t.Errorf("the path was logged %d times, want 1: only changes belong in the journal", n)
	}
}

// Reading the registration is paced: the port belongs to the poll loop, and a
// read on every poll would spend the modem's time on diagnostics.
func TestRegistrationReadIsPaced(t *testing.T) {
	m := newRegistrationModem(directReg(), hello())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	clock := newTestClock(32)
	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	f.svc.cfg.Watch.RegistrationInterval = 10 * time.Minute
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return m.readCount() >= 1 }, 5*time.Second) {
		t.Fatalf("the registration was never read; log=%v", f.log.all())
	}
	// Many polls later, with the clock still inside the interval.
	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}
	time.Sleep(50 * time.Millisecond)
	if got := m.readCount(); got != 1 {
		t.Errorf("read the registration %d times inside one interval, want 1", got)
	}

	clock.advance(11 * time.Minute)
	if !waitFor(func() bool { return m.readCount() >= 2 }, 5*time.Second) {
		t.Fatalf("the registration was not re-read after the interval elapsed; reads=%d", m.readCount())
	}

	// Several more intervals of the same answer. The reads keep happening and
	// the journal stays quiet: only a change is news, or a two-minute timer
	// would bury every other line in the journal.
	for i := 0; i < 3; i++ {
		clock.advance(11 * time.Minute)
		waitFor(func() bool { return m.readCount() >= i+3 }, 2*time.Second)
	}
	if got := m.readCount(); got < 4 {
		t.Fatalf("read the registration %d times across five intervals, want at least 4", got)
	}
	if n := countLines(f.log.all(), "modem_sms_path modem=m1"); n != 1 {
		t.Errorf("an unchanged verdict was logged %d times across %d reads, want 1", n, m.readCount())
	}
}

// The pacing guard refuses a non-positive interval rather than reading on
// every single poll. Config never produces one -- the accessor substitutes the
// default -- so this is checked where the guard lives.
func TestRegistrationCheckRefusesNonPositiveInterval(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, newTestClock(32))
	defer f.close()

	now := f.clock.timeNow()
	for _, interval := range []time.Duration{0, -time.Minute} {
		if f.svc.beginRegistrationCheck("m1", now, interval) {
			t.Errorf("interval %s claimed a check was due", interval)
		}
	}
	if !f.svc.beginRegistrationCheck("m1", now, time.Minute) {
		t.Error("the first check with a real interval should be due immediately")
	}
	if f.svc.beginRegistrationCheck("m1", now, time.Minute) {
		t.Error("a second check inside the interval should not be due")
	}
}

// Whatever the config says, the interval the daemon uses is positive, so the
// check cannot be switched off by accident and leave the condition unwatched.
func TestRegistrationIntervalIsAlwaysPositive(t *testing.T) {
	for _, configured := range []time.Duration{0, time.Second, 10 * time.Minute} {
		cfg := config.Config{}
		cfg.Watch.RegistrationInterval = configured
		if got := cfg.RegistrationInterval(); got <= 0 {
			t.Errorf("configured %s yielded %s", configured, got)
		}
	}
}

// A driver that cannot report the domains separately -- the mm driver -- must
// be skipped in silence, not logged as a failure on every poll.
func TestRegistrationSkippedForDriverWithoutSupport(t *testing.T) {
	m := &fakeModem{parts: []modem.Part{hello()}}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensAs(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}
	for _, line := range f.log.all() {
		if containsAny(line, "modem_sms_path", "registration_unreadable") {
			t.Errorf("a driver without registration support produced %q", line)
		}
	}
}

// A read that fails must cost nothing: it is logged once and reception carries
// on. A diagnostic that can take a SIM out of service is worse than no
// diagnostic.
func TestRegistrationReadFailureIsLoggedOnceAndHarmless(t *testing.T) {
	m := newRegistrationModem(unprovenReg(), hello())
	m.setReadErr(fmt.Errorf("AT+CREG?: timeout waiting for modem response"))
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	clock := newTestClock(32)
	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	f.svc.cfg.Watch.RegistrationInterval = time.Minute
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.log.contains("registration_unreadable modem=m1") }, 5*time.Second) {
		t.Fatalf("the failed read was never reported; log=%v", f.log.all())
	}
	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("a failed registration read stopped forwarding; log=%v", f.log.all())
	}
	if f.log.contains("modem_down modem=m1") {
		t.Errorf("the modem was taken down over a failed read: %v", f.log.all())
	}

	// Several more intervals of the same failure must not repeat the line.
	for i := 0; i < 3; i++ {
		clock.advance(2 * time.Minute)
		waitFor(func() bool { return m.readCount() >= i+2 }, 2*time.Second)
	}
	if n := countLines(f.log.all(), "registration_unreadable"); n != 1 {
		t.Errorf("the failed read was logged %d times, want 1", n)
	}

	// Once it answers again, the verdict is reported.
	m.setReadErr(nil)
	clock.advance(2 * time.Minute)
	if !waitFor(func() bool { return f.log.contains("modem_sms_path_lost modem=m1") }, 5*time.Second) {
		t.Errorf("the recovered read never produced a verdict; log=%v", f.log.all())
	}
}

// Each modem is judged on its own registration.
func TestSMSPathIsPerModem(t *testing.T) {
	good := newRegistrationModem(directReg(), inboundPart("1", "Beeline", "from main", "26/10/27,10:00:00+00"))
	bad := newRegistrationModem(unprovenReg(), inboundPart("2", "MTS", "from backup", "26/10/27,10:00:01+00"))

	byDevice := map[string]*registrationModem{"m1": good, "m2": bad}
	open := func(entry config.ModemEntry) (modem.Modem, error) {
		return byDevice[entry.Serial.Device], nil
	}
	modems := map[string]config.ModemEntry{
		"m1": {Driver: config.DriverSerial, Serial: config.SerialConfig{Device: "m1"}},
		"m2": {Driver: config.DriverSerial, Serial: config.SerialConfig{Device: "m2"}},
	}

	f := newLivenessFixture(t, modems, open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 2 }, 5*time.Second) {
		t.Fatalf("both modems should have forwarded; got %d, log=%v", f.ch.count(), f.log.all())
	}
	if !waitFor(func() bool {
		return f.log.contains("modem_sms_path modem=m1 path=direct") &&
			f.log.contains("modem_sms_path_lost modem=m2")
	}, 5*time.Second) {
		t.Fatalf("expected one healthy and one unproven verdict, got %v", f.log.all())
	}
	if f.log.contains("modem_sms_path_lost modem=m1") {
		t.Errorf("the healthy modem was reported as a problem: %v", f.log.all())
	}
}

// smsPathMonitors isolates the condition under test: the polling monitor has
// its own tests, and evaluating both here would mix two alerts in one stream.
func (a *alertFixture) smsPathMonitors() *monitors {
	return &monitors{smsPath: alert.NewTopicMonitor(a.svc.cfg.AlertAfter(), alert.TopicSMSPath)}
}

// observe puts a modem in a given state as a completed check would, so the
// alert policy can be driven without a modem on the other end.
func (a *alertFixture) observe(name string, reg modem.Registration) {
	a.svc.recordRegistration(name, reg, reg.SMSPath(), a.clock.timeNow())
}

// TestSMSPathAlertFiresAfterThreshold is the operator-facing half: the
// condition has to persist, because reselection passes through it briefly.
func TestSMSPathAlertFiresAfterThreshold(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", unprovenReg())

	a.tick(ctx, mons)
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted immediately: %v", a.sender.messages())
	}

	a.clock.advance(4 * time.Minute)
	a.observe("ec25-backup", unprovenReg())
	a.tick(ctx, mons)
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted before the threshold: %v", a.sender.messages())
	}

	a.clock.advance(time.Minute)
	a.observe("ec25-backup", unprovenReg())
	a.tick(ctx, mons)
	if got := a.sender.matching("no proven path for incoming SMS"); got != 1 {
		t.Fatalf("got %d alerts at the threshold, want 1: %v", got, a.sender.messages())
	}
	if got := a.sender.matching("cs=not registered"); got != 1 {
		t.Errorf("the alert did not name the registration: %v", a.sender.messages())
	}
}

// Still unproven is not news.
func TestSMSPathAlertIsNotRepeated(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	for i := 0; i < 10; i++ {
		a.observe("ec25-backup", unprovenReg())
		a.tick(ctx, mons)
		a.clock.advance(2 * time.Minute)
	}
	if got := len(a.sender.messages()); got != 1 {
		t.Fatalf("sent %d alerts, want 1: %v", got, a.sender.messages())
	}
}

// Recovery closes the loop: the operator who was told is told it is over.
func TestSMSPathRecoveryIsReported(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", unprovenReg())
	a.clock.advance(5 * time.Minute)
	a.observe("ec25-backup", unprovenReg())
	a.tick(ctx, mons)
	if got := a.sender.matching("no proven path"); got != 1 {
		t.Fatalf("expected the problem alert first: %v", a.sender.messages())
	}

	a.clock.advance(time.Minute)
	a.observe("ec25-backup", directReg())
	a.tick(ctx, mons)
	if got := a.sender.matching("can receive SMS again"); got != 1 {
		t.Fatalf("recovery not reported: %v", a.sender.messages())
	}
	if got := a.sender.matching("cs=registered (roaming)"); got != 1 {
		t.Errorf("the recovery did not name the registration it came back on: %v", a.sender.messages())
	}
}

// A reading that cannot be judged must never raise an alarm. A modem searching
// for a network is in this state every time it reselects.
func TestUnknownSMSPathNeverAlerts(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	for i := 0; i < 10; i++ {
		a.observe("ec25-backup", searchingReg())
		a.tick(ctx, mons)
		a.clock.advance(2 * time.Minute)
	}
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted on a reading that proves nothing: %v", a.sender.messages())
	}
}

// A modem that has never been read must not alert either: the condition is
// unproven in both directions until something answers.
func TestNeverReadModemNeverAlerts(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	for i := 0; i < 10; i++ {
		a.tick(ctx, mons)
		a.clock.advance(2 * time.Minute)
	}
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted about a modem nothing was read from: %v", a.sender.messages())
	}
}

// A modem whose port is down is already covered by the polling alert. Sending
// both would teach an operator that one of the two is noise.
func TestDisconnectedModemDoesNotAlertOnSMSPath(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", unprovenReg())
	a.svc.markModemDown("ec25-backup", fmt.Errorf("read response: file already closed"))

	for i := 0; i < 10; i++ {
		a.clock.advance(2 * time.Minute)
		a.tick(ctx, mons)
	}
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("alerted on the SMS path of a disconnected modem: %v", a.sender.messages())
	}
}

// Reconnecting restarts the grace period: registration takes a moment to come
// up after the port does, and an alert on that moment would fire every time a
// modem re-enumerates.
func TestReconnectRestartsTheSMSPathGrace(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", unprovenReg())
	a.clock.advance(4 * time.Minute)
	a.tick(ctx, mons)

	a.svc.markModemDown("ec25-backup", fmt.Errorf("device went away"))
	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", unprovenReg())
	a.clock.advance(4 * time.Minute)
	a.tick(ctx, mons)
	if got := len(a.sender.messages()); got != 0 {
		t.Fatalf("the reconnect did not reset the grace period: %v", a.sender.messages())
	}

	a.clock.advance(2 * time.Minute)
	a.observe("ec25-backup", unprovenReg())
	a.tick(ctx, mons)
	if got := len(a.sender.messages()); got != 1 {
		t.Fatalf("got %d alerts once the grace ran out again, want 1: %v", got, a.sender.messages())
	}
}

// A modem attached to nothing at all is the same operator-facing fact: no SMS
// can arrive, and nothing else says so because the modem still answers AT.
func TestOfflineModemAlertsOnSMSPath(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	offline := modem.Registration{CS: modem.RegNotRegistered, PS: modem.RegNotRegistered, IMS: modem.RegNotRegistered}
	a.svc.markModemUp("ec25-backup")
	a.observe("ec25-backup", offline)
	a.clock.advance(5 * time.Minute)
	a.observe("ec25-backup", offline)
	a.tick(ctx, mons)

	if got := a.sender.matching("no proven path for incoming SMS"); got != 1 {
		t.Fatalf("an unregistered modem was not reported: %v", a.sender.messages())
	}
}

// One modem's unproven path must not implicate the other.
func TestSMSPathAlertNamesOnlyTheAffectedModem(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-main", "ec25-backup")
	ctx := context.Background()
	mons := a.smsPathMonitors()

	a.svc.markModemUp("ec25-main")
	a.svc.markModemUp("ec25-backup")
	for i := 0; i < 4; i++ {
		a.observe("ec25-main", directReg())
		a.observe("ec25-backup", unprovenReg())
		a.clock.advance(2 * time.Minute)
		a.tick(ctx, mons)
	}

	if got := a.sender.matching("ec25-backup"); got != 1 {
		t.Errorf("backup alerted %d times, want 1: %v", got, a.sender.messages())
	}
	if got := a.sender.matching("ec25-main"); got != 0 {
		t.Errorf("the healthy modem was alerted on: %v", a.sender.messages())
	}
}

// The switch turns off the alert and nothing else: the journal still records
// every change, so the condition stays visible without waking anyone.
func TestSMSPathAlertCanBeDisabled(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()

	off := false
	a.svc.cfg.Alerts.SMSPath = &off
	mons := a.svc.newMonitors(a.svc.cfg.AlertAfter())
	if mons.smsPath != nil {
		t.Fatal("the sms path monitor was built although the alert is off")
	}
	if mons.polling == nil {
		t.Fatal("switching off the sms path alert also switched off the polling alert")
	}

	a.svc.markModemUp("ec25-backup")
	for i := 0; i < 10; i++ {
		a.observe("ec25-backup", unprovenReg())
		a.svc.markPollOK("ec25-backup", a.clock.timeNow())
		a.clock.advance(2 * time.Minute)
		a.tick(ctx, mons)
	}
	if got := a.sender.matching("no proven path"); got != 0 {
		t.Fatalf("alerted with the alert switched off: %v", a.sender.messages())
	}
}

// Both conditions can hold at once, and the operator must be told both: a modem
// that stopped polling and has no SMS path are two different repairs.
func TestBothAlertsReachTheOperator(t *testing.T) {
	a := newAlertFixture(t, 5*time.Minute, "ec25-backup")
	ctx := context.Background()
	mons := a.svc.newMonitors(a.svc.cfg.AlertAfter())

	a.svc.markModemUp("ec25-backup")
	a.svc.markPollOK("ec25-backup", a.clock.timeNow())
	a.observe("ec25-backup", unprovenReg())

	a.clock.advance(5 * time.Minute)
	a.observe("ec25-backup", unprovenReg())
	a.tick(ctx, mons)

	if got := a.sender.matching("has not polled"); got != 1 {
		t.Errorf("polling alert sent %d times, want 1: %v", got, a.sender.messages())
	}
	if got := a.sender.matching("no proven path for incoming SMS"); got != 1 {
		t.Errorf("sms path alert sent %d times, want 1: %v", got, a.sender.messages())
	}
}

// The heartbeat carries the verdict, so a journal read long after the change
// still shows the current state.
func TestHealthHeartbeatReportsSMSPath(t *testing.T) {
	m := newRegistrationModem(unprovenReg(), hello())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	clock := newTestClock(10_000)
	f := newLivenessFixture(t, serialModems("m1"), opener.open, clock)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.log.contains("modem_sms_path_lost modem=m1") }, 5*time.Second) {
		t.Fatalf("the check never ran; log=%v", f.log.all())
	}
	f.svc.logHealth(clock.timeNow())
	if !f.log.contains("sms_path=unproven") {
		t.Errorf("the heartbeat does not carry the verdict: %v", f.log.all())
	}
}

// A modem nothing has been read from yet reports unknown rather than an empty
// field, so the heartbeat never prints a bare "sms_path=".
func TestHealthHeartbeatReportsUnknownBeforeAnyRead(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1"), func(config.ModemEntry) (modem.Modem, error) {
		return &fakeModem{}, nil
	}, newTestClock(32))
	defer f.close()

	f.svc.logHealth(f.clock.timeNow())
	if !f.log.contains("sms_path=unknown") {
		t.Errorf("expected sms_path=unknown before any read, got %v", f.log.all())
	}
}

func countLines(lines []string, substr string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}
