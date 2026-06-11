package serial

import (
	"context"
	"fmt"
	"strings"

	"github.com/legion/sms-gateway/internal/modem"
)

// SMSStatus reads SIM and SMS storage state via AT commands.
func (d *Driver) SMSStatus(ctx context.Context) (modem.SMSStatus, error) {
	if _, err := d.exec(ctx, "ATE0"); err != nil {
		return modem.SMSStatus{}, fmt.Errorf("disable echo: %w", err)
	}

	cpinResp, err := d.exec(ctx, "AT+CPIN?")
	simRaw := parseCPIN(cpinResp)
	if err != nil && simRaw == "unknown" {
		return modem.SMSStatus{}, fmt.Errorf("sim status: %w", err)
	}
	simStatus := simStatusLabel(simRaw)

	cregResp, _ := d.exec(ctx, "AT+CREG?")
	networkState := parseCREG(cregResp)

	messageStore := "unknown"
	messageCount := -1
	if cpmsResp, err := d.exec(ctx, "AT+CPMS?"); err == nil {
		if store, used, total, ok := parseCPMS(cpmsResp); ok {
			messageStore = fmt.Sprintf("%s %d/%d", store, used, total)
			messageCount = used
		}
	}

	ready := smsReady(simStatus, networkState)
	detail := strings.Join([]string{
		fmt.Sprintf("sim=%s", simStatus),
		fmt.Sprintf("network=%s", networkState),
	}, ", ")

	return modem.SMSStatus{
		Driver:       modem.DriverSerial,
		Device:       d.device,
		SimStatus:    simStatus,
		NetworkState: networkState,
		ModemState:   "at",
		MessageStore: messageStore,
		MessageCount: messageCount,
		SMSReady:     ready,
		Detail:       detail,
	}, nil
}
