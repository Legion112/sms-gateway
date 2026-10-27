package config

import "fmt"

// Radio access technologies a modem may be restricted to, as named in config.
const (
	// ScanModeAuto lets the modem register on whatever technology is available.
	ScanModeAuto = "auto"
	// ScanModeGSM, ScanModeWCDMA and ScanModeLTE lock the modem to a single
	// technology. See RadioConfig for why that is rarely what an SMS gateway
	// wants.
	ScanModeGSM   = "gsm"
	ScanModeWCDMA = "wcdma"
	ScanModeLTE   = "lte"
	// ScanModeKeep leaves the modem on whatever it was last set to.
	ScanModeKeep = "keep"
)

// Operator selection modes.
const (
	// OperatorAuto lets the modem choose and reselect its network.
	OperatorAuto = "auto"
	// OperatorKeep leaves a deliberately pinned operator in place.
	OperatorKeep = "keep"
)

// RadioConfig is the radio posture the gateway holds a modem in: which radio
// access technologies it may use, and whether it picks its own operator.
//
// Both default to automatic, and that default is doing real work. A modem
// locked to LTE receives SMS only where the visited network offers SGs/CSFB or
// a working IMS registration. On a roaming SIM neither is guaranteed, and where
// they are missing the modem still registers, still answers every AT command
// and still polls clean — it simply never receives a message. Nothing else in
// this daemon can see that: the watch loop is healthy, the stall timer never
// fires, the heartbeat reports the modem up. An operator lock fails the same
// quiet way, by stopping the modem reselecting when the network it is pinned to
// degrades.
//
// So the gateway asserts the safe posture at connect instead of trusting
// whatever the modem happens to hold in NV. Use "keep" to leave a modem that
// was locked on purpose alone.
type RadioConfig struct {
	ScanMode string `yaml:"scan_mode"`
	Operator string `yaml:"operator"`
}

// Mode resolves scan_mode, defaulting to auto.
func (r RadioConfig) Mode() string {
	if r.ScanMode == "" {
		return ScanModeAuto
	}
	return r.ScanMode
}

// OperatorMode resolves operator selection, defaulting to auto.
func (r RadioConfig) OperatorMode() string {
	if r.Operator == "" {
		return OperatorAuto
	}
	return r.Operator
}

// Enforces reports whether this posture asks the gateway to change anything at
// all; a modem set to keep both is only ever read.
func (r RadioConfig) Enforces() bool {
	return r.Mode() != ScanModeKeep || r.OperatorMode() != OperatorKeep
}

func validateRadio(label string, r RadioConfig) error {
	switch r.Mode() {
	case ScanModeAuto, ScanModeGSM, ScanModeWCDMA, ScanModeLTE, ScanModeKeep:
	default:
		return fmt.Errorf("%s.radio.scan_mode: unknown value %q (want auto, gsm, wcdma, lte or keep)", label, r.ScanMode)
	}
	switch r.OperatorMode() {
	case OperatorAuto, OperatorKeep:
	default:
		return fmt.Errorf("%s.radio.operator: unknown value %q (want auto or keep)", label, r.Operator)
	}
	return nil
}
