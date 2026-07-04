package serial

import (
	"context"
	"fmt"
	"strings"

	"github.com/legion/sms-gateway/internal/modem"
)

// SMSStatus reads SIM and SMS storage state via AT commands.
func (d *Driver) SMSStatus(ctx context.Context) (modem.SMSStatus, error) {
	if err := d.setup(ctx); err != nil {
		return modem.SMSStatus{}, err
	}

	cpinResp, err := d.exec(ctx, "AT+CPIN?")
	simRaw := parseCPIN(cpinResp)
	if err != nil && simRaw == "unknown" {
		return modem.SMSStatus{}, fmt.Errorf("sim status: %w", err)
	}
	simStatus := simStatusLabel(simRaw)

	cnumResp, _ := d.exec(ctx, "AT+CNUM")
	phoneNumber := parseCNUM(cnumResp)

	cregResp, _ := d.exec(ctx, "AT+CREG?")
	cgregResp, _ := d.exec(ctx, "AT+CGREG?")
	ceregResp, _ := d.exec(ctx, "AT+CEREG?")
	cregState := parseCREG(cregResp)
	cgregState := parseCGREG(cgregResp)
	ceregState := parseCEREG(ceregResp)
	networkState := bestNetworkState(cregState, cgregState, ceregState)

	csqResp, _ := d.exec(ctx, "AT+CSQ")
	signal := "unknown"
	if rssi, ok := parseCSQ(csqResp); ok {
		signal = csqLabel(rssi)
	}

	messageStore := "unknown"
	messageCount := -1
	if cpmsResp, err := d.exec(ctx, "AT+CPMS?"); err == nil {
		if store, used, total, ok := parseCPMS(cpmsResp); ok {
			messageStore = fmt.Sprintf("%s %d/%d", store, used, total)
			messageCount = used
		}
	}

	ready := smsReady(simStatus, networkState)
	detailParts := []string{
		fmt.Sprintf("sim=%s", simStatus),
		fmt.Sprintf("network=%s", networkState),
		fmt.Sprintf("signal=%s", signal),
	}
	if cregState != networkState {
		detailParts = append(detailParts, fmt.Sprintf("creg=%s", cregState))
	}
	if ceregState != networkState && ceregState != "unknown" {
		detailParts = append(detailParts, fmt.Sprintf("cereg=%s", ceregState))
	}
	detail := strings.Join(detailParts, ", ")

	return modem.SMSStatus{
		Driver:       modem.DriverSerial,
		Device:       d.device,
		SimStatus:    simStatus,
		PhoneNumber:  phoneNumber,
		NetworkState: networkState,
		ModemState:   "at",
		MessageStore: messageStore,
		MessageCount: messageCount,
		SMSReady:     ready,
		Detail:       detail,
	}, nil
}
