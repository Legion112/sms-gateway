package secretscan

import (
	"fmt"
	"strings"
	"testing"
)

// Every value in this file is invented for the test and reserved for nothing:
// valid in shape, assigned to no SIM, line, card or account. That is the point
// -- the rules have to be proved to fire, and proving it with a real
// identifier would reintroduce exactly what the scanner exists to stop.
//
// The lines carrying those values waive the scanner with a reason, which is
// also how the pragma gets exercised in anger.

func TestFlagsIdentifiersThatAreNotReserved(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule string
		line string
	}{
		{"iccid", "iccid", `+QCCID: 8999811111111111111`},                                             // secretscan:allow invented SIM serial, test fixture
		{"msisdn", "msisdn", `to_line: "+998907654321"`},                                              // secretscan:allow invented subscriber number, test fixture
		{"imei", "imei", `+CGSN: 350123456789012`},                                                    // secretscan:allow invented modem identity, test fixture
		{"card tail", "card-tail", `karta ***7777. Miqdor: 1.00 UZS`},                                 // secretscan:allow invented card tail, test fixture
		{"balance", "balance", `Balans: 54321.99 sumov`},                                              // secretscan:allow invented balance, test fixture
		{"one-time code", "one-time-code", `Kod podtverjdeniya: 987654`},                              // secretscan:allow invented one-time code, test fixture
		{"bot token", "telegram-bot-token", `token: 1234567890:AAEabcdefghijklmnopqrstuvwxyz1234567`}, // secretscan:allow invented bot token, test fixture
		{"private address", "private-address", `endpoint: http://192.168.77.88:5000`},                 // secretscan:allow invented LAN address, test fixture
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanBytes("fixture", []byte(tc.line))
			if len(got) == 0 {
				t.Fatalf("ScanBytes(%q) found nothing, want a %s finding", tc.line, tc.rule)
			}
			if got[0].Rule != tc.rule {
				t.Errorf("rule = %q, want %q (findings: %v)", got[0].Rule, tc.rule, got)
			}
		})
	}
}

// The reserved placeholders are the whole reason a contributor can write a
// realistic fixture at all. If one of them ever starts failing, every fixture
// using it starts failing with it, so they are checked as a set.
//
// Each group is checked only in the forms its own rule sees. The sets are
// per-rule on purpose: a reserved card tail is not thereby a reserved one-time
// code, because the question the scanner answers is "is this value a known
// placeholder for this kind of identifier", and widening that would let a
// value vouch for itself in a context nobody vetted it for.
func TestReservedPlaceholdersPass(t *testing.T) {
	for _, group := range []struct {
		name   string
		values map[string]struct{}
		forms  []string // %s is the value
	}{
		{"iccid", reservedICCIDs, []string{"%s", "+QCCID: %s", "iccid=%s"}},
		{"msisdn", reservedMSISDNs, []string{"%s", "+%s", `to_line: "+%s"`}},
		{"imei", reservedIMEIs, []string{"%s", "+CGSN: %s"}},
		{"card tail", reservedCardTails, []string{"*%s", "***%s", "karta ***%s."}},
		{"amount", reservedAmounts, []string{"%s", "Balans: %s sumov", "Miqdor:%s UZS"}},
		{"code", reservedCodes, []string{"kod: %s", "Kod podtverjdeniya: %s", "code: %s"}},
		{"address", reservedAddresses, []string{"%s", "http://%s:5000"}},
	} {
		for v := range group.values {
			for _, form := range group.forms {
				line := fmt.Sprintf(form, v)
				if got := ScanBytes("fixture", []byte(line)); len(got) != 0 {
					t.Errorf("%s: reserved %q as %q was flagged: %v", group.name, v, line, got)
				}
			}
		}
	}
}

// A bot token is a live credential, so unlike every other rule it has no
// reserved form: there is no such thing as a token that is fine to commit.
func TestNoBotTokenIsEverReserved(t *testing.T) {
	const token = "9876543210:AAEzyxwvutsrqponmlkjihgfedcba7654321" // secretscan:allow invented bot token, test fixture
	if got := ScanBytes("fixture", []byte(token)); len(got) != 1 || got[0].Rule != "telegram-bot-token" {
		t.Fatalf("ScanBytes(token) = %v, want one telegram-bot-token finding", got)
	}
}

