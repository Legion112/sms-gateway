//go:build linux

package mm

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/modem"
)

// ModemPath returns the D-Bus object path for this modem.
func (d *Driver) ModemPath() dbus.ObjectPath {
	return d.modemPath
}

// WatchIncoming listens for inbound SMS via ModemManager Messaging.Added.
func (d *Driver) WatchIncoming(ctx context.Context, handler modem.IncomingHandler) error {
	match := fmt.Sprintf(
		"type='signal',interface='%s',member='Added',path='%s'",
		messagingInterface, d.modemPath,
	)
	if err := d.conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match).Err; err != nil {
		return fmt.Errorf("dbus add match: %w", err)
	}
	defer func() {
		_ = d.conn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, match).Err
	}()

	ch := make(chan *dbus.Signal, 8)
	d.conn.Signal(ch)
	defer d.conn.RemoveSignal(ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig := <-ch:
			if sig == nil {
				continue
			}
			if sig.Name != messagingInterface+".Added" || sig.Path != d.modemPath {
				continue
			}
			path, received, ok := parseAddedSignal(sig)
			if !ok || !received {
				continue
			}
			msg, err := d.waitInboundMessage(ctx, path)
			if err != nil {
				if d.verbose {
					d.log("MM: wait message %s: %v", path, err)
				}
				continue
			}
			if err := handler(ctx, msg); err != nil {
				return err
			}
		}
	}
}

func parseAddedSignal(sig *dbus.Signal) (dbus.ObjectPath, bool, bool) {
	if len(sig.Body) < 2 {
		return "", false, false
	}
	var path dbus.ObjectPath
	switch v := sig.Body[0].(type) {
	case dbus.ObjectPath:
		path = v
	case string:
		path = dbus.ObjectPath(v)
	default:
		return "", false, false
	}
	received, ok := sig.Body[1].(bool)
	if !ok {
		return "", false, false
	}
	return path, received, true
}

func (d *Driver) waitInboundMessage(ctx context.Context, path dbus.ObjectPath) (modem.Message, error) {
	deadline := time.Now().Add(d.timeout * 4)
	for {
		if err := ctx.Err(); err != nil {
			return modem.Message{}, err
		}
		if time.Now().After(deadline) {
			return modem.Message{}, fmt.Errorf("timeout waiting for %s", path)
		}
		readCtx, cancel := context.WithTimeout(ctx, d.timeout)
		msg, err := d.readMessage(readCtx, path)
		cancel()
		if err != nil {
			return modem.Message{}, err
		}
		if modem.IsInbound(msg.State) {
			return msg, nil
		}
		select {
		case <-ctx.Done():
			return modem.Message{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
