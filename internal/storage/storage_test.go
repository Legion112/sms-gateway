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
		ID:         storage.MessageKey("ec25-main", "1", "2026-06-12T10:00:00Z"),
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
		ID:         storage.MessageKey("ec25-main", "0", "2026-06-12T10:00:00Z"),
		Modem:      "ec25-main",
		FromNumber: "+7999",
		Body:       "main",
		ReceivedAt: "2026-06-12T10:00:00Z",
	}
	b := storage.Message{
		ID:         storage.MessageKey("ec25-backup", "0", "2026-06-12T10:01:00Z"),
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

func TestInsertMessageSameIndexDifferentTimestamps(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := storage.Message{
		ID:         storage.MessageKey("ec25-main", "37", "26/07/04,21:00:00+00"),
		Modem:      "ec25-main",
		FromNumber: "+7999",
		Body:       "old text",
		ReceivedAt: "26/07/04,21:00:00+00",
	}
	second := storage.Message{
		ID:         storage.MessageKey("ec25-main", "37", "26/07/04,22:19:42+00"),
		Modem:      "ec25-main",
		FromNumber: "+7999",
		Body:       "Another sms to me",
		ReceivedAt: "26/07/04,22:19:42+00",
	}
	if inserted, err := store.InsertMessage(first); err != nil || !inserted {
		t.Fatalf("insert first: inserted=%v err=%v", inserted, err)
	}
	if inserted, err := store.InsertMessage(second); err != nil || !inserted {
		t.Fatalf("insert second: inserted=%v err=%v", inserted, err)
	}
}

func TestReplaceMessageClearsDelivery(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id := storage.MessageKey("ec25-main", "37", "26/07/04,22:19:42+00")
	msg := storage.Message{
		ID: id, Modem: "ec25-main", FromNumber: "+7999",
		Body: "old", ReceivedAt: "26/07/04,22:19:42+00",
	}
	if _, err := store.InsertMessage(msg); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDelivered(id, "tg"); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceMessage(storage.Message{
		ID: id, Modem: "ec25-main", FromNumber: "+7999",
		Body: "I hope this one is last", ReceivedAt: "26/07/04,22:19:42+00",
	}); err != nil {
		t.Fatal(err)
	}
	need, err := store.NeedsDelivery(id, "tg")
	if err != nil || !need {
		t.Fatalf("after replace need=%v err=%v", need, err)
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

func TestIsFullyDelivered(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.InsertMessage(storage.Message{
		ID: "m1:12", Modem: "m1", FromNumber: "UZCARD", Body: "hi", ReceivedAt: "t1",
	}); err != nil {
		t.Fatal(err)
	}

	// No delivery attempt recorded yet: never treat the slot as free.
	if done, err := store.IsFullyDelivered("m1:12"); err != nil || done {
		t.Fatalf("no attempts: done=%v err=%v want false", done, err)
	}

	if err := store.MarkDeliveryFailed("m1:12", "tg", "telegram unreachable"); err != nil {
		t.Fatal(err)
	}
	if done, err := store.IsFullyDelivered("m1:12"); err != nil || done {
		t.Fatalf("failed attempt: done=%v err=%v want false", done, err)
	}

	if err := store.MarkDelivered("m1:12", "tg"); err != nil {
		t.Fatal(err)
	}
	if done, err := store.IsFullyDelivered("m1:12"); err != nil || !done {
		t.Fatalf("delivered: done=%v err=%v want true", done, err)
	}

	// A second channel still pending holds the slot.
	if err := store.MarkDeliveryFailed("m1:12", "email", "smtp down"); err != nil {
		t.Fatal(err)
	}
	if done, err := store.IsFullyDelivered("m1:12"); err != nil || done {
		t.Fatalf("one channel pending: done=%v err=%v want false", done, err)
	}

	if done, err := store.IsFullyDelivered("m1:nonexistent"); err != nil || done {
		t.Fatalf("unknown id: done=%v err=%v want false", done, err)
	}
}

func TestHasPendingDelivery(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// No delivery rows: nothing was ever routed, so nothing is pending.
	if got, err := store.HasPendingDelivery("m1:12"); err != nil || got {
		t.Fatalf("no rows: got=%v err=%v want false", got, err)
	}

	if err := store.MarkDeliveryFailed("m1:12", "tg", "telegram unreachable"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.HasPendingDelivery("m1:12"); err != nil || !got {
		t.Fatalf("failed attempt: got=%v err=%v want true", got, err)
	}

	if err := store.MarkDelivered("m1:12", "tg"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.HasPendingDelivery("m1:12"); err != nil || got {
		t.Fatalf("delivered: got=%v err=%v want false", got, err)
	}
}
