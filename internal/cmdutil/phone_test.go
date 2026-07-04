package cmdutil

import "testing"

func TestIsRoaming(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"registered (roaming)", true},
		{"registered (home)", false},
		{"searching", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isRoaming(tt.state); got != tt.want {
			t.Fatalf("isRoaming(%q) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestShouldQueryPhoneUSSD(t *testing.T) {
	if shouldQueryPhoneUSSD("*148#", "registered (roaming)") {
		t.Fatal("expected USSD skip while roaming")
	}
	if !shouldQueryPhoneUSSD("*148#", "registered (home)") {
		t.Fatal("expected USSD on home network")
	}
	if shouldQueryPhoneUSSD("", "registered (home)") {
		t.Fatal("expected no USSD without code")
	}
}
