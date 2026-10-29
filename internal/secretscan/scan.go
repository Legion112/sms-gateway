// Package secretscan looks for the operator's own identifiers and message
// contents in files that are about to be committed, pushed or published.
//
// This gateway handles real SMS for real SIMs, and the useful test fixture is
// the real thing: the wording operators and banks actually send, the ICCID the
// modem actually reports. That is also how a public repository ends up carrying
// a phone number, a SIM serial, a card tail and an account balance. The leak is
// not carelessness, it is the shortest path to a meaningful test, so it has to
// be blocked mechanically rather than remembered.
//
// The approach is deny-by-shape, allow-by-list. A rule matches the *shape* of an
// identifier (an ICCID is 89 followed by sixteen to eighteen digits) and then
// asks whether that particular value is one of the reserved placeholders in
// reserved.go. Anything shaped like an identifier that is not a reserved
// placeholder is a finding. The consequence is deliberate: a brand new real
// number nobody has seen before is caught on its first commit, which a
// denylist of known-bad values could never do.
package secretscan

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Finding is one suspected identifier, located well enough to fix.
type Finding struct {
	Path  string
	Line  int
	Rule  string
	Value string // masked; see mask
	Why   string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s (%s)", f.Path, f.Line, f.Rule, f.Value, f.Why)
}

// AllowPragma on a line suppresses every finding on that line. It must be
// followed by a reason, so that waiving a rule leaves an argument behind in the
// diff rather than a bare marker nobody can evaluate later.
const AllowPragma = "secretscan:allow"

type rule struct {
	name string
	why  string
	pat  *regexp.Regexp
	// group is the capture group holding the value to test against the
	// reserved set. Zero means the whole match.
	group int
	ok    func(string) bool
}

// rules are ordered most specific first, so that a bot token is reported as a
// bot token rather than as whatever digits happen to sit at its front.
var rules = []rule{
	{
		name: "telegram-bot-token",
		why:  "a bot token is a credential; rotate it rather than committing it",
		pat:  regexp.MustCompile(`\b\d{8,10}:[A-Za-z0-9_-]{30,}\b`),
		ok:   func(string) bool { return false }, // no token is ever acceptable
	},
	{
		name: "iccid",
		why:  "a SIM serial identifies the card itself, not just the line",
		pat:  regexp.MustCompile(`\b89\d{16,18}\b`),
		ok:   isReservedICCID,
	},
	{
		name: "imei",
		why:  "an IMEI identifies the modem hardware",
		pat:  regexp.MustCompile(`\b(?:35|86|01)\d{13}\b`),
		ok:   isReservedIMEI,
	},
	{
		name: "msisdn",
		why:  "a subscriber number; use a reserved placeholder instead",
		pat:  regexp.MustCompile(`\+?\b998\d{9}\b`),
		ok:   isReservedMSISDN,
	},
	{
		name: "card-tail",
		why:  "the last digits of a payment card",
		pat:  regexp.MustCompile(`\*{1,4}\d{4}\b`),
		ok:   isReservedCardTail,
	},
	{
		// Four or more digits before the point and two or more after is the
		// shape of a real account balance, as in 12345.67890 or 100000.00.
		// Round amounts with a single decimal (10000.0) are left alone: they
		// are what a top-up looks like and they identify nobody.
		//
		// The examples above are reserved placeholders on purpose. Writing a
		// rule's comment with the values that prompted it is how the leak
		// gets re-committed by the person fixing it.
		name: "balance",
		why:  "an account balance or transaction amount",
		pat:  regexp.MustCompile(`\b\d{4,}\.\d{2,}\b`),
		ok:   isReservedAmount,
	},
	{
		// The code itself, not the word. markers.go legitimately carries
		// "podtverjdeniya" as a language marker with a weight after it, which
		// is one digit and so never matches.
		name:  "one-time-code",
		why:   "a one-time code from a real message",
		pat:   regexp.MustCompile(`(?i)\b(?:kod|kodi|code|parol|password|otp|podtverjdeniya|tasdiqlash)\b[^0-9\n]{0,16}(\d{4,8})\b`),
		group: 1,
		ok:    isReservedCode,
	},
	{
		name: "private-address",
		why:  "a LAN address describes the operator's own network",
		pat:  regexp.MustCompile(`\b(?:10\.\d{1,3}|192\.168|172\.(?:1[6-9]|2\d|3[01]))\.\d{1,3}\.\d{1,3}\b`),
		ok:   isReservedAddress,
	},
}

// Skip reports whether a path is not worth scanning. Binary blobs and vendored
// or generated trees produce noise, not leaks, and go.sum is a wall of hashes.
func Skip(path string) bool {
	switch {
	case strings.HasPrefix(path, "vendor/"),
		strings.HasPrefix(path, ".git/"),
		path == "go.sum",
		strings.HasSuffix(path, ".png"),
		strings.HasSuffix(path, ".jpg"),
		strings.HasSuffix(path, ".jpeg"),
		strings.HasSuffix(path, ".gif"),
		strings.HasSuffix(path, ".pdf"),
		strings.HasSuffix(path, ".zip"),
		strings.HasSuffix(path, ".gz"):
		return true
	}
	return false
}

// ScanBytes reports every suspected identifier in one file's contents.
//
// A file whose first kilobytes contain a NUL byte is treated as binary and
// skipped: the rules are text shapes, and compiled output matches them by
// coincidence often enough to train people to ignore the scanner.
func ScanBytes(path string, data []byte) []Finding {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return nil
	}

	var out []Finding
	for i, line := range strings.Split(string(data), "\n") {
		if allowed(line) {
			continue
		}
		seen := make(map[string]struct{})
		for _, r := range rules {
			for _, m := range r.pat.FindAllStringSubmatch(line, -1) {
				value := m[r.group]
				if r.ok(normalizeValue(value)) {
					continue
				}
				key := r.name + "\x00" + value
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, Finding{
					Path:  path,
					Line:  i + 1,
					Rule:  r.name,
					Value: mask(value),
					Why:   r.why,
				})
			}
		}
	}
	return out
}

// allowed reports whether the line waives the scanner, which requires a reason
// after the pragma.
func allowed(line string) bool {
	idx := strings.Index(line, AllowPragma)
	if idx < 0 {
		return false
	}
	return strings.TrimSpace(line[idx+len(AllowPragma):]) != ""
}

// normalizeValue strips the decoration a value carries in text so the reserved
// set can be written as bare values: a leading + on a number, the asterisks in
// front of a card tail.
func normalizeValue(v string) string {
	return strings.TrimLeft(v, "+*")
}

// mask keeps a value recognisable without reprinting it in full, because these
// findings are also printed by CI, whose logs are readable by anyone who can
// read the repository.
func mask(v string) string {
	if len(v) <= 8 {
		return v[:1] + strings.Repeat("*", len(v)-1)
	}
	return v[:4] + strings.Repeat("*", len(v)-8) + v[len(v)-4:]
}

// sortedKeys gives the reserved listing a stable order, so that two runs of
// -reserved can be diffed against each other.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
