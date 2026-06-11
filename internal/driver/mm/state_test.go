package mm

import "testing"

func TestModemStateName(t *testing.T) {
	if modemStateName(7) != "registered" {
		t.Fatalf("got %q", modemStateName(7))
	}
	if modemStateName(-1) != "failed" {
		t.Fatalf("got %q", modemStateName(-1))
	}
}

func TestFailedReasonName(t *testing.T) {
	if failedReasonName(2) != "sim-missing" {
		t.Fatalf("got %q", failedReasonName(2))
	}
}

func TestAccessTechRegistered(t *testing.T) {
	if !accessTechRegistered(7) {
		t.Fatal("expected registered")
	}
	if accessTechRegistered(5) {
		t.Fatal("enabled is not registered")
	}
}
