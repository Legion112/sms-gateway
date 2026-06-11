//go:build linux

package mm

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/modem"
)

const smsObjectInterface = "org.freedesktop.ModemManager1.Sms"

// ListMessages returns all SMS messages known to ModemManager.
func (d *Driver) ListMessages(ctx context.Context) ([]modem.Message, error) {
	paths, err := d.listMessages(ctx)
	if err != nil {
		return nil, err
	}

	messages := make([]modem.Message, 0, len(paths))
	for _, path := range paths {
		msg, err := d.readMessage(ctx, path)
		if err != nil {
			return messages, fmt.Errorf("read %s: %w", path, err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (d *Driver) readMessage(ctx context.Context, path dbus.ObjectPath) (modem.Message, error) {
	props, err := d.getObjectProperties(ctx, path, smsObjectInterface, []string{
		"Number", "Text", "State", "Timestamp", "Storage", "SMSC",
	})
	if err != nil {
		return modem.Message{}, err
	}

	return modem.Message{
		ID:        string(path),
		From:      variantString(props["Number"]),
		Text:      variantString(props["Text"]),
		State:     smsStateName(variantUint32(props["State"])),
		Timestamp: variantString(props["Timestamp"]),
		Storage:   smsStorageFromVariant(props["Storage"]),
		SMSC:      variantString(props["SMSC"]),
	}, nil
}

func smsStateName(state uint32) string {
	switch state {
	case 0:
		return "unknown"
	case 1:
		return "stored"
	case 2:
		return "receiving"
	case 3:
		return "received"
	case 4:
		return "sending"
	case 5:
		return "sent"
	default:
		return fmt.Sprintf("state-%d", state)
	}
}

func smsStorageFromVariant(v dbus.Variant) string {
	switch n := v.Value().(type) {
	case uint32:
		return smsStorageCode(n)
	case int32:
		return smsStorageCode(uint32(n))
	case string:
		return smsStorageName(n)
	default:
		return smsStorageName(fmt.Sprint(v.Value()))
	}
}

func smsStorageCode(code uint32) string {
	switch code {
	case 0:
		return "unknown"
	case 1:
		return "sim"
	case 2:
		return "modem"
	case 3:
		return "sim+modem"
	default:
		return fmt.Sprintf("storage-%d", code)
	}
}

func smsStorageName(storage string) string {
	switch storage {
	case "sm":
		return "sim"
	case "me":
		return "modem"
	case "mt":
		return "sim+modem"
	default:
		if storage == "" {
			return "unknown"
		}
		return storage
	}
}
