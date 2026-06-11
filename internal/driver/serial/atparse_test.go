package serial

import "testing"

func TestParseCMEError(t *testing.T) {
	msg, ok := parseCMEError("+CME ERROR: 10")
	if !ok || msg != "+CME ERROR: 10" {
		t.Fatalf("got %q ok=%v", msg, ok)
	}
}

func TestParseCPIN(t *testing.T) {
	tests := []struct {
		resp string
		want string
	}{
		{"+CPIN: READY\nOK", "READY"},
		{"+CME ERROR: 10\n", "missing"},
		{"ERROR", "unknown"},
	}
	for _, tt := range tests {
		got := parseCPIN(tt.resp)
		if got != tt.want {
			t.Fatalf("parseCPIN() = %q, want %q", got, tt.want)
		}
	}
}

func TestParseCREG(t *testing.T) {
	got := parseCREG("+CREG: 0,1\nOK")
	if got != "registered (home)" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCPMS(t *testing.T) {
	store, used, total, ok := parseCPMS(`+CPMS: "SM",2,50,"SM",2,50`)
	if !ok || store != "SM" || used != 2 || total != 50 {
		t.Fatalf("got store=%q used=%d total=%d ok=%v", store, used, total, ok)
	}
}

func TestSMSReady(t *testing.T) {
	if !smsReady("ready", "registered (home)") {
		t.Fatal("expected ready")
	}
	if smsReady("missing", "registered (home)") {
		t.Fatal("expected not ready without sim")
	}
}
