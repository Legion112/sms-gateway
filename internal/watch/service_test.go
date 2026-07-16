package watch

import (
	"context"
	"testing"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
)

type mockChannel struct {
	forwards int
}

func (m *mockChannel) Name() string   { return "mock" }
func (m *mockChannel) Driver() string { return "mock" }
func (m *mockChannel) Ping(context.Context) error {
	return nil
}
func (m *mockChannel) Forward(context.Context, forward.InboundSMS) error {
	m.forwards++
	return nil
}
func (m *mockChannel) SendTest(context.Context, string) error { return nil }
func (m *mockChannel) Close() error                           { return nil }

func TestIngestReusesStorageIndexSameTimestamp(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &mockChannel{}
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
	}
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	ctx := context.Background()
	ts := "26/07/04,22:19:42+00"
	if err := svc.ingest(ctx, "m1", modem.Message{
		ID: "37", From: "+7999", Text: "old", Timestamp: ts, State: "received",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.ingest(ctx, "m1", modem.Message{
		ID: "37", From: "+7999", Text: "I hope this one is last", Timestamp: ts, State: "received",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 2 {
		t.Fatalf("forwards=%d want 2", ch.forwards)
	}
}

func TestIngestReusesStorageIndex(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &mockChannel{}
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
	}
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	ctx := context.Background()
	if err := svc.ingest(ctx, "m1", modem.Message{
		ID: "37", From: "+7999", Text: "old", Timestamp: "26/07/04,21:00:00+00", State: "received",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.ingest(ctx, "m1", modem.Message{
		ID: "37", From: "+7999", Text: "Another sms to me", Timestamp: "26/07/04,22:19:42+00", State: "received",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 2 {
		t.Fatalf("forwards=%d want 2", ch.forwards)
	}
}

func TestIngestDifferentModemsSameIndex(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &mockChannel{}
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{
			"m1": {Driver: config.DriverMM},
			"m2": {Driver: config.DriverMM},
		},
	}
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	ctx := context.Background()
	if err := svc.ingest(ctx, "m1", modem.Message{ID: "0", From: "+1", Text: "a", Timestamp: "t1", State: "received"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.ingest(ctx, "m2", modem.Message{ID: "0", From: "+2", Text: "b", Timestamp: "t2", State: "received"}, ""); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 2 {
		t.Fatalf("forwards=%d want 2", ch.forwards)
	}
}

func TestIngestForwardsOnce(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &mockChannel{}
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverMM}},
	}
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", Modem: "m1", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	msg := modem.Message{ID: "1", From: "+7900", Text: "hi", Timestamp: "2026-06-12T10:00:00Z", State: "received"}
	ctx := context.Background()

	if err := svc.ingest(ctx, "m1", msg, ""); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("forwards=%d want 1", ch.forwards)
	}
	if err := svc.ingest(ctx, "m1", msg, ""); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("duplicate ingest forwards=%d want 1", ch.forwards)
	}
}

func TestIngestPartsConcatSingleForward(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ch := &mockChannel{}
	cfg := config.Config{
		Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}},
	}
	router := forward.NewRouter([]config.ForwardRule{
		{Name: "all", From: "*", To: []string{"tg"}},
	})
	svc := NewService(cfg, Dependencies{
		Store:    store,
		Router:   router,
		Channels: map[string]forward.Channel{"tg": ch},
		Log:      func(string, ...any) {},
	})

	ctx := context.Background()
	ts := "26/07/16,11:21:07+00"
	parts := []modem.Part{
		{ID: "39", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "Balans popolnen na 10000.0 sumov. Balans: 12345.67890 sumov. Balans", ConcatRef: 7, Part: 1, PartTotal: 2},
	}
	if err := svc.ingestParts(ctx, "m1", parts); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 0 {
		t.Fatalf("after part 1 forwards=%d want 0", ch.forwards)
	}

	parts = []modem.Part{
		{ID: "39", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "Balans popolnen na 10000.0 sumov. Balans: 12345.67890 sumov. Balans", ConcatRef: 7, Part: 1, PartTotal: 2},
		{ID: "40", From: "Beeline@", State: "received (read)", Timestamp: ts, Text: "ingizga 10000.0 so'm qabul qilindi. Balans: 12345.67890 sum.", ConcatRef: 7, Part: 2, PartTotal: 2},
	}
	if err := svc.ingestParts(ctx, "m1", parts); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("after merge forwards=%d want 1", ch.forwards)
	}

	segmentID := storage.MessageKey("m1", "39", ts)
	need, err := store.NeedsDelivery(segmentID, "tg")
	if err != nil || need {
		t.Fatalf("segment should be superseded need=%v err=%v", need, err)
	}
}
