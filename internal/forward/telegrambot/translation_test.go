package telegrambot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const uzbekPSA = "Xavfsiz xonadon - baxtli oila! Pech va dudburonlarni soz holatda saqlang. Is gazidan saqlanish qoidalariga amal qiling!"

func TestTranslationIsShownBelowTheOriginal(t *testing.T) {
	out := FormatMessageForTest(SMSMessage{
		Modem:  "ec25-backup",
		From:   "FVV",
		ToLine: "+998901234567",
		Time:   "26/10/29,18:05:00+00",
		Text:   uzbekPSA,
		Translation: Translation{
			Text:   "A safe home is a happy family! Keep stoves and chimneys in good order.",
			Source: "uz",
			Target: "en",
		},
	})
	if !strings.Contains(out, uzbekPSA) {
		t.Error("the original is missing")
	}
	if !strings.Contains(out, "translation (uz -> en):") {
		t.Errorf("no labelled translation:\n%s", out)
	}
	// The original first: it is what a one-time code or an amount gets checked
	// against, and a machine translation of a bank SMS can get a currency
	// wrong.
	if strings.Index(out, uzbekPSA) > strings.Index(out, "A safe home") {
		t.Error("the translation was put above the original")
	}
}

func TestNoTranslationLeavesTheMessageUntouched(t *testing.T) {
	msg := SMSMessage{Modem: "ec25-main", From: "FVV", ToLine: "+998901234567", Text: uzbekPSA}
	plain := FormatMessageForTest(msg)
	if strings.Contains(plain, "translation") {
		t.Errorf("added a heading with nothing under it:\n%s", plain)
	}
	msg.Translation = Translation{Source: "uz", Target: "en"}
	if FormatMessageForTest(msg) != plain {
		t.Error("an empty translation changed the message")
	}
}

// Telegram takes 4096 characters. If the two together do not fit, the
// translation gives way: a cut translation is a nuisance, a cut original is a
// lost message.
func TestTheOriginalKeepsItsRoom(t *testing.T) {
	long := strings.Repeat("Is gazidan saqlanish qoidalariga amal qiling! ", 60)
	out := FormatMessageForTest(SMSMessage{
		Modem:  "ec25-main",
		From:   "FVV",
		ToLine: "+998901234567",
		Text:   long,
		Translation: Translation{
			Text:   strings.Repeat("Follow the rules for protection from flue gas! ", 100),
			Source: "uz",
			Target: "en",
		},
	})
	if len(out) > maxMessageLen {
		t.Errorf("message is %d bytes, over the %d limit", len(out), maxMessageLen)
	}
	if !strings.Contains(out, long) {
		t.Error("the original was truncated to make room for a translation")
	}
}

func TestATranslationWithNoRoomIsDroppedEntirely(t *testing.T) {
	// An original that all but fills the message leaves nothing worth saying.
	original := strings.Repeat("a", maxMessageLen-80)
	out := FormatMessageForTest(SMSMessage{
		Modem:       "ec25-main",
		From:        "FVV",
		ToLine:      "+998901234567",
		Text:        original,
		Translation: Translation{Text: strings.Repeat("b", 500), Source: "uz", Target: "en"},
	})
	if len(out) > maxMessageLen {
		t.Errorf("message is %d bytes", len(out))
	}
	if strings.Contains(out, "translation (") {
		t.Error("kept a heading it had no room to fill")
	}
}

func TestATruncatedTranslationIsMarked(t *testing.T) {
	original := strings.Repeat("a", maxMessageLen-300)
	out := FormatMessageForTest(SMSMessage{
		Modem:       "ec25-main",
		From:        "FVV",
		ToLine:      "+998901234567",
		Text:        original,
		Translation: Translation{Text: strings.Repeat("b", 2000), Source: "uz", Target: "en"},
	})
	if len(out) > maxMessageLen {
		t.Errorf("message is %d bytes", len(out))
	}
	if !strings.HasSuffix(out, ellipsis) {
		t.Errorf("a cut translation was not marked: ends %q", out[len(out)-8:])
	}
}

// Uzbek, Russian and the engine's output are all multi-byte. Half a character
// reaches Telegram as a replacement glyph.
func TestTruncationNeverSplitsACharacter(t *testing.T) {
	original := strings.Repeat("a", maxMessageLen-200)
	out := FormatMessageForTest(SMSMessage{
		Modem:       "ec25-main",
		From:        "FVV",
		ToLine:      "+998901234567",
		Text:        original,
		Translation: Translation{Text: strings.Repeat("Безопасный дом ", 100), Source: "uz", Target: "ru"},
	})
	if !utf8.ValidString(out) {
		t.Error("truncation produced invalid UTF-8")
	}
	if len(out) > maxMessageLen {
		t.Errorf("message is %d bytes", len(out))
	}
}

func TestTruncateRunesKeepsCharactersWhole(t *testing.T) {
	const s = "Безопасный"
	for limit := 0; limit <= len(s)+2; limit++ {
		got := truncateRunes(s, limit)
		if !utf8.ValidString(got) {
			t.Errorf("limit %d produced invalid UTF-8: %q", limit, got)
		}
		if len(got) > limit && limit < len(s) {
			t.Errorf("limit %d returned %d bytes", limit, len(got))
		}
	}
}

// An original that does not fit on its own is a pre-existing case: the send
// path truncates it. What must not happen is a translation being appended to
// it, which would push the original further out of the message.
func TestAnOverlongOriginalGetsNoTranslation(t *testing.T) {
	out := FormatMessageForTest(SMSMessage{
		Modem:       "ec25-main",
		From:        "FVV",
		ToLine:      "+998901234567",
		Text:        strings.Repeat("Is gazidan saqlanish qoidalariga amal qiling! ", 100),
		Translation: Translation{Text: "Follow the rules for protection from flue gas!", Source: "uz", Target: "en"},
	})
	if strings.Contains(out, "translation (") {
		t.Error("appended a translation to a message that was already too long")
	}
}

// A body the gateway could not place a line for still carries its text; a
// translation must not turn that into a message with a heading and no body.
func TestTranslationOnAnUnroutedMessage(t *testing.T) {
	out := FormatMessageForTest(SMSMessage{
		From:        "FVV",
		Text:        uzbekPSA,
		Translation: Translation{Text: "A safe home", Source: "uz", Target: "en"},
	})
	if strings.Contains(out, "translation") {
		t.Errorf("the from-only form gained a translation it cannot show:\n%s", out)
	}
}
