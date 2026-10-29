package secretscan

import "strings"

// The reserved placeholders.
//
// Every value here is deliberately fake and bound to nothing: no SIM in any
// drawer, no line any operator would route, no card any bank issued. They are
// the values fixtures, examples and documentation are supposed to use, and the
// scanner lets them through so that writing a correct test is the easy path.
//
// Two rules keep this list trustworthy:
//
//  1. A value goes in only once somebody has checked it is not real. The
//     numbers below are in ITU-assigned ranges but use obvious filler digits
//     (1234567890123, 9999999999, 0000) so the shape stays valid while the
//     value stays meaningless.
//  2. Nothing real is ever added to make a commit pass. If the scanner blocks
//     a value, the answer is to replace the value in the fixture, not to widen
//     the list. A real identifier is still real after it has been allowed.
//
// For network examples prefer the RFC 5737 documentation ranges (192.0.2.0/24,
// 198.51.100.0/24, 203.0.113.0/24), which the address rule does not match at
// all and so need no reservation.

// reservedICCIDs are the SIM serials fixtures may use. An ICCID identifies the
// card itself and outlives any phone number on it, so it is the one identifier
// here worth the most care.
var reservedICCIDs = map[string]struct{}{
	"8999801234567890123": {}, // the primary example line
	"8999809876543210987": {}, // the second line, for two-SIM examples
	"8999809999999999999": {}, // a third, for "unknown line" paths
	"8970199000000000001": {}, // a non-Uzbek issuer, for roaming examples
}

// reservedMSISDNs are the subscriber numbers fixtures may use. +998901234567
// is the canonical one; the second exists so a routing example can show two
// lines without inventing anything.
var reservedMSISDNs = map[string]struct{}{
	"998901234567": {},
	"998909876543": {},
}

// reservedIMEIs are modem identities fixtures may use. Both are valid in shape
// and assigned to nothing.
var reservedIMEIs = map[string]struct{}{
	"350000000000001": {},
	"860000000000001": {},
}

// reservedCardTails are the payment-card endings fixtures may use, stored
// without the asterisks the operators print in front of them.
var reservedCardTails = map[string]struct{}{
	"1001": {},
	"0001": {},
	"0002": {},
	"0003": {},
	"0004": {},
}

// reservedAmounts are balances and transaction amounts fixtures may use. A real
// balance is a running total that identifies an account over time, which is why
// the shape is flagged at all; these are flat filler values.
var reservedAmounts = map[string]struct{}{
	"12345.67890": {},
	"11111.11111": {},
	"99999.99999": {},
	"100000.00":   {},
	"90000.00":    {},
	"50000.00":    {},
	"25000.00":    {},
	"4000.00":     {},
}

// reservedCodes are one-time codes fixtures may use. These are the codes a
// person would recognise as filler at a glance, which matters: a fixture
// carrying a plausible code invites the next reader to treat it as real data
// and keep it.
var reservedCodes = map[string]struct{}{
	"0000":   {},
	"1000":   {},
	"1234":   {},
	"111111": {},
	"000000": {},
	"123456": {},
	"100001": {},
	"100002": {},
	"100003": {},
	"100004": {},
}

// reservedAddresses are the private addresses the repository may mention.
// 192.168.1.1 is the factory gateway of more or less every consumer router and
// so describes nobody's network in particular; the 10.0.0.x pair exists for
// configuration examples that need a second host.
var reservedAddresses = map[string]struct{}{
	"192.168.1.1": {},
	"10.0.0.1":    {},
	"10.0.0.2":    {},
	"10.0.0.9":    {},
}

func isReservedICCID(v string) bool    { _, ok := reservedICCIDs[v]; return ok }
func isReservedMSISDN(v string) bool   { _, ok := reservedMSISDNs[v]; return ok }
func isReservedIMEI(v string) bool     { _, ok := reservedIMEIs[v]; return ok }
func isReservedCardTail(v string) bool { _, ok := reservedCardTails[v]; return ok }
func isReservedAmount(v string) bool   { _, ok := reservedAmounts[v]; return ok }
func isReservedCode(v string) bool     { _, ok := reservedCodes[v]; return ok }
func isReservedAddress(v string) bool  { _, ok := reservedAddresses[v]; return ok }

// Reserved renders the list for `sms-gateway-scan -reserved`, so that somebody
// whose commit was just blocked can see what to use instead without reading
// the source.
func Reserved() string {
	var b strings.Builder
	for _, group := range []struct {
		title  string
		values map[string]struct{}
	}{
		{"ICCID (SIM serial)", reservedICCIDs},
		{"MSISDN (subscriber number)", reservedMSISDNs},
		{"IMEI (modem identity)", reservedIMEIs},
		{"card tail", reservedCardTails},
		{"balance or amount", reservedAmounts},
		{"one-time code", reservedCodes},
		{"private address", reservedAddresses},
	} {
		b.WriteString(group.title)
		b.WriteString(":\n")
		for _, v := range sortedKeys(group.values) {
			b.WriteString("  ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nFor network examples prefer 192.0.2.0/24, 198.51.100.0/24 or\n")
	b.WriteString("203.0.113.0/24 (RFC 5737), which are not flagged at all.\n")
	return b.String()
}