// The waiver has to carry an argument. A bare marker would let somebody silence
// the scanner without saying why, and the reason is the only thing a reviewer
// reading the diff later has to judge.
func TestAllowPragmaRequiresAReason(t *testing.T) {
	const bad = `iccid 8999822222222222222 // secretscan:allow` // secretscan:allow checks a reasonless waiver is ignored
	if got := ScanBytes("fixture", []byte(bad)); len(got) == 0 {
		t.Error("a waiver with no reason suppressed the finding")
	}
	const good = `iccid 8999822222222222222 // secretscan:allow documented elsewhere` // secretscan:allow checks a reasoned waiver is honoured
	if got := ScanBytes("fixture", []byte(good)); len(got) != 0 {
		t.Errorf("a waiver with a reason did not suppress: %v", got)
	}
}

// markers.go carries the vocabulary the language detector matches on, which is
// the same vocabulary the one-time-code rule looks for. The weight after the
// word is one digit, so it must not read as a code.
func TestDetectionVocabularyIsNotAFinding(t *testing.T) {
	const markers = "\t\"podtverjdeniya\": 3,\n\t\"tasdiqlash\": 2,\n\t\"sumov\": 3,\n"
	if got := ScanBytes("internal/translate/markers.go", []byte(markers)); len(got) != 0 {
		t.Errorf("marker table flagged: %v", got)
	}
}

// A top-up is a round number and identifies nobody; a balance is a running
// total with fractional digits and identifies an account over time. The rule
// is built on that difference, so it is worth pinning.
func TestRoundAmountsAreNotBalances(t *testing.T) {
	for _, s := range []string{
		"Balans popolnen na 10000.0 sumov",
		"Miqdor: 5000.0 UZS",
		"menee 110000.0 sumov",
	} {
		if got := ScanBytes("fixture", []byte(s)); len(got) != 0 {
			t.Errorf("ScanBytes(%q) flagged a round amount: %v", s, got)
		}
	}
}

func TestBinaryContentIsSkipped(t *testing.T) {
	data := append([]byte("8999833333333333333\x00"), make([]byte, 64)...) // secretscan:allow invented SIM serial, test fixture
	if got := ScanBytes("some.bin", data); len(got) != 0 {
		t.Errorf("binary content produced findings: %v", got)
	}
}

func TestSkipsPathsNotWorthScanning(t *testing.T) {
	for _, p := range []string{"go.sum", "vendor/x/y.go", "docs/shot.png", "a.tar.gz"} {
		if !Skip(p) {
			t.Errorf("Skip(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"README.md", "internal/x/y.go", "docs/index.html", "config.example.yaml"} {
		if Skip(p) {
			t.Errorf("Skip(%q) = true, want false", p)
		}
	}
}

// The findings are printed by CI, whose logs anybody who can read the
// repository can read, so a finding must locate a value without reprinting it.
func TestFindingsDoNotReprintTheValue(t *testing.T) {
	const iccid = "8999844444444444444" // secretscan:allow invented SIM serial, test fixture
	got := ScanBytes("fixture", []byte(iccid))
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if strings.Contains(got[0].Value, iccid) {
		t.Errorf("finding reprinted the value in full: %q", got[0].Value)
	}
	if !strings.HasPrefix(got[0].Value, iccid[:4]) {
		t.Errorf("finding %q is not locatable, want it to keep the leading digits", got[0].Value)
	}
}

func TestReservedListingNamesEveryGroup(t *testing.T) {
	out := Reserved()
	for _, want := range []string{"ICCID", "MSISDN", "IMEI", "card tail", "balance", "one-time code", "private address", "RFC 5737"} {
		if !strings.Contains(out, want) {
			t.Errorf("Reserved() does not mention %q", want)
		}
	}
}
