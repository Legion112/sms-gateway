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
		ID:         storage.MessageKey("ec25-main", "1"),
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

func TestInsertMessageSameIndexDifferentModems(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a := storage.Message{
		ID:         storage.MessageKey("ec25-main", "0"),
		Modem:      "ec25-main",
		FromNumber: "+7999",
		Body:       "main",
		ReceivedAt: "2026-06-12T10:00:00Z",
	}
	b := storage.Message{
		ID:         storage.MessageKey("ec25-backup", "0"),
		Modem:      "ec25-backup",
		FromNumber: "MTS",
		Body:       "backup",
		ReceivedAt: "2026-06-12T10:00:00Z",
	}
	if inserted, err := store.InsertMessage(a); err != nil || !inserted {
		t.Fatalf("insert main: inserted=%v err=%v", inserted, err)
	}
	if inserted, err := store.InsertMessage(b); err != nil || !inserted {
		t.Fatalf("insert backup: inserted=%v err=%v", inserted, err)
	}
}

func TestDeliveryTracking(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const id = "ec25-main:/SMS/1"
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
