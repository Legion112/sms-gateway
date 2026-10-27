package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
)

func TestRadioConfigDefaultsToAutomatic(t *testing.T) {
	var r config.RadioConfig
	if got := r.Mode(); got != config.ScanModeAuto {
		t.Fatalf("Mode() = %q, want %q", got, config.ScanModeAuto)
	}
	if got := r.OperatorMode(); got != config.OperatorAuto {
		t.Fatalf("OperatorMode() = %q, want %q", got, config.OperatorAuto)
	}
	if !r.Enforces() {
		t.Fatal("the zero value must enforce; a modem nobody configured is the one most likely to be wrong")
	}
}

func TestRadioConfigResolvesExplicitValues(t *testing.T) {
	for _, tc := range []struct {
		name         string
		radio        config.RadioConfig
		wantMode     string
		wantOperator string
		wantEnforces bool
	}{
		{"lte lock", config.RadioConfig{ScanMode: "lte"}, "lte", "auto", true},
		{"gsm lock", config.RadioConfig{ScanMode: "gsm"}, "gsm", "auto", true},
		{"wcdma lock", config.RadioConfig{ScanMode: "wcdma"}, "wcdma", "auto", true},
		{"keep scan only", config.RadioConfig{ScanMode: "keep"}, "keep", "auto", true},
		{"keep operator only", config.RadioConfig{Operator: "keep"}, "auto", "keep", true},
		{"keep both", config.RadioConfig{ScanMode: "keep", Operator: "keep"}, "keep", "keep", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.radio.Mode(); got != tc.wantMode {
				t.Errorf("Mode() = %q, want %q", got, tc.wantMode)
			}
			if got := tc.radio.OperatorMode(); got != tc.wantOperator {
				t.Errorf("OperatorMode() = %q, want %q", got, tc.wantOperator)
			}
			if got := tc.radio.Enforces(); got != tc.wantEnforces {
				t.Errorf("Enforces() = %v, want %v", got, tc.wantEnforces)
			}
		})
	}
}

func TestLoadRadioPosture(t *testing.T) {
	for _, tc := range []struct {
		name         string
		block        string
		wantErr      string
		wantMode     string
		wantOperator string
	}{
		{
			name:         "absent defaults to automatic",
			block:        "",
			wantMode:     config.ScanModeAuto,
			wantOperator: config.OperatorAuto,
		},
		{
			name:         "explicit lte lock is allowed",
			block:        "    radio:\n      scan_mode: lte\n",
			wantMode:     config.ScanModeLTE,
			wantOperator: config.OperatorAuto,
		},
		{
			name:         "keep both leaves the modem alone",
			block:        "    radio:\n      scan_mode: keep\n      operator: keep\n",
			wantMode:     config.ScanModeKeep,
			wantOperator: config.OperatorKeep,
		},
		{
			name:    "unknown scan mode is rejected at load",
			block:   "    radio:\n      scan_mode: 5g\n",
			wantErr: "radio.scan_mode",
		},
		{
			name:    "unknown operator mode is rejected at load",
			block:   "    radio:\n      operator: manual\n",
			wantErr: "radio.operator",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := `driver: serial
modems:
  ec25-main:
    driver: serial
    serial:
      device: /dev/ttyUSB3
` + tc.block
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, err := config.Load(config.Overrides{ConfigPath: path})
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q, got none", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			radio := cfg.Modems["ec25-main"].Radio
			if got := radio.Mode(); got != tc.wantMode {
				t.Errorf("scan mode = %q, want %q", got, tc.wantMode)
			}
			if got := radio.OperatorMode(); got != tc.wantOperator {
				t.Errorf("operator mode = %q, want %q", got, tc.wantOperator)
			}
		})
	}
}

// A per-modem posture must not leak onto the other modem: the whole point of
// the setting is that one SIM can be locked on purpose while the other is not.
func TestLoadRadioPostureIsPerModem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `driver: serial
modems:
  ec25-main:
    driver: serial
    serial:
      device: /dev/ttyUSB3
  ec25-backup:
    driver: serial
    serial:
      device: /dev/ttyUSB6
    radio:
      scan_mode: keep
      operator: keep
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.Overrides{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Modems["ec25-main"].Radio.Mode(); got != config.ScanModeAuto {
		t.Errorf("ec25-main scan mode = %q, want %q", got, config.ScanModeAuto)
	}
	if got := cfg.Modems["ec25-backup"].Radio.Mode(); got != config.ScanModeKeep {
		t.Errorf("ec25-backup scan mode = %q, want %q", got, config.ScanModeKeep)
	}
	if cfg.Modems["ec25-main"].Radio.Enforces() == cfg.Modems["ec25-backup"].Radio.Enforces() {
		t.Error("one modem enforces and the other does not; both reported the same")
	}
}
