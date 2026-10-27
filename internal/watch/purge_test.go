package watch

import (
	"context"
	"fmt"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
)

// fakeDeleter records the modem storage slots it was asked to free.
type fakeDeleter struct {
	deleted []string
	fail    bool
}

func (f *fakeDeleter) Ping(context.Context) (modem.PingResult, error) {
	return modem.PingResult{}, nil
}
func (f *fakeDeleter) SMSStatus(context.Context) (modem.SMSStatus, error) {
	return modem.SMSStatus{}, nil
}
func (f *fakeDeleter) ListMessages(context.Context) ([]modem.Message, error) { return nil, nil }
func (f *fakeDeleter) SendMessage(context.Context, string, string) (modem.SendResult, error) {
	return modem.SendResult{}, nil
}
func (f *fakeDeleter) Close() error { return nil }
func (f *fakeDeleter) DeleteMessage(_ context.Context, id string) error {
	if f.fail {
		return fmt.Errorf("modem returned +CMS ERROR: 321")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type failingChannel struct{}

func (failingChannel) Name() string                           { return "bad" }
func (failingChannel) Driver() string                         { return "bad" }
func (failingChannel) Ping(context.Context) error             { return nil }
func (failingChannel) SendTest(context.Context, string) error { return nil }
func (failingChannel) Close() error                           { return nil }
func (failingChannel) Forward(context.Context, forward.InboundSMS) error {
	return fmt.Errorf("telegram unreachable")
}

func newPurgeService(t *testing.T, ch forward.Channel) (*Service, *storage.Store) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}},
	}
	cfg.Watch.DeleteAfterForward = true
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})
	return svc, store
}

func TestPurgeFreesSlotAfterForward(t *testing.T) {
	svc, _ := newPurgeService(t, &mockChannel{})
	del := &fakeDeleter{}

	parts := []modem.Part{
		{ID: "12", From: "UZCARD", State: "received (read)", Timestamp: "26/10/27,08:43:05+00", Text: "hello"},
	}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "12" {
		t.Fatalf("deleted=%v want [12]", del.deleted)
	}
}

func TestPurgeSkippedWhenDeliveryFails(t *testing.T) {
	svc, _ := newPurgeService(t, failingChannel{})
	del := &fakeDeleter{}

	parts := []modem.Part{
		{ID: "12", From: "UZCARD", State: "received (read)", Timestamp: "26/10/27,08:43:05+00", Text: "hello"},
	}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted) != 0 {
		t.Fatalf("deleted=%v want none: an undelivered SMS must stay on the modem", del.deleted)
	}
}

// A backlog forwarded by an earlier run has delivery rows but still occupies a slot.
func TestPurgeDrainsAlreadyForwardedBacklog(t *testing.T) {
	svc, _ := newPurgeService(t, &mockChannel{})
	parts := []modem.Part{
		{ID: "12", From: "UZCARD", State: "received (read)", Timestamp: "26/10/27,08:43:05+00", Text: "hello"},
	}
	// First pass without a deleter: forwarded, slot left in place.
	if err := svc.ingestParts(context.Background(), "m1", nil, parts); err != nil {
		t.Fatal(err)
	}

	del := &fakeDeleter{}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "12" {
		t.Fatalf("deleted=%v want [12]", del.deleted)
	}
}

func TestPurgeFreesEveryConcatSegment(t *testing.T) {
	svc, _ := newPurgeService(t, &mockChannel{})
	del := &fakeDeleter{}
	ts := "26/10/27,08:43:05+00"

	parts := []modem.Part{
		{ID: "39", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "first half ", ConcatRef: 7, Part: 1, PartTotal: 2},
		{ID: "40", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "second half", ConcatRef: 7, Part: 2, PartTotal: 2},
	}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted) != 2 {
		t.Fatalf("deleted=%v want both segments", del.deleted)
	}
}

// A segment re-listed after its group was merged takes an early return in
// ingestPart, so it needs purging on that path too or it occupies a slot forever.
func TestPurgeFreesSegmentOfAlreadyMergedGroup(t *testing.T) {
	svc, _ := newPurgeService(t, &mockChannel{})
	ts := "26/10/27,08:43:05+00"
	parts := []modem.Part{
		{ID: "39", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "first half ", ConcatRef: 7, Part: 1, PartTotal: 2},
		{ID: "40", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "second half", ConcatRef: 7, Part: 2, PartTotal: 2},
	}
	if err := svc.ingestParts(context.Background(), "m1", nil, parts); err != nil {
		t.Fatal(err)
	}

	del := &fakeDeleter{}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatal(err)
	}
	if len(del.deleted) != 2 {
		t.Fatalf("deleted=%v want both segments freed on re-list", del.deleted)
	}
}

