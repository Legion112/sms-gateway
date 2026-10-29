// Package translate adds a reading of an inbound SMS in a language the
// recipient understands, without ever standing between a message and its
// destination.
//
// Two constraints shaped everything here. The first is that the gateway's job
// is to deliver SMS: a translation that is slow, wrong or unreachable must cost
// nothing but itself, so every failure in this package ends in the original
// message being forwarded exactly as it would have been. The second is that the
// text must not leave hardware the operator controls, which rules out every
// hosted translation API and leaves a service on the local network -- so the
// engine is reached over HTTP and is expected to be absent half the time.
package translate

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Result is a translation the gateway can attach to a forwarded message.
type Result struct {
	Text   string // the translation
	Source string // the language it was read as
	Target string // the language it was written in
	Engine string // which engine answered, for the journal
}

// Engine turns text from one language into another.
//
// Implementations talk to a translation service the operator runs. Nothing in
// this package reaches a service the operator does not control, and no
// implementation here may send message text anywhere else.
type Engine interface {
	// Name identifies the engine in log lines.
	Name() string
	// Translate returns text rendered in target, or an error. Both languages
	// are ISO 639-1 codes as this package spells them.
	Translate(ctx context.Context, text, source, target string) (string, error)
}

// Options configure a Translator.
type Options struct {
	// Sources are the languages worth translating: the ones the recipient
	// cannot read. Empty means Uzbek alone, which is the case this was built
	// for. A message in any other language is forwarded untouched, because a
	// translation of something already readable is noise with a cost.
	Sources []string
	// Target is the language to translate into. Empty means English.
	Target string
	// Timeout bounds one translation. It is additive to the delivery timeout,
	// so it is deliberately short: a translation that has not arrived by then
	// is worth less than the message it is holding up.
	Timeout time.Duration
	// CacheSize is how many recent translations to remember. Operator
	// broadcasts repeat verbatim -- the same civil-defence advisory arrived
	// eleven times in this gateway's first months, and both modems see the
	// broadcast -- so a small cache removes most of the work and all of the
	// latency for them.
	CacheSize int
	// Log receives one line when the engine starts or stops failing. It is
	// never given message text: bodies carry one-time codes and card numbers,
	// and the journal is not the place for them.
	Log func(format string, args ...any)
}

const (
	defaultTarget    = English
	defaultTimeout   = 10 * time.Second
	defaultCacheSize = 256
)

// Translator decides whether a message needs translating and gets it done,
// or quietly decides that it does not.
type Translator struct {
	engine  Engine
	sources map[string]struct{}
	target  string
	timeout time.Duration
	cache   *cache
	log     func(format string, args ...any)

	// failed makes the error log edge-triggered. The engine being down is a
	// steady state that would otherwise print a line per message forever.
	mu     sync.Mutex
	failed bool
}

// New builds a Translator. A nil engine yields a nil Translator, which is a
// working "translation is switched off": every method tolerates it.
func New(engine Engine, opts Options) *Translator {
	if engine == nil {
		return nil
	}
	sources := make(map[string]struct{})
	for _, s := range opts.Sources {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			sources[s] = struct{}{}
		}
	}
	if len(sources) == 0 {
		sources[Uzbek] = struct{}{}
	}
	t := &Translator{
		engine:  engine,
		sources: sources,
		target:  strings.ToLower(strings.TrimSpace(opts.Target)),
		timeout: opts.Timeout,
		log:     opts.Log,
	}
	if t.target == "" {
		t.target = defaultTarget
	}
	if t.timeout <= 0 {
		t.timeout = defaultTimeout
	}
	size := opts.CacheSize
	if size == 0 {
		size = defaultCacheSize
	}
	t.cache = newCache(size)
	if t.log == nil {
		t.log = func(string, ...any) {}
	}
	return t
}

// Target reports the language this translator renders into.
func (t *Translator) Target() string {
	if t == nil {
		return ""
	}
	return t.target
}

// Annotate returns a translation of text, or reports that there is none to add.
//
// It does not return an error, and that is the whole point: the caller is on
// the path between a received SMS and its delivery, and there is no error here
// worth failing that path for. Everything that can go wrong -- an undetectable
// language, a language the recipient already reads, an engine that is switched
// off, unreachable, slow or broken -- comes back as "nothing to add".
func (t *Translator) Annotate(ctx context.Context, text string) (Result, bool) {
	if t == nil || strings.TrimSpace(text) == "" {
		return Result{}, false
	}
	source := Detect(text)
	if source == Unknown || source == t.target {
		return Result{}, false
	}
	if _, ok := t.sources[source]; !ok {
		return Result{}, false
	}

	if cached, ok := t.cache.get(source, t.target, text); ok {
		return Result{Text: cached, Source: source, Target: t.target, Engine: t.engine.Name()}, true
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	translated, err := t.engine.Translate(callCtx, text, source, t.target)
	cancel()
	if err != nil {
		t.recordFailure(err, text)
		return Result{}, false
	}
	t.recordSuccess()

	translated = strings.TrimSpace(translated)
	// An engine that echoes its input has not translated anything, and
	// printing the message twice under a "translation" heading would be a
	// claim the gateway cannot support.
	if translated == "" || translated == strings.TrimSpace(text) {
		return Result{}, false
	}
	t.cache.put(source, t.target, text, translated)
	return Result{Text: translated, Source: source, Target: t.target, Engine: t.engine.Name()}, true
}

func (t *Translator) recordFailure(err error, body string) {
	t.mu.Lock()
	first := !t.failed
	t.failed = true
	t.mu.Unlock()
	if first {
		t.log("translate_unavailable engine=%s error=%s", t.engine.Name(), scrubBody(err.Error(), body))
	}
}

func (t *Translator) recordSuccess() {
	t.mu.Lock()
	recovered := t.failed
	t.failed = false
	t.mu.Unlock()
	if recovered {
		t.log("translate_available engine=%s (answering again)", t.engine.Name())
	}
}

// cache holds recent translations, evicting the oldest once full.
//
// Insertion order rather than least-recently-used: the thing being cached is an
// operator broadcast that repeats within hours and then never again, so recency
// of use carries no information worth the bookkeeping.
type cache struct {
	limit int

	mu      sync.Mutex
	entries map[string]string
	order   []string
}

func newCache(limit int) *cache {
	if limit < 0 {
		limit = 0
	}
	return &cache{limit: limit, entries: make(map[string]string, limit)}
}

func cacheKey(source, target, text string) string {
	return source + "\x00" + target + "\x00" + text
}

func (c *cache) get(source, target, text string) (string, bool) {
	if c == nil || c.limit == 0 {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[cacheKey(source, target, text)]
	return v, ok
}

func (c *cache) put(source, target, text, translated string) {
	if c == nil || c.limit == 0 {
		return
	}
	key := cacheKey(source, target, text)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	if len(c.order) >= c.limit {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	c.entries[key] = translated
	c.order = append(c.order, key)
}
