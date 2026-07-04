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
	got = parseCREG("+CREG: 2,2\nOK")
	if got != "searching" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCEREG(t *testing.T) {
	got := parseCEREG("+CEREG: 2,5\nOK")
	if got != "registered (roaming)" {
		t.Fatalf("got %q", got)
	}
}

func TestBestNetworkState(t *testing.T) {
	got := bestNetworkState("not registered", "searching", "registered (home)")
	if got != "registered (home)" {
		t.Fatalf("got %q", got)
	}
	got = bestNetworkState("not registered", "searching", "not registered")
	if got != "searching" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCNUM(t *testing.T) {
	got := parseCNUM(`+CNUM: "","+79001234567",145` + "\nOK")
	if got != "+79001234567" {
		t.Fatalf("got %q", got)
	}
	if parseCNUM("OK") != "" {
		t.Fatal("expected empty")
	}
}

func TestParseCUSDAndExtractPhone(t *testing.T) {
	resp := `+CUSD: 0,"Ваш номер +79001234567",15` + "\nOK"
	if parseCUSD(resp) == "" {
		t.Fatal("expected cusd text")
	}
	if extractPhoneFromText(parseCUSD(resp)) != "+79001234567" {
		t.Fatalf("got %q", extractPhoneFromText(parseCUSD(resp)))
	}
	if extractPhoneFromText("номер 89001234567") != "+79001234567" {
		t.Fatalf("8-prefix normalize failed")
	}
	if extractPhoneFromText("Raqamingiz: 901234567") != "+998901234567" {
		t.Fatalf("998-prefix normalize failed, got %q", extractPhoneFromText("Raqamingiz: 901234567"))
	}
}

func TestParseCSQ(t *testing.T) {
	rssi, ok := parseCSQ("+CSQ: 5,99\nOK")
	if !ok || rssi != 5 {
		t.Fatalf("rssi=%d ok=%v", rssi, ok)
	}
	if csqLabel(99) != "no signal" {
		t.Fatalf("csq label")
	}
}

func TestSMSReadyLTE(t *testing.T) {
	if !smsReady("ready", "registered (roaming)") {
		t.Fatal("expected ready on LTE roaming via CEREG")
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
