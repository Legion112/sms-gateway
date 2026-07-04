package smsencoding

import (
	"strings"
	"testing"
)

func TestDecodeTextPlain(t *testing.T) {
	raw := "Hello this sms is send to mine Uzbekistan SIM card"
	if got := DecodeText(raw); got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeTextLatinWithApostrophe(t *testing.T) {
	raw := "Sizning pasportingizga quyidagi raqamlar ro'yxatdan o'tgan"
	if got := DecodeText(raw); got != raw {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeTextUTF16BEHex(t *testing.T) {
	raw := "0445002000280069004F00530029002F04140430043D043D044B04350020043200200440043E0443043C0438043D04330435002000280041006E00640072006F006900640029"
	got := DecodeText(raw)
	if got == raw {
		t.Fatal("expected decoded text")
	}
	if !containsAll(got, "ios", "android") {
		t.Fatalf("unexpected decode: %q", got)
	}
}

func TestDecodeTextBeelineRoamingSnippet(t *testing.T) {
	raw := "0052006F0073007300690079006100670061002000780075007300680020006B0065006C0069006200730069007A0021000A004200650065006C0069006E006500200072006F0075006D0069006E0067"
	got := DecodeText(raw)
	if got == raw {
		t.Fatal("expected decoded text")
	}
	if !containsAll(got, "beeline") {
		t.Fatalf("unexpected decode: %q", got)
	}
}

func TestDecodeTextShortHexNotDecoded(t *testing.T) {
	raw := "1234"
	if got := DecodeText(raw); got != raw {
		t.Fatalf("short numeric should stay raw, got %q", got)
	}
}

func containsAll(s string, parts ...string) bool {
	lower := strings.ToLower(s)
	for _, p := range parts {
		if !strings.Contains(lower, p) {
			return false
		}
	}
	return true
}
