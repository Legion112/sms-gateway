package serial

import (
	"regexp"
	"strings"
)

var (
	cpinRe = regexp.MustCompile(`\+CPIN:\s*(\S+)`)
	cregRe = regexp.MustCompile(`\+CREG:\s*\d+,(\d+)`)
	cpmsRe = regexp.MustCompile(`\+CPMS:\s*"([^"]+)",(\d+),(\d+)`)
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
	if m := cregRe.FindStringSubmatch(response); len(m) == 2 {
		switch m[1] {
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
		}
	}
	return "unknown"
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
