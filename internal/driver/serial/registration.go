package serial

import (
	"context"
	"fmt"
	"regexp"

	"github.com/legion/sms-gateway/internal/modem"
)

// cireg captures the <reg_info> field of +CIREG: <n>,<reg_info>[,<ext_info>],
// which is 0 for no IMS registration and 1 for one.
var ciregRe = regexp.MustCompile(`\+CIREG:\s*\d+,\s*(\d+)`)

// imsRegistered is the +CIREG <reg_info> value meaning the modem holds an IMS
// registration. Anything else, including the "registered for SMS only" values
// some stacks report, is not relied on here.
const imsRegistered = 1

func parseRegState(re *regexp.Regexp, response string) modem.RegState {
	m := re.FindStringSubmatch(response)
	if len(m) != 2 {
		return modem.RegAbsent
	}
	switch stat := modem.RegState(atoi(m[1])); stat {
	case modem.RegNotRegistered, modem.RegHome, modem.RegSearching,
		modem.RegDenied, modem.RegUnknown, modem.RegRoaming:
		return stat
	default:
		// A <stat> outside the standard set is an answer we cannot read, which
		// is not the same as an unanswered query but means just as little.
		return modem.RegUnknown
	}
}

func parseIMSRegState(response string) modem.RegState {
	m := ciregRe.FindStringSubmatch(response)
	if len(m) != 2 {
		return modem.RegAbsent
	}
	if atoi(m[1]) == imsRegistered {
		return modem.RegHome
	}
	return modem.RegNotRegistered
}

// regPriority orders registration states by how much service they represent,
// so the packet-switched domain can be summarised from two queries without
// letting an unanswered or stale one mask a live registration.
func regPriority(s modem.RegState) int {
	switch {
	case s.Registered():
		return 5
	case s == modem.RegSearching:
		return 4
	case s == modem.RegDenied:
		return 3
	case s == modem.RegNotRegistered:
		return 2
	case s == modem.RegUnknown:
		return 1
	default:
		return 0
	}
}

// bestRegState picks the most favourable of several readings of one domain.
// +CGREG answers for 2G/3G and +CEREG for LTE; a modem on LTE reports no GPRS
// attach and vice versa, so the pair has to be reduced rather than combined.
func bestRegState(states ...modem.RegState) modem.RegState {
	best := modem.RegAbsent
	for _, s := range states {
		if regPriority(s) > regPriority(best) {
			best = s
		}
	}
	return best
}

// ReadRegistration reports each domain's registration separately.
//
// It changes nothing and tolerates every query failing: a modem that answers
// none of them yields an all-unanswered Registration, which SMSPath reports as
// unknown rather than as a fault.
func (d *Driver) ReadRegistration(ctx context.Context) (modem.Registration, error) {
	reg := modem.NewRegistration()

	cregResp, cregErr := d.exec(ctx, "AT+CREG?")
	if cregErr != nil {
		d.drainStaleResponse(cregErr)
	} else {
		reg.CS = parseRegState(cregRe, cregResp)
	}

	cgregResp, cgregErr := d.exec(ctx, "AT+CGREG?")
	if cgregErr != nil {
		d.drainStaleResponse(cgregErr)
	}
	ceregResp, ceregErr := d.exec(ctx, "AT+CEREG?")
	if ceregErr != nil {
		d.drainStaleResponse(ceregErr)
	}
	reg.PS = bestRegState(
		parseRegState(cgregRe, cgregResp),
		parseRegState(ceregRe, ceregResp),
	)

	// IMS last, and its failure is never an error: plenty of firmware does not
	// implement +CIREG at all, and an absent answer is already meaningful.
	if ciregResp, err := d.exec(ctx, "AT+CIREG?"); err != nil {
		d.drainStaleResponse(err)
	} else {
		reg.IMS = parseIMSRegState(ciregResp)
	}

	// Only the two domains that decide the verdict are worth failing over, and
	// only when neither answered: one of them answering is enough to judge.
	if cregErr != nil && cgregErr != nil && ceregErr != nil {
		return reg, fmt.Errorf("read registration: %w", cregErr)
	}
	return reg, nil
}
