package cli

import (
	"testing"

	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/storage"
)

func TestPartForwardedWithoutStore(t *testing.T) {
	got, err := partForwarded(nil, "m1", modem.Part{ID: "12", Timestamp: "t1"})
	if err != nil || got {
		t.Fatalf("no store: got=%v err=%v want false", got, err)
	}
}

func TestPartForwardedPlainMessage(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	part := modem.Part{ID: "12", From: "UZCARD", Timestamp: "t1", Text: "hi"}
	id := storage.MessageKey("m1", part.ID, part.Timestamp)
	if _, err := store.InsertMessage(storage.Message{
		ID: id, Modem: "m1", FromNumber: part.From, Body: part.Text, ReceivedAt: part.Timestamp,
	}); err != nil {
		t.Fatal(err)
	}

	if got, err := partForwarded(store, "m1", part); err != nil || got {
		t.Fatalf("before delivery: got=%v err=%v want false", got, err)
	}
	if err := store.MarkDelivered(id, "tg"); err != nil {
		t.Fatal(err)
	}
	if got, err := partForwarded(store, "m1", part); err != nil || !got {
		t.Fatalf("after delivery: got=%v err=%v want true", got, err)
	}
}

func TestPartForwardedConcatSegment(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	part := modem.Part{ID: "39", From: "Beeline@", Timestamp: "t1", Text: "half", ConcatRef: 7, Part: 1, PartTotal: 2}

	// Group not merged yet: the segment is still in flight.
	if got, err := partForwarded(store, "m1", part); err != nil || got {
		t.Fatalf("unmerged group: got=%v err=%v want false", got, err)
	}

	mergedID := storage.ConcatMessageKey("m1", part.From, part.ConcatRef, part.PartTotal, part.Timestamp)
	if err := store.MarkConcatMerged("m1", part.From, part.ConcatRef, part.PartTotal, mergedID, []string{
		storage.MessageKey("m1", "39", "t1"),
	}); err != nil {
		t.Fatal(err)
	}

	// Merged but the merged message was never delivered: hold the slot.
	if got, err := partForwarded(store, "m1", part); err != nil || got {
		t.Fatalf("merged undelivered: got=%v err=%v want false", got, err)
	}
	if err := store.MarkDelivered(mergedID, "tg"); err != nil {
		t.Fatal(err)
	}
	if got, err := partForwarded(store, "m1", part); err != nil || !got {
		t.Fatalf("merged and delivered: got=%v err=%v want true", got, err)
	}
}
