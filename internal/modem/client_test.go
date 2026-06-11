package modem

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
