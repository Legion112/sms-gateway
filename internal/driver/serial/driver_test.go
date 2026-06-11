package serial

import "testing"

func TestParseResultCode(t *testing.T) {
	tests := []struct {
		line string
		want resultCode
	}{
		{"OK", resultOK},
		{"ok", resultOK},
		{" OK ", resultOK},
		{"ERROR", resultError},
		{"error", resultError},
		{"+CME ERROR: 10", resultNone},
		{"AT", resultNone},
		{"", resultNone},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := parseResultCode(tt.line)
			if got != tt.want {
				t.Fatalf("parseResultCode(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestIsPortBusy(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"busy", errString("Serial port busy"), true},
		{"permission", errString("Permission denied"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPortBusy(tt.err)
			if got != tt.want {
				t.Fatalf("IsPortBusy() = %v, want %v", got, tt.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }
