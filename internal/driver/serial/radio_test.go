package serial

import (
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

func TestParseScanMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     int
		wantOK   bool
	}{
		{"auto", "+QCFG: \"nwscanmode\",0\nOK", 0, true},
		{"gsm", "+QCFG: \"nwscanmode\",1\nOK", 1, true},
		{"wcdma", "+QCFG: \"nwscanmode\",2\nOK", 2, true},
		{"lte, as found on the backup modem", "+QCFG: \"nwscanmode\",3\nOK", 3, true},
		{"space after comma", "+QCFG: \"nwscanmode\", 3\nOK", 3, true},
		{"echoed command then answer", "AT+QCFG=\"nwscanmode\"\n+QCFG: \"nwscanmode\",0\nOK", 0, true},
		{"unsupported", "ERROR", 0, false},
		{"another qcfg key", "+QCFG: \"band\",0x93,0x1a0080800c5,0x0\nOK", 0, false},
		{"empty", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseScanMode(tc.response)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("value = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseCOPSMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     int
		wantOK   bool
	}{
		{"automatic, bare", "+COPS: 0\nOK", 0, true},
		{"automatic with operator", "+COPS: 0,0,\"beeline Beeline UZ\",7\nOK", 0, true},
		{"manual, bare, as found on the backup modem", "+COPS: 1\nOK", 1, true},
		{"manual with operator", "+COPS: 1,0,\"MTS\",7\nOK", 1, true},
		{"deregistered", "+COPS: 2\nOK", 2, true},
		{"error", "+CME ERROR: 30", 0, false},
		{"empty", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseCOPSMode(tc.response)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("value = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestScanModeNamesRoundTrip(t *testing.T) {
	for _, name := range []string{config.ScanModeAuto, config.ScanModeGSM, config.ScanModeWCDMA, config.ScanModeLTE} {
		value, ok := scanModeValue(name)
		if !ok {
			t.Fatalf("scanModeValue(%q) not recognised", name)
		}
		if got := scanModeName(value); got != name {
			t.Fatalf("scanModeName(scanModeValue(%q)) = %q", name, got)
		}
	}
	if _, ok := scanModeValue(config.ScanModeKeep); ok {
		t.Error("keep is not a value to write to the modem")
	}
	if got := scanModeName(9); got != modem.RadioUnknown {
		t.Errorf("scanModeName(9) = %q, want %q", got, modem.RadioUnknown)
	}
}

func TestOperatorName(t *testing.T) {
	if got := operatorName(copsAutomatic); got != modem.OperatorAutomatic {
		t.Errorf("operatorName(0) = %q", got)
	}
	if got := operatorName(copsManual); got != modem.OperatorManual {
		t.Errorf("operatorName(1) = %q", got)
	}
	if got := operatorName(2); got != modem.RadioUnknown {
		t.Errorf("operatorName(2) = %q, want %q", got, modem.RadioUnknown)
	}
}

func TestPlanRadio(t *testing.T) {
	automatic := modem.RadioPosture{ScanMode: config.ScanModeAuto, Operator: modem.OperatorAutomatic}

	for _, tc := range []struct {
		name     string
		want     config.RadioConfig
		current  modem.RadioPosture
		wantCmds []string
	}{
		{
			name:     "already correct writes nothing",
			current:  automatic,
			wantCmds: nil,
		},
		{
			name:     "the production fault: lte lock and a manual operator",
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorManual},
			wantCmds: []string{`AT+QCFG="nwscanmode",0,1`, "AT+COPS=0"},
		},
		{
			name:     "lte lock alone",
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorAutomatic},
			wantCmds: []string{`AT+QCFG="nwscanmode",0,1`},
		},
		{
			name:     "manual operator alone",
			current:  modem.RadioPosture{ScanMode: config.ScanModeAuto, Operator: modem.OperatorManual},
			wantCmds: []string{"AT+COPS=0"},
		},
		{
			name:     "an unreadable setting is treated as wrong",
			current:  modem.RadioPosture{ScanMode: modem.RadioUnknown, Operator: modem.RadioUnknown},
			wantCmds: []string{`AT+QCFG="nwscanmode",0,1`, "AT+COPS=0"},
		},
		{
			name:     "keep both leaves even a broken posture alone",
			want:     config.RadioConfig{ScanMode: config.ScanModeKeep, Operator: config.OperatorKeep},
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorManual},
			wantCmds: nil,
		},
		{
			name:     "keep scan mode still frees a pinned operator",
			want:     config.RadioConfig{ScanMode: config.ScanModeKeep},
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorManual},
			wantCmds: []string{"AT+COPS=0"},
		},
		{
			name:     "keep operator still clears a technology lock",
			want:     config.RadioConfig{Operator: config.OperatorKeep},
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorManual},
			wantCmds: []string{`AT+QCFG="nwscanmode",0,1`},
		},
		{
			name:     "a deliberate gsm lock is applied, not just auto",
			want:     config.RadioConfig{ScanMode: config.ScanModeGSM},
			current:  automatic,
			wantCmds: []string{`AT+QCFG="nwscanmode",1,1`},
		},
		{
			name:     "a modem already on its deliberate lte lock is left alone",
			want:     config.RadioConfig{ScanMode: config.ScanModeLTE},
			current:  modem.RadioPosture{ScanMode: config.ScanModeLTE, Operator: modem.OperatorAutomatic},
			wantCmds: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := planRadio(tc.want, tc.current, radioSupport{scanMode: true, operator: true})
			var got []string
			for _, a := range actions {
				got = append(got, a.command)
			}
			if len(got) != len(tc.wantCmds) {
				t.Fatalf("commands = %v, want %v", got, tc.wantCmds)
			}
			for i := range got {
				if got[i] != tc.wantCmds[i] {
					t.Fatalf("command %d = %q, want %q", i, got[i], tc.wantCmds[i])
				}
			}
		})
	}
}

