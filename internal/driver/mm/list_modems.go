//go:build linux

package mm

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// ModemInfo describes one modem discovered via ModemManager.
type ModemInfo struct {
	Index        int
	Path         string
	Manufacturer string
	Model        string
}

// ListModems returns modems present on the system D-Bus, sorted by path.
func ListModems(ctx context.Context, timeout time.Duration) ([]ModemInfo, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	defer conn.Close()

	paths, err := fetchModemPaths(conn, timeout)
	if err != nil {
		return nil, err
	}

	modems := make([]ModemInfo, 0, len(paths))
	for i, path := range paths {
		info := ModemInfo{
			Index: i,
			Path:  string(path),
		}
		props, err := getObjectProperties(ctx, conn, path, modemInterface, []string{
			"Manufacturer", "Model",
		})
		if err == nil {
			if v, ok := props["Manufacturer"]; ok {
				info.Manufacturer = variantString(v)
			}
			if v, ok := props["Model"]; ok {
				info.Model = variantString(v)
			}
		}
		modems = append(modems, info)
	}
	return modems, nil
}

func getObjectProperties(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, iface string, keys []string) (map[string]dbus.Variant, error) {
	d := &Driver{conn: conn, modemPath: path, timeout: 5 * time.Second}
	return d.getObjectProperties(ctx, path, iface, keys)
}
