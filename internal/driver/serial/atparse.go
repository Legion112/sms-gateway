package serial

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	cpinRe         = regexp.MustCompile(`\+CPIN:\s*(\S+)`)
	cregRe         = regexp.MustCompile(`\+CREG:\s*\d+,(\d+)`)
	cgregRe        = regexp.MustCompile(`\+CGREG:\s*\d+,(\d+)`)
	ceregRe        = regexp.MustCompile(`\+CEREG:\s*\d+,(\d+)`)
	cnumRe         = regexp.MustCompile(`\+CNUM:\s*"[^"]*"\s*,\s*"([^"]*)"`)
	cusdRe         = regexp.MustCompile(`\+CUSD:\s*\d+,"([^"]*)"`)
	phoneRe        = regexp.MustCompile(`\+[1-9]\d{6,14}`)
	phone8Re       = regexp.MustCompile(`(?:^|[^\d])8(\d{10})(?:[^\d]|$)`)
	phone998Re     = regexp.MustCompile(`(?:^|[^\d])998(\d{9})(?:[^\d]|$)`)
	phoneUZLocalRe = regexp.MustCompile(`(?:^|[^\d])(9\d{8})(?:[^\d]|$)`)
	csqRe          = regexp.MustCompile(`\+CSQ:\s*(\d+),(\d+)`)
	cpmsRe         = regexp.MustCompile(`\+CPMS:\s*"([^"]+)",(\d+),(\d+)`)
)

func parseCPIN(response string) string {
	if m := cpinRe.FindStringSubmatch(response); len(m) == 2 {
		return m[1]
	}
	upper := strings.ToUpper(response)
	if strings.Contains(upper, "SIM NOT INSERTED") || strings.Contains(upper, "CME ERROR: 10") {
		return "missing"
	}
	return "unknown"
}

func parseCREG(response string) string {
	return parseRegStat(cregRe, response)
}

func parseCGREG(response string) string {
	return parseRegStat(cgregRe, response)
}

func parseCEREG(response string) string {
	return parseRegStat(ceregRe, response)
}

func parseRegStat(re *regexp.Regexp, response string) string {
	if m := re.FindStringSubmatch(response); len(m) == 2 {
		return regStatLabel(m[1])
	}
	return "unknown"
}

func regStatLabel(code string) string {
	switch code {
	case "0":
		return "not registered"
	case "1":
		return "registered (home)"
	case "2":
		return "searching"
	case "3":
		return "registration denied"
	case "4":
		return "unknown"
	case "5":
		return "registered (roaming)"
	default:
		return "unknown"
	}
}

func bestNetworkState(states ...string) string {
	best := "unknown"
	bestPri := -1
	for _, s := range states {
		p := regStatPriority(s)
		if p > bestPri {
			bestPri = p
			best = s
		}
	}
	return best
}

func regStatPriority(state string) int {
	switch {
	case strings.HasPrefix(state, "registered"):
		return 4
	case state == "searching":
		return 3
	case state == "registration denied":
		return 2
	case state == "not registered":
		return 1
	default:
		return 0
	}
}

func parseCNUM(response string) string {
	for _, m := range cnumRe.FindAllStringSubmatch(response, -1) {
		if len(m) == 2 && strings.TrimSpace(m[1]) != "" {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func parseCUSD(response string) string {
	m := cusdRe.FindStringSubmatch(response)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func extractPhoneFromText(text string) string {
	if m := phoneRe.FindString(text); m != "" {
		return m
	}
	if m := phone8Re.FindStringSubmatch(text); len(m) == 2 {
		return "+7" + m[1]
	}
	if m := phone998Re.FindStringSubmatch(text); len(m) == 2 {
		return "+998" + m[1]
	}
	if m := phoneUZLocalRe.FindStringSubmatch(text); len(m) == 2 {
		return "+998" + m[1]
	}
	return ""
}

func parseCSQ(response string) (rssi int, ok bool) {
	m := csqRe.FindStringSubmatch(response)
	if len(m) != 3 {
		return -1, false
	}
	return atoi(m[1]), true
}

func csqLabel(rssi int) string {
	switch {
	case rssi == 99:
		return "no signal"
	case rssi <= 9:
		return fmt.Sprintf("weak (%d)", rssi)
	case rssi <= 14:
		return fmt.Sprintf("fair (%d)", rssi)
	default:
		return fmt.Sprintf("good (%d)", rssi)
	}
}

func parseCPMS(response string) (store string, used, total int, ok bool) {
	m := cpmsRe.FindStringSubmatch(response)
	if len(m) != 4 {
		return "", 0, 0, false
	}
	store = m[1]
	used = atoi(m[2])
	total = atoi(m[3])
	return store, used, total, true
}

func simStatusLabel(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "READY":
		return "ready"
	case "SIM PIN":
		return "pin required"
	case "SIM PUK":
		return "puk required"
	case "MISSING":
		return "missing"
	default:
		if raw == "" || raw == "unknown" {
			return "unknown"
		}
		return strings.ToLower(raw)
	}
}

func smsReady(simStatus, networkState string) bool {
	return simStatus == "ready" && strings.HasPrefix(networkState, "registered")
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
