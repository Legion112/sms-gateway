package serial

import (
	"testing"

	"github.com/legion/sms-gateway/internal/modem"
)

func TestParseRegStateCREG(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     modem.RegState
	}{
		{"not registered, as the backup modem answered", "+CREG: 2,0\nOK", modem.RegNotRegistered},
		{"registered at home", "+CREG: 2,1\nOK", modem.RegHome},
		{"searching", "+CREG: 2,2\nOK", modem.RegSearching},
		{"denied", "+CREG: 2,3\nOK", modem.RegDenied},
		{"unknown", "+CREG: 2,4\nOK", modem.RegUnknown},
		{"roaming, as the main modem answered", "+CREG: 2,5\nOK", modem.RegRoaming},
		{"with location and act fields", `+CREG: 2,1,"10B5","BDBB208",7` + "\nOK", modem.RegHome},
		{"unsolicited reporting off", "+CREG: 0,1\nOK", modem.RegHome},
		{"space after the comma", "+CREG: 2, 5\nOK", modem.RegRoaming},
		{"echoed command then answer", "AT+CREG?\n+CREG: 2,0\nOK", modem.RegNotRegistered},
		{"a stat outside the standard set", "+CREG: 2,9\nOK", modem.RegUnknown},
		{"plain error", "ERROR", modem.RegAbsent},
		{"cme error", "+CME ERROR: 10", modem.RegAbsent},
		{"empty", "", modem.RegAbsent},
		{"another command's answer", "+CEREG: 2,1\nOK", modem.RegAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRegState(cregRe, tc.response); got != tc.want {
				t.Fatalf("parseRegState(%q) = %s, want %s", tc.response, got, tc.want)
			}
		})
	}
}

// Each domain has its own command, and one must never read another's answer:
// that is exactly how an LTE attach would be mistaken for a circuit-switched
// one, which is the failure this feature exists to catch.
func TestParseRegStateKeepsDomainsApart(t *testing.T) {
	const response = "+CREG: 2,0\n+CGREG: 2,0\n+CEREG: 2,5\nOK"

	if got := parseRegState(cregRe, response); got != modem.RegNotRegistered {
		t.Errorf("CS = %s, want not registered", got)
	}
	if got := parseRegState(cgregRe, response); got != modem.RegNotRegistered {
		t.Errorf("GPRS = %s, want not registered", got)
	}
	if got := parseRegState(ceregRe, response); got != modem.RegRoaming {
		t.Errorf("LTE = %s, want registered (roaming)", got)
	}
}

func TestParseIMSRegState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     modem.RegState
	}{
		{"reporting on, not registered", "+CIREG: 1,0\nOK", modem.RegNotRegistered},
		{"reporting on, registered", "+CIREG: 1,1\nOK", modem.RegHome},
		{"reporting off, not registered", "+CIREG: 0,0\nOK", modem.RegNotRegistered},
		{"with the extended info field", `+CIREG: 1,1,"sip:+79956009754@ims.mnc001.mcc250.3gppnetwork.org"` + "\nOK", modem.RegHome},
		{"space after the comma", "+CIREG: 1, 1\nOK", modem.RegHome},
		{"registered for SMS only is not relied on", "+CIREG: 1,2\nOK", modem.RegNotRegistered},
		{"firmware without the command", "ERROR", modem.RegAbsent},
		{"empty", "", modem.RegAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseIMSRegState(tc.response); got != tc.want {
				t.Fatalf("parseIMSRegState(%q) = %s, want %s", tc.response, got, tc.want)
			}
		})
	}
}

