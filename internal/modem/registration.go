package modem

import (
	"context"
	"fmt"
)

// RegState is one domain's registration, as the 3GPP <stat> field reports it in
// +CREG, +CGREG and +CEREG.
type RegState int

// Registration states. RegAbsent is this package's own addition: it means the
// modem never answered the query, which must not be confused with answering
// that it is not registered.
const (
	RegAbsent        RegState = -1
	RegNotRegistered RegState = 0
	RegHome          RegState = 1
	RegSearching     RegState = 2
	RegDenied        RegState = 3
	RegUnknown       RegState = 4
	RegRoaming       RegState = 5
)

// Registered reports a real attach. Only home and roaming count: searching and
// denied are both "no service", however temporarily.
func (s RegState) Registered() bool {
	return s == RegHome || s == RegRoaming
}

// Answered reports whether the modem replied to the query at all.
func (s RegState) Answered() bool { return s != RegAbsent }

func (s RegState) String() string {
	switch s {
	case RegAbsent:
		return "unsupported"
	case RegNotRegistered:
		return "not registered"
	case RegHome:
		return "registered (home)"
	case RegSearching:
		return "searching"
	case RegDenied:
		return "denied"
	case RegRoaming:
		return "registered (roaming)"
	default:
		return "unknown"
	}
}

// SMSPath is what a modem's registration implies about incoming SMS.
type SMSPath string

const (
	// SMSPathDirect means the modem holds a circuit-switched registration, so
	// SMS arrive over it and nothing else has to cooperate. This is the only
	// state that proves an SMS can land.
	SMSPathDirect SMSPath = "direct"
	// SMSPathIMS means there is no circuit-switched registration but there is
	// an IMS one, which carries SMS over IP.
	SMSPathIMS SMSPath = "ims"
	// SMSPathUnproven is the dangerous state: attached for data, with no
	// circuit-switched and no IMS registration. The modem answers AT, polls
	// cleanly and reports itself registered, while an incoming SMS depends
	// entirely on the visited network bridging it in over SGs. For a roaming
	// SIM that is not guaranteed, and when it does not happen nothing anywhere
	// reports a fault.
	SMSPathUnproven SMSPath = "unproven"
	// SMSPathOffline means no domain is registered. The modem still answers AT,
	// so the poll loop and the silence alert both stay happy.
	SMSPathOffline SMSPath = "offline"
	// SMSPathUnknown means the registration could not be judged: the modem did
	// not answer, or it is mid-reselection. Never alert on this -- an alarm
	// raised on missing information is an alarm operators learn to ignore.
	SMSPathUnknown SMSPath = "unknown"
)

// OK reports whether this state is evidence that an SMS can arrive.
func (p SMSPath) OK() bool {
	return p == SMSPathDirect || p == SMSPathIMS
}

// Problem reports whether this state is evidence that an SMS cannot arrive.
// Neither OK nor Problem holds for SMSPathUnknown, which is the point of
// having three answers rather than a boolean.
func (p SMSPath) Problem() bool {
	return p == SMSPathUnproven || p == SMSPathOffline
}

// Registration is where a modem is attached, domain by domain.
//
// The gateway's status line reports one "network state", which is the best of
// the three. That is the right summary for "does this modem have service" and
// the wrong one for "can an SMS reach this SIM": an LTE-only attach reads as
// registered while the domain SMS travels over is not there at all.
type Registration struct {
	// CS is the circuit-switched domain (+CREG): the 2G/3G voice and SMS
	// domain. A registration here is the one unambiguous SMS path.
	CS RegState
	// PS is the packet-switched domain: +CEREG on LTE, +CGREG on 2G/3G. This
	// is what carries data, and what makes a modem look healthy.
	PS RegState
	// IMS is the IMS registration (+CIREG), which can carry SMS with no
	// circuit-switched domain. RegAbsent means the modem does not implement
	// the query, which this package treats as no IMS path: a module that
	// cannot report an IMS registration is not one to bet an SMS on.
	IMS RegState
}

// NewRegistration builds a Registration with every domain unanswered, which is
// what a driver that could read nothing must report.
func NewRegistration() Registration {
	return Registration{CS: RegAbsent, PS: RegAbsent, IMS: RegAbsent}
}

// SMSPath judges what the registration means for incoming SMS.
//
// The order matters. A circuit-switched registration settles the question, so
// it is checked first and nothing else can downgrade it. Searching is checked
// before the offline verdict because reselection is a normal, brief state that
// must not be reported as a fault.
func (r Registration) SMSPath() SMSPath {
	switch {
	case r.CS.Registered():
		return SMSPathDirect
	case r.IMS.Registered():
		return SMSPathIMS
	case r.PS.Registered():
		return SMSPathUnproven
	case !r.CS.Answered() && !r.PS.Answered():
		return SMSPathUnknown
	case r.CS == RegSearching || r.PS == RegSearching:
		return SMSPathUnknown
	default:
		return SMSPathOffline
	}
}

// String renders the registration for a log line or an alert.
func (r Registration) String() string {
	return fmt.Sprintf("cs=%s ps=%s ims=%s", r.CS, r.PS, r.IMS)
}

// RegistrationReader is implemented by drivers that can report the registration
// of each domain separately. The mm driver reports ModemManager's single
// combined state and cannot, so every caller must cope with a modem that does
// not implement this.
type RegistrationReader interface {
	// ReadRegistration reports each domain's registration and changes nothing.
	ReadRegistration(ctx context.Context) (Registration, error)
}
