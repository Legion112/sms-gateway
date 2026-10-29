package translate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine records what it was asked and answers however the test wants.
type fakeEngine struct {
	mu    sync.Mutex
	calls []string
	reply func(text string) (string, error)
}

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) Translate(ctx context.Context, text, source, target string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s->%s:%s", source, target, text))
	f.mu.Unlock()
	if f.reply != nil {
		return f.reply(text)
	}
	return "TRANSLATED:" + text, nil
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const uzbekBody = "Is gazi - ko'rinmas xavf! Isitish pechlari, dudburonlar va shamollatish tizimlarini muntazam tekshiring."

func TestUzbekIsTranslated(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{})
	res, ok := tr.Annotate(context.Background(), uzbekBody)
	if !ok {
		t.Fatal("want a translation")
	}
	if res.Source != Uzbek || res.Target != English {
		t.Errorf("got %s->%s, want uz->en", res.Source, res.Target)
	}
	if res.Engine != "fake" {
		t.Errorf("engine = %q", res.Engine)
	}
	if !strings.HasPrefix(res.Text, "TRANSLATED:") {
		t.Errorf("text = %q", res.Text)
	}
}

// The point of the source list: these SIMs receive more Russian than Uzbek, and
// the recipient reads Russian. Translating it would add a paragraph of noise to
// every message and spend the engine's time on it.
func TestLanguagesTheRecipientReadsAreLeftAlone(t *testing.T) {
	for _, body := range []string{
		"Demobank: data plateja 29 October 2026 08:25:33 summa 30.00 USD s karti ***0002. Kod podtverjdeniya: 100001",
		"Welcome to the MTS network! Internet and SMS are temporarily blocked for 24 hours. To use the Internet tap the link.",
		"Добро пожаловать в Россию! В целях безопасности интернет и SMS ограничены на 24 часа.",
	} {
		eng := &fakeEngine{}
		if _, ok := New(eng, Options{}).Annotate(context.Background(), body); ok {
			t.Errorf("translated %.40q", body)
		}
		if eng.count() != 0 {
			t.Errorf("%.40q reached the engine", body)
		}
	}
}

func TestUndecidedLanguageIsNotGuessed(t *testing.T) {
	eng := &fakeEngine{}
	if _, ok := New(eng, Options{}).Annotate(context.Background(), "*102#."); ok {
		t.Error("translated a fragment with no language in it")
	}
	if eng.count() != 0 {
		t.Error("fragment reached the engine")
	}
}

func TestSourcesAreConfigurable(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{Sources: []string{"ru"}, Target: "en"})
	if _, ok := tr.Annotate(context.Background(), uzbekBody); ok {
		t.Error("uz translated when only ru was configured")
	}
	russian := "Demobank: data plateja 29 October 2026 08:25:33 summa 30.00 USD s karti ***0002. Kod podtverjdeniya: 100001"
	if _, ok := tr.Annotate(context.Background(), russian); !ok {
		t.Error("ru not translated when it was configured")
	}
}

func TestTargetLanguageReachesTheEngine(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{Target: "RU"})
	res, ok := tr.Annotate(context.Background(), uzbekBody)
	if !ok {
		t.Fatal("want a translation")
	}
	if res.Target != "ru" {
		t.Errorf("target = %q, want ru", res.Target)
	}
	if !strings.HasPrefix(eng.calls[0], "uz->ru:") {
		t.Errorf("engine asked for %q", eng.calls[0])
	}
}

// A message in the target language already needs nothing done to it, whatever
// the source list says.
func TestTargetLanguageIsNeverTranslatedIntoItself(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{Sources: []string{"uz"}, Target: "uz"})
	if _, ok := tr.Annotate(context.Background(), uzbekBody); ok {
		t.Error("translated uz into uz")
	}
}

// Civil-defence advisories arrive verbatim over and over, and both modems see
// the same operator broadcast.
func TestRepeatedBroadcastHitsTheEngineOnce(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{})
	for i := 0; i < 5; i++ {
		if _, ok := tr.Annotate(context.Background(), uzbekBody); !ok {
			t.Fatalf("call %d returned nothing", i)
		}
	}
	if eng.count() != 1 {
		t.Errorf("engine called %d times, want 1", eng.count())
	}
}

func TestCacheEvictsTheOldest(t *testing.T) {
	c := newCache(2)
	c.put("uz", "en", "a", "A")
	c.put("uz", "en", "b", "B")
	c.put("uz", "en", "c", "C")
	if _, ok := c.get("uz", "en", "a"); ok {
		t.Error("a survived eviction")
	}
	for _, k := range []string{"b", "c"} {
		if _, ok := c.get("uz", "en", k); !ok {
			t.Errorf("%s was evicted early", k)
		}
	}
}

func TestCacheKeepsTargetsApart(t *testing.T) {
	c := newCache(4)
	c.put("uz", "en", "x", "english")
	if v, ok := c.get("uz", "ru", "x"); ok {
		t.Errorf("en entry answered an ru lookup with %q", v)
	}
}

func TestCacheCanBeDisabled(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{CacheSize: -1})
	for i := 0; i < 3; i++ {
		tr.Annotate(context.Background(), uzbekBody)
	}
	if eng.count() != 3 {
		t.Errorf("engine called %d times, want 3 with the cache off", eng.count())
	}
}