// The packet-switched domain is read from two commands because a modem on LTE
// reports no GPRS attach and one on 3G reports no EPS attach. Reducing the pair
// wrongly would either hide a live data attach or invent one.
func TestBestRegState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []modem.RegState
		want   modem.RegState
	}{
		{"on LTE: cgreg says nothing, cereg is attached",
			[]modem.RegState{modem.RegNotRegistered, modem.RegRoaming}, modem.RegRoaming},
		{"on 3G: cgreg is attached, cereg says nothing",
			[]modem.RegState{modem.RegHome, modem.RegNotRegistered}, modem.RegHome},
		{"neither command answered",
			[]modem.RegState{modem.RegAbsent, modem.RegAbsent}, modem.RegAbsent},
		{"one unanswered must not mask the other",
			[]modem.RegState{modem.RegAbsent, modem.RegHome}, modem.RegHome},
		{"searching beats not registered",
			[]modem.RegState{modem.RegNotRegistered, modem.RegSearching}, modem.RegSearching},
		{"registered beats searching",
			[]modem.RegState{modem.RegSearching, modem.RegRoaming}, modem.RegRoaming},
		{"not registered beats an unreadable stat",
			[]modem.RegState{modem.RegUnknown, modem.RegNotRegistered}, modem.RegNotRegistered},
		{"denied beats not registered: it is an answer from a network",
			[]modem.RegState{modem.RegNotRegistered, modem.RegDenied}, modem.RegDenied},
		{"nothing at all", nil, modem.RegAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bestRegState(tc.states...); got != tc.want {
				t.Fatalf("bestRegState(%v) = %s, want %s", tc.states, got, tc.want)
			}
		})
	}
}

// The reduction must not depend on the order the two commands were asked in,
// in anything a caller can act on. Home and roaming tie deliberately: the two
// commands cover different technologies and either answer means the data
// domain is attached, so the pair needs no invented precedence.
func TestBestRegStateIsOrderIndependent(t *testing.T) {
	all := []modem.RegState{
		modem.RegAbsent, modem.RegNotRegistered, modem.RegHome,
		modem.RegSearching, modem.RegDenied, modem.RegUnknown, modem.RegRoaming,
	}
	for _, a := range all {
		for _, b := range all {
			got, flipped := bestRegState(a, b), bestRegState(b, a)
			if got.Registered() != flipped.Registered() {
				t.Errorf("bestRegState(%s, %s) = %s but reversed = %s: they disagree on whether the domain is attached",
					a, b, got, flipped)
			}
			if got.Answered() != flipped.Answered() {
				t.Errorf("bestRegState(%s, %s) = %s but reversed = %s: they disagree on whether anything answered",
					a, b, got, flipped)
			}
		}
	}
}

// The whole point of reading three domains is the verdict, so the parsers and
// the decision are checked together against responses taken off the hardware.
func TestParsedRegistrationYieldsVerdict(t *testing.T) {
	for _, tc := range []struct {
		name  string
		creg  string
		cgreg string
		cereg string
		cireg string
		want  modem.SMSPath
	}{
		{
			name:  "the backup modem as found: LTE attached, no CS, no IMS",
			creg:  "+CREG: 2,0\nOK",
			cgreg: "+CGREG: 2,0\nOK",
			cereg: `+CEREG: 2,5,"10B5","BDBB208",7` + "\nOK",
			cireg: "+CIREG: 1,0\nOK",
			want:  modem.SMSPathUnproven,
		},
		{
			name:  "the backup modem after the posture correction: WCDMA, CS up",
			creg:  `+CREG: 2,5,"10B5","BDBB208",2` + "\nOK",
			cgreg: `+CGREG: 2,5,"10B5","BDBB208",2` + "\nOK",
			cereg: "+CEREG: 2,0\nOK",
			cireg: "ERROR",
			want:  modem.SMSPathDirect,
		},
		{
			name:  "LTE with IMS up",
			creg:  "+CREG: 2,0\nOK",
			cgreg: "+CGREG: 2,0\nOK",
			cereg: "+CEREG: 2,1\nOK",
			cireg: "+CIREG: 1,1\nOK",
			want:  modem.SMSPathIMS,
		},
		{
			name:  "a modem answering nothing",
			creg:  "ERROR",
			cgreg: "ERROR",
			cereg: "ERROR",
			cireg: "ERROR",
			want:  modem.SMSPathUnknown,
		},
		{
			name:  "no service anywhere",
			creg:  "+CREG: 2,0\nOK",
			cgreg: "+CGREG: 2,0\nOK",
			cereg: "+CEREG: 2,0\nOK",
			cireg: "+CIREG: 1,0\nOK",
			want:  modem.SMSPathOffline,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := modem.Registration{
				CS: parseRegState(cregRe, tc.creg),
				PS: bestRegState(
					parseRegState(cgregRe, tc.cgreg),
					parseRegState(ceregRe, tc.cereg),
				),
				IMS: parseIMSRegState(tc.cireg),
			}
			if got := reg.SMSPath(); got != tc.want {
				t.Fatalf("SMSPath() = %q, want %q (registration: %s)", got, tc.want, reg)
			}
		})
	}
}
