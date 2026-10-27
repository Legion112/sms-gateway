package serial

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

// Quectel AT+QCFG="nwscanmode" values.
const (
	nwScanAuto  = 0
	nwScanGSM   = 1
	nwScanWCDMA = 2
	nwScanLTE   = 3
)

// AT+COPS <mode> values this driver acts on: 0 selects a network automatically,
// 1 pins the modem to one operator.
const (
	copsAutomatic = 0
	copsManual    = 1
)

// radioWriteTimeout bounds the two commands that can set the modem searching.
// Quectel allows AT+COPS up to three minutes, which is far too long to hold up
// a connect; a write only happens when the modem is in the wrong posture, so in
// practice this is paid once and never again.
const radioWriteTimeout = 30 * time.Second

var (
	nwScanModeRe = regexp.MustCompile(`\+QCFG:\s*"nwscanmode",\s*(\d+)`)
	copsModeRe   = regexp.MustCompile(`\+COPS:\s*(\d+)`)
)

func parseScanMode(response string) (int, bool) {
	m := nwScanModeRe.FindStringSubmatch(response)
	if len(m) != 2 {
		return 0, false
	}
	return atoi(m[1]), true
}

func parseCOPSMode(response string) (int, bool) {
	m := copsModeRe.FindStringSubmatch(response)
	if len(m) != 2 {
		return 0, false
	}
	return atoi(m[1]), true
}

func scanModeName(value int) string {
	switch value {
	case nwScanAuto:
		return config.ScanModeAuto
	case nwScanGSM:
		return config.ScanModeGSM
	case nwScanWCDMA:
		return config.ScanModeWCDMA
	case nwScanLTE:
		return config.ScanModeLTE
	default:
		return modem.RadioUnknown
	}
}

func scanModeValue(name string) (int, bool) {
	switch name {
	case config.ScanModeAuto:
		return nwScanAuto, true
	case config.ScanModeGSM:
		return nwScanGSM, true
	case config.ScanModeWCDMA:
		return nwScanWCDMA, true
	case config.ScanModeLTE:
		return nwScanLTE, true
	default:
		return 0, false
	}
}

func operatorName(value int) string {
	switch value {
	case copsAutomatic:
		return modem.OperatorAutomatic
	case copsManual:
		return modem.OperatorManual
	default:
		return modem.RadioUnknown
	}
}

// radioAction is one correction: the AT command to send, and how to describe it
// afterwards.
type radioAction struct {
	command string
	change  string
}

// radioSupport records which posture queries the modem actually answered.
// A setting the modem does not implement must never be written: a modem without
// Quectel's QCFG would otherwise log a failed correction on every connect,
// forever, for a setting it does not have.
type radioSupport struct {
	scanMode bool
	operator bool
}

// planRadio works out what must change to bring current into line with want. It
// is separate from the AT exchange so the decision can be tested on its own,
// and so the common case — a modem already in the right posture — is visibly
// an empty plan that writes nothing.
//
// A setting the modem answered but this driver could not make sense of counts
// as wrong: asserting the posture we want beats leaving a modem in a state we
// cannot describe.
func planRadio(want config.RadioConfig, current modem.RadioPosture, support radioSupport) []radioAction {
	var actions []radioAction

	if mode := want.Mode(); support.scanMode && mode != config.ScanModeKeep && current.ScanMode != mode {
		if value, ok := scanModeValue(mode); ok {
			actions = append(actions, radioAction{
				// effect=1 applies the change immediately instead of at the
				// next reboot, which a headless gateway may not see for months.
				command: fmt.Sprintf(`AT+QCFG="nwscanmode",%d,1`, value),
				change:  fmt.Sprintf("scan_mode %s -> %s", orUnknown(current.ScanMode), mode),
			})
		}
	}

	if support.operator && want.OperatorMode() == config.OperatorAuto && current.Operator != modem.OperatorAutomatic {
		actions = append(actions, radioAction{
			command: "AT+COPS=0",
			change:  fmt.Sprintf("operator %s -> %s", orUnknown(current.Operator), modem.OperatorAutomatic),
		})
	}

	return actions
}

func orUnknown(s string) string {
	if s == "" {
		return modem.RadioUnknown
	}
	return s
}

// readRadio queries both settings, reporting which of them the modem answered.
func (d *Driver) readRadio(ctx context.Context) (modem.RadioPosture, radioSupport, error) {
	posture := modem.RadioPosture{ScanMode: modem.RadioUnknown, Operator: modem.RadioUnknown}
	var support radioSupport

	scanResp, scanErr := d.exec(ctx, `AT+QCFG="nwscanmode"`)
	if scanErr != nil {
		d.drainStaleResponse(scanErr)
	} else {
		support.scanMode = true
		if value, ok := parseScanMode(scanResp); ok {
			posture.ScanMode = scanModeName(value)
		}
	}

	copsResp, copsErr := d.exec(ctx, "AT+COPS?")
	if copsErr != nil {
		d.drainStaleResponse(copsErr)
	} else {
		support.operator = true
		if value, ok := parseCOPSMode(copsResp); ok {
			posture.Operator = operatorName(value)
		}
	}

	// Neither query answering means this is not a modem whose posture we can
	// hold. Say so instead of reporting two unknowns as a successful read.
	if scanErr != nil && copsErr != nil {
		return posture, support, fmt.Errorf("read radio posture: %w", scanErr)
	}
	return posture, support, nil
}

// ReadRadioPosture reports the modem's technology restriction and operator
// selection without changing either.
func (d *Driver) ReadRadioPosture(ctx context.Context) (modem.RadioPosture, error) {
	posture, _, err := d.readRadio(ctx)
	return posture, err
}

// ApplyRadioPosture brings the modem into its configured posture. It reads
// first and writes only what is wrong, so a correctly configured modem is left
// untouched and the journal stays quiet until something actually changes.
func (d *Driver) ApplyRadioPosture(ctx context.Context) (modem.RadioPosture, error) {
	current, support, err := d.readRadio(ctx)
	if err != nil {
		return current, err
	}

	actions := planRadio(d.radio, current, support)
	if len(actions) == 0 {
		return current, nil
	}

	for _, action := range actions {
		// Each write gets a deadline of its own: dropping a technology lock or
		// releasing an operator lock makes the modem search, which outlasts the
		// ordinary command timeout by a wide margin.
		execCtx, cancel := context.WithTimeout(ctx, radioWriteTimeout)
		_, err := d.exec(execCtx, action.command)
		cancel()
		if err != nil {
			d.drainStaleResponse(err)
			return current, fmt.Errorf("%s: %w", action.command, err)
		}
	}

	// Read back rather than trust the OK. A modem that accepts the command and
	// ignores it would otherwise be reported as corrected, which is the one
	// outcome worse than reporting it as broken.
	after, afterSupport, err := d.readRadio(ctx)
	if err != nil {
		return current, err
	}
	for _, action := range actions {
		after.Changed = append(after.Changed, action.change)
	}
	if remaining := planRadio(d.radio, after, afterSupport); len(remaining) > 0 {
		return after, fmt.Errorf("modem did not take the radio posture: still %s", after)
	}
	return after, nil
}

// drainStaleResponse clears whatever a modem sends after a command this driver
// gave up waiting for, so a late OK is not read as the answer to the next
// command. A clean ERROR needs no draining: the exchange finished, the modem
// simply does not have that setting.
func (d *Driver) drainStaleResponse(err error) {
	if d.port != nil && isTimeout(err) {
		drainPort(d.port)
	}
}
