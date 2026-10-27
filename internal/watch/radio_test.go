package watch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

// radioModem is a fakeModem that also answers radio posture queries, as the
// serial driver does. The plain fakeModem stands in for the mm driver, which
// leaves the radio to ModemManager and implements no such thing.
type radioModem struct {
	*fakeModem

	mu       sync.Mutex
	posture  modem.RadioPosture
	applyErr error
	reads    int
	applies  int
}

func newRadioModem(scanMode, operator string, parts ...modem.Part) *radioModem {
	return &radioModem{
		fakeModem: &fakeModem{parts: parts},
		posture:   modem.RadioPosture{ScanMode: scanMode, Operator: operator},
	}
}

func (r *radioModem) ReadRadioPosture(context.Context) (modem.RadioPosture, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return r.posture, nil
}

func (r *radioModem) ApplyRadioPosture(context.Context) (modem.RadioPosture, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applies++
	if r.applyErr != nil {
		return r.posture, r.applyErr
	}

	var changed []string
	if r.posture.ScanMode != config.ScanModeAuto {
		changed = append(changed, fmt.Sprintf("scan_mode %s -> %s", r.posture.ScanMode, config.ScanModeAuto))
		r.posture.ScanMode = config.ScanModeAuto
	}
	if r.posture.Operator != modem.OperatorAutomatic {
		changed = append(changed, fmt.Sprintf("operator %s -> %s", r.posture.Operator, modem.OperatorAutomatic))
		r.posture.Operator = modem.OperatorAutomatic
	}

	result := r.posture
	result.Changed = changed
	return result, nil
}

func (r *radioModem) applyCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applies
}

func (r *radioModem) current() modem.RadioPosture {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.posture
}

func opensModem(m modem.Modem) func() (modem.Modem, error) {
	return func() (modem.Modem, error) { return m, nil }
}

func autoPosture() modem.RadioPosture {
	return modem.RadioPosture{ScanMode: config.ScanModeAuto, Operator: modem.OperatorAutomatic}
}

// TestRadioPostureCorrectedAtConnect is the regression test for the fault found
// on the live backup modem: locked to LTE with a manually pinned operator, so
// it registered, polled clean and reported itself up while having no guaranteed
// path for an SMS to reach it.
func TestRadioPostureCorrectedAtConnect(t *testing.T) {
	m := newRadioModem(config.ScanModeLTE, modem.OperatorManual,
		inboundPart("1", "Beeline", "hello", "26/10/27,10:00:00+00"))
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}
	if got := m.current(); !got.Equal(autoPosture()) {
		t.Errorf("posture after connect = %s, want %s", got, autoPosture())
	}
	if !f.log.contains("modem_radio_corrected modem=m1") {
		t.Errorf("expected the correction to be logged, got %v", f.log.all())
	}
	if !f.log.contains("scan_mode lte -> auto") {
		t.Errorf("expected the log to name the technology change, got %v", f.log.all())
	}
	if !f.log.contains("operator manual -> automatic") {
		t.Errorf("expected the log to name the operator change, got %v", f.log.all())
	}
}

// A modem already in the right posture must be left alone and must not produce
// a correction line; otherwise the journal cries wolf on every reconnect.
func TestRadioPostureLeavesCorrectModemAlone(t *testing.T) {
	m := newRadioModem(config.ScanModeAuto, modem.OperatorAutomatic,
		inboundPart("1", "Beeline", "hello", "26/10/27,10:00:00+00"))
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}
	if !f.log.contains("modem_radio modem=m1 scan_mode=auto operator=automatic") {
		t.Errorf("expected the posture to be reported, got %v", f.log.all())
	}
	if f.log.contains("modem_radio_corrected") {
		t.Errorf("nothing needed correcting, but a correction was logged: %v", f.log.all())
	}
}

