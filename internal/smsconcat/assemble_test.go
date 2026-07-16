package smsconcat

import (
	"fmt"
	"strings"
	"testing"
)

func TestMergeIncompleteGroupAddsPrefix(t *testing.T) {
	parts := []Part{
		{ID: "1", From: "Beeline@", Text: "part one", ConcatRef: 42, Part: 1, PartTotal: 3},
		{ID: "2", From: "Beeline@", Text: "part two", ConcatRef: 42, Part: 2, PartTotal: 3},
	}
	msg := MergeIncompleteGroup(parts)
	if !strings.HasPrefix(msg.Text, "[incomplete 2/3]") {
		t.Fatalf("text = %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "part onepart two") {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestMergeBeelineTwoPartTopUp(t *testing.T) {
	parts := []Part{
		{ID: "39", From: "Beeline@", State: "received (read)", Timestamp: "26/07/16,11:21:07+00", Text: "Balans popolnen na 10000.0 sumov. Balans: 12345.67890 sumov. Balans", ConcatRef: 7, Part: 1, PartTotal: 2},
		{ID: "40", From: "Beeline@", State: "received (read)", Timestamp: "26/07/16,11:21:07+00", Text: "ingizga 10000.0 so'm qabul qilindi. Balans: 12345.67890 sum.", ConcatRef: 7, Part: 2, PartTotal: 2},
	}
	msgs := Merge(parts)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "Balans popolnen") || !strings.Contains(msgs[0].Text, "qabul qilindi") {
		t.Fatalf("merged text = %q", msgs[0].Text)
	}
}

func TestMergeBeelineEightPartTariff(t *testing.T) {
	texts := []string{
		`U Vas ne spisalas' ejemesyachnaya abon. plata po tarifu "Status Sil`,
		`ver", t.k. na schetu menee 90000.0 sumov. Do spisaniya abon. platy`,
		` budut rasxodovat’sya sredstva s balansa. Vo izbejanie etogo rekome`,
		`nduem popolnit’ schet. Balans: *102#.   Sizda "Status Silver" tarif`,
		`i bo'yicha oylik abon. to'lovi yechilmadi, hisobingizda 90000.0 so`,
		`'mdan kam. Abon. to’lovi yechilmaguncha asosiy balansdan mablag’ sa`,
		`rflanadi. Buni oldini olish uchun hisobingizni to’ldiring. Balans:`,
		`*102#.`,
	}
	parts := make([]Part, len(texts))
	for i, text := range texts {
		parts[i] = Part{
			ID:        fmt.Sprintf("%d", 41+i),
			From:      "Beeline@",
			State:     "received (read)",
			Timestamp: "26/07/16,11:21:09+00",
			Text:      text,
			ConcatRef: 99,
			Part:      i + 1,
			PartTotal: 8,
		}
	}
	msgs := Merge(parts)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "Status Silver") || !strings.Contains(msgs[0].Text, "*102#.") {
		t.Fatalf("merged text incomplete: %q", msgs[0].Text)
	}
}
