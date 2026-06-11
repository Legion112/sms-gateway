//go:build linux

package mm

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/modem"
)

// SendMessage creates and sends an SMS via ModemManager.
func (d *Driver) SendMessage(ctx context.Context, number, text string) (modem.SendResult, error) {
	if number == "" {
		return modem.SendResult{}, fmt.Errorf("number is required")
	}
	if text == "" {
		return modem.SendResult{}, fmt.Errorf("text is required")
	}

	path, err := d.createSMS(ctx, number, text)
	if err != nil {
		return modem.SendResult{}, err
	}

	if err := d.invokeSMS(ctx, path, "Send"); err != nil {
		return modem.SendResult{}, fmt.Errorf("send: %w", err)
	}

	state, err := d.waitSMSState(ctx, path, 5)
	if err != nil {
		msg, readErr := d.readMessage(ctx, path)
		if readErr == nil {
			return modem.SendResult{ID: string(path), To: number, State: msg.State, Text: text}, err
		}
		return modem.SendResult{}, err
	}

	return modem.SendResult{
		ID:    string(path),
		To:    number,
		State: state,
		Text:  text,
	}, nil
}

func (d *Driver) createSMS(ctx context.Context, number, text string) (dbus.ObjectPath, error) {
	props := map[string]dbus.Variant{
		"number": dbus.MakeVariant(number),
		"text":   dbus.MakeVariant(text),
	}

	type result struct {
		path dbus.ObjectPath
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		obj := d.conn.Object(mmBusName, d.modemPath)
		call := obj.Call(messagingInterface+".Create", 0, props)
		if call.Err != nil {
			ch <- result{err: call.Err}
			return
		}
		var path dbus.ObjectPath
		if err := call.Store(&path); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{path: path}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(d.timeout):
		return "", fmt.Errorf("timeout creating SMS")
	case res := <-ch:
		return res.path, res.err
	}
}

func (d *Driver) invokeSMS(ctx context.Context, path dbus.ObjectPath, method string) error {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		obj := d.conn.Object(mmBusName, path)
		call := obj.Call(smsObjectInterface+"."+method, 0)
		ch <- result{err: call.Err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d.timeout):
		return fmt.Errorf("timeout calling %s", method)
	case res := <-ch:
		return res.err
	}
}

func (d *Driver) waitSMSState(ctx context.Context, path dbus.ObjectPath, want uint32) (string, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var last string
	for {
		select {
		case <-ctx.Done():
			if last != "" {
				return last, fmt.Errorf("timeout waiting for state %s (last: %s)", smsStateName(want), last)
			}
			return "", ctx.Err()
		case <-ticker.C:
			props, err := d.getObjectProperties(ctx, path, smsObjectInterface, []string{"State"})
			if err != nil {
				return "", err
			}
			last = smsStateName(variantUint32(props["State"]))
			if variantUint32(props["State"]) == want {
				return last, nil
			}
		}
	}
}