// A posture that cannot be applied must not cost the SIM its service. Receiving
// on a bad posture still beats not receiving at all.
func TestRadioPostureFailureDoesNotStopForwarding(t *testing.T) {
	m := newRadioModem(config.ScanModeLTE, modem.OperatorManual,
		inboundPart("1", "Beeline", "hello", "26/10/27,10:00:00+00"))
	m.applyErr = fmt.Errorf(`AT+COPS=0: timeout waiting for modem response`)
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("a failed posture correction stopped forwarding; log=%v", f.log.all())
	}
	if !f.log.contains("radio_posture_failed modem=m1") {
		t.Errorf("expected the failure to be logged, got %v", f.log.all())
	}
	if f.log.contains("modem_down modem=m1") {
		t.Errorf("the modem was taken down over a posture failure: %v", f.log.all())
	}
}

// The mm driver does not implement RadioController. It must be skipped in
// silence rather than logged as a failure.
func TestRadioPostureSkippedForDriverWithoutSupport(t *testing.T) {
	m := &fakeModem{parts: []modem.Part{inboundPart("1", "Beeline", "hello", "26/10/27,10:00:00+00")}}
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensAs(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded; log=%v", f.log.all())
	}
	for _, line := range f.log.all() {
		if containsAny(line, "modem_radio", "radio_posture_failed") {
			t.Errorf("a driver without radio support produced %q", line)
		}
	}
}

// A modem that comes back after a USB re-enumeration may have reverted to its
// stored posture, so every reconnect has to assert it again rather than trust
// what an earlier connection established.
func TestRadioPostureReappliedOnReconnect(t *testing.T) {
	first := newRadioModem(config.ScanModeLTE, modem.OperatorManual)
	first.watchErr = fmt.Errorf("read response: file already closed")
	second := newRadioModem(config.ScanModeLTE, modem.OperatorManual,
		inboundPart("1", "Beeline", "after reconnect", "26/10/27,10:00:00+00"))

	opener := &scriptedOpener{script: []func() (modem.Modem, error){
		opensModem(first),
		opensModem(second),
	}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool { return f.ch.count() == 1 }, 5*time.Second) {
		t.Fatalf("message never forwarded after reconnect; log=%v", f.log.all())
	}
	if got := first.applyCount(); got != 1 {
		t.Errorf("first connection applied the posture %d times, want 1", got)
	}
	if got := second.applyCount(); got != 1 {
		t.Errorf("the reconnected modem applied the posture %d times, want 1", got)
	}
	if got := second.current(); !got.Equal(autoPosture()) {
		t.Errorf("posture after reconnect = %s, want %s", got, autoPosture())
	}
}

// Each modem's posture is handled on its own connection, so one misconfigured
// SIM neither hides nor corrupts the other.
func TestRadioPostureIsPerModem(t *testing.T) {
	good := newRadioModem(config.ScanModeAuto, modem.OperatorAutomatic,
		inboundPart("1", "Beeline", "from main", "26/10/27,10:00:00+00"))
	bad := newRadioModem(config.ScanModeLTE, modem.OperatorManual,
		inboundPart("2", "MTS", "from backup", "26/10/27,10:00:01+00"))

	openers := map[string]*radioModem{"m1": good, "m2": bad}
	open := func(entry config.ModemEntry) (modem.Modem, error) {
		return openers[entry.Serial.Device], nil
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
	if got := good.current(); !got.Equal(autoPosture()) {
		t.Errorf("m1 posture = %s, want unchanged %s", got, autoPosture())
	}
	if got := bad.current(); !got.Equal(autoPosture()) {
		t.Errorf("m2 posture = %s, want corrected to %s", got, autoPosture())
	}
	if !f.log.contains("modem_radio modem=m1") {
		t.Errorf("expected m1 posture reported, got %v", f.log.all())
	}
	if !f.log.contains("modem_radio_corrected modem=m2") {
		t.Errorf("expected m2 correction reported, got %v", f.log.all())
	}
}

func containsAny(line string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
}
