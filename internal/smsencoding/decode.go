package smsencoding

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf16"
)

// DecodeText returns human-readable SMS body text.
// Quectel modems often expose UCS-2 (UTF-16-BE) bodies as hex strings in text mode.
func DecodeText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if decoded, ok := decodeUTF16BEHex(raw); ok {
		return decoded
	}
	return raw
}

func decodeUTF16BEHex(raw string) (string, bool) {
	if len(raw) < 8 || len(raw)%4 != 0 {
		return "", false
	}
	for _, c := range raw {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return "", false
		}
	}

	b, err := hex.DecodeString(raw)
	if err != nil || len(b) < 2 || len(b)%2 != 0 {
		return "", false
	}
	for len(b) >= 2 && b[len(b)-2] == 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-2]
	}
	if len(b) == 0 {
		return "", false
	}

	runes := decodeUTF16BE(b)
	if len(runes) == 0 || !isMostlyPrintable(runes) {
		return "", false
	}
	return string(runes), true
}

func decodeUTF16BE(b []byte) []rune {
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return utf16.Decode(u16)
}

func isMostlyPrintable(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	printable := 0
	for _, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsPrint(r) {
			printable++
		}
	}
	return float64(printable)/float64(len(runes)) >= 0.85
}
