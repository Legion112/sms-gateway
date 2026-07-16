package config

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	iccidKeyRe = regexp.MustCompile(`^[0-9A-F]{15,20}$`)
	e164Re     = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
)

// NormalizeICCID canonicalizes an ICCID for map lookup.
func NormalizeICCID(iccid string) string {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(iccid), " ", ""))
	s = strings.TrimRight(s, "F")
	return s
}

// LookupSimNumber returns the MSISDN for an ICCID from sim_numbers.
func (cfg Config) LookupSimNumber(iccid string) string {
	key := NormalizeICCID(iccid)
	if key == "" {
		return ""
	}
	if n, ok := cfg.SimNumbers[key]; ok {
		return n
	}
	return ""
}

func validateSimNumbers(simNumbers map[string]string) error {
	if len(simNumbers) == 0 {
		return nil
	}
	for iccid, number := range simNumbers {
		key := NormalizeICCID(iccid)
		if key == "" || !iccidKeyRe.MatchString(key) {
			return fmt.Errorf("sim_numbers: invalid ICCID key %q", iccid)
		}
		if !e164Re.MatchString(strings.TrimSpace(number)) {
			return fmt.Errorf("sim_numbers[%q]: number must be E.164 (e.g. +998901234567)", iccid)
		}
	}
	return nil
}

func validateModemEntryLegacyFields(name string, m ModemEntry) error {
	if m.PhoneNumber != "" {
		return fmt.Errorf("modems.%s: phone_number is removed; use top-level sim_numbers keyed by ICCID", name)
	}
	if m.PhoneUSSD != "" {
		return fmt.Errorf("modems.%s: phone_ussd is removed; use top-level sim_numbers keyed by ICCID", name)
	}
	return nil
}
