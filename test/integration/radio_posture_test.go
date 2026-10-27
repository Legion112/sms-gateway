//go:build integration && linux

package integration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/test/integration/harness"
)

// Quectel nwscanmode and COPS values, as the modem stores them.
const (
	scanAuto = 0
	scanLTE  = 3
	copsAuto = 0
	copsLock = 1
)

// writeCommands returns only the AT commands that change the radio, so a test
// can assert that a modem already in the right posture was not written to.
func writeCommands(cmds []string) []string {
	var out []string
	for _, cmd := range cmds {
		if strings.HasPrefix(cmd, `AT+QCFG="nwscanmode",`) || strings.HasPrefix(cmd, "AT+COPS=") {
			out = append(out, cmd)
		}
	}
	return out
}

func radioIs(l *lab, scan, cops int) func() bool {
	return func() bool {
		gotScan, gotCops := l.sim.Radio()
		return gotScan == scan && gotCops == cops
	}
}

// TestRadioPostureCorrectedOverRealTTY is the lab counterpart of the fault
// found on the live backup modem: a module locked to LTE with a manually
// pinned operator. Over a real tty, with the real driver and the real AT
// exchange, the daemon must put it back to automatic on its own.
func TestRadioPostureCorrectedOverRealTTY(t *testing.T) {
	l := startLabWith(t, labOptions{
		before: func(sim *harness.SerialModem) { sim.SetRadio(scanLTE, copsLock) },
	})

	if !waitUntil(radioIs(l, scanAuto, copsAuto), 20*time.Second) {
		scan, cops := l.sim.Radio()
		t.Fatalf("posture still scanmode=%d cops=%d; log=%v cmds=%v", scan, cops, l.log.all(), writeCommands(l.sim.Commands()))
	}

	writes := writeCommands(l.sim.Commands())
	if len(writes) != 2 {
		t.Fatalf("radio writes = %v, want exactly the two corrections", writes)
	}
	if writes[0] != `AT+QCFG="nwscanmode",0,1` {
		t.Errorf("first write = %q", writes[0])
	}
	if writes[1] != "AT+COPS=0" {
		t.Errorf("second write = %q", writes[1])
	}
	if !l.log.has("modem_radio_corrected modem=lab") {
		t.Errorf("expected the correction in the journal; log=%v", l.log.all())
	}

	// And the modem still works afterwards: a correction that cost reception
	// would be no improvement at all.
	l.sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("no message forwarded after the correction; log=%v", l.log.all())
	}
}

// A modem already in the configured posture must be read and left alone. If it
// were rewritten on every connect the journal would be useless for spotting the
// day a modem really did change.
func TestRadioPostureLeavesCorrectModemUntouched(t *testing.T) {
	l := startLabWith(t, labOptions{
		before: func(sim *harness.SerialModem) { sim.SetRadio(scanAuto, copsAuto) },
	})

	l.sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("message never forwarded; log=%v", l.log.all())
	}

	if writes := writeCommands(l.sim.Commands()); len(writes) != 0 {
		t.Errorf("a correct modem was written to: %v", writes)
	}
	if !l.log.has("modem_radio modem=lab scan_mode=auto operator=automatic") {
		t.Errorf("expected the posture to be reported; log=%v", l.log.all())
	}
	if l.log.has("modem_radio_corrected") {
		t.Errorf("nothing needed correcting but a correction was logged; log=%v", l.log.all())
	}
}

// keep is the escape hatch for a modem locked on purpose. It must survive
// contact with the daemon entirely.
func TestRadioPostureKeepLeavesLockInPlace(t *testing.T) {
	l := startLabWith(t, labOptions{
		radio:  config.RadioConfig{ScanMode: config.ScanModeKeep, Operator: config.OperatorKeep},
		before: func(sim *harness.SerialModem) { sim.SetRadio(scanLTE, copsLock) },
	})

	l.sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("message never forwarded; log=%v", l.log.all())
	}

	if scan, cops := l.sim.Radio(); scan != scanLTE || cops != copsLock {
		t.Errorf("posture = scanmode=%d cops=%d, want the configured lock left at %d/%d", scan, cops, scanLTE, copsLock)
	}
	if writes := writeCommands(l.sim.Commands()); len(writes) != 0 {
		t.Errorf("keep still wrote to the modem: %v", writes)
	}
}

// The posture is a policy, not a hardcoded preference for automatic: a modem
// configured to sit on LTE must be put there.
func TestRadioPostureAppliesDeliberateLock(t *testing.T) {
	l := startLabWith(t, labOptions{
		radio:  config.RadioConfig{ScanMode: config.ScanModeLTE},
		before: func(sim *harness.SerialModem) { sim.SetRadio(scanAuto, copsAuto) },
	})

	if !waitUntil(radioIs(l, scanLTE, copsAuto), 20*time.Second) {
		scan, cops := l.sim.Radio()
		t.Fatalf("posture = scanmode=%d cops=%d, want scanmode=%d; log=%v", scan, cops, scanLTE, l.log.all())
	}
	writes := writeCommands(l.sim.Commands())
	if len(writes) != 1 || writes[0] != `AT+QCFG="nwscanmode",3,1` {
		t.Errorf("radio writes = %v, want only the technology lock", writes)
	}
}

// A module that loses power during a USB re-enumeration can come back on its
// stored posture. The daemon only looks at connect, so that connect has to
// catch it — this is the case the unit tests cannot reach, because here the
// device node really does disappear and return as a different pts.
func TestRadioPostureReappliedAfterDeviceLoss(t *testing.T) {
	l := startLabWith(t, labOptions{
		before: func(sim *harness.SerialModem) { sim.SetRadio(scanAuto, copsAuto) },
	})

	l.sim.AddMessage(1, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 1 }, 20*time.Second) {
		t.Fatalf("first message never forwarded; log=%v", l.log.all())
	}
	if l.log.has("modem_radio_corrected") {
		t.Fatalf("the modem started correct but was corrected; log=%v", l.log.all())
	}

	l.sim.Unplug()
	if !waitUntil(func() bool { return l.log.has("modem_down modem=lab") }, 20*time.Second) {
		t.Fatalf("device loss was never noticed; log=%v", l.log.all())
	}

	// It comes back locked, as a module that reset to its stored posture would.
	l.sim.SetRadio(scanLTE, copsLock)
	if err := l.sim.Replug(); err != nil {
		t.Fatalf("replug: %v", err)
	}

	if !waitUntil(radioIs(l, scanAuto, copsAuto), 30*time.Second) {
		scan, cops := l.sim.Radio()
		t.Fatalf("posture after reconnect = scanmode=%d cops=%d; log=%v", scan, cops, l.log.all())
	}
	if !l.log.has("modem_radio_corrected modem=lab") {
		t.Errorf("expected the reconnect correction in the journal; log=%v", l.log.all())
	}

	l.sim.AddMessage(2, pduHello)
	if !waitUntil(func() bool { return l.ch.count() == 2 }, 30*time.Second) {
		t.Fatalf("no message forwarded after the reconnect correction; log=%v", l.log.all())
	}
}
