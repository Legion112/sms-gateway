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

	msg := modem.Message{ID: "1", From: "+7900", Text: "hi", State: "received"}
	ctx := context.Background()

	if err := svc.ingest(ctx, "m1", msg); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("forwards=%d want 1", ch.forwards)
	}
	if err := svc.ingest(ctx, "m1", msg); err != nil {
		t.Fatal(err)
	}
	if ch.forwards != 1 {
		t.Fatalf("duplicate ingest forwards=%d want 1", ch.forwards)
	}
}
