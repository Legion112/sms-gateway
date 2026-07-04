package smspdu

import "strings"

// gsm7Basic is the default GSM 7-bit alphabet (TS 23.038).
var gsm7Basic = []rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', '\x1b', 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

func gsm7Septet(r byte) rune {
	if int(r) < len(gsm7Basic) {
		return gsm7Basic[r]
	}
	return '?'
}

func unpackGSM7(data []byte, septets int) string {
	if septets <= 0 || len(data) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(septets)
	var carry uint16
	var bits int
	idx := 0
	for n := 0; n < septets; n++ {
		for bits < 7 && idx < len(data) {
			carry |= uint16(data[idx]) << bits
			bits += 8
			idx++
		}
		b.WriteRune(gsm7Septet(byte(carry & 0x7F)))
		carry >>= 7
		bits -= 7
	}
	return b.String()
}

func gsm7OctetLength(septets int) int {
	if septets <= 0 {
		return 0
	}
	return (septets*7 + 7) / 8
}

func unpackGSM7All(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	maxSeptets := (len(data) * 8) / 7
	return unpackGSM7(data, maxSeptets)
}

func unpackGSM7FromSeptet(data []byte, startSeptet, septets int) string {
	if septets <= 0 || len(data) == 0 {
		return ""
	}
	skipBits := startSeptet * 7
	byteIdx := skipBits / 8
	bitOff := skipBits % 8
	var carry uint16
	var bits int
	if bitOff > 0 && byteIdx < len(data) {
		carry = uint16(data[byteIdx]) >> bitOff
		bits = 8 - bitOff
		byteIdx++
	}
	var b strings.Builder
	b.Grow(septets)
	for n := 0; n < septets; n++ {
		for bits < 7 && byteIdx < len(data) {
			carry |= uint16(data[byteIdx]) << bits
			bits += 8
			byteIdx++
		}
		b.WriteRune(gsm7Septet(byte(carry & 0x7F)))
		carry >>= 7
		bits -= 7
	}
	return b.String()
}
