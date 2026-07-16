//go:build linux

package mm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/legion/sms-gateway/internal/modem"
)

const (
	messagingInterface = "org.freedesktop.ModemManager1.Modem.Messaging"
	simInterface       = "org.freedesktop.ModemManager1.Sim"
)

// SMSStatus reads SIM and SMS state from ModemManager.
func (d *Driver) SMSStatus(ctx context.Context) (modem.SMSStatus, error) {
	props, err := d.getObjectProperties(ctx, d.modemPath, modemInterface, []string{
		"State", "StateFailedReason", "UnlockRequired", "Sim", "OwnNumbers",
	})
	if err != nil {
		return modem.SMSStatus{}, err
	}

	state := int32(variantInt(props["State"]))
	failedReason := variantUint32(props["StateFailedReason"])
	modemState := modemStateName(state)
	if state == -1 {
		if reason := failedReasonName(failedReason); reason != "" {
			modemState = fmt.Sprintf("failed (%s)", reason)
		}
	}

	simStatus := "unknown"
	simPath := variantObjectPath(props["Sim"])
	var iccid, imsi string
	if simPath == "" || simPath == "/" {
		if failedReason == 2 {
			simStatus = "missing"
		} else {
			simStatus = "absent"
		}
	} else {
		simStatus, iccid, imsi = d.readSimInfo(ctx, simPath)
	}

	unlock := variantUint32(props["UnlockRequired"])
	if unlock == 1 {
		simStatus = "pin required"
	} else if unlock == 2 {
		simStatus = "puk required"
	}

	networkState := "not registered"
	if accessTechRegistered(state) {
		networkState = "registered"
	} else if state == 6 {
		networkState = "searching"
	} else if state == -1 {
		networkState = "unavailable"
	}

	messageCount := -1
	if paths, err := d.listMessages(ctx); err == nil {
		messageCount = len(paths)
	}

	messageStore := "unknown"
	if messageCount >= 0 {
		messageStore = fmt.Sprintf("mm %d messages", messageCount)
	}

	ready := simStatus == "ready" && accessTechRegistered(state)
	phoneNumber := firstOwnNumber(props["OwnNumbers"])
	detailParts := []string{
		fmt.Sprintf("sim=%s", simStatus),
		fmt.Sprintf("modem=%s", modemState),
		fmt.Sprintf("network=%s", networkState),
	}
	if iccid != "" {
		detailParts = append(detailParts, fmt.Sprintf("iccid=%s", iccid))
	}
	if imsi != "" {
		detailParts = append(detailParts, fmt.Sprintf("imsi=%s", imsi))
	}
	if phoneNumber != "" {
		detailParts = append(detailParts, fmt.Sprintf("reported_msisdn=%s", phoneNumber))
	}
	detail := strings.Join(detailParts, ", ")

	if d.verbose {
		d.log("MM: sms status %s", detail)
	}

	return modem.SMSStatus{
		Driver:       modem.DriverMM,
		Device:       string(d.modemPath),
		SimStatus:    simStatus,
		ICCID:        iccid,
		IMSI:         imsi,
		PhoneNumber:  phoneNumber,
		NetworkState: networkState,
		ModemState:   modemState,
		MessageStore: messageStore,
		MessageCount: messageCount,
		SMSReady:     ready,
		Detail:       detail,
	}, nil
}

func (d *Driver) readSimInfo(ctx context.Context, simPath dbus.ObjectPath) (status, iccid, imsi string) {
	props, err := d.getObjectProperties(ctx, simPath, simInterface, []string{"Active", "SimIdentifier", "Imsi"})
	if err != nil {
		return "unknown", "", ""
	}
	if !variantBool(props["Active"]) {
		return "inactive", variantString(props["SimIdentifier"]), variantString(props["Imsi"])
	}
	return "ready", variantString(props["SimIdentifier"]), variantString(props["Imsi"])
}

func (d *Driver) listMessages(ctx context.Context) ([]dbus.ObjectPath, error) {
	type result struct {
		paths []dbus.ObjectPath
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		obj := d.conn.Object(mmBusName, d.modemPath)
		call := obj.Call(messagingInterface+".List", 0)
		if call.Err != nil {
			ch <- result{err: call.Err}
			return
		}
		var paths []dbus.ObjectPath
		if err := call.Store(&paths); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{paths: paths}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(d.timeout):
		return nil, fmt.Errorf("timeout listing messages")
	case res := <-ch:
		return res.paths, res.err
	}
}

func (d *Driver) getObjectProperties(ctx context.Context, path dbus.ObjectPath, iface string, keys []string) (map[string]dbus.Variant, error) {
	type result struct {
		props map[string]dbus.Variant
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		obj := d.conn.Object(mmBusName, path)
		props := make(map[string]dbus.Variant)
		for _, key := range keys {
			prop := obj.Call("org.freedesktop.DBus.Properties.Get", 0, iface, key)
			if prop.Err != nil {
				ch <- result{err: prop.Err}
				return
			}
			var variant dbus.Variant
			if err := prop.Store(&variant); err != nil {
				ch <- result{err: err}
				return
			}
			props[key] = variant
		}
		ch <- result{props: props}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(d.timeout):
		return nil, fmt.Errorf("timeout reading properties")
	case res := <-ch:
		return res.props, res.err
	}
}

func variantInt(v dbus.Variant) int64 {
	switch n := v.Value().(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case uint32:
		return int64(n)
	default:
		return 0
	}
}

func variantBool(v dbus.Variant) bool {
	b, ok := v.Value().(bool)
	return ok && b
}

func variantObjectPath(v dbus.Variant) dbus.ObjectPath {
	switch p := v.Value().(type) {
	case dbus.ObjectPath:
		return p
	case string:
		return dbus.ObjectPath(p)
	default:
		return ""
	}
}

func firstOwnNumber(v dbus.Variant) string {
	for _, n := range variantStringSlice(v) {
		if n != "" {
			return n
		}
	}
	return ""
}

func variantStringSlice(v dbus.Variant) []string {
	switch s := v.Value().(type) {
	case []string:
		return s
	case []interface{}:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
