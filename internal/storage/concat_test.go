package storage

import (
	"testing"

	"github.com/legion/sms-gateway/internal/smsconcat"
)

func TestConcatAssemblyComplete(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	part := ConcatPart{
		Modem: "m1", FromNumber: "Beeline@", ConcatRef: 7, PartTotal: 2, PartNum: 1,
		ModemID: "39", Body: "Balans popolnen", ReceivedAt: "26/07/16,11:21:07+00", State: "received (read)",
	}
	complete, parts, err := store.UpsertConcatPart(part)
	if err != nil {
		t.Fatal(err)
	}
	if complete || len(parts) != 1 {
		t.Fatalf("complete=%v parts=%d", complete, len(parts))
	}

	part2 := part
	part2.PartNum = 2
	part2.ModemID = "40"
	part2.Body = "ingizga qabul qilindi"
	complete, parts, err = store.UpsertConcatPart(part2)
	if err != nil {
		t.Fatal(err)
	}
	if !complete || len(parts) != 2 {
		t.Fatalf("complete=%v parts=%d", complete, len(parts))
	}

	mergedID := ConcatMessageKey("m1", "Beeline@", 7, 2, "26/07/16,11:21:07+00")
	segmentIDs := []string{
		MessageKey("m1", "39", "26/07/16,11:21:07+00"),
		MessageKey("m1", "40", "26/07/16,11:21:07+00"),
	}
	if err := store.MarkConcatMerged("m1", "Beeline@", 7, 2, mergedID, segmentIDs); err != nil {
		t.Fatal(err)
	}

	for _, id := range segmentIDs {
		superseded, err := store.IsSuperseded(id)
		if err != nil || !superseded {
			t.Fatalf("id %s superseded=%v err=%v", id, superseded, err)
		}
		need, err := store.NeedsDelivery(id, "tg")
		if err != nil || need {
			t.Fatalf("segment %s should not need delivery", id)
		}
	}

	merged, id, err := store.IsConcatMerged("m1", "Beeline@", 7, 2)
	if err != nil || !merged || id != mergedID {
		t.Fatalf("merged=%v id=%q err=%v", merged, id, err)
	}

	msg := MergedMessageFromParts(parts, false)
	if msg.Text == "" {
		t.Fatal("empty merged text")
	}
}

func TestConcatPartsToSMS(t *testing.T) {
	parts := []ConcatPart{
		{ModemID: "1", FromNumber: "x", ConcatRef: 1, PartTotal: 2, PartNum: 1, Body: "a"},
		{ModemID: "2", FromNumber: "x", ConcatRef: 1, PartTotal: 2, PartNum: 2, Body: "b"},
	}
	sms := ConcatPartsToSMS(parts)
	if len(sms) != 2 || sms[1].Part != 2 {
		t.Fatalf("parts: %+v", sms)
	}
	_ = smsconcat.MergeGroup(sms)
}
