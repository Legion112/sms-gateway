package watch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
)

// fakeModem is one connection to a modem. Each reconnect attempt in a test gets
// a fresh one, which mirrors the real driver: the supervisor reopens the device
// rather than reusing a dead handle.
type fakeModem struct {
	mu sync.Mutex

	// parts is handed to the watch handler once when the loop starts.
	parts []modem.Part
	// watchErr ends the watch loop after parts are delivered. nil means the
	// loop stays up until the context is cancelled, like a healthy modem.
	watchErr error
	// listErr fails ListMessageParts, which is what catch-up calls.
	listErr error

	listCalls  int
	watchCalls int
	closed     int
}

func (f *fakeModem) Ping(context.Context) (modem.PingResult, error) {
	return modem.PingResult{Driver: modem.DriverSerial, Detail: "OK"}, nil
}

func (f *fakeModem) SMSStatus(context.Context) (modem.SMSStatus, error) {
	return modem.SMSStatus{Driver: modem.DriverSerial, ICCID: "8999", SMSReady: true}, nil
}

func (f *fakeModem) ListMessages(context.Context) ([]modem.Message, error) {
	return nil, nil
}

func (f *fakeModem) SendMessage(context.Context, string, string) (modem.SendResult, error) {
	return modem.SendResult{}, fmt.Errorf("fake modem cannot send")
}

func (f *fakeModem) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

// ListMessageParts makes the fake a modem.PartLister, the interface catch-up uses.
func (f *fakeModem) ListMessageParts(context.Context) ([]modem.Part, error) {
	f.mu.Lock()
	f.listCalls++
	err := f.listErr
	parts := f.parts
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return parts, nil
}

// WatchIncomingParts makes the fake satisfy serialPartsIncoming, so the service
// drives it exactly as it drives the real serial driver.
func (f *fakeModem) WatchIncomingParts(ctx context.Context, _ time.Duration, handler serial.PartsHandler) error {
	f.mu.Lock()
	f.watchCalls++
	parts := f.parts
	watchErr := f.watchErr
	f.mu.Unlock()

	if len(parts) > 0 {
		if err := handler(ctx, parts); err != nil {
			return err
		}
	}
	if watchErr != nil {
		return watchErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeModem) counts() (list, watch, closed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls, f.watchCalls, f.closed
}

// scriptedOpener hands out one outcome per open attempt. Attempts past the end
// of the script repeat the last entry, so "fails forever" needs one entry.
type scriptedOpener struct {
	mu       sync.Mutex
	script   []func() (modem.Modem, error)
	attempts int
	opened   []*fakeModem
}

func (o *scriptedOpener) open(config.ModemEntry) (modem.Modem, error) {
	o.mu.Lock()
	idx := o.attempts
	o.attempts++
	if idx >= len(o.script) {
		idx = len(o.script) - 1
	}
	step := o.script[idx]
	o.mu.Unlock()

	m, err := step()
	if fm, ok := m.(*fakeModem); ok && fm != nil {
		o.mu.Lock()
		o.opened = append(o.opened, fm)
		o.mu.Unlock()
	}
	return m, err
}

func (o *scriptedOpener) attemptCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.attempts
}

// opensFailing returns a script step that fails to open, as a missing device node does.
func opensFailing(msg string) func() (modem.Modem, error) {
	return func() (modem.Modem, error) { return nil, fmt.Errorf("%s", msg) }
}

// opensAs returns a script step yielding the given connection.
func opensAs(m *fakeModem) func() (modem.Modem, error) {
	return func() (modem.Modem, error) { return m, nil }
}

// testClock makes short reconnect delays instant so tests do not wait in real
// time, while long waits (the health supervisor's tick) block on the context so
// Run never spins. After maxFast instant sleeps it blocks too, which bounds a
// test whose modem can never be opened.
type testClock struct {
	mu        sync.Mutex
	delays    []time.Duration
	fastUnder time.Duration
	maxFast   int
	used      int
	now       time.Time
}

func newTestClock(maxFast int) *testClock {
	return &testClock{
		fastUnder: time.Second,
		maxFast:   maxFast,
		now:       time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}
}

func (c *testClock) sleep(ctx context.Context, d time.Duration) bool {
	c.mu.Lock()
	tooLong := d >= c.fastUnder
	exhausted := c.used >= c.maxFast
	if !tooLong && !exhausted {
		c.used++
		c.delays = append(c.delays, d)
		c.now = c.now.Add(d)
	}
	c.mu.Unlock()

	if tooLong || exhausted {
		<-ctx.Done()
		return false
	}
	return ctx.Err() == nil
}

func (c *testClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.delays...)
}

func (c *testClock) timeNow() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// countingChannel records every forwarded message.
type countingChannel struct {
	mu   sync.Mutex
	sent []forward.InboundSMS
}

func (c *countingChannel) Name() string                           { return "tg" }
func (c *countingChannel) Driver() string                         { return "mock" }
func (c *countingChannel) Ping(context.Context) error             { return nil }
func (c *countingChannel) SendTest(context.Context, string) error { return nil }
func (c *countingChannel) Close() error                           { return nil }

func (c *countingChannel) Forward(_ context.Context, msg forward.InboundSMS) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *countingChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// recordingLog collects log lines so tests can assert on observable behaviour.
type recordingLog struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingLog) logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *recordingLog) contains(substr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func (r *recordingLog) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// livenessFixture wires a service around fakes with every timing seam under test control.
type livenessFixture struct {
	svc   *Service
	store *storage.Store
	ch    *countingChannel
	log   *recordingLog
	clock *testClock
}

func newLivenessFixture(t interface{ Fatal(...any) }, modems map[string]config.ModemEntry, opener func(config.ModemEntry) (modem.Modem, error), clock *testClock) *livenessFixture {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ch := &countingChannel{}
	rec := &recordingLog{}

	cfg := config.Config{Modems: modems}
	cfg.Watch.ReconnectInitial = 10 * time.Millisecond
	cfg.Watch.ReconnectMax = 80 * time.Millisecond

	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   forward.NewRouter([]config.ForwardRule{{Name: "all", From: "*", To: []string{"tg"}}}),
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      rec.logf,
	})
	svc.openFn = opener
	svc.sleepFn = clock.sleep
	svc.nowFn = clock.timeNow

	return &livenessFixture{svc: svc, store: store, ch: ch, log: rec, clock: clock}
}

func (f *livenessFixture) close() { f.store.Close() }

// waitFor polls cond until it holds or the deadline passes.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func inboundPart(id, from, text, ts string) modem.Part {
	return modem.Part{ID: id, From: from, Text: text, Timestamp: ts, State: "received"}
}
