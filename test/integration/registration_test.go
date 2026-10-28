//go:build integration && linux

package integration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/test/integration/harness"
)

// registrationCommands are the four queries the verdict is built from. The lab
// asserts on them because the whole check is worthless if the driver asks the
// wrong modem questions.
var registrationCommands = []string{"AT+CREG?", "AT+CGREG?", "AT+CEREG?", "AT+CIREG?"}

func sentAll(cmds []string, want []string) bool {
	seen := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		seen[c] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// TestRegistrationReadOverRealTTY is the baseline: the real driver, a real AT
// exchange over a pty, and a modem whose circuit-switched domain is attached.
func TestRegistrationReadOverRealTTY(t *testing.T) {
	l := startLab(t, false)

	if !waitUntil(func() bool { return l.log.has("modem_sms_path modem=lab path=direct") }, 20*time.Second) {
		t.Fatalf("the SMS path was never reported; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	if !sentAll(l.sim.Commands(), registrationCommands) {
		t.Errorf("not every registration query was sent; cmds=%v", l.sim.Commands())
	}
	if l.log.has("modem_sms_path_lost") {
		t.Errorf("a modem with a circuit-switched registration was reported as a problem: %v", l.log.all())
	}
}

// TestLTEOnlyModemIsReportedOverRealTTY is the fault this feature exists for,
// reproduced end to end: attached over LTE, nothing circuit-switched, no IMS.
// The modem answers every command, polls cleanly and forwards SMS the test
// hands it, and the only thing that says anything is wrong is this check.
func TestLTEOnlyModemIsReportedOverRealTTY(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 0)
	}})
	l.sim.AddMessage(1, pduHello)

	if !waitUntil(func() bool { return l.log.has("modem_sms_path_lost modem=lab path=unproven") }, 20*time.Second) {
		t.Fatalf("the unproven path was never reported; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	if !l.log.has("cs=not registered ps=registered (roaming) ims=not registered") {
		t.Errorf("the log does not name the registration behind the verdict: %v", l.log.all())
	}
	// The point of the alert: everything else about this modem is fine.
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("the check interfered with reception; log=%v", l.log.all())
	}
	if l.log.has("modem_down modem=lab") {
		t.Errorf("the modem was taken down: %v", l.log.all())
	}
}

// An IMS registration is a real SMS path and must not be reported as a fault.
func TestIMSRegistrationCountsAsAPath(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 1)
	}})

	if !waitUntil(func() bool { return l.log.has("modem_sms_path modem=lab path=ims") }, 20*time.Second) {
		t.Fatalf("an IMS registration was not recognised; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	if l.log.has("modem_sms_path_lost") {
		t.Errorf("a modem registered on IMS was reported as a problem: %v", l.log.all())
	}
}

// Firmware that does not implement +CIREG answers ERROR. The driver has to read
// that as no IMS path rather than as a failed read, so an LTE-only modem on
// such firmware is still reported.
func TestModemWithoutCIREGStillGetsAVerdict(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 0)
		m.SetIMSSupported(false)
	}})

	if !waitUntil(func() bool { return l.log.has("modem_sms_path_lost modem=lab path=unproven") }, 20*time.Second) {
		t.Fatalf("no verdict on firmware without +CIREG; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	if !l.log.has("ims=unsupported") {
		t.Errorf("the log does not say the IMS query went unanswered: %v", l.log.all())
	}
	if l.log.has("registration_unreadable") {
		t.Errorf("an unsupported +CIREG was treated as a failed read: %v", l.log.all())
	}
}

// A modem attached to nothing still answers AT, so nothing else notices.
func TestUnregisteredModemIsReported(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegNotRegistered, 0)
	}})

	if !waitUntil(func() bool { return l.log.has("modem_sms_path_lost modem=lab path=offline") }, 20*time.Second) {
		t.Fatalf("an unregistered modem was not reported; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
}

// Reselection passes through a searching state on every network change. It must
// produce no verdict at all rather than a fault.
func TestSearchingModemIsNotReportedAsAFault(t *testing.T) {
	l := startLabWith(t, labOptions{before: func(m *harness.SerialModem) {
		m.SetRegistration(harness.RegSearching, harness.RegSearching, harness.RegNotRegistered, 0)
	}})

	if !waitUntil(func() bool { return l.log.has("modem_sms_path modem=lab path=unknown") }, 20*time.Second) {
		t.Fatalf("no reading was taken; log=%v cmds=%v", l.log.all(), l.sim.Commands())
	}
	if l.log.has("modem_sms_path_lost") {
		t.Errorf("a searching modem was reported as a fault: %v", l.log.all())
	}
}

// The condition the posture correction cannot prevent: a modem that starts fine
// and loses its circuit-switched registration later, with the configuration
// never changing. This is what makes the check worth having on a timer.
func TestSMSPathLossIsNoticedWhileRunning(t *testing.T) {
	l := startLab(t, false)

	if !waitUntil(func() bool { return l.log.has("modem_sms_path modem=lab path=direct") }, 20*time.Second) {
		t.Fatalf("the healthy path was never reported; log=%v", l.log.all())
	}

	// The operator switches 3G off under the modem; it falls back to LTE only.
	l.sim.SetRegistration(harness.RegNotRegistered, harness.RegNotRegistered, harness.RegRoaming, 0)

	if !waitUntil(func() bool { return l.log.has("modem_sms_path_lost modem=lab path=unproven") }, 20*time.Second) {
		t.Fatalf("the loss was never noticed; log=%v", l.log.all())
	}

	// And back again, so an operator who was told is told it is over.
	l.sim.SetRegistration(harness.RegRoaming, harness.RegRoaming, harness.RegNotRegistered, 0)
	if !waitUntil(func() bool {
		for _, line := range l.log.all() {
			if strings.Contains(line, "modem_sms_path modem=lab path=direct") &&
				strings.Contains(line, "cs=registered (roaming)") {
				return true
			}
		}
		return false
	}, 20*time.Second) {
		t.Fatalf("the recovery was never reported; log=%v", l.log.all())
	}
}

// A verdict that has not changed must not be logged again, or a two-minute
// timer would fill the journal with the same line forever.
func TestUnchangedSMSPathIsLoggedOnce(t *testing.T) {
	l := startLab(t, false)

	if !waitUntil(func() bool { return l.log.has("modem_sms_path modem=lab path=direct") }, 20*time.Second) {
		t.Fatalf("the SMS path was never reported; log=%v", l.log.all())
	}
	// Many intervals' worth of polls at the lab's 50ms cadence.
	time.Sleep(time.Second)

	n := 0
	for _, line := range l.log.all() {
		if strings.Contains(line, "modem_sms_path modem=lab") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the unchanged verdict was logged %d times, want 1: %v", n, l.log.all())
	}
}
