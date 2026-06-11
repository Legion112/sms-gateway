package serial

import "testing"

func TestParseCMGS(t *testing.T) {
	resp := "+CMGS: 42\nOK"
	m := cmgsRe.FindStringSubmatch(resp)
	if len(m) != 2 || m[1] != "42" {
		t.Fatalf("got %v", m)
	}
}
