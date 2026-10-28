package modem

import "testing"

func TestRegStateRegistered(t *testing.T) {
	for _, tc := range []struct {
		state RegState
		want  bool
	}{
		{RegAbsent, false},
		{RegNotRegistered, false},
		{RegHome, true},
		{RegSearching, false},
		{RegDenied, false},
		{RegUnknown, false},
		{RegRoaming, true},
	} {
		t.Run(tc.state.String(), func(t *testing.T) {
			if got := tc.state.Registered(); got != tc.want {
				t.Fatalf("%s.Registered() = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// Answered is what keeps "the modem says it is not registered" apart from "the
// modem never answered". Reading those as the same thing would turn a module
// without the query into a permanent fault.
func TestRegStateAnswered(t *testing.T) {
	if RegAbsent.Answered() {
		t.Error("an unanswered query must not count as an answer")
	}
	for _, state := range []RegState{RegNotRegistered, RegHome, RegSearching, RegDenied, RegUnknown, RegRoaming} {
		if !state.Answered() {
			t.Errorf("%s came from an answer but Answered() is false", state)
		}
	}
}

func TestRegStateString(t *testing.T) {
	for _, tc := range []struct {
		state RegState
		want  string
	}{
		{RegAbsent, "unsupported"},
		{RegNotRegistered, "not registered"},
		{RegHome, "registered (home)"},
		{RegSearching, "searching"},
		{RegDenied, "denied"},
		{RegUnknown, "unknown"},
		{RegRoaming, "registered (roaming)"},
		{RegState(99), "unknown"},
	} {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("RegState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// TestSMSPathVerdicts is the decision this whole feature rests on. Each row is
// a registration a modem can really be in, and the verdict it must produce.
func TestSMSPathVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		reg  Registration
		want SMSPath
	}{
		{
			name: "2G/3G at home: SMS arrive over the circuit-switched domain",
			reg:  Registration{CS: RegHome, PS: RegHome, IMS: RegAbsent},
			want: SMSPathDirect,
		},
		{
			name: "roaming with both domains up, as the main modem reads",
			reg:  Registration{CS: RegRoaming, PS: RegRoaming, IMS: RegAbsent},
			want: SMSPathDirect,
		},
		{
			name: "LTE only, no IMS: the fault found on the backup modem",
			reg:  Registration{CS: RegNotRegistered, PS: RegRoaming, IMS: RegNotRegistered},
			want: SMSPathUnproven,
		},
		{
			name: "LTE only, IMS registered: SMS arrive over IP",
			reg:  Registration{CS: RegNotRegistered, PS: RegRoaming, IMS: RegHome},
			want: SMSPathIMS,
		},
		{
			name: "LTE only on firmware without +CIREG counts as no IMS path",
			reg:  Registration{CS: RegNotRegistered, PS: RegHome, IMS: RegAbsent},
			want: SMSPathUnproven,
		},
		{
			name: "attached for data while the modem will not answer +CREG",
			reg:  Registration{CS: RegAbsent, PS: RegRoaming, IMS: RegAbsent},
			want: SMSPathUnproven,
		},
		{
			name: "circuit-switched denied but data attached",
			reg:  Registration{CS: RegDenied, PS: RegHome, IMS: RegNotRegistered},
			want: SMSPathUnproven,
		},
		{
			name: "nothing registered at all",
			reg:  Registration{CS: RegNotRegistered, PS: RegNotRegistered, IMS: RegNotRegistered},
			want: SMSPathOffline,
		},
		{
			name: "both domains denied",
			reg:  Registration{CS: RegDenied, PS: RegDenied, IMS: RegAbsent},
			want: SMSPathOffline,
		},
		{
			name: "out of coverage, with no answer from the data domain",
			reg:  Registration{CS: RegUnknown, PS: RegAbsent, IMS: RegAbsent},
			want: SMSPathOffline,
		},
		{
			name: "mid-reselection must not be reported as a fault",
			reg:  Registration{CS: RegSearching, PS: RegNotRegistered, IMS: RegAbsent},
			want: SMSPathUnknown,
		},
		{
			name: "the data domain searching is just as inconclusive",
			reg:  Registration{CS: RegNotRegistered, PS: RegSearching, IMS: RegAbsent},
			want: SMSPathUnknown,
		},
		{
			name: "no answer from either domain",
			reg:  NewRegistration(),
			want: SMSPathUnknown,
		},
		{
			name: "a circuit-switched registration settles it whatever else says",
			reg:  Registration{CS: RegRoaming, PS: RegAbsent, IMS: RegNotRegistered},
			want: SMSPathDirect,
		},
		{
			name: "searching does not downgrade a live circuit-switched domain",
			reg:  Registration{CS: RegHome, PS: RegSearching, IMS: RegAbsent},
			want: SMSPathDirect,
		},
		{
			name: "IMS registered while searching for a circuit-switched domain",
			reg:  Registration{CS: RegSearching, PS: RegRoaming, IMS: RegHome},
			want: SMSPathIMS,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reg.SMSPath(); got != tc.want {
				t.Fatalf("SMSPath() = %q, want %q (registration: %s)", got, tc.want, tc.reg)
			}
		})
	}
}

// OK and Problem must never both hold, and must both be false for unknown.
// Alerting is driven off Problem, so an unknown reading that answered either
// question would raise an alarm on missing information.
func TestSMSPathOKAndProblemAreExclusive(t *testing.T) {
	for _, tc := range []struct {
		path        SMSPath
		wantOK      bool
		wantProblem bool
	}{
		{SMSPathDirect, true, false},
		{SMSPathIMS, true, false},
		{SMSPathUnproven, false, true},
		{SMSPathOffline, false, true},
		{SMSPathUnknown, false, false},
		{SMSPath(""), false, false},
	} {
		t.Run(string(tc.path), func(t *testing.T) {
			if got := tc.path.OK(); got != tc.wantOK {
				t.Errorf("%q.OK() = %v, want %v", tc.path, got, tc.wantOK)
			}
			if got := tc.path.Problem(); got != tc.wantProblem {
				t.Errorf("%q.Problem() = %v, want %v", tc.path, got, tc.wantProblem)
			}
		})
	}
}

func TestNewRegistrationAnswersNothing(t *testing.T) {
	reg := NewRegistration()
	for name, state := range map[string]RegState{"cs": reg.CS, "ps": reg.PS, "ims": reg.IMS} {
		if state.Answered() {
			t.Errorf("%s = %s, want unanswered", name, state)
		}
	}
}

// The string form goes into a journal line and into the alert an operator
// reads, so it has to name all three domains.
func TestRegistrationString(t *testing.T) {
	reg := Registration{CS: RegNotRegistered, PS: RegRoaming, IMS: RegAbsent}
	want := "cs=not registered ps=registered (roaming) ims=unsupported"
	if got := reg.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
