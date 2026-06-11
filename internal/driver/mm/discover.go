//go:build linux

package mm

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/config"
)

const objectManagerInterface = "org.freedesktop.DBus.ObjectManager"

// resolveModem selects a modem path by explicit config, D-Bus discovery, or probe fallback.
// modem_index is a logical index into the sorted list of present modems (0 = first), not the D-Bus path suffix.
func resolveModem(conn *dbus.Conn, cfg config.MMConfig, timeout time.Duration) (path dbus.ObjectPath, index int, err error) {
	if cfg.ModemPath != "" {
		path = dbus.ObjectPath(cfg.ModemPath)
		if err := modemExists(conn, path, timeout); err != nil {
			return "", 0, fmt.Errorf("modem at %s: %w", path, err)
		}
		return path, cfg.ModemIndex, nil
	}

	paths, err := fetchModemPaths(conn, timeout)
	if err != nil {
		return "", 0, err
	}
	path, err = SelectModemPath(paths, cfg.ModemIndex)
	if err != nil {
		return "", 0, err
	}
	return path, cfg.ModemIndex, nil
}

func fetchModemPaths(conn *dbus.Conn, timeout time.Duration) ([]dbus.ObjectPath, error) {
	paths, err := fetchModemPathsManagedObjects(conn, timeout)
	if err == nil && len(paths) > 0 {
		return paths, nil
	}
	paths, probeErr := probeModemPaths(conn, timeout)
	if len(paths) > 0 {
		return paths, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list modems: %w", err)
	}
	if probeErr != nil {
		return nil, probeErr
	}
	return nil, fmt.Errorf("no modems found")
}

func fetchModemPathsManagedObjects(conn *dbus.Conn, timeout time.Duration) ([]dbus.ObjectPath, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type result struct {
		paths []dbus.ObjectPath
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		obj := conn.Object(mmBusName, dbus.ObjectPath(mmObjectPath))
		call := obj.Call(objectManagerInterface+".GetManagedObjects", 0)
		if call.Err != nil {
			ch <- result{err: call.Err}
			return
		}
		var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
		if err := call.Store(&managed); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{paths: listModemPaths(managed)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout listing modems")
	case res := <-ch:
		return res.paths, res.err
	}
}

func probeModemPaths(conn *dbus.Conn, timeout time.Duration) ([]dbus.ObjectPath, error) {
	var paths []dbus.ObjectPath
	for i := 0; i < 10; i++ {
		path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/ModemManager1/Modem/%d", i))
		if modemExists(conn, path, timeout) == nil {
			paths = append(paths, path)
		}
	}
	sortObjectPaths(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no modems found")
	}
	return paths, nil
}

func modemExists(conn *dbus.Conn, path dbus.ObjectPath, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		obj := conn.Object(mmBusName, path)
		call := obj.Call("org.freedesktop.DBus.Properties.Get", 0, modemInterface, "Manufacturer")
		ch <- result{err: call.Err}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout")
	case res := <-ch:
		return res.err
	}
}

// ResolveModemPathForTest returns the path for config using a provided modem list (tests only).
func ResolveModemPathForTest(cfg config.MMConfig, paths []dbus.ObjectPath) (dbus.ObjectPath, error) {
	if cfg.ModemPath != "" {
		return dbus.ObjectPath(cfg.ModemPath), nil
	}
	return SelectModemPath(paths, cfg.ModemIndex)
}
