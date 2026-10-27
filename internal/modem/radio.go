package modem

import "context"

// Values a RadioPosture field can take beyond the scan-mode names shared with
// config (auto, gsm, wcdma, lte).
const (
	// RadioUnknown means the modem did not answer the query, or answered with
	// something this driver does not recognise.
	RadioUnknown = "unknown"
	// OperatorAutomatic means the modem chooses and reselects its network.
	OperatorAutomatic = "automatic"
	// OperatorManual means the modem is pinned to one operator and will not
	// reselect, however badly that network is serving it.
	OperatorManual = "manual"
)

// RadioPosture is how a modem currently chooses its radio access technology and
// its serving operator.
type RadioPosture struct {
	// ScanMode is the technology restriction in force: auto, gsm, wcdma, lte,
	// or unknown.
	ScanMode string
	// Operator is how the serving network is chosen: automatic, manual, or
	// unknown.
	Operator string
	// Changed describes each setting that Apply corrected. Empty means the
	// modem was already in the configured posture, which is the normal case on
	// every connect after the first.
	Changed []string
}

// Equal reports whether two postures describe the same settings. Changed is
// ignored: it records how a posture was reached, not what it is.
func (p RadioPosture) Equal(other RadioPosture) bool {
	return p.ScanMode == other.ScanMode && p.Operator == other.Operator
}

// String renders the posture for a log line.
func (p RadioPosture) String() string {
	return "scan_mode=" + orUnknown(p.ScanMode) + " operator=" + orUnknown(p.Operator)
}

func orUnknown(s string) string {
	if s == "" {
		return RadioUnknown
	}
	return s
}

// RadioController is implemented by drivers that can read, and correct, their
// modem's radio posture. ModemManager owns that decision for the mm driver, so
// only the serial driver implements this; every caller must cope with a modem
// that does not.
type RadioController interface {
	// ReadRadioPosture reports the current posture and changes nothing.
	ReadRadioPosture(ctx context.Context) (RadioPosture, error)
	// ApplyRadioPosture brings the modem into its configured posture, writing
	// only the settings that are wrong, and reports the result.
	ApplyRadioPosture(ctx context.Context) (RadioPosture, error)
}
