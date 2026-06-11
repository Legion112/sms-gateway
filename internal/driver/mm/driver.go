//go:build linux

package mm

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

const (
	mmBusName      = "org.freedesktop.ModemManager1"
	mmObjectPath   = "/org/freedesktop/ModemManager1"
	modemInterface = "org.freedesktop.ModemManager1.Modem"
)

// Driver talks to a modem through ModemManager on system D-Bus.
type Driver struct {
	conn       *dbus.Conn
	modemIndex int
	modemPath  dbus.ObjectPath
	timeout    time.Duration
	verbose    bool
	log        func(format string, args ...any)
}

// New connects to ModemManager and selects a modem by path or index.
func New(cfg config.MMConfig, verbose bool) (modem.Modem, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	logFn := func(string, ...any) {}
	if verbose {
		logFn = log.Printf
	}

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system D-Bus: %w", err)
	}

	path, modemIndex, err := resolveModem(conn, cfg, cfg.Timeout)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if verbose {
		logFn("MM: using modem path %s (index %d)", path, modemIndex)
	}

	d := &Driver{
		conn:       conn,
		modemIndex: modemIndex,
		modemPath:  path,
		timeout:    cfg.Timeout,
		verbose:    verbose,
		log:        logFn,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if _, err := d.readModemProperties(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("modem at %s: %w", path, err)
	}

	return d, nil
}

// Ping verifies the modem is reachable via ModemManager.
func (d *Driver) Ping(ctx context.Context) (modem.PingResult, error) {
	props, err := d.readModemProperties(ctx)
	if err != nil {
		return modem.PingResult{}, err
	}

	manufacturer := variantString(props["Manufacturer"])
	model := variantString(props["Model"])
	equipmentID := variantString(props["EquipmentIdentifier"])
	state := variantUint32(props["State"])

	detail := strings.TrimSpace(strings.Join([]string{manufacturer, model}, " "))
	if detail == "" {
		detail = string(d.modemPath)
	}
	if equipmentID != "" {
		detail = fmt.Sprintf("%s (IMEI %s, state %d)", detail, equipmentID, state)
	}

	if d.verbose {
		d.log("MM: ping ok %s", detail)
	}

	return modem.PingResult{
		Driver:     modem.DriverMM,
		Detail:     detail,
		Device:     string(d.modemPath),
		ModemIndex: d.modemIndex,
	}, nil
}

// Close releases the D-Bus connection.
func (d *Driver) Close() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

func (d *Driver) readModemProperties(ctx context.Context) (map[string]dbus.Variant, error) {
	return d.getObjectProperties(ctx, d.modemPath, modemInterface, []string{
		"Manufacturer", "Model", "State", "EquipmentIdentifier",
	})
}

// listModemPaths extracts modem object paths from GetManagedObjects output, sorted.
func listModemPaths(managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant) []dbus.ObjectPath {
	var paths []dbus.ObjectPath
	for path, ifaces := range managed {
		if _, ok := ifaces[modemInterface]; ok {
			paths = append(paths, path)
		}
	}
	sortObjectPaths(paths)
	return paths
}

func sortObjectPaths(paths []dbus.ObjectPath) {
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if string(paths[i]) > string(paths[j]) {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
}

func variantString(v dbus.Variant) string {
	if s, ok := v.Value().(string); ok {
		return s
	}
	return fmt.Sprint(v.Value())
}

func variantUint32(v dbus.Variant) uint32 {
	if n, ok := v.Value().(uint32); ok {
		return n
	}
	return 0
}

// ParseManagedObjects is exported for unit tests.
func ParseManagedObjects(managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant) []dbus.ObjectPath {
	return listModemPaths(managed)
}

// SelectModemPath returns the path at logical index or an error.
func SelectModemPath(paths []dbus.ObjectPath, index int) (dbus.ObjectPath, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no modems found")
	}
	if index < 0 || index >= len(paths) {
		return "", fmt.Errorf("modem index %d not found (found %d modems: %v)", index, len(paths), paths)
	}
	return paths[index], nil
}
