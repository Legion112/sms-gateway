package smsconcat

import (
	"strings"
	"testing"
)

func TestMergeThreePartBeeline(t *testing.T) {
	from := "6610110182111117109105110103"
	parts := []Part{
		{ID: "25", From: from, State: "received (read)", Timestamp: "26/07/04,21:23:24+20", Text: "Рекомендации в роуминге:\n1. Отключите услугу Будь в курсе во избежа", Storage: "modem", ConcatRef: 120, Part: 1, PartTotal: 3},
		{ID: "26", From: from, State: "received (read)", Timestamp: "26/07/04,21:23:27+20", Text: " sozlamalardan qo‘lda tanlang.\n\n", Storage: "modem", ConcatRef: 120, Part: 2, PartTotal: 3},
		{ID: "27", From: from, State: "received (read)", Timestamp: "26/07/04,21:23:31+20", Text: "ние нежелательных списаний. Код *110*050#", Storage: "modem", ConcatRef: 120, Part: 3, PartTotal: 3},
	}

	msgs := Merge(parts)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].ID != "25" {
		t.Fatalf("id = %q", msgs[0].ID)
	}
	if !containsAll(msgs[0].Text, "Рекомендации", "sozlamalardan", "*110*050#") {
		t.Fatalf("merged text incomplete: %q", msgs[0].Text)
	}
}

func TestMergeIncompleteGroupStaysSeparate(t *testing.T) {
	from := "6610110182111117109105110103"
	parts := []Part{
		{ID: "25", From: from, State: "received (read)", Text: "part1", Storage: "modem", ConcatRef: 120, Part: 1, PartTotal: 3},
		{ID: "26", From: from, State: "received (read)", Text: "part2", Storage: "modem", ConcatRef: 120, Part: 2, PartTotal: 3},
	}
	msgs := Merge(parts)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
}

func TestMergeSingleMessagesUnchanged(t *testing.T) {
	parts := []Part{
		{ID: "0", From: "+79956009754", State: "received (read)", Text: "Hello", Storage: "modem"},
		{ID: "36", From: "48545552", State: "received (read)", Text: "Uzbek text", Storage: "modem"},
	}
	msgs := Merge(parts)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
