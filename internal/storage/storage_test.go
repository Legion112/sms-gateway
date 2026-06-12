package storage_test

import (
	"testing"

	"github.com/legion/sms-gateway/internal/storage"
)

func TestInsertMessageIdempotent(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	msg := storage.Message{
		ID:         "/SMS/1",
		Modem:      "ec25-main",
		FromNumber: "+79162821457",
		Body:       "hello",
		ReceivedAt: "2026-06-12T10:00:00Z",
	}
	inserted, err := store.InsertMessage(msg)
	if err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	inserted, err = store.InsertMessage(msg)
	if err != nil || inserted {
		t.Fatalf("second insert: inserted=%v err=%v", inserted, err)
	}
}

func TestDeliveryTracking(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const id = "/SMS/1"
	need, err := store.NeedsDelivery(id, "tg")
	if err != nil || !need {
		t.Fatalf("need=%v err=%v", need, err)
	}
	if err := store.MarkDeliveryFailed(id, "tg", "timeout"); err != nil {
		t.Fatal(err)
	}
	need, err = store.NeedsDelivery(id, "tg")
	if err != nil || !need {
		t.Fatalf("after fail still need delivery: need=%v", need)
	}
	if err := store.MarkDelivered(id, "tg"); err != nil {
		t.Fatal(err)
	}
	need, err = store.NeedsDelivery(id, "tg")
	if err != nil || need {
		t.Fatalf("after delivered need=%v err=%v", need, err)
	}
}
