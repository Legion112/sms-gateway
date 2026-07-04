package cmdutil

import (
	"context"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
)

// ResolvePhoneNumber returns the best-known line number for a modem.
// Order: SIM/modem report, config phone_number, optional USSD query.
// USSD is skipped while roaming (carrier USSD is slow and usually unavailable).
func ResolvePhoneNumber(_ context.Context, base config.Config, target ModemHandle, reported, networkState string) string {
	if reported != "" {
		return reported
	}
	entry := modemEntry(base, target.Name)
	if entry.PhoneNumber != "" {
		return entry.PhoneNumber
	}
	if entry.PhoneUSSD == "" || !shouldQueryPhoneUSSD(entry.PhoneUSSD, networkState) {
		return ""
	}
	querier, ok := target.Modem.(modem.PhoneQuerier)
	if !ok {
		return ""
	}
	ussdCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	phone, err := querier.QueryPhoneUSSD(ussdCtx, entry.PhoneUSSD)
	if err != nil || phone == "" {
		return ""
	}
	return phone
}

func modemEntry(base config.Config, name string) config.ModemEntry {
	if name == "" {
		return config.ModemEntry{}
	}
	entry, ok := base.Modems[name]
	if !ok {
		return config.ModemEntry{}
	}
	return entry
}

func isRoaming(networkState string) bool {
	return strings.Contains(strings.ToLower(networkState), "roaming")
}

func shouldQueryPhoneUSSD(phoneUSSD, networkState string) bool {
	return phoneUSSD != "" && !isRoaming(networkState)
}
