//go:build linux

package mm

import "testing"

func TestSmsStateName(t *testing.T) {
	if smsStateName(3) != "received" {
		t.Fatalf("got %q", smsStateName(3))
	}
}

func TestSmsStorageName(t *testing.T) {
	if smsStorageName("me") != "modem" {
		t.Fatalf("got %q", smsStorageName("me"))
	}
	if smsStorageCode(2) != "modem" {
		t.Fatalf("got %q", smsStorageCode(2))
	}
}
