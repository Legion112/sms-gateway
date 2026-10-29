//go:build integration && linux

package integration_test

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/legion/sms-gateway/internal/smspdu"
	"github.com/legion/sms-gateway/internal/translate"
)

// Two bodies this gateway actually received, short enough for one UCS2 PDU,
// which holds 70 characters. The advisory that prompted the feature is longer
// than that and arrives as a concat message; the pieces under test here are
// the detection and the engine call, which do not depend on reassembly.
const (
	uzbekSMS   = "Mustaqillikning 35 yilligi muborak bo'lsin!"
	russianSMS = "Balans popolnen na 5000.0 sumov. Balans: 12345.67890 sumov."
)

// ucs2Deliver builds an SMS-DELIVER PDU carrying text as UCS2, which is how
// these operators send anything with a non-ASCII character in it -- and the
// store holds one body this gateway received as undecoded UCS2, so the
// encoding is not hypothetical.
func ucs2Deliver(t *testing.T, text string) string {
	t.Helper()
	units := utf16.Encode([]rune(text))
	ud := make([]byte, 0, len(units)*2)
	for _, u := range units {
		ud = append(ud, byte(u>>8), byte(u))
	}
	if len(ud) > 140 {
		t.Fatalf("body is %d octets, over one PDU", len(ud))
	}
	var b strings.Builder
	b.WriteString("0791999810584059") // SMSC
	b.WriteString("00")               // SMS-DELIVER, no UDH
	b.WriteString("0B919759069057F4") // originating address
	b.WriteString("00")               // PID
	b.WriteString("08")               // DCS: UCS2
	b.WriteString("62704012120502")   // timestamp
	b.WriteString(hexByte(len(ud)))   // UDL, octets for UCS2
	b.WriteString(strings.ToUpper(hex.EncodeToString(ud)))
	pdu := b.String()

	// Built by hand, so it is checked against the gateway's own parser rather
	// than trusted: a malformed PDU would make this test pass for the wrong
	// reason.
	parsed, err := smspdu.ParseHexDeliver(pdu)
	if err != nil {
		t.Fatalf("hand-built PDU does not parse: %v", err)
	}
	if parsed.Text != text {
		t.Fatalf("PDU round-trip changed the body:\n got %q\nwant %q", parsed.Text, text)
	}
	return pdu
}

func hexByte(n int) string {
	return strings.ToUpper(hex.EncodeToString([]byte{byte(n)}))
}

// fakeService stands in for deploy/translate, speaking the same HTTP API.
func fakeService(t *testing.T, reply string, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/translate" {
			t.Errorf("engine called at %s", r.URL.Path)
		}
		var req struct {
			Q      string `json:"q"`
			Source string `json:"source"`
			Target string `json:"target"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Source != "uz" || req.Target != "en" {
			t.Errorf("engine asked for %s->%s", req.Source, req.Target)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": "model not loaded"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"translatedText": reply})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func labTranslator(t *testing.T, endpoint string) func(func(string, ...any)) *translate.Translator {
	t.Helper()
	return func(logf func(format string, args ...any)) *translate.Translator {
		eng, err := translate.NewLibreTranslate(endpoint, "", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return translate.New(eng, translate.Options{Log: logf})
	}
}

// The feature end to end over a real tty: an Uzbek SMS arrives at the serial
// driver, the daemon reads it, calls the engine and hands the channel both the
// original and the translation.
func TestUzbekSMSIsForwardedWithATranslationOverRealTTY(t *testing.T) {
	const english = "Happy 35th anniversary of Independence!"
	srv, calls := fakeService(t, english, http.StatusOK)

	l := startLabWith(t, labOptions{translator: labTranslator(t, srv.URL)})
	l.sim.AddMessage(1, ucs2Deliver(t, uzbekSMS))

	if !waitUntil(func() bool { return l.ch.count() > 0 }, 20*time.Second) {
		t.Fatalf("nothing was forwarded; log=%v", l.log.all())
	}
	fwd := l.ch.sent[0]
	if fwd.Text != uzbekSMS {
		t.Errorf("the original was altered:\n got %q\nwant %q", fwd.Text, uzbekSMS)
	}
	if !fwd.Translation.Present() {
		t.Fatalf("no translation attached; log=%v", l.log.all())
	}
	if fwd.Translation.Text != english {
		t.Errorf("translation = %q", fwd.Translation.Text)
	}
	if fwd.Translation.Source != "uz" || fwd.Translation.Target != "en" {
		t.Errorf("languages = %s->%s", fwd.Translation.Source, fwd.Translation.Target)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("engine called %d times for one message", got)
	}
}

// The guarantee that matters most: the machine running the engine is allowed
// to be asleep, and an SMS still arrives.
func TestADeadEngineStillDeliversOverRealTTY(t *testing.T) {
	srv, _ := fakeService(t, "", http.StatusServiceUnavailable)

	l := startLabWith(t, labOptions{translator: labTranslator(t, srv.URL)})
	l.sim.AddMessage(1, ucs2Deliver(t, uzbekSMS))

	if !waitUntil(func() bool { return l.ch.count() > 0 }, 20*time.Second) {
		t.Fatalf("a failing engine stopped delivery; log=%v", l.log.all())
	}
	fwd := l.ch.sent[0]
	if fwd.Text != uzbekSMS {
		t.Errorf("body = %q", fwd.Text)
	}
	if fwd.Translation.Present() {
		t.Error("claimed a translation the engine never gave")
	}
	var sawOutage bool
	for _, line := range l.log.all() {
		if strings.Contains(line, "translate_unavailable") {
			sawOutage = true
		}
		if strings.Contains(line, "Mustaqillikning") {
			t.Errorf("the body reached the journal: %q", line)
		}
	}
	if !sawOutage {
		t.Errorf("the outage was not reported; log=%v", l.log.all())
	}
}

// An endpoint with nothing behind it at all -- the common case when the engine
// host is off -- must behave the same way.
func TestAnAbsentEngineStillDeliversOverRealTTY(t *testing.T) {
	// Port 1 on loopback refuses immediately rather than hanging.
	l := startLabWith(t, labOptions{translator: labTranslator(t, "http://127.0.0.1:1")})
	l.sim.AddMessage(1, ucs2Deliver(t, uzbekSMS))

	if !waitUntil(func() bool { return l.ch.count() > 0 }, 20*time.Second) {
		t.Fatalf("an unreachable engine stopped delivery; log=%v", l.log.all())
	}
	if l.ch.sent[0].Translation.Present() {
		t.Error("claimed a translation with no engine running")
	}
}

// Transliterated Russian is most of this traffic, and the engine must never be
// asked about it: decoded as Uzbek it comes back as nonsense around a real
// one-time code.
func TestARussianSMSNeverReachesTheEngineOverRealTTY(t *testing.T) {
	srv, calls := fakeService(t, "should not happen", http.StatusOK)

	l := startLabWith(t, labOptions{translator: labTranslator(t, srv.URL)})
	l.sim.AddMessage(1, ucs2Deliver(t, russianSMS))

	if !waitUntil(func() bool { return l.ch.count() > 0 }, 20*time.Second) {
		t.Fatalf("nothing was forwarded; log=%v", l.log.all())
	}
	if l.ch.sent[0].Translation.Present() {
		t.Errorf("translated a Russian message: %q", l.ch.sent[0].Translation.Text)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("engine was called %d times for a Russian message", got)
	}
}
