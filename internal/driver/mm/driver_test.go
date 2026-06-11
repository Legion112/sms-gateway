//go:build linux

package mm

import (
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/config"
)

func TestResolveModemPathForTest(t *testing.T) {
	paths := []dbus.ObjectPath{
		"/org/freedesktop/ModemManager1/Modem/1",
	}
	p, err := ResolveModemPathForTest(config.MMConfig{ModemIndex: 0}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if p != paths[0] {
		t.Fatalf("path = %s, want %s", p, paths[0])
	}

	p, err = ResolveModemPathForTest(config.MMConfig{ModemPath: "/org/freedesktop/ModemManager1/Modem/2"}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if p != "/org/freedesktop/ModemManager1/Modem/2" {
		t.Fatalf("path = %s", p)
	}
}

func TestParseManagedObjects(t *testing.T) {
	managed := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		"/org/freedesktop/ModemManager1/Modem/1": {
			modemInterface: {},
		},
		"/org/freedesktop/ModemManager1/Modem/0": {
			modemInterface: {},
		},
		"/org/freedesktop/ModemManager1/SIM/0": {
			"org.freedesktop.ModemManager1.Sim": {},
		},
	}

	paths := ParseManagedObjects(managed)
	if len(paths) != 2 {
		t.Fatalf("got %d modems, want 2", len(paths))
	}
	if paths[0] != "/org/freedesktop/ModemManager1/Modem/0" {
		t.Fatalf("first path = %s", paths[0])
	}
	if paths[1] != "/org/freedesktop/ModemManager1/Modem/1" {
		t.Fatalf("second path = %s", paths[1])
	}
}

func TestSelectModemPath(t *testing.T) {
	paths := []dbus.ObjectPath{
		"/org/freedesktop/ModemManager1/Modem/0",
		"/org/freedesktop/ModemManager1/Modem/1",
	}

	p, err := SelectModemPath(paths, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p != paths[1] {
		t.Fatalf("path = %s", p)
	}

	_, err = SelectModemPath(paths, 2)
	if err == nil {
		t.Fatal("expected error for out of range index")
	}

	_, err = SelectModemPath(nil, 0)
	if err == nil {
		t.Fatal("expected error for empty list")
	}
}