// The gateway's job is to deliver SMS. Every one of these is a reason a
// translation does not happen, and not one of them may become a reason a
// message does not.
func TestEngineFailureIsNotAnError(t *testing.T) {
	eng := &fakeEngine{reply: func(string) (string, error) { return "", errors.New("connection refused") }}
	if _, ok := New(eng, Options{}).Annotate(context.Background(), uzbekBody); ok {
		t.Error("want no translation")
	}
}

// A translation is spent on the delivery path, so the engine is handed a
// context that expires. This asserts the budget reaches it, using an engine
// that honours cancellation the way a real HTTP one does.
func TestSlowEngineIsAbandoned(t *testing.T) {
	eng := &ctxEngine{delay: 2 * time.Second}
	tr := New(eng, Options{Timeout: 20 * time.Millisecond})
	start := time.Now()
	if _, ok := tr.Annotate(context.Background(), uzbekBody); ok {
		t.Error("want no translation from an engine that ran out of time")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %s on a 20ms budget", elapsed)
	}
}

// ctxEngine answers only if its context outlives the delay.
type ctxEngine struct{ delay time.Duration }

func (c *ctxEngine) Name() string { return "ctx" }

func (c *ctxEngine) Translate(ctx context.Context, text, source, target string) (string, error) {
	select {
	case <-time.After(c.delay):
		return "late", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestDeadlineReachesTheEngine(t *testing.T) {
	var budget time.Duration
	eng := &fakeEngine{}
	eng.reply = func(string) (string, error) { return "x", nil }
	tr := New(eng, Options{Timeout: 7 * time.Second})
	tr.engine = &deadlineProbe{inner: eng, seen: &budget}
	tr.Annotate(context.Background(), uzbekBody)
	if budget < 6*time.Second || budget > 7*time.Second {
		t.Errorf("engine saw a %s budget, want about 7s", budget)
	}
}

type deadlineProbe struct {
	inner Engine
	seen  *time.Duration
}

func (d *deadlineProbe) Name() string { return d.inner.Name() }

func (d *deadlineProbe) Translate(ctx context.Context, text, source, target string) (string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		*d.seen = time.Until(deadline)
	}
	return d.inner.Translate(ctx, text, source, target)
}

// An engine that hands back its input has translated nothing, and showing the
// message twice under a "translation" heading claims otherwise.
func TestEchoIsNotATranslation(t *testing.T) {
	eng := &fakeEngine{reply: func(text string) (string, error) { return "  " + text + "  ", nil }}
	if _, ok := New(eng, Options{}).Annotate(context.Background(), uzbekBody); ok {
		t.Error("want no translation for an echo")
	}
}

func TestEmptyAnswerIsNotATranslation(t *testing.T) {
	eng := &fakeEngine{reply: func(string) (string, error) { return "   ", nil }}
	if _, ok := New(eng, Options{}).Annotate(context.Background(), uzbekBody); ok {
		t.Error("want no translation for a blank answer")
	}
}

func TestNilTranslatorIsOff(t *testing.T) {
	var tr *Translator
	if _, ok := tr.Annotate(context.Background(), uzbekBody); ok {
		t.Error("nil translator produced a translation")
	}
	if tr.Target() != "" {
		t.Error("nil translator claimed a target")
	}
	if New(nil, Options{}) != nil {
		t.Error("New with no engine should be off")
	}
}

func TestBlankBodyIsSkipped(t *testing.T) {
	eng := &fakeEngine{}
	if _, ok := New(eng, Options{}).Annotate(context.Background(), "   \n "); ok {
		t.Error("translated whitespace")
	}
	if eng.count() != 0 {
		t.Error("whitespace reached the engine")
	}
}

// An engine that is down is a steady state. Logging it per message would bury
// the journal, and never logging it would hide the outage.
func TestOutageIsLoggedOnceAndRecoveryIsReported(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
		fail  = true
	)
	eng := &fakeEngine{reply: func(text string) (string, error) {
		mu.Lock()
		down := fail
		mu.Unlock()
		if down {
			return "", errors.New("connection refused")
		}
		return "ok: " + text, nil
	}}
	tr := New(eng, Options{CacheSize: -1, Log: func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}})

	for i := 0; i < 4; i++ {
		tr.Annotate(context.Background(), uzbekBody)
	}
	mu.Lock()
	downLines := len(lines)
	fail = false
	mu.Unlock()
	if downLines != 1 {
		t.Fatalf("logged %d lines for a steady outage, want 1: %v", downLines, lines)
	}
	if !strings.Contains(lines[0], "translate_unavailable") {
		t.Errorf("outage line = %q", lines[0])
	}

	tr.Annotate(context.Background(), uzbekBody)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 {
		t.Fatalf("recovery logged %d lines: %v", len(lines)-1, lines)
	}
	if !strings.Contains(lines[1], "translate_available") {
		t.Errorf("recovery line = %q", lines[1])
	}
}

// The journal carries the languages, never the body: these messages hold
// one-time codes and card numbers.
func TestTheBodyIsNeverLogged(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)
	eng := &fakeEngine{reply: func(string) (string, error) { return "", errors.New("boom: " + uzbekBody) }}
	tr := New(eng, Options{Log: func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}})
	tr.Annotate(context.Background(), uzbekBody)
	mu.Lock()
	defer mu.Unlock()
	for _, line := range lines {
		if strings.Contains(line, "Isitish pechlari") {
			t.Errorf("message body reached the log: %q", line)
		}
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	eng := &fakeEngine{}
	tr := New(eng, Options{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr.Annotate(context.Background(), fmt.Sprintf("%s %d", uzbekBody, i))
		}(i)
	}
	wg.Wait()
}