func TestPurgeFailureDoesNotFailIngest(t *testing.T) {
	svc, store := newPurgeService(t, &mockChannel{})
	del := &fakeDeleter{fail: true}

	parts := []modem.Part{
		{ID: "12", From: "UZCARD", State: "received (read)", Timestamp: "26/10/27,08:43:05+00", Text: "hello"},
	}
	if err := svc.ingestParts(context.Background(), "m1", del, parts); err != nil {
		t.Fatalf("a failed delete must not fail ingest: %v", err)
	}
	id := storage.MessageKey("m1", "12", "26/10/27,08:43:05+00")
	done, err := store.IsFullyDelivered(id)
	if err != nil || !done {
		t.Fatalf("delivery should still be recorded: done=%v err=%v", done, err)
	}
}

func TestSlotDeleterDisabledByConfig(t *testing.T) {
	svc, _ := newPurgeService(t, &mockChannel{})
	svc.cfg.Watch.DeleteAfterForward = false
	if got := svc.slotDeleter("m1", config.ModemEntry{Driver: config.DriverSerial}, &fakeDeleter{}); got != nil {
		t.Fatal("slotDeleter must be nil while delete_after_forward is off")
	}
	svc.cfg.Watch.DeleteAfterForward = true
	if got := svc.slotDeleter("m1", config.ModemEntry{Driver: config.DriverSerial}, &fakeDeleter{}); got == nil {
		t.Fatal("slotDeleter must be set while delete_after_forward is on")
	}
}

// flakyChannel fails until fail is cleared, counting successful forwards.
type flakyChannel struct {
	fail     bool
	forwards int
}

func (f *flakyChannel) Name() string                           { return "flaky" }
func (f *flakyChannel) Driver() string                         { return "flaky" }
func (f *flakyChannel) Ping(context.Context) error             { return nil }
func (f *flakyChannel) SendTest(context.Context, string) error { return nil }
func (f *flakyChannel) Close() error                           { return nil }
func (f *flakyChannel) Forward(context.Context, forward.InboundSMS) error {
	if f.fail {
		return fmt.Errorf("telegram unreachable")
	}
	f.forwards++
	return nil
}

func concatSegments(ts string) []modem.Part {
	return []modem.Part{
		{ID: "182", From: "hambi", State: "received (read)", Timestamp: ts, Text: "first half ", ConcatRef: 53, Part: 1, PartTotal: 2},
		{ID: "184", From: "hambi", State: "received (read)", Timestamp: ts, Text: "second half", ConcatRef: 53, Part: 2, PartTotal: 2},
	}
}

// Regression: a merged concat message whose forward failed used to be skipped
// on every later poll, so it was never retried and held its slots forever.
func TestMergedConcatRetriesFailedDelivery(t *testing.T) {
	ch := &flakyChannel{fail: true}
	svc, _ := newPurgeService(t, ch)
	del := &fakeDeleter{}
	ts := "26/09/30,16:33:21+00"

	if err := svc.ingestParts(context.Background(), "m1", del, concatSegments(ts)); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 0 {
		t.Fatalf("forwards=%d want 0 while the channel is down", ch.forwards)
	}
	if len(del.deleted) != 0 {
		t.Fatalf("deleted=%v want none: an undelivered message must keep its slots", del.deleted)
	}

	// Channel recovers: the next poll re-lists the same segments.
	ch.fail = false
	if err := svc.ingestParts(context.Background(), "m1", del, concatSegments(ts)); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("forwards=%d want exactly 1 retry", ch.forwards)
	}
	if len(del.deleted) != 2 {
		t.Fatalf("deleted=%v want both slots freed once delivered", del.deleted)
	}
}

func TestMergedConcatDeliveredIsNotForwardedTwice(t *testing.T) {
	ch := &flakyChannel{}
	svc, _ := newPurgeService(t, ch)
	ts := "26/09/30,16:33:21+00"

	if err := svc.ingestParts(context.Background(), "m1", nil, concatSegments(ts)); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("forwards=%d want 1", ch.forwards)
	}
	// Re-listed before anything purged the slots.
	if err := svc.ingestParts(context.Background(), "m1", nil, concatSegments(ts)); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("forwards=%d want still 1: a delivered message must not be re-sent", ch.forwards)
	}
}

// A merged message that matched no rule has no pending delivery to retry, so it
// must not be re-ingested on every poll.
func TestMergedConcatWithoutRuleIsNotRetried(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &flakyChannel{}
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	cfg.Watch.DeleteAfterForward = true
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   forward.NewRouter([]config.ForwardRule{{Name: "other", From: "+100", To: []string{"tg"}}}),
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	del := &fakeDeleter{}
	ts := "26/09/30,16:33:21+00"
	for i := 0; i < 3; i++ {
		if err := svc.ingestParts(context.Background(), "m1", del, concatSegments(ts)); err != nil {
			t.Fatal(err)
		}
	}
	if ch.forwards != 0 {
		t.Fatalf("forwards=%d want 0: no rule matches", ch.forwards)
	}
	if len(del.deleted) != 0 {
		t.Fatalf("deleted=%v want none: an unrouted message must keep its slots", del.deleted)
	}
}
