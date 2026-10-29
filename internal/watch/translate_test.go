package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
	"github.com/legion/sms-gateway/internal/translate"
)

// capturingChannel keeps the payload it was handed, so a test can see what the
// recipient would have received.
type capturingChannel struct {
	got []forward.InboundSMS
}

func (c *capturingChannel) Name() string                           { return "cap" }
func (c *capturingChannel) Driver() string                         { return "cap" }
func (c *capturingChannel) Ping(context.Context) error             { return nil }
func (c *capturingChannel) SendText(context.Context, string) error { return nil }
func (c *capturingChannel) Close() error                           { return nil }
func (c *capturingChannel) Forward(_ context.Context, msg forward.InboundSMS) error {
	c.got = append(c.got, msg)
	return nil
}

type scriptedEngine struct {
	calls int
	reply func(text string) (string, error)
}

func (s *scriptedEngine) Name() string { return "scripted" }

func (s *scriptedEngine) Translate(_ context.Context, text, _, _ string) (string, error) {
	s.calls++
	return s.reply(text)
}

func newTranslateService(t *testing.T, tr *translate.Translator) (*Service, *capturingChannel, []string) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ch := &capturingChannel{}
	var logs []string
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	svc := NewService(cfg, Dependencies{
		Store:      store,
		Router:     forward.NewRouter([]config.ForwardRule{{Name: "all", From: "*", To: []string{"tg"}}}),
		Channels:   map[string]forward.Channel{"tg": ch},
		Log:        func(format string, args ...any) { logs = append(logs, format) },
		Translator: tr,
	})
	return svc, ch, logs
}

const psa = "Is gazi - ko'rinmas xavf! Isitish pechlari, dudburonlar va shamollatish tizimlarini muntazam tekshiring."