// Every planned action must carry a description, because that description is
// the only record in the journal of a modem having been found misconfigured.
func TestPlanRadioDescribesEveryChange(t *testing.T) {
	actions := planRadio(config.RadioConfig{}, modem.RadioPosture{
		ScanMode: config.ScanModeLTE,
		Operator: modem.OperatorManual,
	}, radioSupport{scanMode: true, operator: true})
	if len(actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(actions))
	}
	if !strings.Contains(actions[0].change, "lte") || !strings.Contains(actions[0].change, "auto") {
		t.Errorf("scan mode change %q should name both the old and new value", actions[0].change)
	}
	if !strings.Contains(actions[1].change, modem.OperatorManual) || !strings.Contains(actions[1].change, modem.OperatorAutomatic) {
		t.Errorf("operator change %q should name both the old and new value", actions[1].change)
	}
}

// Technology first, then the operator: clearing a lock can drop registration,
// and the operator reselect should happen after the radio is free to use any
// technology it likes.
func TestPlanRadioOrdersTechnologyBeforeOperator(t *testing.T) {
	actions := planRadio(config.RadioConfig{}, modem.RadioPosture{
		ScanMode: config.ScanModeLTE,
		Operator: modem.OperatorManual,
	}, radioSupport{scanMode: true, operator: true})
	if len(actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(actions))
	}
	if !strings.Contains(actions[0].command, "nwscanmode") {
		t.Errorf("first command = %q, want the scan mode change", actions[0].command)
	}
	if actions[1].command != "AT+COPS=0" {
		t.Errorf("second command = %q, want AT+COPS=0", actions[1].command)
	}
}

func TestRadioPostureString(t *testing.T) {
	p := modem.RadioPosture{ScanMode: config.ScanModeAuto, Operator: modem.OperatorAutomatic}
	if got := p.String(); got != "scan_mode=auto operator=automatic" {
		t.Fatalf("String() = %q", got)
	}
	var empty modem.RadioPosture
	if got := empty.String(); got != "scan_mode=unknown operator=unknown" {
		t.Fatalf("empty String() = %q", got)
	}
}

// A setting the modem never answered must not be written. Without this, a modem
// without Quectel's QCFG would report a failed correction on every connect for
// the rest of its life.
func TestPlanRadioSkipsUnsupportedSettings(t *testing.T) {
	broken := modem.RadioPosture{ScanMode: modem.RadioUnknown, Operator: modem.OperatorManual}

	for _, tc := range []struct {
		name     string
		support  radioSupport
		wantCmds []string
	}{
		{"both supported", radioSupport{scanMode: true, operator: true}, []string{`AT+QCFG="nwscanmode",0,1`, "AT+COPS=0"}},
		{"no qcfg", radioSupport{operator: true}, []string{"AT+COPS=0"}},
		{"no cops", radioSupport{scanMode: true}, []string{`AT+QCFG="nwscanmode",0,1`}},
		{"neither", radioSupport{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := planRadio(config.RadioConfig{}, broken, tc.support)
			if len(actions) != len(tc.wantCmds) {
				t.Fatalf("got %d actions, want %d", len(actions), len(tc.wantCmds))
			}
			for i, a := range actions {
				if a.command != tc.wantCmds[i] {
					t.Fatalf("command %d = %q, want %q", i, a.command, tc.wantCmds[i])
				}
			}
		})
	}
}