func ingestOne(t *testing.T, svc *Service, body string) {
	t.Helper()
	msg := modem.Message{ID: "1", From: "FVV", Text: body, Timestamp: "26/10/29,18:05:00+00"}
	if err := svc.ingest(context.Background(), "m1", msg, "", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestForwardedMessageCarriesTheTranslation(t *testing.T) {
	eng := &scriptedEngine{reply: func(string) (string, error) { return "Flue gas is an invisible hazard!", nil }}
	svc, ch, _ := newTranslateService(t, translate.New(eng, translate.Options{}))
	ingestOne(t, svc, psa)

	if len(ch.got) != 1 {
		t.Fatalf("forwarded %d messages", len(ch.got))
	}
	fwd := ch.got[0]
	// The original is untouched. It is what the recipient reads a code or an
	// amount off, so the translation is strictly an addition.
	if fwd.Text != psa {
		t.Errorf("original was altered:\n%q", fwd.Text)
	}
	if !fwd.Translation.Present() {
		t.Fatal("no translation attached")
	}
	if fwd.Translation.Text != "Flue gas is an invisible hazard!" {
		t.Errorf("translation = %q", fwd.Translation.Text)
	}
	if fwd.Translation.Source != "uz" || fwd.Translation.Target != "en" {
		t.Errorf("languages = %s->%s", fwd.Translation.Source, fwd.Translation.Target)
	}
}

// The whole point of the design: the gateway delivers SMS, and the translation
// service is allowed to be absent, which it will be whenever the machine
// running it is asleep.
func TestAnUnreachableEngineStillDelivers(t *testing.T) {
	eng := &scriptedEngine{reply: func(string) (string, error) {
		return "", errors.New("dial tcp 192.0.2.10:5000: connect: connection refused")
	}}
	svc, ch, _ := newTranslateService(t, translate.New(eng, translate.Options{}))
	ingestOne(t, svc, psa)

	if len(ch.got) != 1 {
		t.Fatalf("forwarded %d messages, want 1", len(ch.got))
	}
	if ch.got[0].Text != psa {
		t.Error("the original did not survive a failed translation")
	}
	if ch.got[0].Translation.Present() {
		t.Error("claimed a translation it never got")
	}
}

func TestTranslationOffStillDelivers(t *testing.T) {
	svc, ch, _ := newTranslateService(t, nil)
	ingestOne(t, svc, psa)
	if len(ch.got) != 1 {
		t.Fatalf("forwarded %d messages", len(ch.got))
	}
	if ch.got[0].Translation.Present() {
		t.Error("a nil translator produced a translation")
	}
}

// Transliterated Russian is most of this traffic and the recipient reads it.
func TestARussianMessageIsForwardedUntranslated(t *testing.T) {
	eng := &scriptedEngine{reply: func(string) (string, error) { return "should not be called", nil }}
	svc, ch, _ := newTranslateService(t, translate.New(eng, translate.Options{}))
	ingestOne(t, svc, "Demobank: data plateja 29 October 2026 08:25:33 summa 30.00 USD s karti ***0002. Kod podtverjdeniya: 100001")

	if eng.calls != 0 {
		t.Errorf("engine was called %d times for a Russian message", eng.calls)
	}
	if ch.got[0].Translation.Present() {
		t.Error("attached a translation to a Russian message")
	}
}

// Two channels on one rule must not mean two trips to the engine.
func TestTranslationHappensOncePerMessage(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := &scriptedEngine{reply: func(string) (string, error) { return "translated", nil }}
	a, b := &capturingChannel{}, &capturingChannel{}
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	svc := NewService(cfg, Dependencies{
		Store:      store,
		Router:     forward.NewRouter([]config.ForwardRule{{Name: "both", From: "*", To: []string{"a", "b"}}}),
		Channels:   map[string]forward.Channel{"a": a, "b": b},
		Log:        func(string, ...any) {},
		Translator: translate.New(eng, translate.Options{CacheSize: -1}),
	})
	ingestOne(t, svc, psa)

	if eng.calls != 1 {
		t.Errorf("engine called %d times for one message, want 1", eng.calls)
	}
	if !a.got[0].Translation.Present() || !b.got[0].Translation.Present() {
		t.Error("both channels should carry the same translation")
	}
}

// A message no rule wants is never delivered, so translating it would spend
// the engine's time on nothing.
func TestAnUnroutedMessageIsNotTranslated(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	eng := &scriptedEngine{reply: func(string) (string, error) { return "translated", nil }}
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	svc := NewService(cfg, Dependencies{
		Store:      store,
		Router:     forward.NewRouter([]config.ForwardRule{{Name: "none", From: "+99999", To: []string{"tg"}}}),
		Channels:   map[string]forward.Channel{"tg": &capturingChannel{}},
		Log:        func(string, ...any) {},
		Translator: translate.New(eng, translate.Options{}),
	})
	ingestOne(t, svc, psa)

	if eng.calls != 0 {
		t.Errorf("engine called %d times for a message nothing wanted", eng.calls)
	}
}

// The journal gets the languages; the body carries one-time codes.
func TestTheJournalDoesNotCarryTheBody(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var lines []string
	eng := &scriptedEngine{reply: func(string) (string, error) { return "Flue gas is an invisible hazard!", nil }}
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   forward.NewRouter([]config.ForwardRule{{Name: "all", From: "*", To: []string{"tg"}}}),
		Channels: map[string]forward.Channel{"tg": &capturingChannel{}},
		Log: func(format string, args ...any) {
			lines = append(lines, fmt.Sprintf(format, args...))
		},
		Translator: translate.New(eng, translate.Options{}),
	})
	ingestOne(t, svc, psa)

	var sawTranslated bool
	for _, line := range lines {
		if strings.HasPrefix(line, "translated ") {
			sawTranslated = true
		}
		if strings.Contains(line, "Isitish pechlari") {
			t.Errorf("body reached the journal: %q", line)
		}
	}
	if !sawTranslated {
		t.Errorf("no translated line in %v", lines)
	}
}
